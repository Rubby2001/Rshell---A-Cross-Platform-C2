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

	// HTTP→HTTPS redirect server
	redirectServer := &http.Server{
		Addr: "0.0.0.0:" + strconv.Itoa(*bindPort-1),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := fmt.Sprintf("https://%s%s", r.Host, r.URL.RequestURI())
			http.Redirect(w, r, target, http.StatusFound)
		}),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	var err error
	switch {
	case *certPath != "" && *keyPath != "":
		logger.Info("TLS mode: using custom certificate")
		logger.Info("HTTP redirect on port " + strconv.Itoa(*bindPort-1))
		go func() {
			if redirectErr := redirectServer.ListenAndServe(); redirectErr != nil && redirectErr != http.ErrServerClosed {
				logger.Error("Redirect server error: " + redirectErr.Error())
			}
		}()
		err = r.RunTLS(addr, *certPath, *keyPath)
	case *enableTLS:
		logger.Info("TLS mode: using self-signed certificate")
		logger.Info("HTTP redirect on port " + strconv.Itoa(*bindPort-1))
		go func() {
			if redirectErr := redirectServer.ListenAndServe(); redirectErr != nil && redirectErr != http.ErrServerClosed {
				logger.Error("Redirect server error: " + redirectErr.Error())
			}
		}()
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
		tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
		listener, listenErr := net.Listen("tcp", addr)
		if listenErr != nil {
			logger.Error("Failed to listen: " + listenErr.Error())
			os.Exit(1)
		}
		tlsListener := tls.NewListener(listener, tlsConfig)
		err = http.Serve(tlsListener, r)
	default:
		err = r.Run(addr)
	}

	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}
