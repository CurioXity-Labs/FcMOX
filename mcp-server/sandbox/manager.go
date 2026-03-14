package sandbox

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Manager orchestrates the lifecycle of all Firecracker MicroVMs.
type Manager struct {
	vms    map[int]*VM
	mu     sync.RWMutex
	Config Config
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		vms:    make(map[int]*VM),
		Config: cfg,
	}
}

func (m *Manager) rootfsPath(id int) string {
	return filepath.Join(m.Config.RootFSDir, fmt.Sprintf("rootfs-vm%d.ext4", id))
}

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
		cmd := exec.Command("cp", "--sparse=always", m.Config.MasterRootFS, rootfs)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("sparse copy: %s: %w", string(out), err)
		}
	}

	vm := &VM{
		ID:     id,
		VCPUs:  vcpus,
		MemMiB: memMiB,
		State:  StateStopped,
		Socket: SocketPath(id),
		TapDev: fmt.Sprintf("tap%d", id),
		MAC:    MacForID(id),
		IP:     IPForID(id),
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

// Start launches the Firecracker process, configures it via API, and boots the guest.
func (m *Manager) Start(id int) error {
	vm, err := m.Get(id)
	if err != nil {
		return err
	}

	vm.Mu.Lock()
	if vm.State == StateRunning || vm.State == StateStarting {
		vm.Mu.Unlock()
		return fmt.Errorf("vm %d is already %s", id, vm.State)
	}
	vm.State = StateStarting
	vm.Mu.Unlock()

	// Cleanup stale socket
	os.Remove(vm.Socket)

	// Resolve absolute paths for Firecracker API
	kernelAbs, _ := filepath.Abs(m.Config.KernelPath)
	rootfsAbs, _ := filepath.Abs(vm.RootFS)

	// Create the TAP interface natively
	if err := setupNetwork(vm.TapDev, vm.MAC); err != nil {
		vm.Mu.Lock()
		vm.State = StateStopped
		vm.Mu.Unlock()
		return fmt.Errorf("setup network: %w", err)
	}

	// Create a log file for the Firecracker process
	logPath := fmt.Sprintf("/tmp/fc-vm%d.log", id)
	logFile, err := os.Create(logPath)
	if err != nil {
		vm.Mu.Lock()
		vm.State = StateStopped
		vm.Mu.Unlock()
		return fmt.Errorf("create log file: %w", err)
	}

	// Start Firecracker process
	cmd := exec.Command(m.Config.FirecrackerBin, "--api-sock", vm.Socket)
	cmd.Env = os.Environ()
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		vm.Mu.Lock()
		vm.State = StateStopped
		vm.Mu.Unlock()
		return fmt.Errorf("start firecracker: %w", err)
	}

	vm.Mu.Lock()
	vm.Cmd = cmd
	vm.Mu.Unlock()

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

		bootArgs := m.Config.BootArgs
		if bootArgs == "" {
			bootArgs = fmt.Sprintf("console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/sbin/init systemd.unit=multi-user.target quiet lpj=4000000 fc_ip=%s", vm.IP)
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
				if vm.Cmd != nil && vm.Cmd.Process != nil {
					_ = vm.Cmd.Process.Kill()
					_ = vm.Cmd.Wait()
				}
				m.markStopped(vm)
				return
			}
		}

		vm.Mu.Lock()
		vm.State = StateRunning
		vm.Mu.Unlock()
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

	vm.Mu.Lock()
	if vm.State != StateRunning && vm.State != StateStarting {
		vm.Mu.Unlock()
		return fmt.Errorf("vm %d is not running (state=%s)", id, vm.State)
	}
	vm.State = StateStopping
	vm.Mu.Unlock()

	// Try graceful shutdown first
	fc := NewFCClient(vm.Socket)
	_ = fc.SendCtrlAltDel()

	// Give it 3 seconds, then force kill
	go func() {
		time.Sleep(3 * time.Second)
		vm.Mu.Lock()
		defer vm.Mu.Unlock()
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
	vm.Mu.Lock()
	running := vm.State == StateRunning || vm.State == StateStarting
	vm.Mu.Unlock()
	if running {
		if err := m.Stop(id); err != nil {
			return err
		}
		// Wait for it to die
		for i := 0; i < 50; i++ {
			vm.Mu.Lock()
			stopped := vm.State == StateStopped
			vm.Mu.Unlock()
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

	cleanupNetwork(vm.TapDev)

	m.mu.Lock()
	delete(m.vms, id)
	m.mu.Unlock()

	log.Printf("[vm %d] destroyed", id)
	return nil
}

// WaitUntilReady blocks until VM is running and SSH is reachable, or timeout.
func (m *Manager) WaitUntilReady(id int, timeout time.Duration) error {
	vm, err := m.Get(id)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		vm.Mu.Lock()
		state := vm.State
		vm.Mu.Unlock()

		if state == StateRunning {
			// Try SSH connection
			if err := SSHCheck(vm.IP, m.Config.SSHUser, m.Config.SSHPassword); err == nil {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("vm %d did not become ready within %v", id, timeout)
}

// markStopped cleans up after a Firecracker process exits.
func (m *Manager) markStopped(vm *VM) {
	vm.Mu.Lock()
	defer vm.Mu.Unlock()
	vm.State = StateStopped
	os.Remove(vm.Socket)
	cleanupNetwork(vm.TapDev)
}

// setupNetwork creates the TAP device and attaches it to the fc-br0 bridge.
func setupNetwork(tapDev, mac string) error {
	bridge := "fc-br0"

	// Cleanup old junk natively
	_ = exec.Command("ip", "tuntap", "del", "dev", tapDev, "mode", "tap").Run()

	commands := [][]string{
		{"ip", "tuntap", "add", "dev", tapDev, "mode", "tap"},
		{"ip", "link", "set", tapDev, "address", mac},
		{"ip", "link", "set", tapDev, "master", bridge},
		{"ip", "link", "set", tapDev, "up"},
	}

	for _, cmdArgs := range commands {
		if out, err := exec.Command(cmdArgs[0], cmdArgs[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("network setup failed (%v): %s: %w", cmdArgs, string(out), err)
		}
	}
	return nil
}

// cleanupNetwork removes a TAP device.
func cleanupNetwork(tapDev string) {
	if tapDev == "" {
		return
	}
	_ = exec.Command("ip", "link", "del", tapDev).Run()
}
