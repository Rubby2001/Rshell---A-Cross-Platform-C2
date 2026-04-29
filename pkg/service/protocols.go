package service

import (
	"Rshell/pkg/api/communication"
	k "Rshell/pkg/connection/kcp"
	"Rshell/pkg/connection/oss"
	"Rshell/pkg/connection/tcp"
	"Rshell/pkg/connection/websocket"
	"Rshell/pkg/logger"
	"fmt"
	"github.com/xtaci/kcp-go/v5"
	"net"
	"net/http"
	"strings"
	"time"
)

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

	PortMgr.mu.Lock()
	PortMgr.Servers[listenerAddress] = instance
	PortMgr.mu.Unlock()

	go func() {
		defer func() {
			instance.IsRunning = false
			PortMgr.mu.Lock()
			delete(PortMgr.Servers, listenerAddress)
			PortMgr.mu.Unlock()
		}()

		logger.Info("WebSocket server starting on", listenerAddress)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("WebSocket server error:", err)
		}
		logger.Info("WebSocket server stopped:", listenerAddress)
	}()

	return nil
}

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

	PortMgr.mu.Lock()
	PortMgr.Servers[listenerAddress] = instance
	PortMgr.mu.Unlock()

	go func() {
		defer func() {
			instance.IsRunning = false
			tcpListener.Close()
			PortMgr.mu.Lock()
			delete(PortMgr.Servers, listenerAddress)
			PortMgr.mu.Unlock()
		}()

		logger.Info("TCP server starting on", listenerAddress)

		for {
			select {
			case <-instance.StopChan:
				logger.Info("TCP server received stop signal:", listenerAddress)
				return
			default:
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

				if tcpConn, ok := conn.(*net.TCPConn); ok {
					tcpConn.SetKeepAlive(true)
					tcpConn.SetKeepAlivePeriod(30 * time.Second)
					tcpConn.SetLinger(0)
				}

				go tcp.HandleTcpConnection(conn)
				instance.Stats.incrementConnections()
			}
		}
	}()

	return nil
}

func startKCPServer(listenerAddress string) error {
	lis, err := kcp.ListenWithOptions(listenerAddress, nil, 10, 3)
	if err != nil {
		return fmt.Errorf("failed to listen on KCP: %w", err)
	}

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

	PortMgr.mu.Lock()
	PortMgr.Servers[listenerAddress] = instance
	PortMgr.mu.Unlock()

	go func() {
		defer func() {
			instance.IsRunning = false
			lis.Close()
			PortMgr.mu.Lock()
			delete(PortMgr.Servers, listenerAddress)
			PortMgr.mu.Unlock()
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

				conn.SetStreamMode(true)
				conn.SetWindowSize(1024, 1024)
				conn.SetNoDelay(1, 10, 2, 1)

				logger.Info("KCP client connected:", conn.RemoteAddr())
				go k.HandleKCPConnection(conn)
				instance.Stats.incrementConnections()
			}
		}
	}()

	return nil
}

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

	PortMgr.mu.Lock()
	PortMgr.Servers[listenerAddress] = instance
	PortMgr.mu.Unlock()

	go func() {
		defer func() {
			instance.IsRunning = false
			PortMgr.mu.Lock()
			delete(PortMgr.Servers, listenerAddress)
			PortMgr.mu.Unlock()
		}()

		logger.Info("HTTP server starting on", listenerAddress)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error:", err)
		}
		logger.Info("HTTP server stopped:", listenerAddress)
	}()

	return nil
}

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
		Server:    nil,
		StopChan:  make(chan struct{}),
		IsRunning: true,
		StartedAt: time.Now(),
		Stats:     &ServerStats{},
	}

	PortMgr.mu.Lock()
	PortMgr.Servers[listenerAddress] = instance
	PortMgr.mu.Unlock()

	go func() {
		defer func() {
			instance.IsRunning = false
			PortMgr.mu.Lock()
			delete(PortMgr.Servers, listenerAddress)
			PortMgr.mu.Unlock()
		}()

		logger.Info("OSS client starting for endpoint:", endpoint)
		oss.HandleOSSConnection(endpoint, accessKeyID, accessKeySecret, bucketName, instance.StopChan)
		logger.Info("OSS client stopped:", endpoint)
	}()

	return nil
}
