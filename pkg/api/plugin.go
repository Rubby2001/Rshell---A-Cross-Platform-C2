package api

import (
	"Rshell/pkg/middlewares"
	"Rshell/pkg/response"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// AddPlugin 添加插件
// @Summary Upload a new plugin
// @Tags Plugins
// @Accept multipart/form-data
// @Produce json
// @Param name formData string true "Plugin name"
// @Param os formData string true "Operating system (windows/linux)"
// @Param type formData string true "Plugin type (execute-assembly/inline-bin/shellcode-inject/inline-execute/script)"
// @Param file formData file true "Plugin file"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Failure 500 {object} object "internal server error"
// @Router /api/v1/plugins [post]
// @Security BearerAuth
func AddPlugin(c *gin.Context) {
	name := c.PostForm("name")
	osType := c.PostForm("os")
	pluginType := c.PostForm("type")

	file, err := c.FormFile("file")
	if err != nil {
		response.BadRequest(c, "No file uploaded")
		return
	}

	execPath, err := os.Executable()
	if err != nil {
		response.InternalError(c)
		return
	}

	extDir := filepath.Join(filepath.Dir(execPath), "Extensions")
	if _, err := os.Stat(extDir); os.IsNotExist(err) {
		os.MkdirAll(extDir, 0755)
	}

	fileName := file.Filename
	filePath := filepath.Join(extDir, fileName)

	if err := c.SaveUploadedFile(file, filePath); err != nil {
		response.InternalError(c)
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Plugin.AddPlugin(name, osType, pluginType, fileName, filePath, time.Now().Unix()); err != nil {
		response.InternalError(c)
		return
	}

	response.OK(c, "success")
}

// ListPlugins 列出插件
// @Summary List all plugins
// @Tags Plugins
// @Accept json
// @Produce json
// @Success 200 {object} object "success"
// @Failure 500 {object} object "internal server error"
// @Router /api/v1/plugins [get]
// @Security BearerAuth
func ListPlugins(c *gin.Context) {
	svc := middlewares.GetServices(c)
	plugins, err := svc.Plugin.ListPlugins()
	if err != nil {
		response.InternalError(c)
		return
	}
	response.OK(c, plugins)
}

// DeletePlugin 删除插件
// @Summary Delete a plugin
// @Tags Plugins
// @Accept json
// @Produce json
// @Param id path int true "Plugin ID"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Failure 404 {object} object "not found"
// @Failure 500 {object} object "internal server error"
// @Router /api/v1/plugins/{id} [delete]
// @Security BearerAuth
func DeletePlugin(c *gin.Context) {
	idStr := c.Param("id")
	id := parseInt64(idStr)
	if id == 0 {
		response.BadRequest(c, "Invalid plugin id")
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Plugin.DeletePlugin(id); err != nil {
		if err.Error() == "not found" {
			response.NotFound(c, "Plugin not found")
		} else {
			response.InternalError(c)
		}
		return
	}

	response.OK(c, "success")
}

// ExecutePlugin 执行插件
// @Summary Execute a plugin on a client
// @Tags Plugins
// @Accept json
// @Produce json
// @Param id path int true "Plugin ID"
// @Param body body object true "{uid: client uid, args: arguments}"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Failure 404 {object} object "not found"
// @Failure 500 {object} object "internal server error"
// @Router /api/v1/plugins/{id}/execute [post]
// @Security BearerAuth
func ExecutePlugin(c *gin.Context) {
	idStr := c.Param("id")
	id := parseInt64(idStr)
	if id == 0 {
		response.BadRequest(c, "Invalid plugin id")
		return
	}

	var req struct {
		Uid  string `json:"uid" binding:"required"`
		Args string `json:"args" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Plugin.ExecutePlugin(id, req.Uid, req.Args); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	response.OK(c, "Plugin executed")
}

func parseInt64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
