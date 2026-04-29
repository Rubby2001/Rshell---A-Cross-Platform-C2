package api

/*
修改说明：
1. api/forward-connection 添加目标地址规范化与校验逻辑。
2. 拒绝 localhost、回环地址、私网地址、未指定地址、组播地址、链路本地地址。
3. 对域名先做解析，只要解析结果指向受限地址就拒绝连接。

*/

import (
	"Rshell/pkg/connection/tcp"
	"Rshell/pkg/connection/websocket"
	"Rshell/pkg/logger"
	"Rshell/pkg/response"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ForwardConnect establish a forward connection
// @Summary Establish forward connection
// @Tags Forward
// @Accept json
// @Produce json
// @Param body body object true "{type: connection type (websocket/tcp), address: target address, proxy: SOCKS5 proxy}"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Failure 500 {object} object "internal server error"
// @Router /api/v1/forward-connection [post]
// @Security BearerAuth
func ForwardConnect(c *gin.Context) {
	var forward struct {
		Type    string `json:"type"`
		Address string `json:"address"`
		Proxy   string `json:"proxy"`
	}
	if err := c.ShouldBindJSON(&forward); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	switch forward.Type {
	case "websocket":
		safeAddress, err := normalizeForwardAddress(forward.Address, "")
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}

		config := &websocket.ForwardConfig{
			ServerURL:   "ws://" + safeAddress + "/ws",
			Socks5Proxy: forward.Proxy,
			Timeout:     30 * time.Second,
			MaxRetries:  5,
			RetryDelay:  10 * time.Second,
			Reconnect:   true,
			Headers:     map[string]string{},
		}

		client, err := websocket.StartForwardClient(config)
		if err != nil {
			logger.Error("Failed to start forward client:", err)
			response.InternalError(c)
			return
		}

		response.OK(c, gin.H{
			"temp_uid":   client.UID,
			"server":     config.ServerURL,
			"proxy_used": config.Socks5Proxy != "",
			"message":    "Forward connection established. Waiting for client registration...",
		})

		go monitorForwardConnection(client.UID, "websocket")
	case "tcp":
		handleTCPForward(forward.Address, forward.Proxy, c)
	default:
		response.BadRequest(c, "unsupported connection type")
	}
}

func handleTCPForward(address, proxy string, c *gin.Context) {
	safeAddress, err := normalizeForwardAddress(address, "8080")
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	config := &tcp.TCPForwardConfig{
		ServerAddress: safeAddress,
		Socks5Proxy:   proxy,
		Timeout:       30 * time.Second,
		MaxRetries:    5,
		RetryDelay:    10 * time.Second,
		Reconnect:     true,
	}

	client, err := tcp.StartTCPForwardClient(config)
	if err != nil {
		logger.Error("Failed to start TCP forward client:", err)
		response.InternalError(c)
		return
	}

	response.OK(c, gin.H{
		"type":       "tcp",
		"temp_uid":   client.UID,
		"server":     config.ServerAddress,
		"proxy_used": config.Socks5Proxy != "",
		"message":    "TCP forward connection established. Waiting for client registration...",
	})

	go monitorForwardConnection(client.UID, "tcp")
}

func normalizeForwardAddress(address, defaultPort string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", fmt.Errorf("address is required")
	}

	if strings.ContainsAny(address, "/?#") {
		return "", fmt.Errorf("invalid address format")
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		if defaultPort == "" || !strings.Contains(err.Error(), "missing port in address") {
			return "", fmt.Errorf("invalid address format, expected host:port")
		}
		host = address
		port = defaultPort
	}

	host = strings.Trim(host, "[]")
	if host == "" {
		return "", fmt.Errorf("host is required")
	}

	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return "", fmt.Errorf("invalid port")
	}

	if err := validateForwardHost(host); err != nil {
		return "", err
	}

	return net.JoinHostPort(host, strconv.Itoa(portNum)), nil
}

func validateForwardHost(host string) error {
	normalizedHost := strings.ToLower(strings.TrimSpace(host))
	if normalizedHost == "localhost" {
		return fmt.Errorf("restricted target host")
	}

	if ip := net.ParseIP(normalizedHost); ip != nil {
		if isRestrictedForwardIP(ip) {
			return fmt.Errorf("restricted target address")
		}
		return nil
	}

	ips, err := net.LookupIP(normalizedHost)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("failed to resolve target host")
	}

	for _, ip := range ips {
		if isRestrictedForwardIP(ip) {
			return fmt.Errorf("target resolves to restricted address")
		}
	}

	return nil
}

func isRestrictedForwardIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast()
}

func monitorForwardConnection(uid, connType string) {
	time.Sleep(3 * time.Second)

	if connType == "websocket" {
		if strings.HasPrefix(uid, "temp_") {
			logger.Warn("WebSocket forward connection still using temporary UID after 3 seconds:", uid)
		} else {
			logger.Info("WebSocket forward connection established with client:", uid)
		}
	} else if connType == "tcp" {
		if strings.HasPrefix(uid, "tcp_temp_") {
			logger.Warn("TCP forward connection still using temporary UID after 3 seconds:", uid)
		} else {
			logger.Info("TCP forward connection established with client:", uid)
		}
	}
}
