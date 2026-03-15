package sandbox

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshClient creates an SSH client to a VM.
func sshClient(ip, user, password string, timeout time.Duration) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	addr := fmt.Sprintf("%s:22", ip)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return client, nil
}

// SSHCheck tests whether SSH is reachable on the given IP.
func SSHCheck(ip, user, password string) error {
	client, err := sshClient(ip, user, password, 2*time.Second)
	if err != nil {
		return err
	}
	client.Close()
	return nil
}

// CommandResult holds the output of a command run inside a VM.
type CommandResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// RunCommand executes a shell command inside a VM via SSH.
// All commands are scoped to the VM — nothing runs on the host.
func RunCommand(ip, user, password, command string, timeoutSec int) (*CommandResult, error) {
	timeout := time.Duration(timeoutSec) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	client, err := sshClient(ip, user, password, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Run with timeout using a channel
	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case err := <-done:
		result := &CommandResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: 0,
		}
		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				result.ExitCode = exitErr.ExitStatus()
			} else {
				return nil, fmt.Errorf("ssh run: %w", err)
			}
		}
		return result, nil
	case <-time.After(timeout):
		session.Signal(ssh.SIGKILL)
		return &CommandResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String() + "\n[TIMEOUT] command exceeded " + fmt.Sprintf("%ds", timeoutSec),
			ExitCode: 124, // convention for timeout
		}, nil
	}
}

// WriteFile uploads content to a file inside a VM via SSH (cat with heredoc).
func WriteFile(ip, user, password, remotePath string, content []byte) error {
	client, err := sshClient(ip, user, password, 5*time.Second)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	// Create parent directory and write file via stdin
	session.Stdin = bytes.NewReader(content)

	dir := remotePath[:strings.LastIndex(remotePath, "/")]
	cmd := fmt.Sprintf("mkdir -p '%s' && cat > '%s'", dir, remotePath)
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("write file %s: %w", remotePath, err)
	}
	return nil
}

// ReadFile downloads a file from a VM via SSH.
func ReadFile(ip, user, password, remotePath string) ([]byte, error) {
	client, err := sshClient(ip, user, password, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	cmd := fmt.Sprintf("cat '%s'", remotePath)
	if err := session.Start(cmd); err != nil {
		return nil, fmt.Errorf("read file %s: %w", remotePath, err)
	}

	data, err := io.ReadAll(stdout)
	if err != nil {
		return nil, fmt.Errorf("read stdout: %w", err)
	}

	if err := session.Wait(); err != nil {
		return nil, fmt.Errorf("read file %s: %w", remotePath, err)
	}

	return data, nil
}

// PortCheck tests whether a TCP port is open on a VM.
func PortCheck(ip string, port int) bool {
	addr := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
