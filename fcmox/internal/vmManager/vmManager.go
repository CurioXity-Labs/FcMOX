package vmmanager

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type VmStatus string

const (
	VmStatusStopped  VmStatus = "Stopped"
	VmStatusRunning  VmStatus = "Running"
	VmStatusCreating VmStatus = "Creating"
	VmStatusError    VmStatus = "Error"
	VmStatusPaused   VmStatus = "Paused"
)

func (s VmStatus) Display() string {
	switch s {
	case VmStatusRunning:
		return "● Running"
	case VmStatusStopped:
		return "○ Stopped"
	case VmStatusCreating:
		return "◌ Creating"
	case VmStatusError:
		return "✗ Error"
	case VmStatusPaused:
		return "⏸ Paused"
	default:
		return "? Unknown"
	}
}

type VmManager struct {
	mu sync.Mutex

	Rootfs  map[string]string
	Kernels map[string]string

	Vms map[string]*Vm

	nextIdx            int
	FirecrackerBinPath string
}

type Vm struct {
	VmCpuCount     int
	PID            int
	VmMemSize      int
	Id             string
	KernelPath     string
	RootfsPath     string
	TapDev         string
	MacAddr        string
	SockPath       string
	LogPath        string
	BootArgs       string
	Ip             string
	FirecrackerBin string
	Status         VmStatus
	Process        *os.Process // the live firecracker child process
}

// NewVmManager creates a new manager with default kernel/rootfs paths.
// firecrackerBinPath must point to a prepared, executable firecracker binary.
func NewVmManager(rootfsPath string, kernelImagesPath string, firecrackerBinPath string) *VmManager {
	rootfs := make(map[string]string)
	kernels := make(map[string]string)
	loadExistingRootFs(rootfsPath, rootfs)
	loadExistingKernels(kernelImagesPath, kernels)
	return &VmManager{
		Rootfs:             rootfs,
		Kernels:            kernels,
		Vms:                make(map[string]*Vm, 100),
		nextIdx:            1,
		FirecrackerBinPath: firecrackerBinPath,
	}
}

// CreateVm allocates a new VM record, spawns a dedicated firecracker process
// for it (listening on its own Unix socket), and returns the Vm.
func (mgr *VmManager) CreateVm(cpus int, memMB int, kernelPath string, rootfsPath string) (*Vm, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	id := mgr.nextVMID()
	idx := mgr.nextIdx - 1 // nextVMID already incremented nextIdx

	tapName, tapMAC, tapOk := createTap(id)
	if !tapOk {
		return nil, fmt.Errorf("failed to create TAP device for %s", id)
	}

	// Copy the rootfs so each VM has its own writable disk image.
	vmRootfs := fmt.Sprintf("/tmp/rootfs-%s.ext4", id)
	if out, err := exec.Command("cp", "--reflink=auto", rootfsPath, vmRootfs).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("copy rootfs for %s: %s: %w", id, string(out), err)
	}

	ip := fmt.Sprintf("172.16.0.%d", idx+1)
	vm := &Vm{
		Id:         id,
		VmCpuCount: cpus,
		VmMemSize:  memMB,
		KernelPath: kernelPath,
		RootfsPath: vmRootfs,
		TapDev:     tapName,
		MacAddr:    tapMAC,
		SockPath:   fmt.Sprintf("/tmp/fc-%s.sock", id),
		BootArgs:   "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/sbin/firecracker-init quiet lpj=4000000 fc_ip=" + ip,
		Ip:         ip,
		Status:     VmStatusCreating,
	}

	proc, logPath, err := startFirecrackerProcess(mgr.FirecrackerBinPath, vm.SockPath, id)
	if err != nil {
		return nil, fmt.Errorf("start firecracker for %s: %w", id, err)
	}
	vm.Process = proc
	vm.PID = proc.Pid
	vm.LogPath = logPath
	// Status stays Creating until StartVm() drives the API boot sequence.
	vm.Status = VmStatusCreating

	mgr.Vms[id] = vm
	return vm, nil
}

// StopVm kills the firecracker process for the VM, removes its TAP device,
// cleans up the socket, and marks it stopped.
func (mgr *VmManager) StopVm(id string) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	vm, ok := mgr.Vms[id]
	if !ok {
		return fmt.Errorf("VM %q not found", id)
	}
	if vm.Status == VmStatusStopped {
		return fmt.Errorf("%s is already stopped", id)
	}
	if vm.Process != nil {
		if err := vm.Process.Kill(); err != nil {
			return fmt.Errorf("kill firecracker process for %s: %w", id, err)
		}
		// Reap the process so it doesn't become a zombie.
		_, _ = vm.Process.Wait()
		vm.Process = nil
	}
	// Remove the TAP device that was created for this VM.
	deleteTap(vm.TapDev)
	os.Remove(vm.SockPath)
	os.Remove(vm.LogPath)
	os.Remove(vm.RootfsPath) // per-VM rootfs copy
	vm.Status = VmStatusStopped
	return nil
}

func (mgr *VmManager) PauseVm(id string) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	vm, ok := mgr.Vms[id]
	if !ok {
		return fmt.Errorf("VM %q not found", id)
	}
	if vm.Status == VmStatusPaused {
		return fmt.Errorf("%s is already paused", id)
	}
	if vm.Status == VmStatusStopped {
		return fmt.Errorf("%s is stopped", id)
	}
	if vm.Process == nil {
		return fmt.Errorf("%s has no firecracker process; call CreateVm first", id)
	}

	if err := pauseVm(vm); err != nil {
		vm.Status = VmStatusError
		return fmt.Errorf("pause %s: %w", id, err)
	}

	vm.Status = VmStatusPaused

	return nil
}

func (mgr *VmManager) ResumeVm(id string) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	vm, ok := mgr.Vms[id]
	if !ok {
		return fmt.Errorf("VM %q not found", id)
	}
	if vm.Status == VmStatusRunning {
		return fmt.Errorf("%s is already running", id)
	}
	if vm.Status == VmStatusStopped {
		return fmt.Errorf("%s is stopped", id)
	}
	if vm.Process == nil {
		return fmt.Errorf("%s has no firecracker process; call CreateVm first", id)
	}

	if err := resumeVm(vm); err != nil {
		vm.Status = VmStatusError
		return fmt.Errorf("resume %s: %w", id, err)
	}

	vm.Status = VmStatusRunning

	return nil
}

// StartVm configures the firecracker VMM via its REST API and boots the guest.
// CreateVm must have been called first to spawn the firecracker process.
// StartVm waits up to 1 s for the Unix socket to become available before
// driving the full API sequence: machine-config → boot-source → drives →
// network-interfaces → InstanceStart.
func (mgr *VmManager) StartVm(id string) error {
	mgr.mu.Lock()
	vm, ok := mgr.Vms[id]
	mgr.mu.Unlock()

	if !ok {
		return fmt.Errorf("VM %q not found", id)
	}
	if vm.Status == VmStatusRunning {
		return fmt.Errorf("%s is already running", id)
	}
	if vm.Process == nil {
		return fmt.Errorf("%s has no firecracker process; call CreateVm first", id)
	}

	// Wait for the Unix socket to appear (firecracker takes a moment to bind).
	if err := waitForSocket(vm.SockPath, time.Second); err != nil {
		return fmt.Errorf("socket %s not ready: %w", vm.SockPath, err)
	}

	// Configure and boot through the Firecracker REST API.
	if err := configureAndBootVm(vm); err != nil {
		vm.Status = VmStatusError
		return fmt.Errorf("boot %s: %w", id, err)
	}

	mgr.mu.Lock()
	vm.Status = VmStatusRunning
	mgr.mu.Unlock()

	return nil
}

// startFirecrackerProcess launches a single firecracker process that exposes
// its API over the given Unix socket path. Stdout/stderr are redirected to
// a per-VM log file at /tmp/fc-<vmID>.log.
func startFirecrackerProcess(binPath string, sockPath string, vmID string) (*os.Process, string, error) {
	logPath := fmt.Sprintf("/tmp/fc-%s.log", vmID)
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, "", fmt.Errorf("create log file %s: %w", logPath, err)
	}

	cmd := exec.Command(
		binPath,
		"--api-sock", sockPath,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		os.Remove(logPath)
		return nil, "", fmt.Errorf("exec %s: %w", binPath, err)
	}
	// Close our handle — the child process inherited the fd.
	logFile.Close()
	return cmd.Process, logPath, nil
}

// VmCount returns total number of VMs.
func (mgr *VmManager) VmCount() int {
	return len(mgr.Vms)
}

// RunningCount returns number of running VMs.
func (mgr *VmManager) RunningCount() int {
	count := 0
	for _, vm := range mgr.Vms {
		if vm.Status == VmStatusRunning {
			count++
		}
	}
	return count
}

func (mgr *VmManager) GetVmByID(id string) (*Vm, bool) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	val, ok := mgr.Vms[id]
	return val, ok
}

func (mgr *VmManager) GetRootfsByName(name string) (string, bool) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	val, ok := mgr.Rootfs[name]
	return val, ok
}

func (mgr *VmManager) GetKernelByName(name string) (string, bool) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	val, ok := mgr.Kernels[name]
	return val, ok
}

func (mgr *VmManager) nextVMID() string {

	id := fmt.Sprintf("vm-%d", mgr.nextIdx)
	mgr.nextIdx++

	return id
}

// loadExistingKernels scans path for vmlinux* files and fills kernels map
// with image-name -> absolute-path entries.
func loadExistingKernels(path string, kernels map[string]string) {
	files, err := os.ReadDir(path)
	if err != nil {
		fmt.Printf("Error reading kernel images directory: %v\n", err)
		return
	}
	for _, file := range files {
		if !file.IsDir() && strings.HasPrefix(file.Name(), "vmlinux") {
			if strings.HasSuffix(file.Name(), ".xz") {
				continue
			}
			absPath := path + "/" + file.Name()
			kernels[file.Name()] = absPath
			fmt.Printf("Kernel found: %s\n", absPath)
		}
	}
	if len(kernels) == 0 {
		fmt.Printf("No vmlinux found in %s\n", path)
	}
}

// loadExistingRootFs scans path for *.ext4 files and fills rootfs map
// with image-name -> absolute-path entries (key is filename without .ext4).
func loadExistingRootFs(path string, rootfs map[string]string) {
	files, err := os.ReadDir(path)
	if err != nil {
		fmt.Printf("Error reading rootfs directory: %v\n", err)
		return
	}
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".ext4") {
			absPath := path + "/" + file.Name()
			// Strip the .ext4 suffix to use as the image name key.
			imageName := strings.TrimSuffix(file.Name(), ".ext4")
			rootfs[imageName] = absPath
			fmt.Printf("Rootfs found: %s -> %s\n", imageName, absPath)
		}
	}
	if len(rootfs) == 0 {
		fmt.Printf("No rootfs found in %s\n", path)
	}
}

func createTap(vmID string) (string, string, bool) {
	u := uuid.NewMD5(uuid.NameSpaceDNS, []byte(vmID))
	b := u[:]

	// Use first 6 bytes of UUID, force LAA bit
	mac := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		(b[0]|2)&0xfe, b[1], b[2], b[3], b[4], b[5])

	tapName := fmt.Sprintf("tap-%s", vmID)
	bridge := "fc-br0"

	// 1. Cleanup old junk (Idempotency)
	_ = exec.Command("ip", "tuntap", "del", "dev", tapName, "mode", "tap").Run()

	// 2. Commands defined as a slice for cleaner execution
	commands := [][]string{
		{"ip", "tuntap", "add", "dev", tapName, "mode", "tap"},
		{"ip", "link", "set", tapName, "address", mac}, // Set MAC on Host side too
		{"ip", "link", "set", tapName, "master", bridge},
		{"ip", "link", "set", tapName, "up"},
	}

	for _, cmdArgs := range commands {
		if out, err := exec.Command(cmdArgs[0], cmdArgs[1:]...).CombinedOutput(); err != nil {
			slog.Error("Network setup failed", "cmd", cmdArgs, "out", string(out), "err", err)
			return "", "", false
		}
	}

	return tapName, mac, true
}

// deleteTap removes a TAP device. Best-effort; errors are logged but not fatal.
func deleteTap(tapName string) {
	if tapName == "" {
		return
	}
	if out, err := exec.Command("ip", "link", "del", tapName).CombinedOutput(); err != nil {
		slog.Warn("failed to delete TAP", "tap", tapName, "err", err, "output", string(out))
	}
}

// Cleanup stops all running VMs, removing their TAP devices, firecracker
// processes, and sockets. Call this on application shutdown.
func (mgr *VmManager) Cleanup() {
	slog.Info("cleaning up all VMs...")
	for id, vm := range mgr.Vms {
		if vm.Status != VmStatusStopped {
			if err := mgr.StopVm(id); err != nil {
				slog.Warn("cleanup: failed to stop VM", "id", id, "err", err)
			}
		}
	}
	slog.Info("all VMs cleaned up")
}
