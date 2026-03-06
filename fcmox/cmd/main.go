package main

import (
	"flag"
	"fmt"
	"os"

	"fcmox/internal/tui"
	vm "fcmox/internal/vmManager"
)

var (
	LinuxImagesPath string
	RootfsPath      string
)

func main() {
	// Create the VM manager (single instance shared with TUI)
	flag.StringVar(&RootfsPath, "rootfs-path", "/home/nikhil/firecracker-ebpf-lab/lk-rootfs", "Path to rootfs directory")
	flag.StringVar(&LinuxImagesPath, "linux-images-path", "/home/nikhil/firecracker-ebpf-lab/lk-images", "Path to Linux images directory")
	flag.Parse()
	mgr := vm.NewVmManager(
		RootfsPath,
		LinuxImagesPath,
	)

	// Seed with some demo VMs
	v1 := mgr.CreateVm(2, 512)
	v1.Status = vm.VmStatusRunning

	v2 := mgr.CreateVm(4, 1024)
	v2.Status = vm.VmStatusRunning

	v3 := mgr.CreateVm(1, 256)
	v3.Status = vm.VmStatusStopped

	v4 := mgr.CreateVm(2, 512)
	_ = v4 // stays in Creating status

	if err := tui.Run(mgr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
