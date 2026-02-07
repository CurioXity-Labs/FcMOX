package handlers

import (
	"log"
	"net/http"
	"strconv"

	"fireadmin/vm"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true }, // Allow all origins for lab use
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

type ConsoleHandler struct {
	Mgr *vm.Manager
}

// ServeConsole GET /ws/console/:id  — upgrades to WebSocket and attaches to VM PTY.
func (h *ConsoleHandler) ServeConsole(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}

	v, err := h.Mgr.Get(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": err.Error()})
	}

	v.Mu().Lock()
	console := v.Console
	v.Mu().Unlock()

	if console == nil {
		return c.JSON(http.StatusConflict, echo.Map{"error": "vm is not running, no console available"})
	}

	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		log.Printf("[console %d] upgrade failed: %v", id, err)
		return err
	}

	log.Printf("[console %d] client connected: %s", id, ws.RemoteAddr())
	disconnected := console.Attach(ws)

	// Block until client disconnects (Echo needs this)
	<-disconnected
	log.Printf("[console %d] client disconnected: %s", id, ws.RemoteAddr())
	return nil
}
