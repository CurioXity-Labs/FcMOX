package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"fireadmin/vm"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/ssh"
)

type SSHHandler struct {
	Mgr *vm.Manager
}

// ServeSSH GET /ws/ssh/:id — upgrades to WebSocket, SSH-dials the VM, and bridges an interactive PTY shell.
// Each connection gets its own independent SSH session.
func (h *SSHHandler) ServeSSH(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}

	v, err := h.Mgr.Get(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": err.Error()})
	}

	info := v.Info()
	if info.State != "running" {
		return c.JSON(http.StatusConflict, echo.Map{"error": "vm is not running"})
	}

	// Upgrade HTTP -> WebSocket
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		log.Printf("[ssh %d] upgrade failed: %v", id, err)
		return err
	}
	defer ws.Close()

	log.Printf("[ssh %d] client connected: %s", id, ws.RemoteAddr())

	// SSH dial the VM
	sshConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password("root"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	addr := fmt.Sprintf("%s:22", info.IP)
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		log.Printf("[ssh %d] dial failed: %v", id, err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[31m--- SSH connection failed: %v ---\x1b[0m\r\n", err)))
		return nil
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		log.Printf("[ssh %d] session failed: %v", id, err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[31m--- SSH session failed: %v ---\x1b[0m\r\n", err)))
		return nil
	}
	defer session.Close()

	// Request PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 115200,
		ssh.TTY_OP_OSPEED: 115200,
	}
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		log.Printf("[ssh %d] pty request failed: %v", id, err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[31m--- PTY request failed: %v ---\x1b[0m\r\n", err)))
		return nil
	}

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return err
	}
	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return err
	}

	if err := session.Shell(); err != nil {
		log.Printf("[ssh %d] shell failed: %v", id, err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[31m--- Shell start failed: %v ---\x1b[0m\r\n", err)))
		return nil
	}

	ws.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[90m--- SSH session to VM "+strconv.Itoa(id)+" ---\x1b[0m\r\n"))

	done := make(chan struct{}, 1)

	// SSH stdout -> WebSocket
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 4096)
		for {
			n, err := stdoutPipe.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket -> SSH stdin
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if _, err := stdinPipe.Write(msg); err != nil {
				return
			}
		}
	}()

	<-done
	log.Printf("[ssh %d] client disconnected: %s", id, ws.RemoteAddr())
	return nil
}
