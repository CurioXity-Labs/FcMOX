package vmmanager

import (
	"fmt"
	"os"
	"strings"
)

type VmStatus string

const (
	VmStatusStopped  VmStatus = "Stopped"
	VmStatusRunning  VmStatus = "Running"
	VmStatusCreating VmStatus = "Creating"
	VmStatusError    VmStatus = "Error"
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
	default:
		return "? Unknown"
	}
}

type VmManager struct {
	RootfsPath           []string
	ExistingKernelImages []string
	Vms                  []*Vm
	nextIdx              int
}

type Vm struct {
	Id         string
	VmCpuCount int
	VmMemSize  int
	KernelPath string
	RootfsPath string
	TapDev     string
	MacAddr    string
	SockPath   string
	BootArgs   string
	Ip         string
	Status     VmStatus
}

// NewVmManager creates a new manager with default kernel/rootfs paths.
func NewVmManager(rootfsPath string, KernelImagesPath string) *VmManager {
	return &VmManager{
		RootfsPath:           loadExitingRootFs(rootfsPath),
		ExistingKernelImages: loadExistingKernels(KernelImagesPath),
		Vms:                  make([]*Vm, 0),
		nextIdx:              1,
	}
}

// CreateVm adds a new VM with auto-generated network config.
func (mgr *VmManager) CreateVm(cpus int, memMB int) *Vm {
	id := fmt.Sprintf("vm%d", mgr.nextIdx)
	idx := mgr.nextIdx
	mgr.nextIdx++

	vm := &Vm{
		Id:         id,
		VmCpuCount: cpus,
		VmMemSize:  memMB,
		KernelPath: mgr.ExistingKernelImages[0],
		RootfsPath: fmt.Sprintf("%s/rootfs-%s.ext4", mgr.RootfsPath[0], id),
		TapDev:     fmt.Sprintf("tap%d", idx-1),
		MacAddr:    fmt.Sprintf("06:00:AC:10:00:%02X", idx+1),
		SockPath:   fmt.Sprintf("/tmp/fc-%s.sock", id),
		BootArgs:   "console=ttyS0 reboot=k panic=1",
		Ip:         fmt.Sprintf("172.16.0.%d", idx+1),
		Status:     VmStatusCreating,
	}

	mgr.Vms = append(mgr.Vms, vm)
	return vm
}

// DeleteVm removes a VM by index. Returns the deleted VM's ID or error.
func (mgr *VmManager) DeleteVm(index int) (string, error) {
	if index < 0 || index >= len(mgr.Vms) {
		return "", fmt.Errorf("invalid VM index: %d", index)
	}
	id := mgr.Vms[index].Id
	mgr.Vms = append(mgr.Vms[:index], mgr.Vms[index+1:]...)
	return id, nil
}

// StartVm sets a VM to Running. Returns error if already running.
func (mgr *VmManager) StartVm(index int) error {
	if index < 0 || index >= len(mgr.Vms) {
		return fmt.Errorf("invalid VM index: %d", index)
	}
	vm := mgr.Vms[index]
	if vm.Status == VmStatusRunning {
		return fmt.Errorf("%s is already running", vm.Id)
	}
	vm.Status = VmStatusRunning
	return nil
}

// StopVm sets a VM to Stopped. Returns error if already stopped.
func (mgr *VmManager) StopVm(index int) error {
	if index < 0 || index >= len(mgr.Vms) {
		return fmt.Errorf("invalid VM index: %d", index)
	}
	vm := mgr.Vms[index]
	if vm.Status == VmStatusStopped {
		return fmt.Errorf("%s is already stopped", vm.Id)
	}
	vm.Status = VmStatusStopped
	return nil
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

func loadExistingKernels(path string) []string {
	files, err := os.ReadDir(path)
	if err != nil {
		fmt.Printf("Error reading kernel images directory: %v\n", err)
		return []string{}
	}
	kernels := []string{}
	for _, file := range files {
		if !file.IsDir() && strings.HasPrefix(file.Name(), "vmlinux") {
			kernels = append(kernels, path+"/"+file.Name())
			fmt.Printf("Kernels found %s\n", path+"/"+file.Name())
		}
	}
	if len(kernels) == 0 {
		fmt.Printf("No vmlinux found in %s\n", path)
	}
	return kernels
}

func loadExitingRootFs(path string) []string {
	files, err := os.ReadDir(path)
	if err != nil {
		fmt.Printf("Error reading rootfs directory: %v\n", err)
		return []string{}
	}
	rootfs := []string{}
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".ext4") {
			rootfs = append(rootfs, path+"/"+file.Name())
			fmt.Printf("Rootfs found %s\n", path+"/"+file.Name())
		}
	}
	if len(rootfs) == 0 {
		fmt.Printf("No rootfs found in %s\n", path)
	}
	return rootfs
}