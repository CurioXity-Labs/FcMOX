package rest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"fcmox/internal/ssh"
	vm "fcmox/internal/vmManager"
)

// Server holds the REST API server state.
type Server struct {
	mgr    *vm.VmManager
	ssh    ssh.Config
	addr   string
}

// NewServer creates a new REST API server.
func NewServer(mgr *vm.VmManager, sshCfg ssh.Config, addr string) *Server {
	return &Server{
		mgr:  mgr,
		ssh:  sshCfg,
		addr: addr,
	}
}

// Start begins listening. Blocks until the server errors out.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// VM lifecycle
	mux.HandleFunc("/api/v1/vms", s.handleVMs)
	mux.HandleFunc("/api/v1/vms/", s.handleVMByID)
	mux.HandleFunc("/api/v1/cluster", s.handleCluster)

	log.Printf("🌐 REST API listening on %s", s.addr)
	return http.ListenAndServe(s.addr, mux)
}

// ── JSON helpers ─────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// ── Path helpers ─────────────────────────────────────────────────────────────

// extractVMID extracts VM ID from paths like /api/v1/vms/{id} or /api/v1/vms/{id}/action
func extractVMID(path string) string {
	// strip prefix /api/v1/vms/
	rest := strings.TrimPrefix(path, "/api/v1/vms/")
	// take up to next slash
	if idx := strings.Index(rest, "/"); idx != -1 {
		return rest[:idx]
	}
	return rest
}

// extractAction extracts the sub-action from paths like /api/v1/vms/{id}/exec
func extractAction(path string) string {
	rest := strings.TrimPrefix(path, "/api/v1/vms/")
	if idx := strings.Index(rest, "/"); idx != -1 {
		return rest[idx+1:]
	}
	return ""
}

// ── VM Routes: /api/v1/vms ───────────────────────────────────────────────────

func (s *Server) handleVMs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listVMs(w, r)
	case http.MethodPost:
		s.createVM(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ── VM By ID Router: /api/v1/vms/{id}[/action] ──────────────────────────────

func (s *Server) handleVMByID(w http.ResponseWriter, r *http.Request) {
	vmID := extractVMID(r.URL.Path)
	if vmID == "" {
		writeError(w, http.StatusBadRequest, "missing vm_id in path")
		return
	}

	action := extractAction(r.URL.Path)

	switch action {
	case "": // /api/v1/vms/{id}
		switch r.Method {
		case http.MethodGet:
			s.getVM(w, r, vmID)
		case http.MethodDelete:
			s.destroyVM(w, r, vmID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case "stop":
		s.stopVM(w, r, vmID)
	case "wait":
		s.waitReady(w, r, vmID)
	case "exec":
		s.execCommand(w, r, vmID)
	case "script":
		s.runScript(w, r, vmID)
	case "packages":
		s.installPackages(w, r, vmID)
	case "files":
		switch r.Method {
		case http.MethodPut:
			s.uploadFile(w, r, vmID)
		case http.MethodGet:
			s.downloadFile(w, r, vmID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case "processes":
		s.listProcesses(w, r, vmID)
	case "network":
		s.networkStatus(w, r, vmID)
	case "service":
		s.manageService(w, r, vmID)
	default:
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown action: %s", action))
	}
}

// ── VM Lifecycle Handlers ────────────────────────────────────────────────────

type createRequest struct {
	VCPUs    int    `json:"vcpus"`
	MemMiB   int    `json:"mem_mib"`
	Kernel   string `json:"kernel"`
	Rootfs   string `json:"rootfs"`
	DiskSize int64  `json:"disk_size"`
}

func (s *Server) createVM(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	if req.VCPUs < 1 {
		req.VCPUs = 2
	}
	if req.MemMiB < 128 {
		req.MemMiB = 1024
	}
	if req.DiskSize <= 0 {
		req.DiskSize = 1 << 30 // 1GB
	}

	// Pick defaults from available kernels/rootfs if not specified
	kernelPath := req.Kernel
	if kernelPath == "" {
		for _, v := range s.mgr.Kernels {
			kernelPath = v
			break
		}
	}
	if kernelPath == "" {
		writeError(w, http.StatusBadRequest, "no kernel available and none specified")
		return
	}

	rootfsPath := req.Rootfs
	if rootfsPath == "" {
		if path, ok := s.mgr.Rootfs["debian-ebpf"]; ok {
			rootfsPath = path
		} else {
			for _, v := range s.mgr.Rootfs {
				rootfsPath = v
				break
			}
		}
	}
	if rootfsPath == "" {
		writeError(w, http.StatusBadRequest, "no rootfs available and none specified")
		return
	}

	vmObj, err := s.mgr.CreateVm(req.VCPUs, req.MemMiB, kernelPath, rootfsPath, req.DiskSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Start the VM immediately after creation
	if err := s.mgr.StartVm(vmObj.Id); err != nil {
		writeError(w, http.StatusInternalServerError, "created but failed to start: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, vmToInfo(vmObj))
}

func (s *Server) listVMs(w http.ResponseWriter, _ *http.Request) {
	vms := make([]vmInfo, 0)
	for _, v := range s.mgr.Vms {
		vms = append(vms, vmToInfo(v))
	}
	writeJSON(w, http.StatusOK, vms)
}

func (s *Server) getVM(w http.ResponseWriter, _ *http.Request, id string) {
	v, ok := s.mgr.GetVmByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("vm %s not found", id))
		return
	}
	writeJSON(w, http.StatusOK, vmToInfo(v))
}

func (s *Server) stopVM(w http.ResponseWriter, _ *http.Request, id string) {
	if err := s.mgr.StopVm(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped", "vm_id": id})
}

func (s *Server) destroyVM(w http.ResponseWriter, _ *http.Request, id string) {
	if err := s.mgr.StopVm(id); err != nil {
		// If it's already stopped, that's fine for destroy
		if !strings.Contains(err.Error(), "already stopped") && !strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "destroyed", "vm_id": id})
}

// ── Wait Ready ───────────────────────────────────────────────────────────────

type waitRequest struct {
	TimeoutSeconds int `json:"timeout_seconds"`
}

func (s *Server) waitReady(w http.ResponseWriter, r *http.Request, id string) {
	v, ok := s.mgr.GetVmByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("vm %s not found", id))
		return
	}

	var req waitRequest
	_ = decodeJSON(r, &req) // optional body
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 60
	}

	deadline := time.Now().Add(time.Duration(req.TimeoutSeconds) * time.Second)
	for time.Now().Before(deadline) {
		if v.Status == vm.VmStatusRunning {
			if err := ssh.Check(v.Ip, s.ssh.User, s.ssh.Password); err == nil {
				writeJSON(w, http.StatusOK, map[string]any{
					"ready": true,
					"vm_id": id,
					"ip":    v.Ip,
				})
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	writeError(w, http.StatusGatewayTimeout, fmt.Sprintf("vm %s did not become ready within %ds", id, req.TimeoutSeconds))
}

// ── SSH Exec Handlers ────────────────────────────────────────────────────────

type execRequest struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (s *Server) execCommand(w http.ResponseWriter, r *http.Request, id string) {
	v, ok := s.mgr.GetVmByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("vm %s not found", id))
		return
	}
	if v.Status != vm.VmStatusRunning {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("vm %s is not running (state=%s)", id, v.Status))
		return
	}

	var req execRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 30
	}

	result, err := ssh.RunCommand(v.Ip, s.ssh.User, s.ssh.Password, req.Command, req.TimeoutSeconds)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ssh exec failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type scriptRequest struct {
	Script         string `json:"script"`
	Interpreter    string `json:"interpreter"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (s *Server) runScript(w http.ResponseWriter, r *http.Request, id string) {
	v, ok := s.mgr.GetVmByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("vm %s not found", id))
		return
	}
	if v.Status != vm.VmStatusRunning {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("vm %s is not running", id))
		return
	}

	var req scriptRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Interpreter == "" {
		req.Interpreter = "/bin/bash"
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 60
	}

	// Upload script
	scriptPath := fmt.Sprintf("/tmp/mcp_script_%d", time.Now().UnixNano())
	if err := ssh.WriteFile(v.Ip, s.ssh.User, s.ssh.Password, scriptPath, []byte(req.Script)); err != nil {
		writeError(w, http.StatusInternalServerError, "upload script failed: "+err.Error())
		return
	}

	// Execute and cleanup
	cmd := fmt.Sprintf("chmod +x '%s' && '%s' '%s'; EXIT=$?; rm -f '%s'; exit $EXIT",
		scriptPath, req.Interpreter, scriptPath, scriptPath)
	result, err := ssh.RunCommand(v.Ip, s.ssh.User, s.ssh.Password, cmd, req.TimeoutSeconds)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "script exec failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type packagesRequest struct {
	Packages []string `json:"packages"`
}

func (s *Server) installPackages(w http.ResponseWriter, r *http.Request, id string) {
	v, ok := s.mgr.GetVmByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("vm %s not found", id))
		return
	}
	if v.Status != vm.VmStatusRunning {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("vm %s is not running", id))
		return
	}

	var req packagesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	pkgs := strings.Join(req.Packages, " ")
	cmd := fmt.Sprintf("apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq %s", pkgs)
	result, err := ssh.RunCommand(v.Ip, s.ssh.User, s.ssh.Password, cmd, 120)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "package install failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── File Transfer Handlers ───────────────────────────────────────────────────

type uploadRequest struct {
	RemotePath string `json:"remote_path"`
	Content    string `json:"content"`
	IsBase64   bool   `json:"base64"`
}

func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request, id string) {
	v, ok := s.mgr.GetVmByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("vm %s not found", id))
		return
	}
	if v.Status != vm.VmStatusRunning {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("vm %s is not running", id))
		return
	}

	var req uploadRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	var data []byte
	var err error
	if req.IsBase64 {
		data, err = base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid base64: "+err.Error())
			return
		}
	} else {
		data = []byte(req.Content)
	}

	if err := ssh.WriteFile(v.Ip, s.ssh.User, s.ssh.Password, req.RemotePath, data); err != nil {
		writeError(w, http.StatusInternalServerError, "upload failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "uploaded",
		"remote_path": req.RemotePath,
		"size":        len(data),
	})
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request, id string) {
	v, ok := s.mgr.GetVmByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("vm %s not found", id))
		return
	}
	if v.Status != vm.VmStatusRunning {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("vm %s is not running", id))
		return
	}

	remotePath := r.URL.Query().Get("path")
	if remotePath == "" {
		writeError(w, http.StatusBadRequest, "missing ?path= query parameter")
		return
	}

	data, err := ssh.ReadFile(v.Ip, s.ssh.User, s.ssh.Password, remotePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "download failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"remote_path": remotePath,
		"content":     string(data),
		"size":        len(data),
	})
}

// ── Process / Network / Service Handlers ─────────────────────────────────────

func (s *Server) listProcesses(w http.ResponseWriter, _ *http.Request, id string) {
	v, ok := s.mgr.GetVmByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("vm %s not found", id))
		return
	}
	if v.Status != vm.VmStatusRunning {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("vm %s is not running", id))
		return
	}

	result, err := ssh.RunCommand(v.Ip, s.ssh.User, s.ssh.Password, "ps aux", 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) networkStatus(w http.ResponseWriter, _ *http.Request, id string) {
	v, ok := s.mgr.GetVmByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("vm %s not found", id))
		return
	}
	if v.Status != vm.VmStatusRunning {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("vm %s is not running", id))
		return
	}

	cmd := "echo '=== Interfaces ===' && ip addr && echo '=== Listening Ports ===' && ss -tlnp"
	result, err := ssh.RunCommand(v.Ip, s.ssh.User, s.ssh.Password, cmd, 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type serviceRequest struct {
	Service string `json:"service"`
	Action  string `json:"action"`
}

func (s *Server) manageService(w http.ResponseWriter, r *http.Request, id string) {
	v, ok := s.mgr.GetVmByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("vm %s not found", id))
		return
	}
	if v.Status != vm.VmStatusRunning {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("vm %s is not running", id))
		return
	}

	var req serviceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	validActions := map[string]bool{"start": true, "stop": true, "restart": true, "status": true, "enable": true, "disable": true}
	if !validActions[req.Action] {
		writeError(w, http.StatusBadRequest, "action must be one of: start, stop, restart, status, enable, disable")
		return
	}

	cmd := fmt.Sprintf("systemctl %s %s", req.Action, req.Service)
	result, err := ssh.RunCommand(v.Ip, s.ssh.User, s.ssh.Password, cmd, 15)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── Cluster Info ─────────────────────────────────────────────────────────────

func (s *Server) handleCluster(w http.ResponseWriter, _ *http.Request) {
	vms := make([]vmInfo, 0)
	for _, v := range s.mgr.Vms {
		vms = append(vms, vmToInfo(v))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"bridge":  "fc-br0",
		"gateway": "172.16.0.1/24",
		"network": "172.16.0.0/24",
		"vm_count": len(vms),
		"vms":     vms,
	})
}

// ── VM Info serialization ────────────────────────────────────────────────────

type vmInfo struct {
	ID     string `json:"id"`
	VCPUs  int    `json:"vcpus"`
	MemMiB int    `json:"mem_mib"`
	State  string `json:"state"`
	MAC    string `json:"mac"`
	IP     string `json:"ip"`
	PID    int    `json:"pid,omitempty"`
}

func vmToInfo(v *vm.Vm) vmInfo {
	info := vmInfo{
		ID:     v.Id,
		VCPUs:  v.VmCpuCount,
		MemMiB: v.VmMemSize,
		State:  string(v.Status),
		MAC:    v.MacAddr,
		IP:     v.Ip,
	}
	if v.Process != nil {
		info.PID = v.PID
	}
	return info
}
