package api

import (
	"Rshell/pkg/database"
	"Rshell/pkg/response"

	"github.com/gin-gonic/gin"
)

var settings []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func ListSettings(c *gin.Context) {
	var settings []database.Settings
	database.Engine.Find(&settings)
	response.OK(c, settings)
}
func EditSettings(c *gin.Context) {
	if err := c.BindJSON(&settings); err != nil {
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
