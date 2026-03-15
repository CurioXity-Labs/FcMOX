package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"mcp-sandbox/sandbox"
	"mcp-sandbox/tools"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// CRITICAL: Force all standard log output to stderr immediately so it doesn't corrupt MCP's stdout JSON
	log.SetOutput(os.Stderr)

	var (
		transport   string
		httpAddr    string
		fcBin       string
		kernelPath  string
		masterRoot  string
		rootfsDir   string
		bootArgs    string
		sshUser     string
		sshPassword string
	)

	flag.StringVar(&transport, "transport", "stdio", "Transport mode: 'stdio' or 'http'")
	flag.StringVar(&httpAddr, "http-addr", ":8081", "HTTP listen address (only used with --transport=http)")
	flag.StringVar(&fcBin, "firecracker", "", "Path to firecracker binary (default: auto-detect)")
	flag.StringVar(&kernelPath, "kernel", "", "Path to kernel image (default: ../lk-images/vmlinux-6.12-ebpf)")
	flag.StringVar(&masterRoot, "rootfs", "", "Path to master rootfs image (default: ../lk-rootfs/rootfs.ext4)")
	flag.StringVar(&rootfsDir, "rootfs-dir", "", "Directory for VM rootfs copies (default: ../)")
	flag.StringVar(&bootArgs, "boot-args", "", "Kernel boot arguments (auto-generated if empty)")
	flag.StringVar(&sshUser, "ssh-user", "root", "SSH username for guest VMs")
	flag.StringVar(&sshPassword, "ssh-password", "root", "SSH password for guest VMs")
	flag.Parse()

	// Resolve paths relative to the binary's parent (project root)
	baseDir := resolveBaseDir()

	if fcBin == "" {
		fcBin = filepath.Join(baseDir, "firecracker")
	}
	if kernelPath == "" {
		kernelPath = filepath.Join(baseDir, "lk-images", "vmlinux-6.12-ebpf")
	}
	if masterRoot == "" {
		masterRoot = filepath.Join(baseDir, "lk-rootfs", "rootfs.ext4")
	}
	if rootfsDir == "" {
		rootfsDir = baseDir
	}
	if bootArgs == "" {
		bootArgs = "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/sbin/firecracker-init quiet lpj=4000000"
	}

	// Validate critical paths
	for _, check := range []struct{ name, path string }{
		{"firecracker binary", fcBin},
		{"kernel image", kernelPath},
		{"master rootfs", masterRoot},
	} {
		if _, err := os.Stat(check.path); os.IsNotExist(err) {
			log.Printf("⚠  %s not found: %s", check.name, check.path)
		}
	}

	cfg := sandbox.Config{
		FirecrackerBin: fcBin,
		KernelPath:     kernelPath,
		MasterRootFS:   masterRoot,
		RootFSDir:      rootfsDir,
		BootArgs:       bootArgs,
		SSHUser:        sshUser,
		SSHPassword:    sshPassword,
	}

	mgr := sandbox.NewManager(cfg)

	// Create MCP server
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "mcp-sandbox",
			Version: "v1.0.0",
		},
		&mcp.ServerOptions{
			Instructions: `MCP Sandbox Server — AI-powered Firecracker MicroVM management.

This server provides sandboxed access to Firecracker microVMs. All commands
run INSIDE the guest VMs via SSH — the host system is never directly accessible.

Typical workflow:
1. sandbox_create_vm   → allocate a new VM (id, vcpus, mem)
2. sandbox_start_vm    → boot the VM
3. sandbox_wait_ready  → wait for SSH to become available
4. sandbox_exec        → run commands inside the VM
5. sandbox_install_packages → install software
6. sandbox_upload_file / sandbox_download_file → transfer files
7. sandbox_stop_vm or sandbox_destroy_vm → cleanup

Available tools: sandbox_create_vm, sandbox_start_vm, sandbox_stop_vm,
sandbox_destroy_vm, sandbox_list_vms, sandbox_get_vm, sandbox_exec,
sandbox_install_packages, sandbox_run_script, sandbox_upload_file,
sandbox_download_file, sandbox_list_processes, sandbox_network_status,
sandbox_manage_service, sandbox_cluster_info, sandbox_wait_ready`,
		},
	)

	// Register all tools
	tools.RegisterVMTools(server, mgr)
	tools.RegisterExecTools(server, mgr)
	tools.RegisterNetTools(server, mgr)

	// Run on the selected transport
	ctx := context.Background()

	switch transport {
	case "stdio":
		log.Println("🔥 MCP Sandbox Server starting on stdio transport")
		log.Printf("   Firecracker: %s", fcBin)
		log.Printf("   Kernel:      %s", kernelPath)
		log.Printf("   Master RFS:  %s", masterRoot)
		log.Printf("   SSH User:    %s", sshUser)

		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Fatalf("Server failed: %v", err)
		}

	case "http":
		log.Printf("🔥 MCP Sandbox Server starting on HTTP transport at %s", httpAddr)
		log.Printf("   Firecracker: %s", fcBin)
		log.Printf("   Kernel:      %s", kernelPath)
		log.Printf("   Master RFS:  %s", masterRoot)
		log.Printf("   SSH User:    %s", sshUser)

		handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
			return server
		}, nil)
		http.Handle("/mcp", handler)
		if err := http.ListenAndServe(httpAddr, nil); err != nil {
			log.Fatalf("HTTP server failed: %v", err)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown transport: %s (use 'stdio' or 'http')\n", transport)
		os.Exit(1)
	}
}

// resolveBaseDir finds the project root (parent of mcp-server/).
func resolveBaseDir() string {
	// Try relative to CWD first
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "mcp-server" {
		return filepath.Dir(cwd)
	}

	// Try relative to executable
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		if filepath.Base(dir) == "mcp-server" {
			return filepath.Dir(dir)
		}
	}

	// Fallback: assume CWD is project root
	return cwd
}
