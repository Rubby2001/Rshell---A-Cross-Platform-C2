package main

//	@title			RShell API
//	@version		1.0
//	@description	Cross-platform C2 framework RESTful API
//	@BasePath		/api/v1
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Token
//	@security	BearerAuth

import (
	"Rshell/pkg/cert"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/logger"
	"Rshell/pkg/mcp"
	"Rshell/pkg/routers"
	"Rshell/pkg/utils"
	"bufio"
	"crypto/tls"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

//go:embed dist
var embedFS embed.FS

func main() {
	utils.InitFunction()

	if len(os.Args) >= 2 && os.Args[1] == "mcp" {
		database.ConnectDateBase()
		defer database.Engine.Close()
		mcp.InitMCP()
		mcp.StartStdioServer()
		return
	}

	var bindPort = flag.Int("p", 8089, "Specify alternate port")
	enableTLS := flag.Bool("tls", false, "Enable self-signed HTTPS")
	certPath := flag.String("tls-cert", "", "TLS certificate file path")
	keyPath := flag.String("tls-key", "", "TLS key file path")
	flag.Parse()

	if *bindPort > 65535 || *bindPort < 0 {
		flag.Usage()
		os.Exit(0)
	}

	database.ConnectDateBase()
	defer database.Engine.Close()
	encrypt.GenerateKeyPair()

	mcp.InitMCP()
	database.Engine.Update(&database.Clients{Online: "2"})
	database.Engine.Update(&database.Listener{Status: 2})
	database.Engine.Update(&database.Socks5{Status: 2})
	database.Engine.Update(&database.WebDelivery{Status: 2})

	distFS, _ := fs.Sub(embedFS, "dist")
	staticFs, _ := fs.Sub(distFS, "static")
	r := routers.NewRouter(embedFS, staticFs)

	addr := "0.0.0.0:" + strconv.Itoa(*bindPort)
	logger.Info("Listening on port " + strconv.Itoa(*bindPort))

	var err error
	switch {
	case *certPath != "" && *keyPath != "":
		logger.Info("TLS mode: using custom certificate, HTTP→HTTPS redirect on same port")
		err = runTLSWithRedirect(r, addr, *certPath, *keyPath)
	case *enableTLS:
		logger.Info("TLS mode: using self-signed certificate, HTTP→HTTPS redirect on same port")
		certPEM, keyPEM, genErr := cert.GenerateSelfSignedCert()
		if genErr != nil {
			logger.Error("Failed to generate self-signed cert: " + genErr.Error())
			os.Exit(1)
		}
		err = runTLSWithRedirect(r, addr, string(certPEM), string(keyPEM))
	default:
		err = r.Run(addr)
	}

	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

// peekListener 单端口复用：通过首字节判断 TLS(0x16) 还是 HTTP
type peekListener struct {
	net.Listener
	tlsConfig *tls.Config
	handler   http.Handler
}

func (l *peekListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	br := bufio.NewReader(conn)
	firstByte, err := br.Peek(1)
	if err != nil {
		conn.Close()
		return nil, err
	}

	if firstByte[0] == 0x16 {
		// TLS handshake → wrap with TLS
		tlsConn := tls.Server(&readWrapper{br, conn}, l.tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			tlsConn.Close()
			return nil, err
		}
		return tlsConn, nil
	}

	// HTTP request → return redirect connection
	return &redirectConn{br, conn}, nil
}

// readWrapper 让 bufio.Reader + 原始 conn 组合实现 net.Conn
type readWrapper struct {
	br   *bufio.Reader
	conn net.Conn
}

func (r *readWrapper) Read(b []byte) (int, error)     { return r.br.Read(b) }
func (r *readWrapper) Write(b []byte) (int, error)    { return r.conn.Write(b) }
func (r *readWrapper) Close() error                   { return r.conn.Close() }
func (r *readWrapper) LocalAddr() net.Addr            { return r.conn.LocalAddr() }
func (r *readWrapper) RemoteAddr() net.Addr           { return r.conn.RemoteAddr() }
func (r *readWrapper) SetDeadline(t time.Time) error  { return r.conn.SetDeadline(t) }
func (r *readWrapper) SetReadDeadline(t time.Time) error {
	return r.conn.SetReadDeadline(t)
}
func (r *readWrapper) SetWriteDeadline(t time.Time) error {
	return r.conn.SetWriteDeadline(t)
}

// redirectConn 对 HTTP 连接返回 302 重定向
type redirectConn struct {
	br   *bufio.Reader
	conn net.Conn
}

func (r *redirectConn) Read(b []byte) (int, error) {
	n, err := r.br.Read(b)
	if n == 0 && err != nil {
		return 0, err
	}
	// 写入 302 响应
	resp := "HTTP/1.1 302 Found\r\nLocation: https://" + r.conn.LocalAddr().String() + "\r\nConnection: close\r\n\r\n"
	r.conn.Write([]byte(resp))
	r.conn.Close()
	return n, nil
}
func (r *redirectConn) Write(b []byte) (int, error)    { return 0, net.ErrClosed }
func (r *redirectConn) Close() error                   { return r.conn.Close() }
func (r *redirectConn) LocalAddr() net.Addr            { return r.conn.LocalAddr() }
func (r *redirectConn) RemoteAddr() net.Addr           { return r.conn.RemoteAddr() }
func (r *redirectConn) SetDeadline(t time.Time) error  { return r.conn.SetDeadline(t) }
func (r *redirectConn) SetReadDeadline(t time.Time) error {
	return r.conn.SetReadDeadline(t)
}
func (r *redirectConn) SetWriteDeadline(t time.Time) error {
	return r.conn.SetWriteDeadline(t)
}

func runTLSWithRedirect(handler http.Handler, addr, certPEM, keyPEM string) error {
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	pl := &peekListener{
		Listener:  listener,
		tlsConfig: tlsConfig,
		handler:   handler,
	}

	return http.Serve(pl, handler)
}
