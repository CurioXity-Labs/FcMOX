package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// FCClient wraps HTTP-over-UDS calls to a single Firecracker instance.
type FCClient struct {
	socketPath string
	client     *http.Client
}

func NewFCClient(socketPath string) *FCClient {
	return &FCClient{
		socketPath: socketPath,
		client: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
			Timeout: 5 * time.Second,
		},
	}
}

// WaitForSocket blocks until the API socket appears or context is cancelled.
func (fc *FCClient) WaitForSocket(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if _, err := os.Stat(fc.socketPath); err == nil {
			conn, err := net.DialTimeout("unix", fc.socketPath, 200*time.Millisecond)
			if err == nil {
				conn.Close()
				return nil
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (fc *FCClient) put(path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, "http://localhost"+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := fc.client.Do(req)
	if err != nil {
		return fmt.Errorf("do %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT %s returned %d: %s", path, resp.StatusCode, string(errBody))
	}
	return nil
}

// --- Firecracker Configuration API structs ---

type machineConfig struct {
	VCPUCount  int `json:"vcpu_count"`
	MemSizeMiB int `json:"mem_size_mib"`
}

type driveConfig struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

type bootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args"`
}

type networkInterface struct {
	IfaceID     string `json:"iface_id"`
	GuestMAC    string `json:"guest_mac"`
	HostDevName string `json:"host_dev_name"`
}

type action struct {
	ActionType string `json:"action_type"`
}

// --- Public API ---

func (fc *FCClient) SetMachineConfig(vcpus, memMiB int) error {
	return fc.put("/machine-config", machineConfig{
		VCPUCount:  vcpus,
		MemSizeMiB: memMiB,
	})
}

func (fc *FCClient) SetRootDrive(path string) error {
	return fc.put("/drives/rootfs", driveConfig{
		DriveID:      "rootfs",
		PathOnHost:   path,
		IsRootDevice: true,
		IsReadOnly:   false,
	})
}

func (fc *FCClient) SetBootSource(kernelPath, bootArgs string) error {
	return fc.put("/boot-source", bootSource{
		KernelImagePath: kernelPath,
		BootArgs:        bootArgs,
	})
}

func (fc *FCClient) SetNetworkInterface(mac, tapDev string) error {
	return fc.put("/network-interfaces/eth0", networkInterface{
		IfaceID:     "eth0",
		GuestMAC:    mac,
		HostDevName: tapDev,
	})
}

func (fc *FCClient) StartInstance() error {
	return fc.put("/actions", action{ActionType: "InstanceStart"})
}

func (fc *FCClient) SendCtrlAltDel() error {
	return fc.put("/actions", action{ActionType: "SendCtrlAltDel"})
}
