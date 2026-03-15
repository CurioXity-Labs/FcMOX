package tools

import (
	"context"
	"fmt"
	"time"

	"mcp-sandbox/sandbox"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterNetTools adds network/cluster information tools to the MCP server.
func RegisterNetTools(server *mcp.Server, mgr *sandbox.Manager) {

	// ── sandbox_cluster_info ─────────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_cluster_info",
		Description: "Show the current cluster configuration: bridge network, gateway, and all registered VMs with their TAP devices, MAC addresses, and IPs.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
		vms := mgr.List()

		info := "=== MicroVM Cluster Info ===\n"
		info += "Bridge:  fc-br0\n"
		info += "Gateway: 172.16.0.1/24\n"
		info += "Network: 172.16.0.0/24\n"
		info += fmt.Sprintf("VMs:     %d registered\n\n", len(vms))

		if len(vms) == 0 {
			info += "No VMs registered yet. Use sandbox_create_vm to create one."
		} else {
			for _, vm := range vms {
				info += fmt.Sprintf("  VM %d | %-8s | IP: %-14s | MAC: %s | TAP: tap%d",
					vm.ID, vm.State, vm.IP, vm.MAC, vm.ID)
				if vm.PID > 0 {
					info += fmt.Sprintf(" | PID: %d", vm.PID)
				}
				info += "\n"
			}
		}

		return textResult(info), nil, nil
	})

	// ── sandbox_wait_ready ───────────────────────────────────────────────
	type waitArgs struct {
		VMID           int `json:"vm_id"           jsonschema:"ID of the target VM"`
		TimeoutSeconds int `json:"timeout_seconds" jsonschema:"Max time to wait for SSH to become available (default 60)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_wait_ready",
		Description: "Wait until a microVM is fully booted and SSH is reachable. Use this after sandbox_start_vm before running commands.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args waitArgs) (*mcp.CallToolResult, any, error) {
		if args.TimeoutSeconds == 0 {
			args.TimeoutSeconds = 60
		}

		timeout := time.Duration(args.TimeoutSeconds) * time.Second
		err := mgr.WaitUntilReady(args.VMID, timeout)
		if err != nil {
			return errorResult(fmt.Sprintf("VM %d not ready: %v", args.VMID, err)), nil, nil
		}

		vm, _ := mgr.Get(args.VMID)
		info := vm.Info()
		return textResult(fmt.Sprintf("VM %d is ready! SSH accessible at %s:22", args.VMID, info.IP)), nil, nil
	})
}
