package api

/*
修改说明：
1.  ChangePasswordHandler 添加用户存在判断。
2. LoginHandler 添加用户存在判断。
*/

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

	// 处理密码修改逻辑
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

//
//// 获取用户信息处理函数
//func GetUserInfoHandler(c *gin.Context) {
//	// 获取用户名从上下文中
//	username := c.MustGet("username").(string)
//
//	var user database.Users
//	_, err := database.Engine.Where("username = ?", username).Get(&user)
//	if err != nil {
//		return
//	}
//	userInfo := gin.H{
//		"username":    username,
//		"permissions": user.Permissions, // 示例：1表示管理员
//		"phone":       user.Phone,
//	}
//	c.JSON(http.StatusOK, gin.H{"code": 200, "data": userInfo})
//}
//
//// User 返回给客户端的结构
//type User struct {
//	ID          string `json:"id"` // 使用 username 作为 ID
//	Username    string `json:"username"`
//	Permissions int    `json:"permissions"`
//	Phone       string `json:"phone"`
//}
//
//func GetUserListHandler(c *gin.Context) {
//	var query struct {
//		Page     int    `form:"page"`
//		PageSize int    `form:"page_size"`
//		Search   string `form:"search"`
//	}
//	if err := c.ShouldBindQuery(&query); err != nil {
//		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid query parameters"})
//		return
//	}
//
//	// 默认分页值
//	if query.Page == 0 {
//		query.Page = 1
//	}
//	if query.PageSize == 0 {
//		query.PageSize = 10
//	}
//
//	// 构建查询条件
//	session := database.Engine.NewSession()
//	defer session.Close()
//
//	if query.Search != "" {
//		// 模糊查询 username
//		session = session.Where("username LIKE ? COLLATE NOCASE", "%"+query.Search+"%")
//	}
//
//	// 获取总记录数
//	total, err := session.Count(new(database.Users))
//	if err != nil {
//		log.Fatalf("获取总记录数失败: %v", err)
//		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count users"})
//		return
//	}
//	if query.Search != "" {
//		// 模糊查询 username
//		session = session.Where("username LIKE ? COLLATE NOCASE", "%"+query.Search+"%")
//	}
//	// 分页查询
//	users := []database.Users{}
//	err = session.Limit(query.PageSize, (query.Page-1)*query.PageSize).Find(&users)
//	if err != nil {
//		log.Fatalf("获取用户列表失败: %v", err)
//		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users"})
//		return
//	}
//	var UserList []User
//	for id, user := range users {
//		var user1 User
//		user1.ID = strconv.Itoa(id)
//		user1.Username = user.Username
//		user1.Permissions = user.Permissions
//		user1.Phone = user.Phone
//		UserList = append(UserList, user1)
//	}
//
//	// 返回用户列表和总数
//	c.JSON(http.StatusOK, gin.H{
//		"code": 200,
//		"data": gin.H{
//			"list":  UserList,
//			"total": total,
//		},
//	})
//}
//
//// 创建用户处理函数
//func CreateUserHandler(c *gin.Context) {
//	var userData struct {
//		Username      string `json:"username"`
//		Password      string `json:"password"`
//		PasswordAgain string `json:"password_again"`
//		Phone         string `json:"phone"`
//		Email         string `json:"email"`
//		Permissions   string `json:"permissions"`
//	}
//	if err := c.ShouldBindJSON(&userData); err != nil {
//		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
//		return
//	}
//	if userData.Password == userData.PasswordAgain {
//		var user database.Users
//		user.Username = userData.Username
//		user.Password = userData.Password
//		user.Permissions, _ = strconv.Atoi(userData.Permissions)
//		user.Phone = userData.Phone
//		user.Email = userData.Email
//
//		exists, _ := database.Engine.Where("username = ?", userData.Username).Exist(new(database.Users))
//		if !exists {
//			database.Engine.Insert(&user)
//			// 创建用户逻辑
//			c.JSON(http.StatusOK, gin.H{"code": 200, "data": "User created successfully"})
//		}
//	}
//
//}
//
//// 删除用户处理函数
//func DeleteUserHandler(c *gin.Context) {
//	var deleteData struct {
//		Username string `json:"username"`
//	}
//	if err := c.ShouldBindJSON(&deleteData); err != nil {
//		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
//		return
//	}
//	database.Engine.Where("username = ?", deleteData.Username).Delete(new(database.Users))
//	// 删除用户逻辑
//	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "User deleted successfully"})
//
//}
