#!/bin/bash
set -euo pipefail

# --- CONFIGURATION ---
# Path to your Kernel Source (Where you ran 'make modules')
# Leave empty if you don't want to copy modules (eBPF might break)
KERNEL_SRC="../linux.git"

ROOTFS_SIZE_MB=4096
ROOTFS_IMAGE="debian-java.ext4"
BUILD_DIR=".firecracker-build-$(date +%s)"
MOUNT_POINT="${BUILD_DIR}/mount"
DOCKER_IMAGE_NAME="fc-debian-builder"

# --- SETUP ---
mkdir -p "${BUILD_DIR}"
echo -e "\033[0;32m[INFO]\033[0m Building in: ${BUILD_DIR}"

cleanup() {
  if mountpoint -q "${MOUNT_POINT}" 2>/dev/null; then sudo umount "${MOUNT_POINT}"; fi
  rm -rf "${BUILD_DIR}"
}
trap cleanup EXIT

# --- STEP 1: DEFINE THE OS (Java Debian) ---
cat >"${BUILD_DIR}/Dockerfile" <<'EOF'
FROM debian:bookworm-slim
ENV DEBIAN_FRONTEND=noninteractive

# 1. Install Minimal System Dependencies & Java
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    iproute2 \
    openssh-server \
    procps \
    xz-utils \
    default-jdk-headless && \
    apt-get clean

# 2. Configure SSH (Permit Root)
RUN mkdir -p /var/run/sshd /run/sshd && \
    echo 'root:root' | chpasswd && \
    sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    ssh-keygen -A

# 3. Create a simple "Hello World" app to test the VM
RUN mkdir -p /app
COPY Main.java /app/Main.java
RUN javac /app/Main.java
EOF

cat >"${BUILD_DIR}/Main.java" <<'APP'
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpHandler;
import com.sun.net.httpserver.HttpServer;

public class Main {
    public static void main(String[] args) throws Exception {
        HttpServer server = HttpServer.create(new InetSocketAddress("0.0.0.0", 3000), 0);
        server.createContext("/", new MyHandler());
        server.setExecutor(null); // creates a default executor
        System.out.println("Server running at http://0.0.0.0:3000/");
        server.start();
    }

    static class MyHandler implements HttpHandler {
        @Override
        public void handle(HttpExchange t) throws IOException {
            String response = "Hello from Firecracker!\nJava version: " + System.getProperty("java.version") + "\n";
            t.sendResponseHeaders(200, response.length());
            OutputStream os = t.getResponseBody();
            os.write(response.getBytes());
            os.close();
        }
    }
}
APP

# --- STEP 1b: CREATE CUSTOM INIT SCRIPT ---
cat >"${BUILD_DIR}/firecracker-init" <<'INITEOF'
#!/bin/sh
# Custom init for Firecracker microVMs (Debian Bookworm)



# Go environment
echo export PATH="/usr/local/go/bin:/root/go/bin:$PATH" >> /root/.bashrc
echo 'alias reboot="echo b > /proc/sysrq-trigger"' >> /root/.bashrc

# 1. Mount essential filesystems
mount -t proc     proc     /proc

# Boot benchmark — read uptime immediately after /proc is available
UPTIME=$(cut -d' ' -f1 /proc/uptime)
echo ""
echo "TTI (Kernel + Init): ${UPTIME}s"

mount -t debugfs  debugfs  /sys/kernel/debug 2>/dev/null || true
mount -t tmpfs    tmpfs    /tmp
mount -t tmpfs    tmpfs    /run

mkdir -p /dev/pts
mount -t devpts devpts /dev/pts

# Remount root as read-write
mount -o remount,rw /

# 2. Set hostname
hostname firecracker

# 3. Configure Network
# Reads fc_ip= from kernel cmdline (set by orchestrator), falls back to MAC derivation
FC_IP=$(sed -n 's/.*fc_ip=\([^ ]*\).*/\1/p' /proc/cmdline)

if [ -n "$FC_IP" ]; then
  IP="$FC_IP"
  echo "[init] Network: fc_ip=$IP (from kernel cmdline)"
else
  # Wait briefly for eth0 to appear
  for i in $(seq 1 10); do
    [ -e /sys/class/net/eth0/address ] && break
    sleep 0.1
  done

  if [ -e /sys/class/net/eth0/address ]; then
    MAC=$(cat /sys/class/net/eth0/address)
    HEX_ID=${MAC##*:}
    DECIMAL_ID=$(printf "%d" "0x$HEX_ID")
    SUFFIX=$((DECIMAL_ID + 10))
    IP="172.16.0.$SUFFIX"
    echo "[init] Network: MAC=$MAC -> ID=$DECIMAL_ID -> IP=$IP (fallback)"
  else
    echo "[init] WARNING: eth0 not found, skipping network config"
    IP=""
  fi
fi

if [ -n "$IP" ]; then
  ip addr add "$IP/24" dev eth0
  ip link set eth0 up
  ip route add default via 172.16.0.1
  echo "nameserver 8.8.8.8" > /etc/resolv.conf
fi

# 4. Start SSH daemon
mkdir -p /run/sshd
/usr/sbin/sshd

echo 'export PATH="/usr/local/bin:$PATH"' >> /root/.bashrc

# Start the Java application in the background
echo "[init] Starting Java application..."
java -cp /app Main > /var/log/java-app.log 2>&1 &

# 5. Print banner
echo ""
echo "======================================"
echo "  Firecracker microVM (Debian Bookworm)"
echo "  IP: ${IP:-N/A}"
echo "  SSH: root@${IP}"
echo "  Password: root"
echo "======================================"
echo ""


sleep infinity
INITEOF

chmod +x "${BUILD_DIR}/firecracker-init"

# --- STEP 2: BUILD & EXPORT ---
echo -e "\033[0;32m[INFO]\033[0m Building Docker Image..."
docker build -t "${DOCKER_IMAGE_NAME}" -f "${BUILD_DIR}/Dockerfile" "${BUILD_DIR}"

echo -e "\033[0;32m[INFO]\033[0m Creating Disk Image (${ROOTFS_SIZE_MB}MB)..."
dd if=/dev/zero of="${ROOTFS_IMAGE}" bs=1M count=0 seek=${ROOTFS_SIZE_MB}
mkfs.ext4 -F "${ROOTFS_IMAGE}"

echo -e "\033[0;32m[INFO]\033[0m Exporting Filesystem..."
CONTAINER_ID=$(docker create "${DOCKER_IMAGE_NAME}")
docker export "${CONTAINER_ID}" | tar -xf - -C "${BUILD_DIR}"
docker rm "${CONTAINER_ID}"

# --- STEP 3: INJECT INIT, KERNEL MODULES & CONFIG ---
echo -e "\033[0;32m[INFO]\033[0m Mounting & Configuring..."
mkdir -p "${MOUNT_POINT}"
sudo mount "${ROOTFS_IMAGE}" "${MOUNT_POINT}"

# A. Copy Docker contents to Disk Image
sudo cp -a "${BUILD_DIR}"/* "${MOUNT_POINT}/" 2>/dev/null || true

# B. Install custom init script
sudo cp "${BUILD_DIR}/firecracker-init" "${MOUNT_POINT}/sbin/firecracker-init"
sudo chmod +x "${MOUNT_POINT}/sbin/firecracker-init"

# C. INSTALL KERNEL MODULES (Crucial for eBPF)
if [ -d "$KERNEL_SRC" ]; then
  echo -e "\033[0;32m[INFO]\033[0m Installing Kernel Modules from ${KERNEL_SRC}..."
  cd "$KERNEL_SRC"
  sudo make modules_install INSTALL_MOD_PATH="$(readlink -f ${OLDPWD}/${MOUNT_POINT})"
  cd - >/dev/null
else
  echo -e "\033[0;33m[WARN]\033[0m Kernel source not found at $KERNEL_SRC. Skipping modules."
  echo "      eBPF programs may fail if they depend on specific kernel headers/modules."
fi

# D. CREATE /etc/fstab
echo -e "\033[0;32m[INFO]\033[0m Generating fstab..."
cat <<FSTAB | sudo tee "${MOUNT_POINT}/etc/fstab" >/dev/null
# <file system> <mount point>   <type>  <options>       <dump>  <pass>
/dev/vda        /               ext4    defaults        0       1
proc            /proc           proc    defaults        0       0
sysfs           /sys            sysfs   defaults        0       0
debugfs         /sys/kernel/debug debugfs defaults      0       0
tmpfs           /tmp            tmpfs   defaults        0       0
FSTAB

# E. FIX HOSTNAME & HOSTS
echo "firecracker" | sudo tee "${MOUNT_POINT}/etc/hostname" >/dev/null
echo "127.0.0.1 localhost firecracker" | sudo tee "${MOUNT_POINT}/etc/hosts" >/dev/null

# F. Generate SSH Host Keys (So sshd starts)
echo -e "\033[0;32m[INFO]\033[0m Pre-generating SSH Keys..."
sudo ssh-keygen -A -f "${MOUNT_POINT}"

# --- FINISH ---
sync
sudo umount "${MOUNT_POINT}"
echo -e "\033[0;32m[SUCCESS]\033[0m Debian Bookworm rootfs created: ${ROOTFS_IMAGE} (${ROOTFS_SIZE_MB}MB)"
echo "Use this ONE image for all your VMs. The custom init script will handle networking."
