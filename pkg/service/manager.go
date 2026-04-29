package service

import (
	"Rshell/pkg/logger"
	"context"
	"fmt"
	"github.com/xtaci/kcp-go/v5"
	"net"
	"net/http"
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
	Server    interface{}
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
	PortMgr = &PortManager{
		Servers: make(map[string]*ServerInstance),
	}
)

func (s *ServerStats) incrementConnections() {
	s.mu.Lock()
	s.Connections++
	s.mu.Unlock()
}

// StartListener 启动监听器
func StartListener(listenerType, listenerAddress string) error {
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

// StopListener 停止监听器
func StopListener(listenerType, listenerAddress string) error {
	logger.Info("Stopping listener:", listenerType, "on", listenerAddress)

	PortMgr.mu.Lock()
	instance, exists := PortMgr.Servers[listenerAddress]
	PortMgr.mu.Unlock()

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
		time.Sleep(1 * time.Second)
	}

	PortMgr.mu.Lock()
	delete(PortMgr.Servers, listenerAddress)
	PortMgr.mu.Unlock()

	logger.Info("Listener stopped:", listenerAddress)
	return nil
}

// GetServerInstance 获取服务器实例
func GetServerInstance(address string) (*ServerInstance, bool) {
	PortMgr.mu.RLock()
	instance, exists := PortMgr.Servers[address]
	PortMgr.mu.RUnlock()
	return instance, exists
}

// IsValidListenerType 验证监听器类型
func IsValidListenerType(listenerType string) bool {
	validTypes := map[string]bool{
		"websocket": true,
		"tcp":       true,
		"kcp":       true,
		"http":      true,
		"oss":       true,
	}
	return validTypes[listenerType]
}

// StopAllServers 停止所有服务器（用于程序退出）
func StopAllServers() {
	logger.Info("Stopping all servers...")

	PortMgr.mu.Lock()
	defer PortMgr.mu.Unlock()

	for address, instance := range PortMgr.Servers {
		if instance.IsRunning {
			logger.Info("Stopping server:", address)
			instance.IsRunning = false
			close(instance.StopChan)

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

	time.Sleep(3 * time.Second)
	PortMgr.Servers = make(map[string]*ServerInstance)
	logger.Info("All servers stopped")
}

// GetServerStats 获取服务器统计信息
func GetServerStats() map[string]interface{} {
	stats := make(map[string]interface{})

	PortMgr.mu.RLock()
	defer PortMgr.mu.RUnlock()

	stats["total_servers"] = len(PortMgr.Servers)

	runningCount := 0
	for _, instance := range PortMgr.Servers {
		if instance.IsRunning {
			runningCount++
		}
	}
	stats["running_servers"] = runningCount

	typeStats := make(map[string]int)
	for _, instance := range PortMgr.Servers {
		typeStats[instance.Type]++
	}
	stats["servers_by_type"] = typeStats

	return stats
}
