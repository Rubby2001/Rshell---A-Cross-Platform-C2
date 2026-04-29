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
	"Rshell/internal/service"
	"Rshell/pkg/cert"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/logger"
	"Rshell/pkg/mcp"
	"Rshell/pkg/routers"
	"Rshell/pkg/utils"
	"crypto/tls"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

//go:embed dist
var embedFS embed.FS

// tlsErrorDiscarder is an io.Writer that silently discards TLS handshake error logs.
type tlsErrorDiscarder struct{}

func (tlsErrorDiscarder) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "TLS handshake error") {
		return len(p), nil
	}
	os.Stderr.Write(p)
	return len(p), nil
}

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
	svc := service.NewServices()
	r := routers.NewRouter(embedFS, staticFs, svc)

	addr := "0.0.0.0:" + strconv.Itoa(*bindPort)
	logger.Info("Listening on port " + strconv.Itoa(*bindPort))

	var err error
	switch {
	case *certPath != "" && *keyPath != "":
		logger.Info("TLS mode: using custom certificate")
		err = r.RunTLS(addr, *certPath, *keyPath)
	case *enableTLS:
		logger.Info("TLS mode: using self-signed certificate")
		certPEM, keyPEM, genErr := cert.GenerateSelfSignedCert()
		if genErr != nil {
			logger.Error("Failed to generate self-signed cert: " + genErr.Error())
			os.Exit(1)
		}
		cert, certErr := tls.X509KeyPair(certPEM, keyPEM)
		if certErr != nil {
			logger.Error("Failed to parse certificate: " + certErr.Error())
			os.Exit(1)
		}
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h2", "http/1.1"},
		}
		listener, listenErr := net.Listen("tcp", addr)
		if listenErr != nil {
			logger.Error("Failed to listen: " + listenErr.Error())
			os.Exit(1)
		}
		tlsListener := tls.NewListener(listener, tlsConfig)
		srv := &http.Server{
			Handler: r,
			ErrorLog: log.New(&tlsErrorDiscarder{}, "", 0),
		}
		err = srv.Serve(tlsListener)
	default:
		err = r.Run(addr)
	}

	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}
