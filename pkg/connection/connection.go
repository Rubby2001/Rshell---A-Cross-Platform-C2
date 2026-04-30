package connection

import (
	"github.com/xtaci/kcp-go/v5"
	"net"
	"net/http"
	"sync"
)

// Manager holds all connection-level global state.
type Manager struct {
	mu               sync.RWMutex
	listenerType     map[string]string // uid -> transport type
	httpServer       map[string]*http.Server
	tcpServer        map[string]net.Listener
	kcpServer        map[string]*kcp.Listener
	stopChan         map[string]chan bool
}

var GlobalManager = &Manager{
	listenerType: make(map[string]string),
	httpServer:   make(map[string]*http.Server),
	tcpServer:    make(map[string]net.Listener),
	kcpServer:    make(map[string]*kcp.Listener),
	stopChan:     make(map[string]chan bool),
}

func (m *Manager) SetListenerType(uid, typ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listenerType[uid] = typ
}

func (m *Manager) GetListenerType(uid string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.listenerType[uid]
	return v, ok
}

func (m *Manager) DeleteListenerType(uid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.listenerType, uid)
}

func (m *Manager) SetHTTPServer(addr string, s *http.Server) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.httpServer[addr] = s
}

func (m *Manager) GetHTTPServer(addr string) (*http.Server, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.httpServer[addr]
	return v, ok
}

func (m *Manager) DeleteHTTPServer(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.httpServer, addr)
}

func (m *Manager) SetTCPServer(addr string, l net.Listener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tcpServer[addr] = l
}

func (m *Manager) GetTCPServer(addr string) (net.Listener, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.tcpServer[addr]
	return v, ok
}

func (m *Manager) DeleteTCPServer(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tcpServer, addr)
}

func (m *Manager) SetKCPServer(addr string, l *kcp.Listener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kcpServer[addr] = l
}

func (m *Manager) GetKCPServer(addr string) (*kcp.Listener, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.kcpServer[addr]
	return v, ok
}

func (m *Manager) DeleteKCPServer(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.kcpServer, addr)
}

func (m *Manager) SetStopChan(addr string, ch chan bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopChan[addr] = ch
}

func (m *Manager) GetStopChan(addr string) (chan bool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.stopChan[addr]
	return v, ok
}

func (m *Manager) DeleteStopChan(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.stopChan, addr)
}

// Legacy compatibility aliases (deprecated, use GlobalManager methods).
var (
	MuClientListenerType sync.Mutex
	ClientListenerType   = GlobalManager.listenerType
	HttpServer           = GlobalManager.httpServer
	TCPServer            = GlobalManager.tcpServer
	KCPServer            = GlobalManager.kcpServer
	StopChan             = GlobalManager.stopChan
)
