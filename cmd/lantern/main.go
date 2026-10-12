// Lantern — a portable, self-hosted chat server. One binary, no install.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/skyle/lantern/internal/api"
	"github.com/skyle/lantern/internal/auth"
	"github.com/skyle/lantern/internal/blob"
	"github.com/skyle/lantern/internal/config"
	"github.com/skyle/lantern/internal/gateway"
	"github.com/skyle/lantern/internal/store"
	"github.com/skyle/lantern/internal/tlsx"
)

var version = "dev"

func main() {
	configPath := flag.String("config", config.DefaultPath(), "path to lantern.toml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("lantern", version)
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("load config", "err", err)
		os.Exit(1)
	}
	dataDir, err := cfg.ResolveDataDir()
	if err != nil {
		log.Error("resolve data dir", "err", err)
		os.Exit(1)
	}
	cfg.ResolvedDataDir = dataDir
	log.Info("starting lantern", "version", version, "config", *configPath, "data", dataDir)

	st, err := store.Open(filepath.Join(dataDir, "lantern.db"))
	if err != nil {
		log.Error("open database", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	blobs, err := blob.New(dataDir)
	if err != nil {
		log.Error("open blob store", "err", err)
		os.Exit(1)
	}

	authMgr := auth.NewManager(st)
	hub := gateway.NewHub(st, log)
	srv := &api.Server{
		Store: st, Hub: hub, Auth: authMgr, Blobs: blobs,
		Config: cfg, Log: log, Version: version,
	}
	srv.StartAttachmentGC()
	startSessionGC(st, log)
	handler := srv.Router()

	// Decide the serving mode.
	useACME := false
	useSelfSigned := false
	switch cfg.TLS.Mode {
	case "acme":
		useACME = cfg.TLS.Domain != ""
		if !useACME {
			log.Error("tls.mode=acme requires tls.domain; falling back to plain HTTP")
		}
	case "selfsigned":
		useSelfSigned = true
	case "off":
	default: // "auto"
		useACME = cfg.TLS.Domain != ""
	}
	authMgr.Secure = useACME || useSelfSigned

	var servers []*http.Server
	errCh := make(chan error, 3)

	if useACME {
		m := tlsx.NewACMEManager(dataDir, cfg.TLS.Domain, cfg.TLS.ACMEEmail)
		challenge := tlsx.HTTPChallengeServer(m, cfg.TLS.Domain)
		servers = append(servers, challenge)
		go func() {
			log.Info("listening for ACME challenges + redirect", "addr", ":80")
			if err := challenge.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf(":80 listener (needed for Let's Encrypt): %w", err)
			}
		}()
		tlsSrv := &http.Server{
			Addr:      ":" + strconv.Itoa(cfg.TLS.HTTPSPort),
			Handler:   handler,
			TLSConfig: &tls.Config{GetCertificate: m.GetCertificate, MinVersion: tls.VersionTLS12},
		}
		servers = append(servers, tlsSrv)
		go func() {
			log.Info("listening", "addr", tlsSrv.Addr, "mode", "acme", "domain", cfg.TLS.Domain)
			if err := tlsSrv.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
	} else if useSelfSigned {
		cert, err := tlsx.SelfSigned(dataDir, cfg.TLS.Domain)
		if err != nil {
			log.Error("self-signed cert", "err", err)
			os.Exit(1)
		}
		tlsSrv := &http.Server{
			Addr:      ":" + strconv.Itoa(cfg.TLS.HTTPSPort),
			Handler:   handler,
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		}
		servers = append(servers, tlsSrv)
		go func() {
			log.Info("listening", "addr", tlsSrv.Addr, "mode", "selfsigned")
			log.Warn("browsers will warn about the self-signed certificate; set tls.domain for trusted HTTPS")
			if err := tlsSrv.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
	}

	// Plain HTTP listener: always on for localhost access (and the only
	// listener when TLS is off). getUserMedia works on localhost without TLS.
	plainAddr := ":" + strconv.Itoa(cfg.Server.HTTPPort)
	plain := &http.Server{Addr: plainAddr, Handler: handler}
	servers = append(servers, plain)
	go func() {
		log.Info("listening", "addr", plainAddr, "mode", "http")
		if err := plain.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	printWelcome(log, st, cfg)

	// First run: open the browser at the setup wizard.
	if cfg.Server.OpenBrowser {
		if done, _ := st.GetSetting(context.Background(), "setup_complete"); done != "true" {
			openBrowser(fmt.Sprintf("http://localhost:%d/setup", cfg.Server.HTTPPort))
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	select {
	case err := <-errCh:
		log.Error("server error", "err", err)
	case <-stop:
		log.Info("shutting down")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range servers {
		s.Shutdown(ctx)
	}
}

func startSessionGC(st *store.Store, log *slog.Logger) {
	go func() {
		t := time.NewTicker(6 * time.Hour)
		for range t.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := st.DeleteExpiredSessions(ctx, time.Now().UnixMilli()); err != nil {
				log.Error("session GC", "err", err)
			}
			cancel()
		}
	}()
}

func printWelcome(log *slog.Logger, st *store.Store, cfg *config.Config) {
	fmt.Println()
	fmt.Println("  Lantern is running!")
	fmt.Printf("    Local:   http://localhost:%d\n", cfg.Server.HTTPPort)
	if ip := lanIP(); ip != "" {
		fmt.Printf("    LAN:     http://%s:%d\n", ip, cfg.Server.HTTPPort)
	}
	if cfg.TLS.Domain != "" {
		fmt.Printf("    Remote:  https://%s\n", cfg.TLS.Domain)
	}
	fmt.Println()
}

func lanIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
}
