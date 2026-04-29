package api

import (
	"Rshell/internal/service"
	"Rshell/pkg/logger"
	"Rshell/pkg/middlewares"
	"Rshell/pkg/response"
	"fmt"

	"github.com/gin-gonic/gin"
)

// AddListener 添加监听器
// @Summary Add a new listener
// @Tags Listeners
// @Accept json
// @Produce json
// @Param body body object{type=string,listenAddress=string,connectAddress=string} true "Listener configuration"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/listeners [post]
// @Security BearerAuth
func AddListener(c *gin.Context) {
	var listener struct {
		Type           string `json:"type" binding:"required"`
		ListenAddress  string `json:"listenAddress" binding:"required"`
		ConnectAddress string `json:"connectAddress" binding:"required"`
	}

	if err := c.ShouldBindJSON(&listener); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Listener.AddListener(listener.Type, listener.ListenAddress, listener.ConnectAddress); err != nil {
		switch err.(type) {
		case *service.ServiceError:
			response.BadRequest(c, err.Error())
		default:
			logger.Error("Failed to add listener:", err)
			response.BadRequest(c, fmt.Sprintf("Failed to start listener: %v", err))
		}
		return
	}

	response.OK(c, "Listener added and started successfully")
}

// ListListener 列出监听器
// @Summary List all listeners
// @Tags Listeners
// @Accept json
// @Produce json
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/listeners [get]
// @Security BearerAuth
func ListListener(c *gin.Context) {
	svc := middlewares.GetServices(c)
	listeners, err := svc.Listener.ListListeners()
	if err != nil {
		response.InternalError(c)
		return
	}
	response.OK(c, listeners)
}

// UpdateListenerStatus 统一的监听器状态处理
// @Summary Update listener status (open/close)
// @Tags Listeners
// @Accept json
// @Produce json
// @Param addr path string true "Listener address"
// @Param body body object{action=string} true "Action: open or close"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/listeners/{addr}/status [patch]
// @Security BearerAuth
func UpdateListenerStatus(c *gin.Context) {
	var body struct {
		Action string `json:"action" binding:"required"` // "open" or "close"
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	addr := c.Param("addr")
	if addr == "" {
		response.BadRequest(c, "listenAddress is required")
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Listener.UpdateListenerStatus(addr, body.Action); err != nil {
		response.BadRequest(c, fmt.Sprintf("Failed to update listener: %v", err))
		return
	}
	response.OK(c, "Listener status updated successfully")
}

// DeleteListener 删除监听器
// @Summary Delete a listener
// @Tags Listeners
// @Accept json
// @Produce json
// @Param addr path string true "Listener address"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/listeners/{addr} [delete]
// @Security BearerAuth
func DeleteListener(c *gin.Context) {
	addr := c.Param("addr")
	if addr == "" {
		response.BadRequest(c, "listenAddress is required")
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Listener.DeleteListener(addr); err != nil {
		response.BadRequest(c, fmt.Sprintf("Failed to delete listener: %v", err))
		return
	}
	response.OK(c, "Listener deleted successfully")
}
