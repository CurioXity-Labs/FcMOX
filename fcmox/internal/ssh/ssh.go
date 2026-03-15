package ssh

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Config holds SSH credentials for guest VMs.
type Config struct {
	User     string
	Password string
}

// CommandResult holds the output of a command run inside a VM.
type CommandResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// client creates an SSH client to a VM.
func client(ip, user, password string, timeout time.Duration) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	addr := fmt.Sprintf("%s:22", ip)
	c, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return c, nil
}

// Check tests whether SSH is reachable on the given IP.
func Check(ip, user, password string) error {
	c, err := client(ip, user, password, 2*time.Second)
	if err != nil {
		return err
	}
	c.Close()
	return nil
}

// RunCommand executes a shell command inside a VM via SSH.
func RunCommand(ip, user, password, command string, timeoutSec int) (*CommandResult, error) {
	timeout := time.Duration(timeoutSec) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	c, err := client(ip, user, password, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	session, err := c.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

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
			ExitCode: 124,
		}, nil
	}
}

// WriteFile uploads content to a file inside a VM via SSH.
func WriteFile(ip, user, password, remotePath string, content []byte) error {
	c, err := client(ip, user, password, 5*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()

	session, err := c.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	session.Stdin = bytes.NewReader(content)
	dir := remotePath[:strings.LastIndex(remotePath, "/")]
	cmd := fmt.Sprintf("mkdir -p '%s' && cat > '%s'", dir, remotePath)
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("write file %s: %w", remotePath, err)
	}
	return nil
}

// ReadFile downloads a file from inside a VM via SSH.
func ReadFile(ip, user, password, remotePath string) ([]byte, error) {
	c, err := client(ip, user, password, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	session, err := c.NewSession()
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
