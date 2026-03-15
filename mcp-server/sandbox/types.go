package sandbox

// VMInfo is the JSON-serializable view of a VM returned by the fcmox REST API.
type VMInfo struct {
	ID     string `json:"id"`
	VCPUs  int    `json:"vcpus"`
	MemMiB int    `json:"mem_mib"`
	State  string `json:"state"`
	MAC    string `json:"mac"`
	IP     string `json:"ip"`
	PID    int    `json:"pid,omitempty"`
}

// CommandResult holds the output of a command run inside a VM.
type CommandResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}
