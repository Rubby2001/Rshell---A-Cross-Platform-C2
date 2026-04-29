package routers

import (
	"Rshell/pkg/api"
	"Rshell/pkg/mcp"
	"Rshell/pkg/middlewares"
	"Rshell/pkg/swagger"
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func NewRouter(embedFS embed.FS, staticFs fs.FS) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// 配置 CORS
	r.Use(middlewares.Cors())

	// 为前端页面和静态资源创建需要Basic认证的路由组
	webGroup := r.Group("/")
	webGroup.Use(middlewares.BasicAuthMiddleware())
	{
		// 提供静态文件，文件夹是 ./static
		webGroup.StaticFS("/static/", http.FS(staticFs))

		// 引入html
		r.SetHTMLTemplate(template.Must(template.New("").ParseFS(embedFS, "dist/*.html")))

		// 处理未匹配的路由（前端页面）
	}
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "route not found"})
			return
		}
		// 手动执行 BasicAuth
		middlewares.BasicAuthMiddleware()(c)
		if c.IsAborted() {
			return
		}

		c.HTML(http.StatusOK, "index.html", nil)
	})

	// API v1 路由组
	a := r.Group("/api/v1")
	{
		// 认证接口 - 不需要任何认证
		a.POST("/auth/login", api.LoginHandler)

		// WebSocket连接端点 - 不需要JWT，会在处理器中验证WebSocket专用token
		a.GET("/ws/interactive/:uid/:sessionId", api.InteractiveShell)
	}

	// 使用 JWT 中间件保护以下路由
	protected := a.Group("/")
	protected.Use(middlewares.AuthMiddleware())

	// 认证相关（已登录用户）
	auth := protected.Group("/auth")
	{
		auth.POST("/logout", api.LogoutHandler)
		auth.PUT("/password", api.ChangePasswordHandler)
	}

	// 客户端管理
	clients := protected.Group("/clients")
	{
		clients.GET("", api.GetClients)
		clients.POST("/:uid/shell/commands", api.SendCommands)
		clients.GET("/:uid/shell/output", api.GetShellContent)
		clients.GET("/:uid/processes", api.GetPidList)
		clients.DELETE("/:uid/processes/:pid", api.KillPid)
		clients.GET("/:uid/files", api.FileBrowse)
		clients.DELETE("/:uid/files", api.FileDelete)
		clients.POST("/:uid/files/directories", api.MakeDir)
		clients.POST("/:uid/files/upload", api.FileUpload)
		clients.GET("/:uid/note", api.GetNote)
		clients.PUT("/:uid/note", api.SaveNote)
		clients.POST("/:uid/files/download", api.DownloadFile)
		clients.GET("/:uid/downloads", api.GetDownloadsInfo)
		clients.POST("/:uid/downloads/fetch", api.DownloadDownloadedFile)
		clients.GET("/:uid/drives", api.ListDrives)
		clients.GET("/:uid/files/content", api.FetchFileContent)
		clients.DELETE("/:uid", api.ExitClient)
		clients.PUT("/:uid/sleep", api.EditSleep)
		clients.PUT("/:uid/color", api.EditColor)
	}

	// 生成器
	generators := protected.Group("/generators")
	{
		generators.POST("/servers", api.GenServer)
		generators.GET("/listeners/active", api.ShowListener)
	}

	// 监听器管理
	listeners := protected.Group("/listeners")
	{
		listeners.POST("", api.AddListener)
		listeners.GET("", api.ListListener)
		listeners.PATCH("/:addr/status", api.UpdateListenerStatus)
		listeners.DELETE("/:addr", api.DeleteListener)
	}

	// WebDelivery 管理
	webDelivery := protected.Group("/webdelivery")
	{
		webDelivery.GET("", api.ListWebDelivery)
		webDelivery.POST("", api.StartWebDelivery)
		webDelivery.PATCH("/:port/status", api.UpdateWebDeliveryStatus)
		webDelivery.DELETE("/:port", api.DeleteWebDelivery)
	}

	// SOCKS5 管理
	socks5 := protected.Group("/clients/:uid/socks5")
	{
		socks5.GET("", api.Socks5List)
		socks5.POST("", api.Socks5Start)
		socks5.POST("/open", api.Socks5Open)
		socks5.POST("/close", api.Socks5Close)
		socks5.POST("/delete", api.Socks5Delete)
	}

	// 设置
	settings := protected.Group("/settings")
	{
		settings.GET("", api.ListSettings)
		settings.PUT("", api.EditSettings)
	}

	// 二进制执行
	protected.POST("/clients/:uid/bin/execute", api.ExecuteBin)
	protected.POST("/clients/:uid/bin/executelinuxscript", api.ExecuteLinuxScript)

	// Shellcode 生成
	shellcode := protected.Group("/shellcode")
	{
		shellcode.POST("/stage", api.StageShellCodeGen)
	}

	// 插件管理
	plugin := protected.Group("/plugins")
	{
		plugin.GET("", api.ListPlugins)
		plugin.POST("", api.AddPlugin)
		plugin.DELETE("/:id", api.DeletePlugin)
		plugin.POST("/:id/execute", api.ExecutePlugin)
	}

	// WebSocket认证token获取端点
	protected.GET("/ws/auth/:uid", api.GetWebSocketAuthToken)

	// 转发连接
	protected.POST("/forward-connection", api.ForwardConnect)

	// MCP Endpoints (Protected by JWT AuthMiddleware)
	mcpGroup := protected.Group("/mcp")
	mcpGroup.Use(mcp.MCPEnabledMiddleware())
	{
		mcpGroup.GET("/sse", mcp.HandleSSE)
		mcpGroup.POST("/message", mcp.HandleMessage)
	}

	// 注册 Swagger UI（仅开发模式）
	swagger.Register(r)

	mcp.GlobalEngine = r

	return r
}
