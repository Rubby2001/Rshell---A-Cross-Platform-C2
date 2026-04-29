package middlewares

/*
修改说明：
1. BasicAuthMiddleware 添加用户存在判断。
2. Token 添加合法性判断。
*/

import (
	"Rshell/pkg/common"
	"Rshell/pkg/database"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func BasicAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.Request.Header.Get("Authorization")

		if authHeader == "" || !strings.HasPrefix(authHeader, "Basic ") {
			c.Header("WWW-Authenticate", `Basic realm="Restricted"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		encodedCreds := authHeader[len("Basic "):]
		creds, err := base64.StdEncoding.DecodeString(encodedCreds)
		if err != nil {
			c.Header("WWW-Authenticate", `Basic realm="Restricted"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		credParts := strings.SplitN(string(creds), ":", 2)
		if len(credParts) != 2 {
			c.Header("WWW-Authenticate", `Basic realm="Restricted"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		user, pass := credParts[0], credParts[1]

		var userPass database.Users
		has, err := database.Engine.Where("username = ?", user).Get(&userPass)
		if err != nil || !has {
			c.Header("WWW-Authenticate", `Basic realm="Restricted"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		if userPass.Password != pass || userPass.Password == "" {
			c.Header("WWW-Authenticate", `Basic realm="Restricted"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("user", user)
		c.Next()
	}
}

// AuthMiddleware validates JWT from Token or query token.
func AuthMiddleware() gin.HandlerFunc {
        return func(c *gin.Context) {
                authHeader := strings.TrimSpace(c.GetHeader("Token"))
                // 支持URL查询参数传递token，方便SSE之类的客户端
                if authHeader == "" {
                        queryToken := c.Query("token")
                        if queryToken != "" {
                                authHeader = "Bearer " + queryToken
                        }
                }

		if authHeader == "" {
			c.String(http.StatusUnauthorized, "Token required")
			c.Abort()
			return
		}

		if len(authHeader) < len("Bearer ") || !strings.EqualFold(authHeader[:len("Bearer ")], "Bearer ") {
			c.String(http.StatusUnauthorized, "Invalid token format")
			c.Abort()
			return
		}

		tokenString := strings.TrimSpace(authHeader[len("Bearer "):])
		if tokenString == "" {
			c.String(http.StatusUnauthorized, "Token required")
			c.Abort()
			return
		}

		claims, err := common.ValidateJWT(tokenString)
		if err != nil {
			// MCP SDK 期望标准的文本错误，避免抛出解析异常
			c.String(http.StatusUnauthorized, "Invalid token")
			c.Abort()
			return
		}
		c.Set("username", claims.Username)
		c.Next()
	}
}
