# Firecracker eBPF Research Lab

This repository contains the infrastructure code for deploying a Firecracker MicroVM cluster tailored for eBPF systems research and agentless cloud inventory. It includes custom kernel configurations, rootfs generation scripts, and cluster management tools.

## Prerequisites

* Linux Host with KVM support (`/dev/kvm`)
* Docker (for building the rootfs)
* Firecracker binary (v1.0+)
* `iproute2` and `iptables` (for networking)

## Directory Structure

* **lk-images/**: Compiled kernel binaries. The primary kernel is `vmlinux-6.12-ebpf`.
* **lk-rootfs/**: Scripts and artifacts for the guest filesystem.
* `build-rootfs.sh`: Generates an Ubuntu 24.04 image with pre-installed eBPF tools (bpftool, libbpf, clang).


* **net/**: Networking utilities.
* `setup-cluster-net`: Configures the host bridge (`fc-br0`) and TAP interfaces for the cluster.


* **launch.sh**: The main entry point script to provision and boot specific VM instances.

## Usage

### 1. Network Setup

Initialize the host networking bridge and TAP interfaces. This must be run as root.

```bash
sudo ./net/setup-cluster-net <COUNT>

```

*Replace `<COUNT>` with the number of VMs you intend to run (default is 3).*

### 2. Root Filesystem

If the master image (`lk-rootfs/rootfs.ext4`) does not exist, generate it:

```bash
cd lk-rootfs
./build-rootfs.sh

```

### 3. Launching Instances

To start a specific VM instance, run the launcher with an ID. The script automatically creates a copy of the master rootfs for persistence (`rootfs-vm{ID}.ext4`).

```bash
./launch.sh 1

```

Open a new terminal session to launch additional nodes:

```bash
./launch.sh 2

```

## Access

* **Console:** The launcher attaches directly to the serial console.
* **Login:** `root` / `root`
* **Networking:**
* VM 1 IP: `172.16.0.11`
* VM 2 IP: `172.16.0.12`
* Gateway: `172.16.0.1`



## Configuration

Modify `launch.sh` to adjust the following resource limits:

* `VM_CPU`: Default is 2 vCPUs.
* `VM_MEM`: Default is 1024 MB.
* `KERNEL`: Path to the boot kernel (default `lk-images/vmlinux-6.12-ebpf`).
