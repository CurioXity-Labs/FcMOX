package vm

import (
	"os"
	"os/exec"
	"sync"

	"github.com/gorilla/websocket"
)

// VMState represents the lifecycle state of a MicroVM.
type VMState int

const (
	StateStopped VMState = iota
	StateStarting
	StateRunning
	StateStopping
)

func (s VMState) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	default:
		return "unknown"
	}
}

// VM holds all state for a single Firecracker MicroVM instance.
type VM struct {
	ID     int     `json:"id"`
	VCPUs  int     `json:"vcpus"`
	MemMiB int     `json:"mem_mib"`
	State  VMState `json:"-"`

	// Derived / runtime
	Socket string `json:"-"`
	TapDev string `json:"-"`
	MAC    string `json:"mac"`
	IP     string `json:"ip"`
	RootFS string `json:"-"`

	// Process management
	Cmd *exec.Cmd `json:"-"`
	Pty *os.File  `json:"-"`

	// Console broadcast
	Console *Console `json:"-"`

	mu sync.Mutex
}

// Mu exposes the VM mutex for external synchronization (console handler).
func (v *VM) Mu() *sync.Mutex { return &v.mu }

// VMInfo is the JSON-serializable view returned by the API.
type VMInfo struct {
	ID     int    `json:"id"`
	VCPUs  int    `json:"vcpus"`
	MemMiB int    `json:"mem_mib"`
	State  string `json:"state"`
	MAC    string `json:"mac"`
	IP     string `json:"ip"`
	PID    int    `json:"pid,omitempty"`
}

// Info returns a safe, serializable snapshot of VM state.
func (v *VM) Info() VMInfo {
	v.mu.Lock()
	defer v.mu.Unlock()
	info := VMInfo{
		ID:     v.ID,
		VCPUs:  v.VCPUs,
		MemMiB: v.MemMiB,
		State:  v.State.String(),
		MAC:    v.MAC,
		IP:     v.IP,
	}
	if v.Cmd != nil && v.Cmd.Process != nil {
		info.PID = v.Cmd.Process.Pid
	}
	return info
}

// Console manages PTY fan-out to multiple WebSocket clients.
type Console struct {
	pty     *os.File
	clients map[*websocket.Conn]bool
	mu      sync.Mutex
	done    chan struct{}
	buf     *RingBuffer
}

// RingBuffer keeps the last N bytes of console output for new clients.
type RingBuffer struct {
	data []byte
	size int
	mu   sync.Mutex
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{data: make([]byte, 0, size), size: size}
}

func (r *RingBuffer) Write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, p...)
	if len(r.data) > r.size {
		r.data = r.data[len(r.data)-r.size:]
	}
}

func (r *RingBuffer) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]byte, len(r.data))
	copy(out, r.data)
	return out
}

// Config holds paths resolved at startup.
type Config struct {
	FirecrackerBin string
	KernelPath     string
	MasterRootFS   string
	RootFSDir      string
	BootArgs       string
}
