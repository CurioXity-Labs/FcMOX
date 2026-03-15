package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"mcp-sandbox/sandbox"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterVMTools adds all VM lifecycle tools to the MCP server.
func RegisterVMTools(server *mcp.Server, mgr *sandbox.Manager) {

	// ── sandbox_create_vm ────────────────────────────────────────────────
	type createArgs struct {
		ID     int `json:"id"      jsonschema:"Unique VM ID (1-254)"`
		VCPUs  int `json:"vcpus"   jsonschema:"Number of vCPUs (default 2)"`
		MemMiB int `json:"mem_mib" jsonschema:"Memory in MiB (default 1024)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_create_vm",
		Description: "Create a new Firecracker microVM sandbox. This allocates an ID, clones the root filesystem, and prepares the VM for booting. The VM is not started yet — call sandbox_start_vm next.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createArgs) (*mcp.CallToolResult, any, error) {
		if args.ID < 1 || args.ID > 254 {
			return errorResult("id must be between 1 and 254"), nil, nil
		}
		if args.VCPUs < 1 {
			args.VCPUs = 2
		}
		if args.MemMiB < 128 {
			args.MemMiB = 1024
		}

		vm, err := mgr.Create(args.ID, args.VCPUs, args.MemMiB)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return jsonResult("VM created successfully", vm.Info()), nil, nil
	})

	// ── sandbox_start_vm ─────────────────────────────────────────────────
	type vmIDArgs struct {
		VMID int `json:"vm_id" jsonschema:"ID of the target VM"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_start_vm",
		Description: "Boot a created microVM. This launches the Firecracker process, configures resources via its API, and starts the guest kernel. The VM will be assigned an IP on the 172.16.0.0/24 network. SSH becomes available once the guest finishes booting.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args vmIDArgs) (*mcp.CallToolResult, any, error) {
		if err := mgr.Start(args.VMID); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		vm, _ := mgr.Get(args.VMID)
		info := vm.Info()
		return jsonResult(fmt.Sprintf("VM %d is starting. IP will be %s. SSH will be available once boot completes.", args.VMID, info.IP), info), nil, nil
	})

	// ── sandbox_stop_vm ──────────────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_stop_vm",
		Description: "Gracefully stop a running microVM. Sends Ctrl+Alt+Del and waits 3 seconds for clean shutdown before force-killing.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args vmIDArgs) (*mcp.CallToolResult, any, error) {
		if err := mgr.Stop(args.VMID); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("VM %d is stopping.", args.VMID)), nil, nil
	})

	// ── sandbox_destroy_vm ───────────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_destroy_vm",
		Description: "Stop a VM (if running) and permanently delete it along with its root filesystem. This action is irreversible.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args vmIDArgs) (*mcp.CallToolResult, any, error) {
		if err := mgr.Destroy(args.VMID); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("VM %d destroyed.", args.VMID)), nil, nil
	})

	// ── sandbox_list_vms ─────────────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_list_vms",
		Description: "List all registered microVMs with their current state, IP address, resource allocation, and PID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
		vms := mgr.List()
		if len(vms) == 0 {
			return textResult("No VMs registered. Use sandbox_create_vm to create one."), nil, nil
		}
		return jsonResult(fmt.Sprintf("Found %d VM(s)", len(vms)), vms), nil, nil
	})

	// ── sandbox_get_vm ───────────────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_get_vm",
		Description: "Get detailed information for a specific microVM including state, IP, resources, and process ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args vmIDArgs) (*mcp.CallToolResult, any, error) {
		vm, err := mgr.Get(args.VMID)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return jsonResult("", vm.Info()), nil, nil
	})
}

// --- helpers ---

func textResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "Error: " + msg},
		},
		IsError: true,
	}
}

func jsonResult(prefix string, data interface{}) *mcp.CallToolResult {
	b, _ := json.MarshalIndent(data, "", "  ")
	text := string(b)
	if prefix != "" {
		text = prefix + "\n" + text
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}
