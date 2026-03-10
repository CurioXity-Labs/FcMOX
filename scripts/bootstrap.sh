#!/bin/bash
set -e

# Define absolute paths dynamically based on where the repo is cloned
BASE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGES_DIR="$BASE_DIR/lk-images"
ROOTFS_DIR="$BASE_DIR/lk-rootfs"

echo "=== Firecracker Bootstrap Script ==="

# 1. Unzip all kernels
echo "1. Unzipping kernels in $IMAGES_DIR..."
cd "$IMAGES_DIR"
for archive in *.xz; do
    # Skip if no .xz files exist
    [ -e "$archive" ] || continue 
    
    kernel_name="${archive%.xz}"
    if [ ! -f "$kernel_name" ]; then
        echo "   -> Extracting $archive to $kernel_name..."
        unxz -k "$archive"
    else
        echo "   -> $kernel_name already extracted, skipping."
    fi
done

# 2. Build rootfs images
echo ""
echo "2. Building rootfs images in $ROOTFS_DIR..."
cd "$ROOTFS_DIR"

if [ ! -f "debian.ext4" ]; then
    echo "   -> Running build-debianfs.sh..."
    sudo ./build-debianfs.sh
else
    echo "   -> debian.ext4 already exists, skipping build."
fi

if [ ! -f "debian-ebpf.ext4" ]; then
    echo "   -> Running build-debianfs-ebpf.sh..."
    sudo ./build-debianfs-ebpf.sh
else
    echo "   -> debian-ebpf.ext4 already exists, skipping build."
fi
