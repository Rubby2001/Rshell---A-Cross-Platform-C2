package api

import (
	"Rshell/pkg/database"
	"Rshell/pkg/logger"
	"Rshell/pkg/response"
	"Rshell/pkg/service"
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
		Type           string `json:"type"`
		ListenAddress  string `json:"listenAddress"`
		ConnectAddress string `json:"connectAddress"`
	}

	if err := c.ShouldBindJSON(&listener); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if !service.IsValidListenerType(listener.Type) {
		response.BadRequest(c, "Invalid listener type")
		return
	}

	if exists, _ := database.Engine.Where("listen_address = ?", listener.ListenAddress).Exist(&database.Listener{}); exists {
		response.BadRequest(c, "Listener already exists")
		return
	}
	if listener.Type != "oss" {
		if !service.IsPortAvailable(listener.ListenAddress) {
			response.BadRequest(c, "Port is not available")
			return
		}
	}

	listenerRecord := &database.Listener{
		Type:           listener.Type,
		ListenAddress:  listener.ListenAddress,
		ConnectAddress: listener.ConnectAddress,
		Status:         1,
	}

	if _, err := database.Engine.Insert(listenerRecord); err != nil {
		logger.Error("Failed to save listener to database:", err)
		response.BadRequest(c, "Failed to save listener")
		return
	}

	if err := service.StartListener(listener.Type, listener.ListenAddress); err != nil {
		database.Engine.Where("listen_address = ?", listener.ListenAddress).Update(&database.Listener{Status: 2})
		response.BadRequest(c, fmt.Sprintf("Failed to start listener: %v", err))
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
	var listeners []database.Listener
	database.Engine.Find(&listeners)
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
		Action string `json:"action"` // "open" or "close"
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

	var lis database.Listener
	if _, err := database.Engine.Where("listen_address = ?", addr).Get(&lis); err != nil {
		logger.Error("Failed to query listener:", err)
		response.BadRequest(c, "Listener not found")
		return
	}

	if body.Action == "open" {
		if instance, exists := service.GetServerInstance(addr); exists && instance.IsRunning {
			response.BadRequest(c, "Listener is already running")
			return
		}
		if lis.Type != "oss" {
			if !service.IsPortAvailable(addr) {
				response.BadRequest(c, "Port is not available")
				return
			}
		}
		if err := service.StartListener(lis.Type, lis.ListenAddress); err != nil {
			response.BadRequest(c, fmt.Sprintf("Failed to start listener: %v", err))
			return
		}
		database.Engine.Where("listen_address = ?", lis.ListenAddress).Update(&database.Listener{Status: 1})
		response.OK(c, "Listener opened successfully")
	} else {
		if err := service.StopListener(lis.Type, lis.ListenAddress); err != nil {
			response.BadRequest(c, fmt.Sprintf("Failed to stop listener: %v", err))
			return
		}
		database.Engine.Where("listen_address = ?", lis.ListenAddress).Update(&database.Listener{Status: 2})
		response.OK(c, "Listener closed successfully")
	}
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

	var lis database.Listener
	if _, err := database.Engine.Where("listen_address = ?", addr).Get(&lis); err != nil {
		logger.Error("Failed to query listener:", err)
		response.BadRequest(c, "Listener not found")
		return
	}

	if instance, exists := service.GetServerInstance(addr); exists && instance.IsRunning {
		if err := service.StopListener(lis.Type, lis.ListenAddress); err != nil {
			logger.Error("Failed to stop listener before deletion:", err)
		}
	}

	if _, err := database.Engine.Where("listen_address = ?", addr).Delete(&database.Listener{}); err != nil {
		logger.Error("Failed to delete listener:", err)
		response.BadRequest(c, "Failed to delete listener")
		return
	}

	response.OK(c, "Listener deleted successfully")
}

// StopAllServers 停止所有服务器（用于程序退出）
func StopAllServers() {
	service.StopAllServers()
}

// GetServerStats 获取服务器统计信息
func GetServerStats() map[string]interface{} {
	return service.GetServerStats()
}
