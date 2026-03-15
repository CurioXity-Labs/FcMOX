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
sudo ./scripts/setup-cluster-net
```

### 3. Launching fcmox (VM & API Manager)

The `fcmox` manager provides a high-performance Terminal User Interface and a background REST API (on `:8090` by default) for programmatic and AI-driven control.

```bash
cd fcmox
go build -o fcmox-bin ./cmd/
sudo ./fcmox-bin --api-addr=:8090 --rootfs-path=../lk-rootfs --linux-images-path=../lk-images
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

## Model Context Protocol (MCP) Integration

The `fcmox` manager includes a **Model Context Protocol (MCP)** server, allowing AI agents (like Claude or Cursor) to autonomously manage the Firecracker VMs. This enables "agent-in-the-loop" workflows for automated eBPF research and systems testing.

### 1. Build the MCP Server

```bash
cd mcp-server
go build -o mcp-sandbox .
```

### 2. Configuration

Add the server to your AI agent's MCP settings (e.g., `mcp-config.json` or Cline settings):

```json
{
  "mcpServers": {
    "firecracker-sandbox": {
      "command": "/home/n/firecracker/mcp-server/mcp-sandbox",
      "args": ["--fcmox-url=http://localhost:8090"]
    }
  }
}
```

*Note: The MCP server is a thin client that talks to the `fcmox` REST API. `fcmox` must be running for these tools to be available.*

### AI-Powered Tools

The agent gains access to a specialized suite of management tools:
* `sandbox_create_vm`: Provision and boot fresh microVMs.
* `sandbox_exec`: Run research commands in guests via SSH.
* `sandbox_install_packages`: Dynamic software management.
* `sandbox_run_script`: Automated research script execution.
* `sandbox_upload_file`: Transfer files/eBPF programs into guests.
* `sandbox_list_vms`: Real-time cluster inventory.

## Access

* **Login:** `root` / `root`
* **Networking:** IP addresses are generated incrementally starting from `172.16.0.2`.
* **Gateway:** `172.16.0.1`
* **REST API:** `http://localhost:8090` (starts automatically with `fcmox`)
