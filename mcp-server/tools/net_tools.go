package tools

import (
	"context"
	"fmt"

	"mcp-sandbox-server/sandbox"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterNetTools adds network/cluster information tools to the MCP server.
func RegisterNetTools(server *mcp.Server, mgr *sandbox.Manager) {

	// ── sandbox_cluster_info ─────────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_cluster_info",
		Description: "Show the current cluster configuration: bridge network, gateway, and all registered VMs with their TAP devices, MAC addresses, and IPs.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
		cluster, err := mgr.ClusterInfo()
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}

		info := "=== MicroVM Cluster Info ===\n"
		info += fmt.Sprintf("Bridge:  %s\n", cluster.Bridge)
		info += fmt.Sprintf("Gateway: %s\n", cluster.Gateway)
		info += fmt.Sprintf("Network: %s\n", cluster.Network)
		info += fmt.Sprintf("VMs:     %d registered\n\n", cluster.VMCount)

		if cluster.VMCount == 0 {
			info += "No VMs registered yet. Use sandbox_create_vm to create one."
		} else {
			for _, vm := range cluster.VMs {
				info += fmt.Sprintf("  %s | %-8s | IP: %-14s | MAC: %s",
					vm.ID, vm.State, vm.IP, vm.MAC)
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
		VMID           string `json:"vm_id"           jsonschema:"ID of the target VM"`
		TimeoutSeconds int    `json:"timeout_seconds" jsonschema:"Max time to wait for SSH to become available (default 60)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_wait_ready",
		Description: "Wait until a microVM is fully booted and SSH is reachable. Use this after sandbox_create_vm before running commands.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args waitArgs) (*mcp.CallToolResult, any, error) {
		if args.TimeoutSeconds == 0 {
			args.TimeoutSeconds = 60
		}

		resp, err := mgr.WaitUntilReady(args.VMID, args.TimeoutSeconds)
		if err != nil {
			return errorResult(fmt.Sprintf("VM %s not ready: %v", args.VMID, err)), nil, nil
		}

		return textResult(fmt.Sprintf("VM %s is ready! SSH accessible at %s:22", args.VMID, resp.IP)), nil, nil
	})
}
