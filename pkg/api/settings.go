package api

import (
	"Rshell/pkg/database"
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
	var settings []database.Settings
	database.Engine.Find(&settings)
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
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := c.ShouldBindJSON(&settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	for _, setting := range settings {
		data := database.Settings{
			Name:  setting.Name,
			Value: setting.Value,
		}
		database.Engine.Where("name = ?", setting.Name).Update(&data)
	}
	response.OK(c, nil)
}
