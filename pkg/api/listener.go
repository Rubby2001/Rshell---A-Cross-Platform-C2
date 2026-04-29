package api

import (
	"Rshell/pkg/api/communication"
	k "Rshell/pkg/connection/kcp"
	"Rshell/pkg/connection/oss"
	"Rshell/pkg/connection/tcp"
	"Rshell/pkg/connection/websocket"
	"Rshell/pkg/database"
	"Rshell/pkg/logger"
	"Rshell/pkg/response"
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/xtaci/kcp-go/v5"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// PortManager 端口管理器
type PortManager struct {
	Servers map[string]*ServerInstance
	mu      sync.RWMutex
}

// ServerInstance 服务器实例
type ServerInstance struct {
	Type      string
	Address   string
	Server    interface{} // *http.Server, net.Listener, 等
	StopChan  chan struct{}
	IsRunning bool
	StartedAt time.Time
	Stats     *ServerStats
}

// ServerStats 服务器统计
type ServerStats struct {
	Connections int64
	Requests    int64
	Errors      int64
	mu          sync.RWMutex
}

var (
	portManager = &PortManager{
		Servers: make(map[string]*ServerInstance),
	}
)

// AddListener 添加监听器
func AddListener(c *gin.Context) {
	var listener struct {
		Type           string `json:"type"`
		ListenAddress  string `json:"listenAddress"`
		ConnectAddress string `json:"connectAddress"`
	}

	if err := c.ShouldBindJSON(&listener); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 验证监听器类型
	if !isValidListenerType(listener.Type) {
		response.BadRequest(c, "Invalid listener type")
		return
	}

	// 检查是否已存在
	if exists, _ := database.Engine.Where("listen_address = ?", listener.ListenAddress).Exist(&database.Listener{}); exists {
		response.BadRequest(c, "Listener already exists")
		return
	}
	if listener.Type != "oss" {
		// 检查端口是否可用
		if !isPortAvailable(listener.ListenAddress) {
			response.BadRequest(c, "Port is not available")
			return
		}
	}

	// 保存到数据库
	listenerRecord := &database.Listener{
		Type:           listener.Type,
		ListenAddress:  listener.ListenAddress,
		ConnectAddress: listener.ConnectAddress,
		Status:         1, // 默认开启
		//CreatedAt:      time.Now(),
	}

	if _, err := database.Engine.Insert(listenerRecord); err != nil {
		logger.Error("Failed to save listener to database:", err)
		response.BadRequest(c, "Failed to save listener")
		return
	}

	// 启动监听器
	if err := startListener(listener.Type, listener.ListenAddress); err != nil {
		// 启动失败，更新状态
		database.Engine.Where("listen_address = ?", listener.ListenAddress).Update(&database.Listener{Status: 2})
		response.BadRequest(c, fmt.Sprintf("Failed to start listener: %v", err))
		return
	}

	response.OK(c, "Listener added and started successfully")
}

// ListListener 列出监听器
func ListListener(c *gin.Context) {
	var listeners []database.Listener
	database.Engine.Find(&listeners)
	response.OK(c, listeners)
}

// OpenListener 开启监听器
func OpenListener(c *gin.Context) {
	handleListenerStatus(c, "open")
}

// CloseListener 关闭监听器
func CloseListener(c *gin.Context) {
	handleListenerStatus(c, "close")
}

// handleListenerStatus 统一的监听器状态处理
func handleListenerStatus(c *gin.Context, action string) {
	addr := c.Param("addr")
	if addr == "" {
		response.BadRequest(c, "listenAddress is required")
		return
	}

	// 查询监听器配置
	var lis database.Listener
	if _, err := database.Engine.Where("listen_address = ?", addr).Get(&lis); err != nil {
		logger.Error("Failed to query listener:", err)
		response.BadRequest(c, "Listener not found")
		return
	}

	if action == "open" {
		// 检查是否已在运行
		if instance, exists := getServerInstance(addr); exists && instance.IsRunning {
			response.BadRequest(c, "Listener is already running")
			return
		}
		if lis.Type != "oss" {
			// 检查端口是否可用
			if !isPortAvailable(addr) {
				response.BadRequest(c, "Port is not available")
				return
			}
		}

		// 启动监听器
		if err := startListener(lis.Type, lis.ListenAddress); err != nil {
			response.BadRequest(c, fmt.Sprintf("Failed to start listener: %v", err))
			return
		}

		// 更新数据库状态
		if _, err := database.Engine.Where("listen_address = ?", lis.ListenAddress).Update(&database.Listener{Status: 1}); err != nil {
			logger.Error("Failed to update listener status:", err)
		}

		response.OK(c, "Listener opened successfully")
	} else {
		// 停止监听器
		if err := stopListener(lis.Type, lis.ListenAddress); err != nil {
			response.BadRequest(c, fmt.Sprintf("Failed to stop listener: %v", err))
			return
		}

		// 更新数据库状态
		if _, err := database.Engine.Where("listen_address = ?", lis.ListenAddress).Update(&database.Listener{Status: 2}); err != nil {
			logger.Error("Failed to update listener status:", err)
		}

		response.OK(c, "Listener closed successfully")
	}
}

// DeleteListener 删除监听器
func DeleteListener(c *gin.Context) {
	addr := c.Param("addr")
	if addr == "" {
		response.BadRequest(c, "listenAddress is required")
		return
	}

	// 查询监听器配置
	var lis database.Listener
	if _, err := database.Engine.Where("listen_address = ?", addr).Get(&lis); err != nil {
		logger.Error("Failed to query listener:", err)
		response.BadRequest(c, "Listener not found")
		return
	}

	// 如果正在运行，先停止
	if instance, exists := getServerInstance(addr); exists && instance.IsRunning {
		if err := stopListener(lis.Type, lis.ListenAddress); err != nil {
			logger.Error("Failed to stop listener before deletion:", err)
			// 继续删除，但记录错误
		}
	}

	// 从数据库中删除
	if _, err := database.Engine.Where("listen_address = ?", addr).Delete(&database.Listener{}); err != nil {
		logger.Error("Failed to delete listener:", err)
		response.BadRequest(c, "Failed to delete listener")
		return
	}

	response.OK(c, "Listener deleted successfully")
}

// startListener 启动监听器
func startListener(listenerType, listenerAddress string) error {
	logger.Info("Starting listener:", listenerType, "on", listenerAddress)

	switch listenerType {
	case "websocket":
		return startWebSocketServer(listenerAddress)
	case "tcp":
		return startTCPServer(listenerAddress)
	case "kcp":
		return startKCPServer(listenerAddress)
	case "http":
		return startHTTPServer(listenerAddress)
	case "oss":
		return startOSSServer(listenerAddress)
	default:
		return fmt.Errorf("unsupported listener type: %s", listenerType)
	}
}

// stopListener 停止监听器
func stopListener(listenerType, listenerAddress string) error {
	logger.Info("Stopping listener:", listenerType, "on", listenerAddress)

	portManager.mu.Lock()
	instance, exists := portManager.Servers[listenerAddress]
	portManager.mu.Unlock()

	if !exists {
		return fmt.Errorf("listener not found: %s", listenerAddress)
	}

	instance.IsRunning = false
	close(instance.StopChan)

	switch listenerType {
	case "websocket", "http":
		if server, ok := instance.Server.(*http.Server); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return server.Shutdown(ctx)
		}
	case "tcp":
		if listener, ok := instance.Server.(net.Listener); ok {
			return listener.Close()
		}
	case "kcp":
		if listener, ok := instance.Server.(*kcp.Listener); ok {
			return listener.Close()
		}
	case "oss":
		// OSS客户端会通过StopChan优雅关闭
		// 等待一小段时间确保关闭完成
		time.Sleep(1 * time.Second)
	}

	// 从管理器移除
	portManager.mu.Lock()
	delete(portManager.Servers, listenerAddress)
	portManager.mu.Unlock()

	logger.Info("Listener stopped:", listenerAddress)
	return nil
}

// startWebSocketServer 启动WebSocket服务器
func startWebSocketServer(listenerAddress string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebSocket)

	server := &http.Server{
		Addr:           listenerAddress,
		Handler:        mux,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	instance := &ServerInstance{
		Type:      "websocket",
		Address:   listenerAddress,
		Server:    server,
		StopChan:  make(chan struct{}),
		IsRunning: true,
		StartedAt: time.Now(),
		Stats:     &ServerStats{},
	}

	portManager.mu.Lock()
	portManager.Servers[listenerAddress] = instance
	portManager.mu.Unlock()

	go func() {
		defer func() {
			instance.IsRunning = false
			portManager.mu.Lock()
			delete(portManager.Servers, listenerAddress)
			portManager.mu.Unlock()
		}()

		logger.Info("WebSocket server starting on", listenerAddress)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("WebSocket server error:", err)
		}
		logger.Info("WebSocket server stopped:", listenerAddress)
	}()

	return nil
}

// startTCPServer 启动TCP服务器
func startTCPServer(listenerAddress string) error {
	tcpListener, err := net.Listen("tcp", listenerAddress)
	if err != nil {
		return fmt.Errorf("failed to listen on TCP: %w", err)
	}

	instance := &ServerInstance{
		Type:      "tcp",
		Address:   listenerAddress,
		Server:    tcpListener,
		StopChan:  make(chan struct{}),
		IsRunning: true,
		StartedAt: time.Now(),
		Stats:     &ServerStats{},
	}

	portManager.mu.Lock()
	portManager.Servers[listenerAddress] = instance
	portManager.mu.Unlock()

	go func() {
		defer func() {
			instance.IsRunning = false
			tcpListener.Close()
			portManager.mu.Lock()
			delete(portManager.Servers, listenerAddress)
			portManager.mu.Unlock()
		}()

		logger.Info("TCP server starting on", listenerAddress)

		for {
			select {
			case <-instance.StopChan:
				logger.Info("TCP server received stop signal:", listenerAddress)
				return
			default:
				// 设置接受超时，以便可以响应停止信号
				tcpListener.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))

				conn, err := tcpListener.Accept()
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue
					}
					if strings.Contains(err.Error(), "use of closed network connection") {
						return
					}
					logger.Error("TCP accept error:", err)
					continue
				}

				// 设置TCP参数
				if tcpConn, ok := conn.(*net.TCPConn); ok {
					tcpConn.SetKeepAlive(true)
					tcpConn.SetKeepAlivePeriod(30 * time.Second)
					tcpConn.SetLinger(0)
				}

				// 处理连接
				go tcp.HandleTcpConnection(conn)

				// 更新统计
				instance.Stats.incrementConnections()
			}
		}
	}()

	return nil
}

// startKCPServer 启动KCP服务器
func startKCPServer(listenerAddress string) error {
	// KCP加密配置
	//block, _ := kcp.NewAESBlockCrypt(pbkdf2.Key([]byte("default-key"), []byte("default-salt"), 1024, 32, sha1.New))

	lis, err := kcp.ListenWithOptions(listenerAddress, nil, 10, 3)
	if err != nil {
		return fmt.Errorf("failed to listen on KCP: %w", err)
	}

	// 设置KCP参数
	if err := lis.SetReadBuffer(4194304); err != nil {
		logger.Warn("SetReadBuffer error:", err)
	}
	if err := lis.SetWriteBuffer(4194304); err != nil {
		logger.Warn("SetWriteBuffer error:", err)
	}

	instance := &ServerInstance{
		Type:      "kcp",
		Address:   listenerAddress,
		Server:    lis,
		StopChan:  make(chan struct{}),
		IsRunning: true,
		StartedAt: time.Now(),
		Stats:     &ServerStats{},
	}

	portManager.mu.Lock()
	portManager.Servers[listenerAddress] = instance
	portManager.mu.Unlock()

	go func() {
		defer func() {
			instance.IsRunning = false
			lis.Close()
			portManager.mu.Lock()
			delete(portManager.Servers, listenerAddress)
			portManager.mu.Unlock()
		}()

		logger.Info("KCP server starting on", listenerAddress)

		for {
			select {
			case <-instance.StopChan:
				logger.Info("KCP server received stop signal:", listenerAddress)
				return
			default:
				conn, err := lis.AcceptKCP()
				if err != nil {
					if strings.Contains(err.Error(), "use of closed network connection") {
						return
					}
					logger.Error("KCP accept error:", err)
					continue
				}

				// 设置KCP会话参数
				conn.SetStreamMode(true)
				conn.SetWindowSize(1024, 1024)
				conn.SetNoDelay(1, 10, 2, 1)

				logger.Info("KCP client connected:", conn.RemoteAddr())

				// 处理连接
				go k.HandleKCPConnection(conn)

				// 更新统计
				instance.Stats.incrementConnections()
			}
		}
	}()

	return nil
}

// startHTTPServer 启动HTTP服务器
func startHTTPServer(listenerAddress string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/tencent/mcp/pc/pcsearch", communication.GetHttp)
	mux.HandleFunc("/tencent/sensearch/collection/item/check", communication.PostHttp)

	server := &http.Server{
		Addr:           listenerAddress,
		Handler:        mux,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	instance := &ServerInstance{
		Type:      "http",
		Address:   listenerAddress,
		Server:    server,
		StopChan:  make(chan struct{}),
		IsRunning: true,
		StartedAt: time.Now(),
		Stats:     &ServerStats{},
	}

	portManager.mu.Lock()
	portManager.Servers[listenerAddress] = instance
	portManager.mu.Unlock()

	go func() {
		defer func() {
			instance.IsRunning = false
			portManager.mu.Lock()
			delete(portManager.Servers, listenerAddress)
			portManager.mu.Unlock()
		}()

		logger.Info("HTTP server starting on", listenerAddress)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error:", err)
		}
		logger.Info("HTTP server stopped:", listenerAddress)
	}()

	return nil
}

// startOSSServer 启动OSS服务器
func startOSSServer(listenerAddress string) error {
	parts := strings.Split(listenerAddress, ":")
	if len(parts) != 4 {
		return fmt.Errorf("invalid OSS address format, expected endpoint:accessKeyID:accessKeySecret:bucketName")
	}

	endpoint := parts[0]
	accessKeyID := parts[1]
	accessKeySecret := parts[2]
	bucketName := parts[3]

	instance := &ServerInstance{
		Type:      "oss",
		Address:   listenerAddress,
		Server:    nil, // OSS没有服务器对象
		StopChan:  make(chan struct{}),
		IsRunning: true,
		StartedAt: time.Now(),
		Stats:     &ServerStats{},
	}

	portManager.mu.Lock()
	portManager.Servers[listenerAddress] = instance
	portManager.mu.Unlock()

	go func() {
		defer func() {
			instance.IsRunning = false
			portManager.mu.Lock()
			delete(portManager.Servers, listenerAddress)
			portManager.mu.Unlock()
		}()

		logger.Info("OSS client starting for endpoint:", endpoint)

		// 启动OSS处理
		oss.HandleOSSConnection(endpoint, accessKeyID, accessKeySecret, bucketName, instance.StopChan)

		logger.Info("OSS client stopped:", endpoint)
	}()

	return nil
}

// isValidListenerType 验证监听器类型
func isValidListenerType(listenerType string) bool {
	validTypes := map[string]bool{
		"websocket": true,
		"tcp":       true,
		"kcp":       true,
		"http":      true,
		"oss":       true,
	}
	return validTypes[listenerType]
}

// isPortAvailable 检查端口是否可用
func isPortAvailable(address string) bool {
	// 提取端口号
	parts := strings.Split(address, ":")
	var host, port string
	if len(parts) == 1 {
		port = parts[0]
	} else if len(parts) == 2 {
		host = parts[0]
		port = parts[1]
		if host == "" {
			host = "0.0.0.0"
		}
	} else {
		return false
	}

	// 尝试监听端口
	listener, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return false
	}
	listener.Close()

	return true
}

// getServerInstance 获取服务器实例
func getServerInstance(address string) (*ServerInstance, bool) {
	portManager.mu.RLock()
	instance, exists := portManager.Servers[address]
	portManager.mu.RUnlock()
	return instance, exists
}

// incrementConnections 增加连接计数
func (s *ServerStats) incrementConnections() {
	s.mu.Lock()
	s.Connections++
	s.mu.Unlock()
}

// getConnections 获取连接计数
func (s *ServerStats) getConnections() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Connections
}

// incrementRequests 增加请求计数
func (s *ServerStats) incrementRequests() {
	s.mu.Lock()
	s.Requests++
	s.mu.Unlock()
}

// incrementErrors 增加错误计数
func (s *ServerStats) incrementErrors() {
	s.mu.Lock()
	s.Errors++
	s.mu.Unlock()
}

// getStats 获取统计信息
func (s *ServerStats) getStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"connections": s.Connections,
		"requests":    s.Requests,
		"errors":      s.Errors,
	}
}

// StopAllServers 停止所有服务器（用于程序退出）
func StopAllServers() {
	logger.Info("Stopping all servers...")

	portManager.mu.Lock()
	defer portManager.mu.Unlock()

	for address, instance := range portManager.Servers {
		if instance.IsRunning {
			logger.Info("Stopping server:", address)
			instance.IsRunning = false
			close(instance.StopChan)

			// 根据类型执行不同的停止逻辑
			switch instance.Type {
			case "websocket", "http":
				if server, ok := instance.Server.(*http.Server); ok {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					server.Shutdown(ctx)
					cancel()
				}
			case "tcp":
				if listener, ok := instance.Server.(net.Listener); ok {
					listener.Close()
				}
			case "kcp":
				if listener, ok := instance.Server.(*kcp.Listener); ok {
					listener.Close()
				}
			}
		}
	}

	// 等待所有服务器停止
	time.Sleep(3 * time.Second)

	// 清空服务器列表
	portManager.Servers = make(map[string]*ServerInstance)
	logger.Info("All servers stopped")
}

// GetServerStats 获取服务器统计信息
func GetServerStats() map[string]interface{} {
	stats := make(map[string]interface{})

	portManager.mu.RLock()
	defer portManager.mu.RUnlock()

	stats["total_servers"] = len(portManager.Servers)

	runningCount := 0
	for _, instance := range portManager.Servers {
		if instance.IsRunning {
			runningCount++
		}
	}
	stats["running_servers"] = runningCount

	// 按类型统计
	typeStats := make(map[string]int)
	for _, instance := range portManager.Servers {
		typeStats[instance.Type]++
	}
	stats["servers_by_type"] = typeStats

	return stats
}
