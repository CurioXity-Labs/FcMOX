package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"mcp-sandbox-server/sandbox"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterVMTools adds all VM lifecycle tools to the MCP server.
func RegisterVMTools(server *mcp.Server, mgr *sandbox.Manager) {

	// ── sandbox_create_vm ────────────────────────────────────────────────
	type createArgs struct {
		VCPUs  int `json:"vcpus"   jsonschema:"Number of vCPUs (default 2)"`
		MemMiB int `json:"mem_mib" jsonschema:"Memory in MiB (default 1024)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_create_vm",
		Description: "Create and boot a new Firecracker microVM sandbox. Allocates a VM with dedicated rootfs, configures it, and starts it. Returns the VM info including its ID and IP address.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createArgs) (*mcp.CallToolResult, any, error) {
		if args.VCPUs < 1 {
			args.VCPUs = 2
		}
		if args.MemMiB < 128 {
			args.MemMiB = 1024
		}

		vm, err := mgr.Create(args.VCPUs, args.MemMiB)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return jsonResult("VM created and starting", vm), nil, nil
	})

	// ── sandbox_stop_vm ──────────────────────────────────────────────────
	type vmIDArgs struct {
		VMID string `json:"vm_id" jsonschema:"ID of the target VM (e.g. vm-1)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_stop_vm",
		Description: "Gracefully stop a running microVM. Kills the Firecracker process and cleans up resources.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args vmIDArgs) (*mcp.CallToolResult, any, error) {
		if err := mgr.Stop(args.VMID); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("VM %s stopped.", args.VMID)), nil, nil
	})

	// ── sandbox_destroy_vm ───────────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_destroy_vm",
		Description: "Stop a VM (if running) and permanently delete it along with its root filesystem. This action is irreversible.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args vmIDArgs) (*mcp.CallToolResult, any, error) {
		if err := mgr.Destroy(args.VMID); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("VM %s destroyed.", args.VMID)), nil, nil
	})

	// ── sandbox_list_vms ─────────────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_list_vms",
		Description: "List all registered microVMs with their current state, IP address, resource allocation, and PID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
		vms, err := mgr.List()
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
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
		return jsonResult("", vm), nil, nil
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

// parseVMID attempts to parse a VM ID as an int (for back-compat with old tools
// that used numeric IDs); falls back to string.
func parseVMID(raw string) string {
	if _, err := strconv.Atoi(raw); err == nil {
		return "vm-" + raw
	}
	return raw
}
