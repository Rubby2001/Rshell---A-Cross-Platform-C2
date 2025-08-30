package api

import (
	"BackendTemplate/pkg/database"
	"net/http"

	"github.com/gin-gonic/gin"
)

var settings []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func ListSettings(c *gin.Context) {
	var settings []database.Settings
	database.Engine.Find(&settings)
	c.JSON(http.StatusOK, gin.H{"status": 200, "data": settings})
}
func EditSettings(c *gin.Context) {
	if err := c.BindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for _, setting := range settings {
		data := database.Settings{
			Name:  setting.Name,
			Value: setting.Value,
		}
		database.Engine.Where("name = ?", setting.Name).Update(&data)
	}
	c.JSON(http.StatusOK, gin.H{"status": 200, "data": "", "msg": "ok"})
}
