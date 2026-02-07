package vm

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/creack/pty"
)

// Manager orchestrates the lifecycle of all Firecracker MicroVMs.
type Manager struct {
	vms    map[int]*VM
	mu     sync.RWMutex
	config Config
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		vms:    make(map[int]*VM),
		config: cfg,
	}
}

// --- Helpers ---

func macForID(id int) string {
	return fmt.Sprintf("AA:FC:00:00:00:%02X", id)
}

func ipForID(id int) string {
	return fmt.Sprintf("172.16.0.%d", 10+id)
}

func socketPath(id int) string {
	return fmt.Sprintf("/tmp/firecracker-%d.socket", id)
}

func (m *Manager) rootfsPath(id int) string {
	return filepath.Join(m.config.RootFSDir, fmt.Sprintf("rootfs-vm%d.ext4", id))
}

// --- CRUD ---

// Create registers a new VM and prepares its sparse rootfs copy.
func (m *Manager) Create(id, vcpus, memMiB int) (*VM, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.vms[id]; exists {
		return nil, fmt.Errorf("vm %d already exists", id)
	}

	rootfs := m.rootfsPath(id)
	if _, err := os.Stat(rootfs); os.IsNotExist(err) {
		log.Printf("[vm %d] creating sparse rootfs copy -> %s", id, rootfs)
		src, err := os.Open(m.config.MasterRootFS)
		if err != nil {
			return nil, fmt.Errorf("open master rootfs: %w", err)
		}
		defer src.Close()

		// cp --sparse=always equivalent: create file, use SEEK_HOLE/SEEK_DATA
		// Simplest portable approach: shell out to cp
		cmd := exec.Command("cp", "--sparse=always", m.config.MasterRootFS, rootfs)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("sparse copy: %s: %w", string(out), err)
		}
	}

	vm := &VM{
		ID:     id,
		VCPUs:  vcpus,
		MemMiB: memMiB,
		State:  StateStopped,
		Socket: socketPath(id),
		TapDev: fmt.Sprintf("tap%d", id),
		MAC:    macForID(id),
		IP:     ipForID(id),
		RootFS: rootfs,
	}
	m.vms[id] = vm
	log.Printf("[vm %d] created: vcpus=%d mem=%dMiB mac=%s ip=%s", id, vcpus, memMiB, vm.MAC, vm.IP)
	return vm, nil
}

// Get returns a VM by ID.
func (m *Manager) Get(id int) (*VM, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	vm, ok := m.vms[id]
	if !ok {
		return nil, fmt.Errorf("vm %d not found", id)
	}
	return vm, nil
}

// List returns info for all registered VMs.
func (m *Manager) List() []VMInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]VMInfo, 0, len(m.vms))
	for _, vm := range m.vms {
		out = append(out, vm.Info())
	}
	return out
}

// Start launches the Firecracker process with a PTY, configures it via API, and boots the guest.
func (m *Manager) Start(id int) error {
	vm, err := m.Get(id)
	if err != nil {
		return err
	}

	vm.mu.Lock()
	if vm.State == StateRunning || vm.State == StateStarting {
		vm.mu.Unlock()
		return fmt.Errorf("vm %d is already %s", id, vm.State)
	}
	vm.State = StateStarting
	vm.mu.Unlock()

	// Cleanup stale socket
	os.Remove(vm.Socket)

	// Resolve absolute paths for Firecracker API
	kernelAbs, _ := filepath.Abs(m.config.KernelPath)
	rootfsAbs, _ := filepath.Abs(vm.RootFS)

	// Start Firecracker with PTY
	cmd := exec.Command(m.config.FirecrackerBin, "--api-sock", vm.Socket)
	cmd.Env = os.Environ()

	ptmx, err := pty.Start(cmd)
	if err != nil {
		vm.mu.Lock()
		vm.State = StateStopped
		vm.mu.Unlock()
		return fmt.Errorf("pty.Start: %w", err)
	}

	vm.mu.Lock()
	vm.Cmd = cmd
	vm.Pty = ptmx
	vm.Console = NewConsole(ptmx)
	vm.mu.Unlock()

	log.Printf("[vm %d] firecracker started pid=%d", id, cmd.Process.Pid)

	// Configure via API in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		fc := NewFCClient(vm.Socket)
		if err := fc.WaitForSocket(ctx); err != nil {
			log.Printf("[vm %d] socket wait failed: %v", id, err)
			m.markStopped(vm)
			return
		}

		bootArgs := m.config.BootArgs
		if bootArgs == "" {
			bootArgs = "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/sbin/init systemd.unit=multi-user.target"
		}

		steps := []struct {
			name string
			fn   func() error
		}{
			{"machine-config", func() error { return fc.SetMachineConfig(vm.VCPUs, vm.MemMiB) }},
			{"root-drive", func() error { return fc.SetRootDrive(rootfsAbs) }},
			{"boot-source", func() error { return fc.SetBootSource(kernelAbs, bootArgs) }},
			{"network", func() error { return fc.SetNetworkInterface(vm.MAC, vm.TapDev) }},
			{"instance-start", func() error { return fc.StartInstance() }},
		}

		for _, step := range steps {
			if err := step.fn(); err != nil {
				log.Printf("[vm %d] config step '%s' failed: %v", id, step.name, err)
				m.markStopped(vm)
				return
			}
		}

		vm.mu.Lock()
		vm.State = StateRunning
		vm.mu.Unlock()
		log.Printf("[vm %d] instance started successfully", id)
	}()

	// Wait for process exit in background
	go func() {
		_ = cmd.Wait()
		log.Printf("[vm %d] firecracker process exited", id)
		m.markStopped(vm)
	}()

	return nil
}

// Stop sends Ctrl+Alt+Del then kills the Firecracker process.
func (m *Manager) Stop(id int) error {
	vm, err := m.Get(id)
	if err != nil {
		return err
	}

	vm.mu.Lock()
	if vm.State != StateRunning && vm.State != StateStarting {
		vm.mu.Unlock()
		return fmt.Errorf("vm %d is not running (state=%s)", id, vm.State)
	}
	vm.State = StateStopping
	vm.mu.Unlock()

	// Try graceful shutdown first
	fc := NewFCClient(vm.Socket)
	_ = fc.SendCtrlAltDel()

	// Give it 3 seconds, then force kill
	go func() {
		time.Sleep(3 * time.Second)
		vm.mu.Lock()
		defer vm.mu.Unlock()
		if vm.State == StateStopping && vm.Cmd != nil && vm.Cmd.Process != nil {
			log.Printf("[vm %d] force killing pid=%d", id, vm.Cmd.Process.Pid)
			_ = vm.Cmd.Process.Kill()
		}
	}()

	return nil
}

// Destroy stops a VM and removes its rootfs.
func (m *Manager) Destroy(id int) error {
	vm, err := m.Get(id)
	if err != nil {
		return err
	}

	// Stop if running
	vm.mu.Lock()
	running := vm.State == StateRunning || vm.State == StateStarting
	vm.mu.Unlock()
	if running {
		if err := m.Stop(id); err != nil {
			return err
		}
		// Wait for it to die
		for i := 0; i < 50; i++ {
			vm.mu.Lock()
			stopped := vm.State == StateStopped
			vm.mu.Unlock()
			if stopped {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Remove rootfs
	if err := os.Remove(vm.RootFS); err != nil && !os.IsNotExist(err) {
		log.Printf("[vm %d] warning: could not remove rootfs: %v", id, err)
	}

	m.mu.Lock()
	delete(m.vms, id)
	m.mu.Unlock()

	log.Printf("[vm %d] destroyed", id)
	return nil
}

// markStopped cleans up after a Firecracker process exits.
func (m *Manager) markStopped(vm *VM) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.Console != nil {
		vm.Console.Close()
		vm.Console = nil
	}
	if vm.Pty != nil {
		vm.Pty.Close()
		vm.Pty = nil
	}
	vm.State = StateStopped
	os.Remove(vm.Socket)
}
