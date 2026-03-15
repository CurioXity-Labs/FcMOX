package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"mcp-sandbox-server/sandbox"
	"mcp-sandbox-server/tools"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// Force all log output to stderr so it doesn't corrupt MCP's stdout JSON
	log.SetOutput(os.Stderr)

	var (
		transport string
		httpAddr  string
		fcmoxURL  string
	)

	flag.StringVar(&transport, "transport", "stdio", "Transport mode: 'stdio' or 'http'")
	flag.StringVar(&httpAddr, "http-addr", ":8081", "HTTP listen address (only used with --transport=http)")
	flag.StringVar(&fcmoxURL, "fcmox-url", "http://localhost:8090", "URL of the fcmox REST API")
	flag.Parse()

	cfg := sandbox.Config{
		BaseURL: fcmoxURL,
	}

	mgr := sandbox.NewManager(cfg)

	// Create MCP server
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "mcp-sandbox",
			Version: "v2.0.0",
		},
		&mcp.ServerOptions{
			Instructions: `MCP Sandbox Server — AI-powered Firecracker MicroVM management.

This server provides sandboxed access to Firecracker microVMs managed by fcmox.
All commands run INSIDE the guest VMs via SSH — the host system is never directly accessible.

Typical workflow:
1. sandbox_create_vm   → allocate and boot a new VM
2. sandbox_wait_ready  → wait for SSH to become available
3. sandbox_exec        → run commands inside the VM
4. sandbox_install_packages → install software
5. sandbox_upload_file / sandbox_download_file → transfer files
6. sandbox_stop_vm or sandbox_destroy_vm → cleanup

Available tools: sandbox_create_vm, sandbox_stop_vm,
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
		log.Printf("   fcmox API: %s", fcmoxURL)

		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Fatalf("Server failed: %v", err)
		}

	case "http":
		log.Printf("🔥 MCP Sandbox Server starting on HTTP transport at %s", httpAddr)
		log.Printf("   fcmox API: %s", fcmoxURL)

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
