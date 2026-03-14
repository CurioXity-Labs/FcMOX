package sandbox

import (
	"fmt"
	"os/exec"
	"sync"
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
	ID     int
	VCPUs  int
	MemMiB int
	State  VMState

	// Derived / runtime
	Socket string
	TapDev string
	MAC    string
	IP     string
	RootFS string

	// Process management
	Cmd *exec.Cmd
	Mu  sync.Mutex
}

// VMInfo is the JSON-serializable view returned by tools.
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
	v.Mu.Lock()
	defer v.Mu.Unlock()
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

// Config holds paths and credentials resolved at startup.
type Config struct {
	FirecrackerBin string
	KernelPath     string
	MasterRootFS   string
	RootFSDir      string
	BootArgs       string

	// SSH credentials for guest VMs (configurable)
	SSHUser     string
	SSHPassword string
}

// Helper functions

func MacForID(id int) string {
	return fmt.Sprintf("AA:FC:00:00:00:%02X", id)
}

func IPForID(id int) string {
	return fmt.Sprintf("172.16.0.%d", 10+id)
}

func SocketPath(id int) string {
	return fmt.Sprintf("/tmp/firecracker-%d.socket", id)
}
