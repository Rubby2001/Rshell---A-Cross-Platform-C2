package api

import (
	"Rshell/pkg/middlewares"
	"Rshell/pkg/response"

	"github.com/gin-gonic/gin"
)

// ListSettings list all settings
// @Summary List settings
// @Tags Settings
// @Accept json
// @Produce json
// @Success 200 {object} object "success"
// @Router /api/v1/settings [get]
// @Security BearerAuth
func ListSettings(c *gin.Context) {
	svc := middlewares.GetServices(c)
	settings, err := svc.Settings.ListSettings()
	if err != nil {
		response.InternalError(c)
		return
	}
	response.OK(c, settings)
}

// EditSettings edit settings
// @Summary Edit settings
// @Tags Settings
// @Accept json
// @Produce json
// @Param body body object true "Array of {name: setting name, value: setting value}"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/settings [put]
// @Security BearerAuth
func EditSettings(c *gin.Context) {
	var settings []struct {
		Name  string `json:"name" binding:"required"`
		Value string `json:"value" binding:"required"`
	}
	if err := c.ShouldBindJSON(&settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Settings.UpdateSettings(settings); err != nil {
		response.InternalError(c)
		return
	}
	response.OK(c, nil)
}
