package api

import (
	"Rshell/pkg/common"
	"Rshell/pkg/database"
	"Rshell/pkg/response"

	"github.com/gin-gonic/gin"
)

// LoginHandler handles user login
// @Summary User login
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body object true "{username: string, password: string}"
// @Success 200 {object} object "success with token"
// @Failure 400 {object} object "bad request"
// @Failure 401 {object} object "unauthorized"
// @Router /api/v1/auth/login [post]
func LoginHandler(c *gin.Context) {
	var loginData struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&loginData); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	var users database.Users
	has, err := database.Engine.Where("username = ?", loginData.Username).Get(&users)
	if err != nil {
		response.InternalError(c)
		return
	}

	if !has || users.Password != loginData.Password {
		response.Unauthorized(c)
		return
	}

	token, err := common.GenerateJWT(loginData.Username)
	if err != nil {
		response.InternalError(c)
		return
	}
	response.OK(c, gin.H{
		"token":       token,
		"permissions": 1,
		"refresh":     "mock-refresh-token",
		"username":    loginData.Username,
	})
}

// LogoutHandler handles user logout
// @Summary User logout
// @Tags Auth
// @Accept json
// @Produce json
// @Success 200 {object} object "success"
// @Router /api/v1/auth/logout [post]
// @Security BearerAuth
func LogoutHandler(c *gin.Context) {
	response.OK(c, nil)
}

// ChangePasswordHandler handles password change
// @Summary Change user password
// @Tags Auth
// @Accept multipart/form-data
// @Produce json
// @Param old_password formData string true "Old password"
// @Param new_password formData string true "New password"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Failure 500 {object} object "internal server error"
// @Router /api/v1/auth/password [put]
// @Security BearerAuth
func ChangePasswordHandler(c *gin.Context) {
	var passwordData struct {
		OldPassword string `form:"old_password"`
		NewPassword string `form:"new_password"`
	}
	if err := c.ShouldBind(&passwordData); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	if passwordData.OldPassword != passwordData.NewPassword {
		username := c.MustGet("username").(string)
		var users database.Users
		has, err := database.Engine.Where("username = ?", username).Get(&users)
		if err != nil {
			response.InternalError(c)
			return
		}
		if !has {
			response.BadRequest(c, "password changed failed")
			return
		}
		if users.Password == passwordData.OldPassword {
			users.Password = passwordData.NewPassword
			affected, err := database.Engine.Where("username = ?", username).Cols("password").Update(&users)
			if err != nil {
				response.InternalError(c)
				return
			}
			if affected != 1 {
				response.InternalError(c)
				return
			}
			response.OK(c, nil)
		} else {
			response.BadRequest(c, "password changed failed")
		}
	} else {
		response.BadRequest(c, "password changed failed")
	}
}
