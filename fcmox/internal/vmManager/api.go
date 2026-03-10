package vmmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// waitForSocket polls until the Unix socket at path is connectable or the
// deadline elapses. Called by StartVm before driving the API sequence.
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", path)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s waiting for socket %s", timeout, path)
}

// fcClient returns an *http.Client that routes all traffic through the
// firecracker Unix domain socket at sockPath.
func fcClient(sockPath string) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
			},
		},
	}
}

// fcPut sends a PUT request to the firecracker API.
// baseURL should be "http://localhost" (the host part is ignored by the socket transport).
func fcPut(client *http.Client, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %s body: %w", path, err)
	}

	req, err := http.NewRequest(http.MethodPut, "http://localhost"+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return fmt.Errorf("PUT %s returned %d: %s", path, resp.StatusCode, buf.String())
	}
	return nil
}

func fcPatch(client *http.Client, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %s body: %w", path, err)
	}

	req, err := http.NewRequest(http.MethodPatch, "http://localhost"+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("PATCH %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return fmt.Errorf("PATCH %s returned %d: %s", path, resp.StatusCode, buf.String())
	}
	return nil
}

// ── Firecracker API request bodies ───────────────────────────────────────────

type fcMachineConfig struct {
	VcpuCount  int  `json:"vcpu_count"`
	MemSizeMib int  `json:"mem_size_mib"`
	Smt        bool `json:"smt"`
}

type fcBootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args"`
}

type fcDrive struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

type fcNetworkInterface struct {
	IfaceID     string `json:"iface_id"`
	GuestMac    string `json:"guest_mac"`
	HostDevName string `json:"host_dev_name"`
}

type fcState struct {
	State string `json:"state"`
}

type fcAction struct {
	ActionType string `json:"action_type"`
}

// configureAndBootVm sends all necessary PUT requests to fully configure the
// firecracker VMM and then issues an InstanceStart action.
// The socket must already be listening (firecracker process running).
func configureAndBootVm(vm *Vm) error {
	client := fcClient(vm.SockPath)

	// 1. Machine configuration (vCPUs + RAM)
	if err := fcPut(client, "/machine-config", fcMachineConfig{
		VcpuCount:  vm.VmCpuCount,
		MemSizeMib: vm.VmMemSize,
		Smt:        false,
	}); err != nil {
		return fmt.Errorf("machine-config: %w", err)
	}

	// 2. Boot source (kernel + boot args)
	if err := fcPut(client, "/boot-source", fcBootSource{
		KernelImagePath: vm.KernelPath,
		BootArgs:        vm.BootArgs,
	}); err != nil {
		return fmt.Errorf("boot-source: %w", err)
	}

	// 3. Root drive (rootfs image)
	if err := fcPut(client, "/drives/rootfs", fcDrive{
		DriveID:      "rootfs",
		PathOnHost:   vm.RootfsPath,
		IsRootDevice: true,
		IsReadOnly:   false,
	}); err != nil {
		return fmt.Errorf("drives/rootfs: %w", err)
	}

	// 4. Network interface (TAP device)
	if err := fcPut(client, "/network-interfaces/eth0", fcNetworkInterface{
		IfaceID:     "eth0",
		GuestMac:    vm.MacAddr,
		HostDevName: vm.TapDev,
	}); err != nil {
		return fmt.Errorf("network-interfaces/eth0: %w", err)
	}

	// 5. Boot the VM
	if err := fcPut(client, "/actions", fcAction{ActionType: "InstanceStart"}); err != nil {
		return fmt.Errorf("InstanceStart: %w", err)
	}

	return nil
}

func pauseVm(vm *Vm) error {
	client := fcClient(vm.SockPath)
	if err := fcPatch(client, "/vm", fcState{State: "Paused"}); err != nil {
		return fmt.Errorf("InstancePause: %w", err)
	}
	return nil
}

func resumeVm(vm *Vm) error {
	client := fcClient(vm.SockPath)
	if err := fcPatch(client, "/vm", fcState{State: "Resumed"}); err != nil {
		return fmt.Errorf("Resume: %w", err)
	}
	return nil
}
