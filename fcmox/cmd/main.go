package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"fcmox/internal/rest"
	"fcmox/internal/ssh"
	"fcmox/internal/tui"
	vm "fcmox/internal/vmManager"
)

var (
	LinuxImagesPath string
	RootfsPath      string
	APIAddr         string
	SSHUser         string
	SSHPassword     string
)

func main() {
	flag.StringVar(&RootfsPath, "rootfs-path", "/home/n/firecracker/lk-rootfs", "Path to rootfs directory")
	flag.StringVar(&LinuxImagesPath, "linux-images-path", "lk-images", "Path to Linux images directory")
	flag.StringVar(&APIAddr, "api-addr", ":8090", "REST API listen address")
	flag.StringVar(&SSHUser, "ssh-user", "root", "SSH username for guest VMs")
	flag.StringVar(&SSHPassword, "ssh-password", "root", "SSH password for guest VMs")
	flag.Parse()

	err := initialCleanup()
	if err != nil {
		log.Fatalf("initial cleanup: %v", err)
	}
	// Extract the embedded firecracker binary to a temp file and make it executable.
	fcBinPath, err := PrepareBinary()
	if err != nil {
		log.Fatalf("prepare firecracker binary: %v", err)
	}
	// Clean up the temp binary on exit — runs on signal or normal return.
	cleanup := func() {
		fmt.Println("\nCleaning up temp firecracker binary...")
		os.Remove(fcBinPath)
	}
	defer cleanup()

	// Trap SIGINT/SIGTERM so cleanup actually runs (defer won't fire on raw kill).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("Firecracker binary ready: %s\n", fcBinPath)

	RootfsAbsPath, err := filepath.Abs(RootfsPath)
	if err != nil {
		fmt.Printf("Not Found Abs Path for Rootfs")
	}
	LinuxImagesAbsPath, err := filepath.Abs(LinuxImagesPath)
	if err != nil {
		fmt.Printf("Not Found Abs Path for Linux Images")
	}

	mgr := vm.NewVmManager(
		RootfsAbsPath,
		LinuxImagesAbsPath,
		fcBinPath,
	)

	// Start REST API server in the background
	apiServer := rest.NewServer(mgr, ssh.Config{
		User:     SSHUser,
		Password: SSHPassword,
	}, APIAddr)
	go func() {
		if err := apiServer.Start(); err != nil {
			log.Printf("REST API server error: %v", err)
		}
	}()

	err = tui.Run(mgr)
	if err != nil {
		log.Fatalf("run tui: %v", err)
	}
	// Block until signal — cleanup() fires via defer on return.
	sig := <-sigCh
	fmt.Printf("\nReceived %v, shutting down...\n", sig)

	// Stop all VMs, remove TAP devices, kill firecracker processes.
	mgr.Cleanup()
}

func initialCleanup() error {
	patterns := []string{
		"/tmp/firecracker-*",    // temp binaries
		"/tmp/fc-*.sock",        // API sockets
		"/tmp/fc-*.log",         // VM logs
		"/tmp/rootfs-vm-*.ext4", // rootfs copies
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			_ = os.RemoveAll(match)
		}
	}
	return nil
}
