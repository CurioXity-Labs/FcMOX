package vm

import (
	"log"
	"os"

	"github.com/gorilla/websocket"
)

// NewConsole creates a console broadcaster attached to a PTY file descriptor.
func NewConsole(ptyFile *os.File) *Console {
	c := &Console{
		pty:     ptyFile,
		clients: make(map[*websocket.Conn]bool),
		done:    make(chan struct{}),
		buf:     NewRingBuffer(64 * 1024), // 64KB scrollback
	}
	go c.readLoop()
	return c
}

// readLoop reads from the PTY and fans out to all connected WebSocket clients.
func (c *Console) readLoop() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-c.done:
			return
		default:
		}

		n, err := c.pty.Read(buf)
		if err != nil {
			log.Printf("[console] PTY read error: %v", err)
			return
		}
		if n == 0 {
			continue
		}

		data := buf[:n]
		c.buf.Write(data)

		c.mu.Lock()
		for ws := range c.clients {
			if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
				log.Printf("[console] WS write error, removing client: %v", err)
				ws.Close()
				delete(c.clients, ws)
			}
		}
		c.mu.Unlock()
	}
}

// Attach registers a WebSocket client and sends buffered history.
// Returns a channel that closes when the client disconnects.
func (c *Console) Attach(ws *websocket.Conn) <-chan struct{} {
	// Send scrollback buffer first
	history := c.buf.Bytes()
	if len(history) > 0 {
		_ = ws.WriteMessage(websocket.BinaryMessage, history)
	}

	c.mu.Lock()
	c.clients[ws] = true
	c.mu.Unlock()

	// Read from WS -> write to PTY
	disconnected := make(chan struct{})
	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.clients, ws)
			c.mu.Unlock()
			close(disconnected)
		}()

		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if _, err := c.pty.Write(msg); err != nil {
				log.Printf("[console] PTY write error: %v", err)
				return
			}
		}
	}()

	return disconnected
}

// Close shuts down the console broadcaster.
func (c *Console) Close() {
	select {
	case <-c.done:
		return // already closed
	default:
	}
	close(c.done)

	c.mu.Lock()
	for ws := range c.clients {
		ws.Close()
		delete(c.clients, ws)
	}
	c.mu.Unlock()
}
