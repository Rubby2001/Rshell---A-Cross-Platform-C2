package api

import (
	"Rshell/pkg/database"
	"Rshell/pkg/logger"
	"Rshell/pkg/proxy"
	"Rshell/pkg/response"
	"fmt"
	"github.com/gin-gonic/gin"
	"net"
)

func Socks5List(c *gin.Context) {
	uid := c.Param("uid")
	var socks5 []database.Socks5
	database.Engine.Where("uid = ?", uid).Find(&socks5)
	response.OK(c, socks5)
}
func Socks5Start(c *gin.Context) {
	uid := c.Param("uid")
	var socks5Body struct {
		Socks5port string `json:"Socks5port"`
		UserName   string `json:"UserName"`
		Password   string `json:"Password"`
	}
	if err := c.ShouldBindJSON(&socks5Body); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	inUse, err := isPortInUse(socks5Body.Socks5port)
	if err != nil {
		logger.Error("检测端口时发生错误: ", socks5Body.Socks5port, err)
		return
	}
	if inUse {
		response.BadRequest(c, socks5Body.Socks5port+"port already in use")
		return
	}

	database.Engine.Insert(&database.Socks5{Type: "socks5", Uid: uid, Socks5port: socks5Body.Socks5port, UserName: socks5Body.UserName, Password: socks5Body.Password, Status: 1})

	go proxy.StartSocks5Proxy(socks5Body.Socks5port, uid, socks5Body.UserName, socks5Body.Password)
	response.OK(c, "socks5 started")
}
func Socks5Open(c *gin.Context) {
	uid := c.Param("uid")
	var socks5Body struct {
		Socks5port string `json:"Socks5port"`
		UserName   string `json:"UserName"`
		Password   string `json:"Password"`
	}
	if err := c.ShouldBindJSON(&socks5Body); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	inUse, err := isPortInUse(socks5Body.Socks5port)
	if err != nil {
		logger.Error("检测端口时发生错误:", socks5Body.Socks5port, err)
	}
	if inUse {
		response.BadRequest(c, socks5Body.Socks5port+"port already in use")
		return
	}
	database.Engine.Where("uid = ? AND socks5port = ? AND user_name = ? AND password = ?", uid, socks5Body.Socks5port, socks5Body.UserName, socks5Body.Password).Update(&database.Socks5{Status: 1})
	go proxy.StartSocks5Proxy(socks5Body.Socks5port, uid, socks5Body.UserName, socks5Body.Password)
	response.OK(c, "socks5 started")
}
func Socks5Close(c *gin.Context) {
	uid := c.Param("uid")
	var socks5Body struct {
		Socks5port string `json:"Socks5port"`
		UserName   string `json:"UserName"`
		Password   string `json:"Password"`
	}
	if err := c.ShouldBindJSON(&socks5Body); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	database.Engine.Where("uid = ? AND socks5port = ? AND user_name = ? AND password = ?", uid, socks5Body.Socks5port, socks5Body.UserName, socks5Body.Password).Update(&database.Socks5{Status: 2})

	if _, exists := proxy.Socks5Serve[socks5Body.Socks5port]; exists {
		err := proxy.Socks5Serve[socks5Body.Socks5port].Close()
		proxy.MuSocks5Serve.Lock()
		delete(proxy.Socks5Serve, socks5Body.Socks5port)
		proxy.MuSocks5Serve.Unlock()
		if err != nil {
			response.BadRequest(c, "socks5 close failed")
			return
		}
	}

	response.OK(c, "socks5 closed")
}
func Socks5Delete(c *gin.Context) {
	uid := c.Param("uid")
	var socks5Body struct {
		Socks5port string `json:"Socks5port"`
		UserName   string `json:"UserName"`
		Password   string `json:"Password"`
	}
	if err := c.ShouldBindJSON(&socks5Body); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	var s database.Socks5
	database.Engine.Where("uid = ? AND socks5port = ? AND user_name = ? AND password = ?", uid, socks5Body.Socks5port, socks5Body.UserName, socks5Body.Password).Get(&s)
	if s.Status == 1 {
		if _, exists := proxy.Socks5Serve[socks5Body.Socks5port]; exists {
			err := proxy.Socks5Serve[socks5Body.Socks5port].Close()
			proxy.MuSocks5Serve.Lock()
			delete(proxy.Socks5Serve, socks5Body.Socks5port)
			proxy.MuSocks5Serve.Unlock()
			if err != nil {
				response.BadRequest(c, "socks5 close failed")
				return
			}
		}
	}
	database.Engine.Where("uid = ? AND socks5port = ? AND user_name = ? AND password = ?", uid, socks5Body.Socks5port, socks5Body.UserName, socks5Body.Password).Delete(&database.Socks5{})
	response.OK(c, "socks5 deleted")
}

// isPortInUse 检测指定端口是否被占用
func isPortInUse(port string) (bool, error) {
	// 尝试监听该端口
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		// 如果监听失败，判断是否是端口被占用
		if opErr, ok := err.(*net.OpError); ok {
			if opErr.Err.Error() == "bind: address already in use" ||
				opErr.Err.Error() == "listen tcp :"+fmt.Sprintf("%s", port)+": bind: Only one usage of each socket address (protocol/network address/port) is normally permitted." {
				return true, nil // 端口被占用
			}
		}
		return false, err // 其他错误
	}

	// 如果监听成功，关闭 listener 并返回未占用
	_ = listener.Close()
	return false, nil
}
