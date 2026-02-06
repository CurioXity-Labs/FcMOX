#!/bin/bash
# Usage: ./launch.sh <VM_ID>

# --- CONFIGURATION ---
ID=${1:-1}
VM_MEM="1024"
VM_CPU="2"
KERNEL="./lk-images/vmlinux-6.12-ebpf" # Adjusted to match your output
MASTER_IMAGE="./lk-rootfs/rootfs.ext4" # Adjusted to match your previous context
VM_IMAGE="rootfs-vm${ID}.ext4"
TAP_DEV="tap${ID}"
MAC_ADDR=$(printf "AA:FC:00:00:00:%02X" $ID)
SOCK="/tmp/firecracker-${ID}.socket"

if [ ! -f "$KERNEL" ]; then
  echo "❌ [ERROR] Kernel not found at: $KERNEL"
  echo "   Please check the path or compile it first."
  exit 1
fi

# --- PRE-FLIGHT ---
# 1. Create Private Disk
if [ ! -f "$VM_IMAGE" ]; then
  echo "📦 [VM $ID] Creating private disk ($VM_IMAGE)..."
  cp --sparse=always "$MASTER_IMAGE" "$VM_IMAGE"
fi

# 2. Cleanup
rm -f "$SOCK"

# --- THE CONFIGURATION FUNCTION ---
# This runs in the background while Firecracker starts
configure_vm() {
  # Wait for socket
  while [ ! -e "$SOCK" ]; do sleep 0.01; done

  # 1. Machine Config
  curl --unix-socket "$SOCK" -X PUT 'http://localhost/machine-config' \
    -d "{ \"vcpu_count\": $VM_CPU, \"mem_size_mib\": $VM_MEM }" >/dev/null 2>&1

  # 2. Rootfs
  curl --unix-socket "$SOCK" -X PUT 'http://localhost/drives/rootfs' \
    -d "{ \"drive_id\": \"rootfs\", \"path_on_host\": \"$(pwd)/$VM_IMAGE\", \"is_root_device\": true, \"is_read_only\": false }" >/dev/null 2>&1

  # 3. Kernel
  curl --unix-socket "$SOCK" -X PUT 'http://localhost/boot-source' \
    -d "{ \"kernel_image_path\": \"$(pwd)/$KERNEL\", \"boot_args\": \"console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/sbin/init systemd.unit=multi-user.target\" }" >/dev/null 2>&1

  # 4. Network
  curl --unix-socket "$SOCK" -X PUT 'http://localhost/network-interfaces/eth0' \
    -d "{ \"iface_id\": \"eth0\", \"guest_mac\": \"$MAC_ADDR\", \"host_dev_name\": \"$TAP_DEV\" }" >/dev/null 2>&1

  # 5. START!
  curl --unix-socket "$SOCK" -X PUT 'http://localhost/actions' \
    -d "{ \"action_type\": \"InstanceStart\" }" >/dev/null 2>&1
}

# --- MAIN LOOP ---
while true; do
  echo "🚀 [VM $ID] Starting... (Press Enter if console appears blank)"

  # Run configuration in background
  configure_vm &

  # Run Firecracker in FOREGROUND (so you can type)
  firecracker --api-sock "$SOCK"

  # If we get here, Firecracker exited
  echo "🛑 [VM $ID] Process exited."
  rm -f "$SOCK"
  sleep 1
done
