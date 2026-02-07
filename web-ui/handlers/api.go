package handlers

import (
	"net/http"
	"strconv"

	"fireadmin/vm"

	"github.com/labstack/echo/v4"
)

type APIHandler struct {
	Mgr *vm.Manager
}

// --- Request types ---

type createRequest struct {
	ID     int `json:"id"`
	VCPUs  int `json:"vcpus"`
	MemMiB int `json:"mem_mib"`
}

// --- Handlers ---

// ListVMs GET /api/vms
func (h *APIHandler) ListVMs(c echo.Context) error {
	return c.JSON(http.StatusOK, h.Mgr.List())
}

// CreateVM POST /api/vms
func (h *APIHandler) CreateVM(c echo.Context) error {
	var req createRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid body: " + err.Error()})
	}
	if req.ID < 1 || req.ID > 254 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "id must be 1-254"})
	}
	if req.VCPUs < 1 {
		req.VCPUs = 2
	}
	if req.MemMiB < 128 {
		req.MemMiB = 1024
	}

	v, err := h.Mgr.Create(req.ID, req.VCPUs, req.MemMiB)
	if err != nil {
		return c.JSON(http.StatusConflict, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, v.Info())
}

// GetVM GET /api/vms/:id
func (h *APIHandler) GetVM(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}
	v, err := h.Mgr.Get(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, v.Info())
}

// StartVM POST /api/vms/:id/start
func (h *APIHandler) StartVM(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}
	if err := h.Mgr.Start(id); err != nil {
		return c.JSON(http.StatusConflict, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "starting", "id": id})
}

// StopVM POST /api/vms/:id/stop
func (h *APIHandler) StopVM(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}
	if err := h.Mgr.Stop(id); err != nil {
		return c.JSON(http.StatusConflict, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "stopping", "id": id})
}

// DestroyVM DELETE /api/vms/:id
func (h *APIHandler) DestroyVM(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}
	if err := h.Mgr.Destroy(id); err != nil {
		return c.JSON(http.StatusConflict, echo.Map{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
