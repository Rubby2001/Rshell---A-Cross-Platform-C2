package api

import (
	"Rshell/pkg/middlewares"
	"Rshell/pkg/response"

	"github.com/gin-gonic/gin"
)

// ListWebDelivery list web delivery configs
// @Summary List all web delivery configs
// @Tags WebDelivery
// @Accept json
// @Produce json
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/webdelivery [get]
// @Security BearerAuth
func ListWebDelivery(c *gin.Context) {
	svc := middlewares.GetServices(c)
	list, err := svc.WebDelivery.ListWebDelivery()
	if err != nil {
		response.InternalError(c)
		return
	}
	response.OK(c, list)
}

// StartWebDelivery start a web delivery
// @Summary Start web delivery
// @Tags WebDelivery
// @Accept json
// @Produce json
// @Param body body object{listener=string,os=string,arch=string,port=string,filename=string,pass=string} true "Web delivery configuration"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/webdelivery [post]
// @Security BearerAuth
func StartWebDelivery(c *gin.Context) {
	var web struct {
		Listener string `json:"listener" binding:"required"`
		OS       string `json:"os" binding:"required"`
		Arch     string `json:"arch" binding:"required"`
		Port     string `json:"port" binding:"required"`
		Filename string `json:"filename" binding:"required"`
		Pass     string `json:"pass" binding:"required"`
	}
	if err := c.ShouldBindJSON(&web); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.WebDelivery.StartWebDelivery(web.Listener, web.OS, web.Arch, web.Port, web.Filename, web.Pass); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, nil)
}

// UpdateWebDeliveryStatus 统一的 WebDelivery 状态处理
// @Summary Update web delivery status (open/close)
// @Tags WebDelivery
// @Accept json
// @Produce json
// @Param port path string true "Port"
// @Param body body object{action=string} true "Action: open or close"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/webdelivery/{port}/status [patch]
// @Security BearerAuth
func UpdateWebDeliveryStatus(c *gin.Context) {
	var body struct {
		Action string `json:"action" binding:"required"` // "open" or "close"
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	port := c.Param("port")
	if port == "" {
		response.BadRequest(c, "port is required")
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.WebDelivery.UpdateWebDeliveryStatus(port, body.Action); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, nil)
}

// DeleteWebDelivery 删除 WebDelivery
// @Summary Delete web delivery
// @Tags WebDelivery
// @Accept json
// @Produce json
// @Param port path string true "Port"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/webdelivery/{port} [delete]
// @Security BearerAuth
func DeleteWebDelivery(c *gin.Context) {
	port := c.Param("port")
	if port == "" {
		response.BadRequest(c, "port is required")
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.WebDelivery.DeleteWebDelivery(port); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, nil)
}
