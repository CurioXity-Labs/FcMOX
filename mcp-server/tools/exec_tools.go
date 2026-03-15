package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"mcp-sandbox/sandbox"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterExecTools adds all in-VM execution tools to the MCP server.
// IMPORTANT: Every tool here runs INSIDE the microVM via SSH.
// Nothing ever executes on the host system.
func RegisterExecTools(server *mcp.Server, mgr *sandbox.Manager) {

	// ── sandbox_exec ─────────────────────────────────────────────────────
	type execArgs struct {
		VMID           int    `json:"vm_id"           jsonschema:"ID of the target VM"`
		Command        string `json:"command"         jsonschema:"Shell command to execute inside the VM"`
		TimeoutSeconds int    `json:"timeout_seconds" jsonschema:"Max execution time in seconds (default 30)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_exec",
		Description: "Execute a shell command inside a running microVM via SSH. The command runs as the configured user inside the guest — never on the host. Returns stdout, stderr, and exit code.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args execArgs) (*mcp.CallToolResult, any, error) {
		vm, err := mgr.Get(args.VMID)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		info := vm.Info()
		if info.State != "running" {
			return errorResult(fmt.Sprintf("VM %d is not running (state=%s). Start it first.", args.VMID, info.State)), nil, nil
		}
		if args.TimeoutSeconds == 0 {
			args.TimeoutSeconds = 30
		}

		result, err := sandbox.RunCommand(info.IP, mgr.Config.SSHUser, mgr.Config.SSHPassword, args.Command, args.TimeoutSeconds)
		if err != nil {
			return errorResult(fmt.Sprintf("SSH execution failed: %v", err)), nil, nil
		}
		return jsonResult(fmt.Sprintf("[VM %d] command completed (exit code %d)", args.VMID, result.ExitCode), result), nil, nil
	})

	// ── sandbox_install_packages ─────────────────────────────────────────
	type installArgs struct {
		VMID     int      `json:"vm_id"    jsonschema:"ID of the target VM"`
		Packages []string `json:"packages" jsonschema:"List of package names to install via apt-get"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_install_packages",
		Description: "Install packages inside a microVM using apt-get (Debian/Ubuntu-based rootfs). Runs 'apt-get update' followed by 'apt-get install -y'. This runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args installArgs) (*mcp.CallToolResult, any, error) {
		vm, err := mgr.Get(args.VMID)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		info := vm.Info()
		if info.State != "running" {
			return errorResult(fmt.Sprintf("VM %d is not running.", args.VMID)), nil, nil
		}

		pkgs := strings.Join(args.Packages, " ")
		cmd := fmt.Sprintf("apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq %s", pkgs)

		result, err := sandbox.RunCommand(info.IP, mgr.Config.SSHUser, mgr.Config.SSHPassword, cmd, 120)
		if err != nil {
			return errorResult(fmt.Sprintf("Package install failed: %v", err)), nil, nil
		}
		if result.ExitCode != 0 {
			return jsonResult(fmt.Sprintf("[VM %d] package install failed (exit %d)", args.VMID, result.ExitCode), result), nil, nil
		}
		return textResult(fmt.Sprintf("[VM %d] Successfully installed: %s\n%s", args.VMID, pkgs, result.Stdout)), nil, nil
	})

	// ── sandbox_run_script ───────────────────────────────────────────────
	type scriptArgs struct {
		VMID           int    `json:"vm_id"           jsonschema:"ID of the target VM"`
		Script         string `json:"script"          jsonschema:"Script content to upload and execute"`
		Interpreter    string `json:"interpreter"     jsonschema:"Interpreter to use (default /bin/bash)"`
		TimeoutSeconds int    `json:"timeout_seconds" jsonschema:"Max execution time in seconds (default 60)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_run_script",
		Description: "Upload a script to the microVM and execute it. The script is written to /tmp/mcp_script, made executable, and run with the specified interpreter. Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args scriptArgs) (*mcp.CallToolResult, any, error) {
		vm, err := mgr.Get(args.VMID)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		info := vm.Info()
		if info.State != "running" {
			return errorResult(fmt.Sprintf("VM %d is not running.", args.VMID)), nil, nil
		}
		if args.Interpreter == "" {
			args.Interpreter = "/bin/bash"
		}
		if args.TimeoutSeconds == 0 {
			args.TimeoutSeconds = 60
		}

		// Upload the script
		scriptPath := fmt.Sprintf("/tmp/mcp_script_%d", time.Now().UnixNano())
		if err := sandbox.WriteFile(info.IP, mgr.Config.SSHUser, mgr.Config.SSHPassword, scriptPath, []byte(args.Script)); err != nil {
			return errorResult(fmt.Sprintf("Failed to upload script: %v", err)), nil, nil
		}

		// Make executable and run
		cmd := fmt.Sprintf("chmod +x '%s' && '%s' '%s'; EXIT=$?; rm -f '%s'; exit $EXIT", scriptPath, args.Interpreter, scriptPath, scriptPath)
		result, err := sandbox.RunCommand(info.IP, mgr.Config.SSHUser, mgr.Config.SSHPassword, cmd, args.TimeoutSeconds)
		if err != nil {
			return errorResult(fmt.Sprintf("Script execution failed: %v", err)), nil, nil
		}
		return jsonResult(fmt.Sprintf("[VM %d] script completed (exit %d)", args.VMID, result.ExitCode), result), nil, nil
	})

	// ── sandbox_upload_file ──────────────────────────────────────────────
	type uploadArgs struct {
		VMID       int    `json:"vm_id"       jsonschema:"ID of the target VM"`
		RemotePath string `json:"remote_path" jsonschema:"Absolute path inside the VM to write to"`
		Content    string `json:"content"     jsonschema:"File content (text or base64-encoded)"`
		IsBase64   bool   `json:"base64"      jsonschema:"If true the content is base64-encoded binary"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_upload_file",
		Description: "Write content to a file inside a microVM. Parent directories are created automatically. Supports both text and base64-encoded binary content. Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args uploadArgs) (*mcp.CallToolResult, any, error) {
		vm, err := mgr.Get(args.VMID)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		info := vm.Info()
		if info.State != "running" {
			return errorResult(fmt.Sprintf("VM %d is not running.", args.VMID)), nil, nil
		}

		var data []byte
		if args.IsBase64 {
			data, err = base64.StdEncoding.DecodeString(args.Content)
			if err != nil {
				return errorResult(fmt.Sprintf("Invalid base64: %v", err)), nil, nil
			}
		} else {
			data = []byte(args.Content)
		}

		if err := sandbox.WriteFile(info.IP, mgr.Config.SSHUser, mgr.Config.SSHPassword, args.RemotePath, data); err != nil {
			return errorResult(fmt.Sprintf("Upload failed: %v", err)), nil, nil
		}
		return textResult(fmt.Sprintf("[VM %d] File written: %s (%d bytes)", args.VMID, args.RemotePath, len(data))), nil, nil
	})

	// ── sandbox_download_file ────────────────────────────────────────────
	type downloadArgs struct {
		VMID       int    `json:"vm_id"       jsonschema:"ID of the target VM"`
		RemotePath string `json:"remote_path" jsonschema:"Absolute path inside the VM to read"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_download_file",
		Description: "Read the contents of a file from inside a microVM. Returns the file content as text. Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args downloadArgs) (*mcp.CallToolResult, any, error) {
		vm, err := mgr.Get(args.VMID)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		info := vm.Info()
		if info.State != "running" {
			return errorResult(fmt.Sprintf("VM %d is not running.", args.VMID)), nil, nil
		}

		data, err := sandbox.ReadFile(info.IP, mgr.Config.SSHUser, mgr.Config.SSHPassword, args.RemotePath)
		if err != nil {
			return errorResult(fmt.Sprintf("Download failed: %v", err)), nil, nil
		}
		return textResult(fmt.Sprintf("[VM %d] %s:\n%s", args.VMID, args.RemotePath, string(data))), nil, nil
	})

	// ── sandbox_list_processes ────────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_list_processes",
		Description: "List all running processes inside a microVM (ps aux). Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		VMID int `json:"vm_id" jsonschema:"ID of the target VM"`
	}) (*mcp.CallToolResult, any, error) {
		vm, err := mgr.Get(args.VMID)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		info := vm.Info()
		if info.State != "running" {
			return errorResult(fmt.Sprintf("VM %d is not running.", args.VMID)), nil, nil
		}

		result, err := sandbox.RunCommand(info.IP, mgr.Config.SSHUser, mgr.Config.SSHPassword, "ps aux", 10)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("[VM %d] Processes:\n%s", args.VMID, result.Stdout)), nil, nil
	})

	// ── sandbox_network_status ───────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_network_status",
		Description: "Show network interfaces and listening ports inside a microVM (ip addr + ss -tlnp). Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		VMID int `json:"vm_id" jsonschema:"ID of the target VM"`
	}) (*mcp.CallToolResult, any, error) {
		vm, err := mgr.Get(args.VMID)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		info := vm.Info()
		if info.State != "running" {
			return errorResult(fmt.Sprintf("VM %d is not running.", args.VMID)), nil, nil
		}

		result, err := sandbox.RunCommand(info.IP, mgr.Config.SSHUser, mgr.Config.SSHPassword, "echo '=== Interfaces ===' && ip addr && echo '=== Listening Ports ===' && ss -tlnp", 10)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("[VM %d] Network Status:\n%s", args.VMID, result.Stdout)), nil, nil
	})

	// ── sandbox_manage_service ───────────────────────────────────────────
	type serviceArgs struct {
		VMID    int    `json:"vm_id"   jsonschema:"ID of the target VM"`
		Service string `json:"service" jsonschema:"Name of the systemd service"`
		Action  string `json:"action"  jsonschema:"Action: start stop restart status enable disable"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_manage_service",
		Description: "Manage a systemd service inside a microVM (start/stop/restart/status/enable/disable). Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args serviceArgs) (*mcp.CallToolResult, any, error) {
		validActions := map[string]bool{"start": true, "stop": true, "restart": true, "status": true, "enable": true, "disable": true}
		if !validActions[args.Action] {
			return errorResult("action must be one of: start, stop, restart, status, enable, disable"), nil, nil
		}

		vm, err := mgr.Get(args.VMID)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		info := vm.Info()
		if info.State != "running" {
			return errorResult(fmt.Sprintf("VM %d is not running.", args.VMID)), nil, nil
		}

		cmd := fmt.Sprintf("systemctl %s %s", args.Action, args.Service)
		result, err := sandbox.RunCommand(info.IP, mgr.Config.SSHUser, mgr.Config.SSHPassword, cmd, 15)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}

		output := result.Stdout
		if result.Stderr != "" {
			output += "\n" + result.Stderr
		}
		return textResult(fmt.Sprintf("[VM %d] systemctl %s %s (exit %d):\n%s", args.VMID, args.Action, args.Service, result.ExitCode, output)), nil, nil
	})
}
