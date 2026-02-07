# FireAdmin — Firecracker MicroVM Control Plane

Go-based HTTP/WebSocket management server for orchestrating Firecracker MicroVM clusters. Provides a REST API for VM lifecycle operations and browser-based serial console access via xterm.js.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Browser (index.html)                                       │
│  ├─ REST API calls (create/start/stop/destroy)             │
│  └─ WebSocket (/ws/console/:id) ←→ xterm.js terminal       │
└─────────────────────────────────────────────────────────────┘
                            ↕ HTTP/WS
┌─────────────────────────────────────────────────────────────┐
│  fireadmin (Echo + Gorilla WS)                              │
│  ├─ vm.Manager: VM lifecycle orchestrator                   │
│  ├─ vm.FCClient: HTTP-over-UDS → Firecracker API           │
│  └─ vm.Console: PTY fan-out broadcaster (64KB scrollback)  │
└─────────────────────────────────────────────────────────────┘
                            ↕ UDS + PTY
┌─────────────────────────────────────────────────────────────┐
│  Firecracker Processes (one per VM)                         │
│  └─ /tmp/firecracker-{id}.socket (API)                     │
│  └─ PTY (/dev/ptmx) attached to guest ttyS0                 │
└─────────────────────────────────────────────────────────────┘
```

### Components

| Package | Role |
|---------|------|
| `vm/manager.go` | VM CRUD, process lifecycle, reboot loop handling |
| `vm/firecracker.go` | Firecracker API client (machine-config, drives, boot-source, network-interfaces) |
| `vm/console.go` | PTY → WebSocket broadcaster with ring buffer for scrollback |
| `handlers/api.go` | REST endpoints for VM management |
| `handlers/console.go` | WebSocket upgrade handler for console attachment |
| `static/index.html` | Single-page web UI (xterm.js + vanilla JS) |

## Prerequisites

- Go 1.22+
- Firecracker binary in project root (`../firecracker`)
- Kernel image (`../lk-images/vmlinux-6.12-ebpf`)
- Master rootfs (`../lk-rootfs/rootfs.ext4`)
- TAP interfaces configured (`sudo ../net/setup-cluster-net <count>`)

## Build

```bash
cd web-ui
go mod download
go build -o fireadmin .
```

## Usage

### Start the control plane

```bash
# Auto-detect paths (relative to project root)
sudo ./fireadmin --port 8080

# Override paths
sudo ./fireadmin \
  --firecracker /usr/local/bin/firecracker \
  --kernel /path/to/vmlinux \
  --rootfs /path/to/master.ext4 \
  --rootfs-dir /vm/instances \
  --port 8080
```

**Why sudo?** Needs access to `/dev/kvm` and TAP interfaces.

### Web UI

Open `http://localhost:8080` in your browser.

**Features:**
- Create VMs with custom vCPU/memory profiles (dropdown presets: 1/2/4 CPU, 512M/1G/2G)
- Start/Stop/Destroy operations
- Real-time serial console via xterm.js (multi-client fan-out)
- 64KB scrollback buffer for late-joining clients
- Tab-based console management (multiple VMs simultaneously)

## REST API

### VM Lifecycle

**List all VMs**
```http
GET /api/vms
```
Response:
```json
[
  {
    "id": 1,
    "vcpus": 2,
    "mem_mib": 1024,
    "state": "running",
    "mac": "AA:FC:00:00:00:01",
    "ip": "172.16.0.11",
    "pid": 12345
  }
]
```

**Create a VM**
```http
POST /api/vms
Content-Type: application/json

{
  "id": 1,
  "vcpus": 2,
  "mem_mib": 1024
}
```
- Creates sparse copy: `rootfs-vm{id}.ext4` (persistent disk)
- MAC: `AA:FC:00:00:00:{id:02X}`
- IP: `172.16.0.{10+id}`
- TAP: `tap{id}`

**Get VM info**
```http
GET /api/vms/:id
```

**Start a VM**
```http
POST /api/vms/:id/start
```
- Launches Firecracker with PTY
- Configures via UDS API socket
- Returns immediately (state: "starting")
- Background goroutine waits for API socket, then configures machine/drive/boot/network
- State transitions to "running" after `InstanceStart` succeeds

**Stop a VM**
```http
POST /api/vms/:id/stop
```
- Sends Ctrl+Alt+Del (ACPI shutdown)
- Force-kills after 3s timeout
- Disk writes persist; RAM does not

**Destroy a VM**
```http
DELETE /api/vms/:id
```
- Stops VM if running
- Deletes `rootfs-vm{id}.ext4`
- Unregisters from manager

### Console

**Attach to serial console**
```
ws://localhost:8080/ws/console/:id
```
- Protocol: Binary WebSocket
- Server → Client: Raw PTY output (ANSI/VT100)
- Client → Server: Keyboard input
- On connect: Sends 64KB scrollback buffer
- Fan-out: Multiple clients can attach simultaneously
- Auto-cleanup: Client removed on disconnect/error

## Implementation Details

### VM State Machine

```
stopped → starting → running → stopping → stopped
                          ↓
                       (crash/reboot) → stopped
```

State transitions are mutex-protected. The `cmd.Wait()` goroutine handles unexpected exits (guest crashes, reboots).

### Sparse Rootfs Copies

```bash
cp --sparse=always master.ext4 rootfs-vm{id}.ext4
```
Only allocated blocks consume disk space. A 2GB ext4 image with 500MB of data uses ~500MB on the host.

### PTY Console Architecture

Each Firecracker process runs under a PTY (pseudoterminal):
- **Master (host side)**: `vm.Pty *os.File` — read/write by console broadcaster
- **Slave (guest side)**: Firecracker's `ttyS0` kernel console

The `Console` struct:
1. Spawns `readLoop()` goroutine → reads from PTY, broadcasts to all WebSocket clients
2. Maintains a 64KB ring buffer for scrollback
3. On client attach: sends buffer history, then live output
4. On client input: writes directly to PTY (echoed by guest kernel)

### Firecracker API Configuration Flow

1. Launch: `./firecracker --api-sock /tmp/firecracker-{id}.socket`
2. Wait for socket to appear (poll with 10ms sleep)
3. PUT `/machine-config` (vCPUs, memory)
4. PUT `/drives/rootfs` (path to rootfs-vm{id}.ext4)
5. PUT `/boot-source` (kernel path, boot args)
6. PUT `/network-interfaces/eth0` (MAC, TAP device)
7. PUT `/actions` (`InstanceStart`)

All API calls use HTTP-over-UDS via `net.Dial("unix", socketPath)`.

### Reboot Handling

The guest kernel is compiled with `reboot=k` (KVM hypercall). When the guest runs `reboot`:
1. Firecracker process exits (KVM_EXIT_SHUTDOWN)
2. `cmd.Wait()` unblocks
3. `markStopped()` cleans up: closes PTY, removes socket, transitions state
4. **No automatic restart** — use the web UI or API to `POST /api/vms/:id/start` again

The old `launch.sh` reboot loop is **not needed** when using FireAdmin.

## Development

### Add a new Firecracker API call

1. Define the JSON struct in [vm/firecracker.go](vm/firecracker.go)
2. Add a method: `func (fc *FCClient) MyAction() error { return fc.put("/path", payload) }`
3. Call it from [vm/manager.go](vm/manager.go) or handlers

### Add a new REST endpoint

1. Define handler in [handlers/api.go](handlers/api.go) or create new file
2. Wire route in [main.go](main.go): `api.POST("/vms/:id/action", h.MyAction)`

### Hot-reload during development

```bash
# Terminal 1: Auto-rebuild on file changes
while true; do go build -o fireadmin . && echo "Built $(date)"; inotifywait -e modify *.go vm/*.go handlers/*.go; done

# Terminal 2: Run with auto-restart
while true; do sudo ./fireadmin --port 8080; sleep 1; done
```

## Security Notes

- **No authentication** — this is a research lab tool, not production software
- **CORS disabled** — `AllowOrigins: ["*"]`
- WebSocket origin check: `return true` (allows any origin)
- Runs as root (KVM + TAP access)

For production use, add:
- TLS (`e.StartTLS()` with Let's Encrypt)
- JWT or session-based auth middleware
- RBAC for multi-tenant isolation
- Input validation (VM ID ranges, resource limits)

## Troubleshooting

**Binary won't start: "kernel not found"**
```bash
# Check paths
./fireadmin --kernel /path/to/vmlinux --rootfs /path/to/rootfs.ext4
```

**VM stuck in "starting"**
```bash
# Check Firecracker logs (if any) and socket
ls -la /tmp/firecracker-*.socket
journalctl -f | grep firecracker
```

**Console shows "Disconnected" immediately**
- VM not running (start it first)
- PTY closed (VM crashed — check `dmesg` on guest)
- WebSocket proxy stripping binary frames (don't use nginx without `proxy_http_version 1.1`)

**VM crashes on boot**
```bash
# Missing kernel modules in rootfs
# Rebuild with: cd ../lk-rootfs && ./build-rootfs.sh

# Wrong boot args
./fireadmin --boot-args "console=ttyS0 reboot=k panic=1 root=/dev/vda rw"
```

## Future Enhancements

- [ ] Snapshot/restore via Firecracker's snapshot API
- [ ] Metrics endpoint (Prometheus exporter for VM resource usage)
- [ ] Event stream (SSE for state change notifications)
- [ ] Batch operations (`POST /api/cluster/start-all`)
- [ ] VM resource limits (cgroups integration)
- [ ] Hot-resize (vCPU/memory balloon device)
- [ ] Multi-node orchestration (clustered control plane)

## License

MIT — see parent [README.md](../Readme.md)
