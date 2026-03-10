# Firecracker eBPF Research Lab

This repository contains the infrastructure code for deploying a Firecracker MicroVM cluster tailored for eBPF systems research and agentless cloud inventory. It includes custom kernel configurations, rootfs generation scripts, and a full-featured Terminal User Interface (TUI) for cluster management.

## Prerequisites

* Linux Host with KVM support (`/dev/kvm`)
* Docker (for building the rootfs)
* `iproute2` and `iptables` (for networking)
* Go 1.25.7 (to build the TUI manager)

---

## Getting Started

### 1. Bootstrap Kernels & Rootfs

You first need to prepare your kernels (extracting them from `.xz`) and build the Debian root image systems. We have provided an automated bootstrap script to do this for you.

Run this from the root of the repository:
```bash
cd scripts
./bootstrap.sh
```

This will automatically extract the compressed kernels in `lk-images/`, build the Debian testing and eBPF filesystems in `lk-rootfs/`.

### 2. Network Setup

Initialize the host networking bridge (`fc-br0`) and TAP interfaces. The script automatically detects your active internet interface and handles the NAT/routing so your VMs have internet access.

```bash
sudo ./net/setup-cluster-net
```

### 3. Launching fcmox (TUI VM Manager)

Instead of manually crafting API calls or managing sockets, use the custom `fcmox` Terminal User Interface to manage your Firecracker cluster.

```bash
cd fcmox
go build -o fcmox-bin ./cmd/
sudo setcap cap_net_admin,cap_net_raw+ep ./fcmox-bin
./fcmox-bin --rootfs-path=../lk-rootfs --linux-images-path=../lk-images
```

## Using the TUI

Once inside `fcmox`, you can:
* press `c` to Create and boot a new VM via the interactive multi-step wizard.
* press `p` to Pause a running instance, or `r` to Resume it.
* press `s` to Start a stopped instance.
* press `d` to forcefully Delete and destroy a selected VM.
* press `x` to quickly SSH into the selected VM's root shell.
* press `l` to view the serial console logs of the boot process for a selected VM.

All instances automatically clone the template rootfs via `cp --reflink=auto` so they have their own independent, writable virtual disks.

## Access

* **Login:** `root` / `root`
* **Networking:** IP addresses are generated incrementally starting from `172.16.0.2`.
* **Gateway:** `172.16.0.1`
