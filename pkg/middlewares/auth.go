package middlewares

import (
	"BackendTemplate/pkg/common"
	"BackendTemplate/pkg/database"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func BasicAuthMiddleware() gin.HandlerFunc {

	return func(c *gin.Context) {
		authHeader := c.Request.Header.Get("Authorization")

		if authHeader == "" || !strings.HasPrefix(authHeader, "Basic ") {
			// 返回WWW-Authenticate头，触发浏览器的弹框
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

		var user_pass database.Users
		database.Engine.Where("username = ?", user).Get(&user_pass)
		if user_pass.Password != pass || user_pass.Password == "" {
			c.Header("WWW-Authenticate", `Basic realm="Restricted"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("user", user)
		c.Next()
	}
}

// JWT 验证中间件
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Authorization2") == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token required"})
			c.Abort()
			return
		}
		tokenString := c.GetHeader("Authorization2")[len("Bearer "):]
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token required"})
			c.Abort()
			return
		}
		claims, err := common.ValidateJWT(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}
		c.Set("username", claims.Username)
		c.Next()
	}
}
