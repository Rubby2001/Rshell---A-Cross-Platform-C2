package api

import (
	"Rshell/internal/service"
	"Rshell/pkg/middlewares"
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
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&loginData); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	svc := middlewares.GetServices(c)
	token, ok, err := svc.Auth.Login(loginData.Username, loginData.Password)
	if err != nil {
		response.InternalError(c)
		return
	}
	if !ok {
		response.Unauthorized(c)
		return
	}
	response.OK(c, gin.H{
		"token":    token,
		"username": loginData.Username,
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
		OldPassword string `form:"old_password" binding:"required"`
		NewPassword string `form:"new_password" binding:"required"`
	}
	if err := c.ShouldBind(&passwordData); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	username := c.MustGet("username").(string)
	svc := middlewares.GetServices(c)
	if err := svc.Auth.ChangePassword(username, passwordData.OldPassword, passwordData.NewPassword); err != nil {
		if _, ok := err.(*service.PasswordError); ok {
			response.BadRequest(c, "password changed failed")
		} else {
			response.InternalError(c)
		}
		return
	}
	response.OK(c, nil)
}
