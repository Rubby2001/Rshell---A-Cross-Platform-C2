package api

import (
	"Rshell/pkg/command"
	"Rshell/pkg/common"
	"Rshell/pkg/interactive" // 使用新的interactive包
	"Rshell/pkg/logger"
	"Rshell/pkg/response"
	"Rshell/pkg/sendcommand"
	"Rshell/pkg/utils"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"github.com/golang-jwt/jwt"
	"github.com/gorilla/websocket"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true // 生产环境需要严格检查
		},
	}

	// 命令管理器，避免循环导入问题
	commandHandlers = make(map[string]CommandHandler)
)

// CommandHandler 命令处理器接口
type CommandHandler interface {
	HandleCommand(uid, sessionID string, command []byte)
}

// RegisterCommandHandler 注册命令处理器
func RegisterCommandHandler(name string, handler CommandHandler) {
	commandHandlers[name] = handler
}

// GetWebSocketAuthToken 生成WebSocket专用token
func GetWebSocketAuthToken(c *gin.Context) {
	// 从上下文获取用户名（已经在AuthMiddleware中设置）
	username, exists := c.Get("username")
	if !exists {
		response.Unauthorized(c)
		return
	}

	uid := c.Param("uid")
	usernameStr, ok := username.(string)
	if !ok {
		response.InternalError(c)
		return
	}

	// 生成一个短期有效的WebSocket专用token
	tokenString, err := common.GenerateJWTWithExtras(usernameStr, uid, "websocket", 5*time.Minute)
	if err != nil {
		response.InternalError(c)
		return
	}

	response.OK(c, gin.H{"token": tokenString, "expires_in": 300, "uid": uid, "username": usernameStr})
}

// 验证WebSocket Token的函数
func validateWebSocketToken(tokenString string) (*common.Claims, error) {
	// 使用现有的JWT验证逻辑
	claims, err := common.ValidateJWT(tokenString)
	if err != nil {
		return nil, err
	}

	// 额外验证：确保这个token是用于WebSocket的
	if claims.Purpose != "websocket" {
		return nil, jwt.NewValidationError("token not for websocket", jwt.ValidationErrorClaimsInvalid)
	}

	return claims, nil
}

// InteractiveShell provides interactive shell via WebSocket
// @Summary Interactive shell WebSocket
// @Tags WebSocket
// @Produce json
// @Param uid path string true "Client UID"
// @Param sessionId path string true "Session ID"
// @Param auth query string false "Authentication token"
// @Success 101 "Switching Protocols"
// @Failure 400 {object} object "bad request"
// @Failure 401 {object} object "unauthorized"
// @Failure 403 {object} object "forbidden"
// @Router /api/v1/ws/interactive/{uid}/{sessionId} [get]
func InteractiveShell(c *gin.Context) {
	uid := c.Param("uid")
	sessionID := c.Param("sessionId")

	if uid == "" || sessionID == "" {
		response.BadRequest(c, "uid and sessionId are required")
		return
	}

	// 从查询参数获取token
	token := c.Query("auth")
	if token == "" {
		// 尝试从Authorization头部获取
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			// 尝试从Authorization2头部获取（你的前端可能使用这个）
			authHeader2 := c.GetHeader("Authorization2")
			if authHeader2 != "" && strings.HasPrefix(authHeader2, "Bearer ") {
				token = strings.TrimPrefix(authHeader2, "Bearer ")
			}
		}
	}

	if token == "" {
		response.Unauthorized(c)
		return
	}

	// 验证token
	claims, err := validateWebSocketToken(token)
	if err != nil {
		// 更详细的错误信息
		logger.Error("Token验证失败: %v, token: ", err, token[:min(len(token), 20)]+"...")
		response.Unauthorized(c)
		return
	}

	// 验证uid是否匹配
	if claims.UID != uid {
		logger.Error("UID不匹配: token UID=", claims.UID, " request UID=", uid)
		response.Forbidden(c)
		return
	}

	// 验证token是否是WebSocket专用的
	if claims.Purpose != "websocket" {
		logger.Error("Token用途不正确: ", claims.Purpose)
		response.Forbidden(c)
		return
	}

	// 升级到WebSocket连接
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Error("Failed to upgrade connection: ", err)
		return
	}
	defer conn.Close()

	// 创建或获取会话
	session := interactive.DefaultManager.GetOrCreateSession(sessionID, uid, conn)
	if session == nil {
		sendErrorMessage(conn, "创建会话失败")
		return
	}

	// 设置会话处理器
	session.CommandHandler = func(uid, sessionID string, input []byte) {
		handleBrowserInput(uid, sessionID, input)
	}

	session.CloseHandler = func(uid, sessionID string) {
		handleSessionClose(uid, sessionID)
	}

	// 设置心跳处理器
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(30 * time.Minute))
		return nil
	})

	// 启动心跳
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-session.CloseChan:
				return
			}
		}
	}()

	// 立即发送认证成功消息
	conn.WriteJSON(map[string]interface{}{
		"type":    "auth_response",
		"success": true,
		"message": "认证成功",
	})

	go session.WritePump()
	// 启动会话处理 - 这会处理浏览器消息
	go session.HandleBrowserConnection()

	// 发送初始化命令到agent
	initSession(uid, sessionID)

	// 等待会话关闭
	select {
	case <-session.CloseChan:
		//logger.Info("会话关闭: %s", sessionID)
	case <-time.After(2 * time.Hour):
		logger.Info("会话超时: %s", sessionID)
		session.Close()
	}
}

// 初始化会话，发送创建命令到agent
func initSession(uid, sessionID string) {
	// 发送创建交互式shell的命令
	cmdData := prepareCreateShellCommand(sessionID, 80, 24)
	sendInteractiveCommand(uid, command.InteractiveShell, cmdData)
	//log.Printf("Sent init command to agent for session: %s", sessionID)
}

// 处理浏览器输入
func handleBrowserInput(uid, sessionID string, input []byte) {
	// 准备命令数据
	cmdData := prepareAgentCommand(sessionID, string(input))
	sendInteractiveCommand(uid, command.WriteInteractieShell, cmdData)
	//log.Printf("Sent input to agent for session %s: %s", sessionID, string(input))
}

// 处理会话关闭
func handleSessionClose(uid, sessionID string) {
	// 发送关闭命令到agent
	cmdData := []byte(sessionID)
	sendInteractiveCommand(uid, command.StopInteractiveShell, cmdData)
	//log.Printf("Sent close command to agent for session: %s", sessionID)
}

// 发送交互式命令
func sendInteractiveCommand(uid string, cmdType int, data []byte) {
	cmdTypeBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(cmdTypeBytes, uint32(cmdType))
	commandData := utils.BytesCombine(cmdTypeBytes, data)
	sendcommand.SendCommandBytes(uid, commandData)
}

// 准备创建shell的命令数据
func prepareCreateShellCommand(sessionID string, width, height int) []byte {
	sessionIDBytes := []byte(sessionID)
	//sessionIDLen := len(sessionIDBytes)

	buf := bytes.NewBuffer(nil)
	//buf.Write(utils.WriteInt(sessionIDLen)) // sessionID长度
	buf.Write(sessionIDBytes) // sessionID
	//buf.Write(utils.WriteInt(width))        // 宽度
	//buf.Write(utils.WriteInt(height))       // 高度

	return buf.Bytes()
}

// 准备agent命令数据
func prepareAgentCommand(sessionID, cmd string) []byte {
	sessionIDBytes := []byte(sessionID)
	sessionIDLen := len(sessionIDBytes)

	buf := bytes.NewBuffer(nil)
	buf.Write(utils.WriteInt(sessionIDLen)) // 4字节长度
	buf.Write(sessionIDBytes)               // sessionID
	buf.Write([]byte(cmd))                  // 命令内容

	return buf.Bytes()
}

// 辅助函数，取最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sendErrorMessage(conn *websocket.Conn, message string) {
	conn.WriteJSON(map[string]interface{}{
		"type":    "error",
		"message": message,
	})
}

// 接收agent输出（这个函数会在其他地方调用，比如tcp.go中）
//func ReceiveAgentOutput(data []byte) {
//	// 使用新的interactive包解析输出
//	uid, sessionID, output, ok := interactive.HandleAgentOutput(data)
//	if !ok {
//		log.Printf("Failed to parse agent output")
//		return
//	}
//
//	// 发送输出到会话
//	success := interactive.SendOutputToSession(uid, sessionID, output)
//	if !success {
//		log.Printf("Failed to send output to session %s", sessionID)
//	}
//}

// 处理终端尺寸调整
//func HandleTerminalResize(uid, sessionID string, cols, rows int) {
//	// 准备尺寸调整命令
//	resizeData := prepareResizeCommand(sessionID, cols, rows)
//	sendInteractiveCommand(uid, command.ResizeInteractiveShell, resizeData)
//	log.Printf("Sent resize command for session %s: %dx%d", sessionID, cols, rows)
//}

// 准备尺寸调整命令
func prepareResizeCommand(sessionID string, cols, rows int) []byte {
	sessionIDBytes := []byte(sessionID)
	sessionIDLen := len(sessionIDBytes)

	buf := bytes.NewBuffer(nil)
	buf.Write(utils.WriteInt(sessionIDLen)) // sessionID长度
	buf.Write(sessionIDBytes)               // sessionID
	buf.Write(utils.WriteInt(cols))         // 列数
	buf.Write(utils.WriteInt(rows))         // 行数

	return buf.Bytes()
}

// 处理完整的浏览器消息
func handleBrowserMessage(session *interactive.InteractiveSession, data []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		//log.Printf("Failed to parse browser message: %v", err)
		return
	}

	switch msg["type"] {
	case "init":
		handleBrowserInit(session, msg)
	case "input":
		handleUserInput(session, msg)
	//case "resize":
	//	handleTerminalResizeMsg(session, msg)
	case "close":
		session.Close()
	default:
		//log.Printf("Unknown message type: %v", msg["type"])
	}
}

func handleBrowserInit(session *interactive.InteractiveSession, msg map[string]interface{}) {
	//log.Printf("Browser initialized for session: %s", session.SessionID)

	uid, _ := msg["uid"].(string)
	sessionId, _ := msg["sessionId"].(string)

	if uid != session.Uid || sessionId != session.SessionID {
		session.SendToBrowser(map[string]interface{}{
			"type":    "error",
			"message": "会话验证失败",
		})
		return
	}

	// 获取终端尺寸
	//cols, _ := msg["cols"].(float64)
	//rows, _ := msg["rows"].(float64)

	//if cols > 0 && rows > 0 {
	//	HandleTerminalResize(session.Uid, session.SessionID, int(cols), int(rows))
	//}

	session.SendToBrowser(map[string]interface{}{
		"type":    "session_info",
		"message": "",
	})
}

func handleUserInput(session *interactive.InteractiveSession, msg map[string]interface{}) {
	input, ok := msg["data"].(string)
	if !ok || input == "" {
		return
	}

	// 通过会话的CommandHandler处理
	session.ProcessInput([]byte(input))
}

//func handleTerminalResizeMsg(session *interactive.InteractiveSession, msg map[string]interface{}) {
//	cols, colsOk := msg["cols"].(float64)
//	rows, rowsOk := msg["rows"].(float64)
//
//	if colsOk && rowsOk {
//		HandleTerminalResize(session.Uid, session.SessionID, int(cols), int(rows))
//	}
//}
//
//// 获取活跃会话列表（用于监控）
//func GetActiveSessions(c *gin.Context) {
//	sessions := interactive.DefaultManager.GetAllSessions()
//
//	sessionInfos := make([]map[string]interface{}, 0)
//	for _, session := range sessions {
//		sessionInfo := map[string]interface{}{
//			"session_id": session.SessionID,
//			"uid":        session.Uid,
//			"connected":  session.Connected,
//		}
//		sessionInfos = append(sessionInfos, sessionInfo)
//	}
//
//	c.JSON(http.StatusOK, gin.H{
//		"status": http.StatusOK,
//		"count":  len(sessionInfos),
//		"data":   sessionInfos,
//	})
//}
