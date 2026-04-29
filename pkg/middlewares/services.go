package middlewares

import (
	"Rshell/internal/service"

	"github.com/gin-gonic/gin"
)

// ServicesKey is the gin.Context key for the Services instance.
const ServicesKey = "services"

// ServicesMiddleware injects the Services instance into gin.Context.
func ServicesMiddleware(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ServicesKey, svc)
		c.Next()
	}
}

// GetServices retrieves the Services instance from gin.Context.
func GetServices(c *gin.Context) *service.Services {
	return c.MustGet(ServicesKey).(*service.Services)
}
