package api

import (
	"Rshell/pkg/database"
	"Rshell/pkg/response"
	"Rshell/pkg/service"
	"strings"

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
	var webs []database.WebDelivery
	database.Engine.Find(&webs)
	response.OK(c, webs)
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

	var w database.WebDelivery
	if exist, _ := database.Engine.Where("listening_port = ?", web.Port).Exist(&w); exist {
		response.BadRequest(c, web.Port+"端口已被配置")
		return
	}

	if err := service.StartWebDeliveryServer(web.Port, web.OS, web.Arch, web.Listener, web.Pass, web.Filename); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	tmp := strings.Split(strings.Split(web.Listener, "://")[1], ":")
	database.Engine.Insert(&database.WebDelivery{
		ListenerConfig: web.Listener,
		OS:             web.OS,
		Arch:           web.Arch,
		ListeningPort:  web.Port,
		Status:         1,
		FileName:       web.Filename,
		ServerAddress:  "http://" + tmp[0] + ":" + web.Port + "/" + web.Filename,
		Pass:           web.Pass,
	})

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

	var wd database.WebDelivery
	database.Engine.Where("listening_port = ?", port).Get(&wd)

	if body.Action == "open" {
		if err := service.RebuildWebDeliveryServer(port); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		database.Engine.Where("listening_port = ?", port).Update(&database.WebDelivery{Status: 1})
		response.OK(c, nil)
	} else {
		if err := service.StopWebDeliveryServer(port); err != nil {
			response.BadRequest(c, "Listener closed failed")
			return
		}
		database.Engine.Where("listening_port = ?", port).Update(&database.WebDelivery{Status: 2})
		response.OK(c, nil)
	}
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

	var webdelivery database.WebDelivery
	database.Engine.Where("listening_port = ?", port).Get(&webdelivery)
	if webdelivery.Status == 1 {
		if err := service.DeleteWebDeliveryServer(port); err != nil {
			response.BadRequest(c, "Listener closed failed")
			return
		}
	}
	database.Engine.Where("listening_port = ?", port).Delete(&database.WebDelivery{})
	response.OK(c, nil)
}
