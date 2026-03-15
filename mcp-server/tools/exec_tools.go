package tools

import (
	"context"
	"fmt"

	"mcp-sandbox-server/sandbox"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterExecTools adds all in-VM execution tools to the MCP server.
// All commands run INSIDE the microVM via SSH (managed by fcmox).
func RegisterExecTools(server *mcp.Server, mgr *sandbox.Manager) {

	// ── sandbox_exec ─────────────────────────────────────────────────────
	type execArgs struct {
		VMID           string `json:"vm_id"           jsonschema:"ID of the target VM (e.g. vm-1)"`
		Command        string `json:"command"         jsonschema:"Shell command to execute inside the VM"`
		TimeoutSeconds int    `json:"timeout_seconds" jsonschema:"Max execution time in seconds (default 30)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_exec",
		Description: "Execute a shell command inside a running microVM via SSH. The command runs as the configured user inside the guest — never on the host. Returns stdout, stderr, and exit code.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args execArgs) (*mcp.CallToolResult, any, error) {
		if args.TimeoutSeconds == 0 {
			args.TimeoutSeconds = 30
		}

		result, err := mgr.Exec(args.VMID, args.Command, args.TimeoutSeconds)
		if err != nil {
			return errorResult(fmt.Sprintf("SSH execution failed: %v", err)), nil, nil
		}
		return jsonResult(fmt.Sprintf("[VM %s] command completed (exit code %d)", args.VMID, result.ExitCode), result), nil, nil
	})

	// ── sandbox_install_packages ─────────────────────────────────────────
	type installArgs struct {
		VMID     string   `json:"vm_id"    jsonschema:"ID of the target VM"`
		Packages []string `json:"packages" jsonschema:"List of package names to install via apt-get"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_install_packages",
		Description: "Install packages inside a microVM using apt-get (Debian/Ubuntu-based rootfs). Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args installArgs) (*mcp.CallToolResult, any, error) {
		result, err := mgr.InstallPackages(args.VMID, args.Packages)
		if err != nil {
			return errorResult(fmt.Sprintf("Package install failed: %v", err)), nil, nil
		}
		if result.ExitCode != 0 {
			return jsonResult(fmt.Sprintf("[VM %s] package install failed (exit %d)", args.VMID, result.ExitCode), result), nil, nil
		}
		return textResult(fmt.Sprintf("[VM %s] Packages installed successfully\n%s", args.VMID, result.Stdout)), nil, nil
	})

	// ── sandbox_run_script ───────────────────────────────────────────────
	type scriptArgs struct {
		VMID           string `json:"vm_id"           jsonschema:"ID of the target VM"`
		Script         string `json:"script"          jsonschema:"Script content to upload and execute"`
		Interpreter    string `json:"interpreter"     jsonschema:"Interpreter to use (default /bin/bash)"`
		TimeoutSeconds int    `json:"timeout_seconds" jsonschema:"Max execution time in seconds (default 60)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_run_script",
		Description: "Upload a script to the microVM and execute it. The script is written to /tmp, made executable, and run with the specified interpreter. Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args scriptArgs) (*mcp.CallToolResult, any, error) {
		if args.Interpreter == "" {
			args.Interpreter = "/bin/bash"
		}
		if args.TimeoutSeconds == 0 {
			args.TimeoutSeconds = 60
		}

		result, err := mgr.RunScript(args.VMID, args.Script, args.Interpreter, args.TimeoutSeconds)
		if err != nil {
			return errorResult(fmt.Sprintf("Script execution failed: %v", err)), nil, nil
		}
		return jsonResult(fmt.Sprintf("[VM %s] script completed (exit %d)", args.VMID, result.ExitCode), result), nil, nil
	})

	// ── sandbox_upload_file ──────────────────────────────────────────────
	type uploadArgs struct {
		VMID       string `json:"vm_id"       jsonschema:"ID of the target VM"`
		RemotePath string `json:"remote_path" jsonschema:"Absolute path inside the VM to write to"`
		Content    string `json:"content"     jsonschema:"File content (text or base64-encoded)"`
		IsBase64   bool   `json:"base64"      jsonschema:"If true the content is base64-encoded binary"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_upload_file",
		Description: "Write content to a file inside a microVM. Parent directories are created automatically. Supports both text and base64-encoded binary content. Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args uploadArgs) (*mcp.CallToolResult, any, error) {
		resp, err := mgr.UploadFile(args.VMID, args.RemotePath, args.Content, args.IsBase64)
		if err != nil {
			return errorResult(fmt.Sprintf("Upload failed: %v", err)), nil, nil
		}
		return textResult(fmt.Sprintf("[VM %s] File written: %s (%d bytes)", args.VMID, args.RemotePath, resp.Size)), nil, nil
	})

	// ── sandbox_download_file ────────────────────────────────────────────
	type downloadArgs struct {
		VMID       string `json:"vm_id"       jsonschema:"ID of the target VM"`
		RemotePath string `json:"remote_path" jsonschema:"Absolute path inside the VM to read"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_download_file",
		Description: "Read the contents of a file from inside a microVM. Returns the file content as text. Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args downloadArgs) (*mcp.CallToolResult, any, error) {
		resp, err := mgr.DownloadFile(args.VMID, args.RemotePath)
		if err != nil {
			return errorResult(fmt.Sprintf("Download failed: %v", err)), nil, nil
		}
		return textResult(fmt.Sprintf("[VM %s] %s:\n%s", args.VMID, args.RemotePath, resp.Content)), nil, nil
	})

	// ── sandbox_list_processes ────────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_list_processes",
		Description: "List all running processes inside a microVM (ps aux). Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		VMID string `json:"vm_id" jsonschema:"ID of the target VM"`
	}) (*mcp.CallToolResult, any, error) {
		result, err := mgr.ListProcesses(args.VMID)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("[VM %s] Processes:\n%s", args.VMID, result.Stdout)), nil, nil
	})

	// ── sandbox_network_status ───────────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_network_status",
		Description: "Show network interfaces and listening ports inside a microVM (ip addr + ss -tlnp). Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		VMID string `json:"vm_id" jsonschema:"ID of the target VM"`
	}) (*mcp.CallToolResult, any, error) {
		result, err := mgr.NetworkStatus(args.VMID)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("[VM %s] Network Status:\n%s", args.VMID, result.Stdout)), nil, nil
	})

	// ── sandbox_manage_service ───────────────────────────────────────────
	type serviceArgs struct {
		VMID    string `json:"vm_id"   jsonschema:"ID of the target VM"`
		Service string `json:"service" jsonschema:"Name of the systemd service"`
		Action  string `json:"action"  jsonschema:"Action: start stop restart status enable disable"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandbox_manage_service",
		Description: "Manage a systemd service inside a microVM (start/stop/restart/status/enable/disable). Runs inside the VM only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args serviceArgs) (*mcp.CallToolResult, any, error) {
		result, err := mgr.ManageService(args.VMID, args.Service, args.Action)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		output := result.Stdout
		if result.Stderr != "" {
			output += "\n" + result.Stderr
		}
		return textResult(fmt.Sprintf("[VM %s] systemctl %s %s (exit %d):\n%s", args.VMID, args.Action, args.Service, result.ExitCode, output)), nil, nil
	})
}
