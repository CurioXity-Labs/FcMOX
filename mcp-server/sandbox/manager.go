package sandbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Manager is a thin HTTP client that delegates all VM operations to fcmox's REST API.
type Manager struct {
	baseURL string
	client  *http.Client
}

// Config holds the fcmox API connection settings.
type Config struct {
	BaseURL string
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		baseURL: cfg.BaseURL,
		client: &http.Client{
			Timeout: 120 * time.Second, // generous for long ops like package install
		},
	}
}

// ── HTTP helpers ─────────────────────────────────────────────────────────────

func (m *Manager) doJSON(method, path string, body any) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, m.baseURL+path, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

func (m *Manager) get(path string) ([]byte, error) {
	data, status, err := m.doJSON(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, parseAPIError(data, status)
	}
	return data, nil
}

func (m *Manager) post(path string, body any) ([]byte, error) {
	data, status, err := m.doJSON(http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, parseAPIError(data, status)
	}
	return data, nil
}

func (m *Manager) put(path string, body any) ([]byte, error) {
	data, status, err := m.doJSON(http.MethodPut, path, body)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, parseAPIError(data, status)
	}
	return data, nil
}

func (m *Manager) delete(path string) ([]byte, error) {
	data, status, err := m.doJSON(http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, parseAPIError(data, status)
	}
	return data, nil
}

func parseAPIError(data []byte, status int) error {
	var errResp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &errResp) == nil && errResp.Error != "" {
		return fmt.Errorf("api error (%d): %s", status, errResp.Error)
	}
	return fmt.Errorf("api error (%d): %s", status, string(data))
}

// ── VM Lifecycle ─────────────────────────────────────────────────────────────

type CreateRequest struct {
	VCPUs    int   `json:"vcpus"`
	MemMiB   int   `json:"mem_mib"`
	DiskSize int64 `json:"disk_size,omitempty"`
}

func (m *Manager) Create(vcpus, memMiB int) (*VMInfo, error) {
	data, err := m.post("/api/v1/vms", CreateRequest{
		VCPUs:  vcpus,
		MemMiB: memMiB,
	})
	if err != nil {
		return nil, err
	}
	var info VMInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("decode vm info: %w", err)
	}
	return &info, nil
}

func (m *Manager) Get(id string) (*VMInfo, error) {
	data, err := m.get(fmt.Sprintf("/api/v1/vms/%s", id))
	if err != nil {
		return nil, err
	}
	var info VMInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("decode vm info: %w", err)
	}
	return &info, nil
}

func (m *Manager) List() ([]VMInfo, error) {
	data, err := m.get("/api/v1/vms")
	if err != nil {
		return nil, err
	}
	var vms []VMInfo
	if err := json.Unmarshal(data, &vms); err != nil {
		return nil, fmt.Errorf("decode vm list: %w", err)
	}
	return vms, nil
}

func (m *Manager) Stop(id string) error {
	_, err := m.post(fmt.Sprintf("/api/v1/vms/%s/stop", id), nil)
	return err
}

func (m *Manager) Destroy(id string) error {
	_, err := m.delete(fmt.Sprintf("/api/v1/vms/%s", id))
	return err
}

// ── Wait Ready ───────────────────────────────────────────────────────────────

type WaitRequest struct {
	TimeoutSeconds int `json:"timeout_seconds"`
}

type WaitResponse struct {
	Ready bool   `json:"ready"`
	VMID  string `json:"vm_id"`
	IP    string `json:"ip"`
}

func (m *Manager) WaitUntilReady(id string, timeoutSec int) (*WaitResponse, error) {
	data, err := m.post(fmt.Sprintf("/api/v1/vms/%s/wait", id), WaitRequest{
		TimeoutSeconds: timeoutSec,
	})
	if err != nil {
		return nil, err
	}
	var resp WaitResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode wait response: %w", err)
	}
	return &resp, nil
}

// ── SSH Operations ───────────────────────────────────────────────────────────

type ExecRequest struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (m *Manager) Exec(id, command string, timeoutSec int) (*CommandResult, error) {
	data, err := m.post(fmt.Sprintf("/api/v1/vms/%s/exec", id), ExecRequest{
		Command:        command,
		TimeoutSeconds: timeoutSec,
	})
	if err != nil {
		return nil, err
	}
	var result CommandResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode exec result: %w", err)
	}
	return &result, nil
}

type ScriptRequest struct {
	Script         string `json:"script"`
	Interpreter    string `json:"interpreter"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (m *Manager) RunScript(id string, script, interpreter string, timeoutSec int) (*CommandResult, error) {
	data, err := m.post(fmt.Sprintf("/api/v1/vms/%s/script", id), ScriptRequest{
		Script:         script,
		Interpreter:    interpreter,
		TimeoutSeconds: timeoutSec,
	})
	if err != nil {
		return nil, err
	}
	var result CommandResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode script result: %w", err)
	}
	return &result, nil
}

type PackagesRequest struct {
	Packages []string `json:"packages"`
}

func (m *Manager) InstallPackages(id string, packages []string) (*CommandResult, error) {
	data, err := m.post(fmt.Sprintf("/api/v1/vms/%s/packages", id), PackagesRequest{
		Packages: packages,
	})
	if err != nil {
		return nil, err
	}
	var result CommandResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode packages result: %w", err)
	}
	return &result, nil
}

// ── File Operations ──────────────────────────────────────────────────────────

type UploadRequest struct {
	RemotePath string `json:"remote_path"`
	Content    string `json:"content"`
	IsBase64   bool   `json:"base64"`
}

type UploadResponse struct {
	Status     string `json:"status"`
	RemotePath string `json:"remote_path"`
	Size       int    `json:"size"`
}

func (m *Manager) UploadFile(id, remotePath, content string, isBase64 bool) (*UploadResponse, error) {
	data, err := m.put(fmt.Sprintf("/api/v1/vms/%s/files", id), UploadRequest{
		RemotePath: remotePath,
		Content:    content,
		IsBase64:   isBase64,
	})
	if err != nil {
		return nil, err
	}
	var resp UploadResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode upload response: %w", err)
	}
	return &resp, nil
}

type DownloadResponse struct {
	RemotePath string `json:"remote_path"`
	Content    string `json:"content"`
	Size       int    `json:"size"`
}

func (m *Manager) DownloadFile(id, remotePath string) (*DownloadResponse, error) {
	data, err := m.get(fmt.Sprintf("/api/v1/vms/%s/files?path=%s", id, remotePath))
	if err != nil {
		return nil, err
	}
	var resp DownloadResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode download response: %w", err)
	}
	return &resp, nil
}

// ── Process / Network / Service ──────────────────────────────────────────────

func (m *Manager) ListProcesses(id string) (*CommandResult, error) {
	data, err := m.get(fmt.Sprintf("/api/v1/vms/%s/processes", id))
	if err != nil {
		return nil, err
	}
	var result CommandResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode processes: %w", err)
	}
	return &result, nil
}

func (m *Manager) NetworkStatus(id string) (*CommandResult, error) {
	data, err := m.get(fmt.Sprintf("/api/v1/vms/%s/network", id))
	if err != nil {
		return nil, err
	}
	var result CommandResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode network: %w", err)
	}
	return &result, nil
}

type ServiceRequest struct {
	Service string `json:"service"`
	Action  string `json:"action"`
}

func (m *Manager) ManageService(id, service, action string) (*CommandResult, error) {
	data, err := m.post(fmt.Sprintf("/api/v1/vms/%s/service", id), ServiceRequest{
		Service: service,
		Action:  action,
	})
	if err != nil {
		return nil, err
	}
	var result CommandResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode service result: %w", err)
	}
	return &result, nil
}

// ── Cluster Info ─────────────────────────────────────────────────────────────

type ClusterInfo struct {
	Bridge  string   `json:"bridge"`
	Gateway string   `json:"gateway"`
	Network string   `json:"network"`
	VMCount int      `json:"vm_count"`
	VMs     []VMInfo `json:"vms"`
}

func (m *Manager) ClusterInfo() (*ClusterInfo, error) {
	data, err := m.get("/api/v1/cluster")
	if err != nil {
		return nil, err
	}
	var info ClusterInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("decode cluster info: %w", err)
	}
	return &info, nil
}
