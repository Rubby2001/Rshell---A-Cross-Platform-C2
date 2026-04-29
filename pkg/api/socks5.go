package api

import (
	"Rshell/pkg/middlewares"
	"Rshell/pkg/response"

	"github.com/gin-gonic/gin"
)

// Socks5List list SOCKS5 proxies for a client
// @Summary List SOCKS5 proxies
// @Tags SOCKS5
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/socks5 [get]
// @Security BearerAuth
func Socks5List(c *gin.Context) {
	uid := c.Param("uid")
	svc := middlewares.GetServices(c)
	socks5, err := svc.Socks5.ListSocks5(uid)
	if err != nil {
		response.InternalError(c)
		return
	}
	response.OK(c, socks5)
}

// Socks5Start start a SOCKS5 proxy
// @Summary Start SOCKS5 proxy
// @Tags SOCKS5
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param body body object true "{Socks5port: port, UserName: username, Password: password}"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/socks5 [post]
// @Security BearerAuth
func Socks5Start(c *gin.Context) {
	uid := c.Param("uid")
	var socks5Body struct {
		Socks5port string `json:"Socks5port" binding:"required"`
		UserName   string `json:"UserName" binding:"required"`
		Password   string `json:"Password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&socks5Body); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Socks5.StartSocks5(uid, socks5Body.Socks5port, socks5Body.UserName, socks5Body.Password); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, "socks5 started")
}

// Socks5Open open a SOCKS5 proxy
// @Summary Open SOCKS5 proxy
// @Tags SOCKS5
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param body body object true "{Socks5port: port, UserName: username, Password: password}"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/socks5/open [post]
// @Security BearerAuth
func Socks5Open(c *gin.Context) {
	uid := c.Param("uid")
	var socks5Body struct {
		Socks5port string `json:"Socks5port" binding:"required"`
		UserName   string `json:"UserName" binding:"required"`
		Password   string `json:"Password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&socks5Body); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Socks5.OpenSocks5(uid, socks5Body.Socks5port, socks5Body.UserName, socks5Body.Password); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, "socks5 started")
}

// Socks5Close close a SOCKS5 proxy
// @Summary Close SOCKS5 proxy
// @Tags SOCKS5
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param body body object true "{Socks5port: port, UserName: username, Password: password}"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/socks5/close [post]
// @Security BearerAuth
func Socks5Close(c *gin.Context) {
	uid := c.Param("uid")
	var socks5Body struct {
		Socks5port string `json:"Socks5port" binding:"required"`
		UserName   string `json:"UserName" binding:"required"`
		Password   string `json:"Password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&socks5Body); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Socks5.CloseSocks5(uid, socks5Body.Socks5port); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, "socks5 closed")
}

// Socks5Delete delete a SOCKS5 proxy
// @Summary Delete SOCKS5 proxy
// @Tags SOCKS5
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param body body object true "{Socks5port: port, UserName: username, Password: password}"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/socks5 [delete]
// @Security BearerAuth
func Socks5Delete(c *gin.Context) {
	uid := c.Param("uid")
	var socks5Body struct {
		Socks5port string `json:"Socks5port" binding:"required"`
		UserName   string `json:"UserName" binding:"required"`
		Password   string `json:"Password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&socks5Body); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Socks5.DeleteSocks5(uid, socks5Body.Socks5port); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, "socks5 deleted")
}
