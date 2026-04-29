//go:build !dev

package swagger

import "github.com/gin-gonic/gin"

// Register 生产模式为空实现，不注册 Swagger UI
func Register(r *gin.Engine) {
	// 生产环境不提供 Swagger UI
}
