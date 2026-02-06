#!/bin/bash
set -euo pipefail

# --- CONFIGURATION ---
# Path to your Kernel Source (Where you ran 'make modules')
# Leave empty if you don't want to copy modules (eBPF might break)
KERNEL_SRC="../linux.git"

ROOTFS_SIZE_GB=2
ROOTFS_IMAGE="rootfs.ext4"
BUILD_DIR=".firecracker-build-$(date +%s)"
MOUNT_POINT="${BUILD_DIR}/mount"
DOCKER_IMAGE_NAME="fc-research-builder"

# --- SETUP ---
mkdir -p "${BUILD_DIR}"
echo -e "\033[0;32m[INFO]\033[0m Building in: ${BUILD_DIR}"

cleanup() {
  if mountpoint -q "${MOUNT_POINT}" 2>/dev/null; then sudo umount "${MOUNT_POINT}"; fi
  rm -rf "${BUILD_DIR}"
}
trap cleanup EXIT

# --- STEP 1: DEFINE THE OS (Docker) ---
cat >"${BUILD_DIR}/Dockerfile" <<'EOF'
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive

# 1. Install Core Dependencies
RUN apt-get update && apt-get install -y \
    bash-completion \
    build-essential \
    ca-certificates \
    curl \
    git \
    htop \
    iproute2 \
    iputils-ping \
    kmod \
    nano \
    net-tools \
    openssh-server \
    sudo \
    systemd \
    systemd-sysv \
    udev \
    vim \
    wget \
    zstd

# 2. Install eBPF & Research Tools
RUN apt-get install -y \
    bpfcc-tools \
    bpftrace \
    clang \
    gcc \
    libbpf-dev \
    libelf-dev \
    llvm \
    make \
    pkg-config \
    python3 \
    python3-pip \
    tcpdump

# 3. Configure SSH (Permit Root)
RUN mkdir -p /var/run/sshd && \
    echo 'root:root' | chpasswd && \
    sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config

# 4. Setup Serial Console (Crucial for Firecracker)
RUN systemctl enable serial-getty@ttyS0.service

# 5. Create the "Magic Network" Script
# This script reads the MAC address and sets the IP automatically
RUN echo '#!/bin/bash\n\
MAC=$(cat /sys/class/net/eth0/address)\n\
ID=${MAC##*:}\n\
# Convert hex ID to decimal if needed, or just append\n\
# Mapping: :01 -> .11, :02 -> .12, :03 -> .13\n\
if [ "$ID" == "01" ]; then SUFFIX="11"; fi\n\
if [ "$ID" == "02" ]; then SUFFIX="12"; fi\n\
if [ "$ID" == "03" ]; then SUFFIX="13"; fi\n\
\n\
if [ -z "$SUFFIX" ]; then SUFFIX="14"; fi\n\
\n\
IP="172.16.0.$SUFFIX"\n\
echo "Configuring Network: MAC=$MAC -> IP=$IP"\n\
ip addr add $IP/24 dev eth0\n\
ip link set eth0 up\n\
ip route add default via 172.16.0.1\n\
echo "nameserver 8.8.8.8" > /etc/resolv.conf\n\
' > /usr/local/bin/fc-net-setup.sh && chmod +x /usr/local/bin/fc-net-setup.sh

# 6. Create Systemd Service for Network
RUN echo '[Unit]\n\
Description=Firecracker Magic Network\n\
After=network.target\n\
\n\
[Service]\n\
Type=oneshot\n\
ExecStart=/usr/local/bin/fc-net-setup.sh\n\
RemainAfterExit=yes\n\
\n\
[Install]\n\
WantedBy=multi-user.target\n\
' > /etc/systemd/system/fc-net.service && systemctl enable fc-net.service

EOF

# --- STEP 2: BUILD & EXPORT ---
echo -e "\033[0;32m[INFO]\033[0m Building Docker Image..."
docker build -t "${DOCKER_IMAGE_NAME}" -f "${BUILD_DIR}/Dockerfile" "${BUILD_DIR}"

echo -e "\033[0;32m[INFO]\033[0m Creating Disk Image (${ROOTFS_SIZE_GB}GB)..."
dd if=/dev/zero of="${ROOTFS_IMAGE}" bs=1M count=0 seek=$((ROOTFS_SIZE_GB * 1024))
mkfs.ext4 -F "${ROOTFS_IMAGE}"

echo -e "\033[0;32m[INFO]\033[0m Exporting Filesystem..."
CONTAINER_ID=$(docker create "${DOCKER_IMAGE_NAME}")
docker export "${CONTAINER_ID}" | tar -xf - -C "${BUILD_DIR}"
docker rm "${CONTAINER_ID}"

# --- STEP 3: INJECT KERNEL MODULES & CONFIG ---
echo -e "\033[0;32m[INFO]\033[0m Mounting & Configuring..."
mkdir -p "${MOUNT_POINT}"
sudo mount "${ROOTFS_IMAGE}" "${MOUNT_POINT}"

# A. Copy Docker contents to Disk Image
sudo cp -a "${BUILD_DIR}"/* "${MOUNT_POINT}/" 2>/dev/null || true

# B. INSTALL KERNEL MODULES (Crucial for eBPF)
if [ -d "$KERNEL_SRC" ]; then
  echo -e "\033[0;32m[INFO]\033[0m Installing Kernel Modules from ${KERNEL_SRC}..."
  # We run make modules_install pointing to the MOUNT POINT
  cd "$KERNEL_SRC"
  sudo make modules_install INSTALL_MOD_PATH="$(readlink -f ${OLDPWD}/${MOUNT_POINT})"
  cd - >/dev/null
else
  echo -e "\033[0;33m[WARN]\033[0m Kernel source not found at $KERNEL_SRC. Skipping modules."
  echo "      eBPF programs may fail if they depend on specific kernel headers/modules."
fi

# C. CREATE /etc/fstab (Fixes 'Read-Only' issues)
echo -e "\033[0;32m[INFO]\033[0m Generating fstab..."
echo "
# <file system> <mount point>   <type>  <options>       <dump>  <pass>
/dev/vda        /               ext4    defaults        0       1
proc            /proc           proc    defaults        0       0
sysfs           /sys            sysfs   defaults        0       0
debugfs         /sys/kernel/debug debugfs defaults      0       0
tmpfs           /tmp            tmpfs   defaults        0       0
" | sudo tee "${MOUNT_POINT}/etc/fstab" >/dev/null

# D. FIX HOSTNAME & HOSTS (Fixes 'sudo' complaints)
echo "firecracker" | sudo tee "${MOUNT_POINT}/etc/hostname"
echo "127.0.0.1 localhost firecracker" | sudo tee "${MOUNT_POINT}/etc/hosts"

# E. Generate SSH Host Keys (So sshd starts)
echo -e "\033[0;32m[INFO]\033[0m Pre-generating SSH Keys..."
sudo ssh-keygen -A -f "${MOUNT_POINT}"

# --- FINISH ---
sync
sudo umount "${MOUNT_POINT}"
echo -e "\033[0;32m[SUCCESS]\033[0m Rootfs created: ${ROOTFS_IMAGE}"
echo "Use this ONE image for all your VMs. The Magic Network script will handle IPs."
