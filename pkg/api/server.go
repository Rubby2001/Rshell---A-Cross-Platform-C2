package api

import (
	"Rshell/pkg/middlewares"
	"Rshell/pkg/response"
	"strconv"

	"github.com/gin-gonic/gin"
)

// GenServer generate a server binary
// @Summary Generate server binary
// @Tags Generators
// @Accept json
// @Produce octet-stream
// @Param body body object true "{osType: OS type, archType: architecture, listener: listener URL, pass: password}"
// @Success 200 {file} binary "server binary"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/generators/servers [post]
// @Security BearerAuth
func GenServer(c *gin.Context) {
	var serverBody struct {
		OsType   string `json:"osType" binding:"required"`
		ArchType string `json:"archType" binding:"required"`
		Listener string `json:"listener" binding:"required"`
		Pass     string `json:"pass" binding:"required"`
	}
	if err := c.ShouldBindJSON(&serverBody); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	svc := middlewares.GetServices(c)
	modifiedData, binaryFileName, err := svc.Generator.GenerateServer(serverBody.OsType, serverBody.ArchType, serverBody.Listener, serverBody.Pass)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", "attachment; filename="+binaryFileName)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.Itoa(len(modifiedData)))
	c.Writer.Write(modifiedData)
}

// ShowListener list active listeners
// @Summary List active listeners
// @Tags Generators
// @Accept json
// @Produce json
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/generators/listeners/active [get]
// @Security BearerAuth
func ShowListener(c *gin.Context) {
	svc := middlewares.GetServices(c)
	result, err := svc.Generator.ListActiveListeners()
	if err != nil {
		response.InternalError(c)
		return
	}
	response.OK(c, result)
}
