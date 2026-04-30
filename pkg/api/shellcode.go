package api

import (
	"Rshell/pkg/middlewares"
	"Rshell/pkg/response"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// StageShellCodeGen generate stage shellcode
// @Summary Generate stage shellcode
// @Tags Shellcode
// @Accept json
// @Produce octet-stream
// @Param body body object true "{listener: listener URL, port: port, format: output format (exe/bin/hex/c)}"
// @Success 200 {file} binary "shellcode payload"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/shellcode/stage [post]
// @Security BearerAuth
func StageShellCodeGen(c *gin.Context) {
	var shellcode struct {
		Listener string `json:"listener" binding:"required"`
		Port     string `json:"port" binding:"required"`
		Format   string `json:"format" binding:"required"`
	}
	if err := c.ShouldBind(&shellcode); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	svc := middlewares.GetServices(c)
	content, filename, ctype, err := svc.Shellcode.GenerateStageShellcode(shellcode.Port, shellcode.Format)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", ctype)
	c.Header("Content-Length", fmt.Sprintf("%d", len(content)))
	c.Data(http.StatusOK, ctype, content)
}
