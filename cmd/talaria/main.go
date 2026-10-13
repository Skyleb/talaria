// Talaria — a portable, self-hosted chat server. One binary, no install.
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
	"syscall"
	"time"

	"github.com/skyleb/talaria/internal/api"
	"github.com/skyleb/talaria/internal/auth"
	"github.com/skyleb/talaria/internal/blob"
	"github.com/skyleb/talaria/internal/config"
	"github.com/skyleb/talaria/internal/ddns"
	"github.com/skyleb/talaria/internal/ddnshost"
	"github.com/skyleb/talaria/internal/gateway"
	"github.com/skyleb/talaria/internal/lanaccess"
	"github.com/skyleb/talaria/internal/landns"
	"github.com/skyleb/talaria/internal/listen"
	"github.com/skyleb/talaria/internal/push"
	"github.com/skyleb/talaria/internal/rtc"
	"github.com/skyleb/talaria/internal/store"
	"github.com/skyleb/talaria/internal/tlsx"
)

var version = "dev"

// migrateLegacyFile renames old -> new when old exists and new doesn't.
func migrateLegacyFile(old, new string, log *slog.Logger) {
	if _, err := os.Stat(new); err == nil {
		return
	}
	if _, err := os.Stat(old); err != nil {
		return
	}
	if err := os.Rename(old, new); err != nil {
		log.Error("migrate legacy file", "from", old, "to", new, "err", err)
	} else {
		log.Info("migrated legacy file", "from", filepath.Base(old), "to", filepath.Base(new))
	}
}

func main() {
	if isWindowsService() {
		runWindowsService(run)
		return
	}
	run(signalStop())
}

// signalStop returns a channel that closes on Ctrl-C / SIGTERM.
func signalStop() <-chan struct{} {
	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		close(stop)
	}()
	return stop
}

// run starts everything and returns once stop closes (or a listener fails).
func run(stop <-chan struct{}) {
	configPath := flag.String("config", config.DefaultPath(), "path to talaria.toml")
	showVersion := flag.Bool("version", false, "print version and exit")
	svcCmd := flag.String("service", "", "Windows service control: install | uninstall | start | stop")
	flag.Parse()
	if *showVersion {
		fmt.Println("talaria", version)
		return
	}
	if *svcCmd != "" {
		if err := serviceControl(*svcCmd, *configPath); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Rebrand migration (Lantern -> Talaria): adopt old config and database
	// files if the new names don't exist yet.
	migrateLegacyFile(filepath.Join(filepath.Dir(*configPath), "lantern.toml"), *configPath, log)

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
	log.Info("starting talaria", "version", version, "config", *configPath, "data", dataDir)

	for _, suffix := range []string{"", "-wal", "-shm"} {
		migrateLegacyFile(
			filepath.Join(dataDir, "lantern.db"+suffix),
			filepath.Join(dataDir, "talaria.db"+suffix), log)
	}

	st, err := store.Open(filepath.Join(dataDir, "talaria.db"))
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

	// Phase 2: the embedded SFU for voice/video/screen share. The public IP
	// (for remote callers' ICE) comes from config, else the DDNS domain —
	// resolved on the wire (landns.ResolveA): the OS resolver may be fed by
	// a hosts-file entry or split DNS pointing the domain at the LAN IP,
	// and advertising that gives remote callers no reachable candidate.
	publicIP := cfg.WebRTC.PublicIP
	if publicIP == "" && cfg.TLS.Domain != "" {
		if ips, err := landns.ResolveA(cfg.TLS.Domain, []string{"1.1.1.1:53", "8.8.8.8:53"}); err == nil {
			for _, ip := range ips {
				if v4 := ip.To4(); v4 != nil && !v4.IsPrivate() && !v4.IsLoopback() {
					publicIP = v4.String()
					break
				}
			}
		}
	}
	if publicIP != "" {
		log.Info("webrtc advertising public ip", "ip", publicIP)
	} else {
		log.Warn("webrtc has no public ip; calls will only work on the LAN")
	}
	var srvCalls api.CallCaps
	sfu, err := rtc.NewManager(st, hub, log, cfg.WebRTC.UDPPort, publicIP, cfg.WebRTC.MaxDownlinkKbps)
	if err != nil {
		log.Error("start webrtc sfu (voice/video disabled)", "err", err)
	} else {
		sfu.SetCaps(cfg.WebRTC.MaxCamKbps, cfg.WebRTC.MaxScreenKbps)
		hub.SetCallHandler(sfu)
		srvCalls = sfu
	}

	srv := &api.Server{
		Store: st, Hub: hub, Auth: authMgr, Blobs: blobs,
		Config: cfg, Log: log, Version: version,
	}
	srv.Calls = srvCalls
	srv.StartAttachmentGC()
	startSessionGC(st, log)

	// Phase 4: synchronized listening sessions — now part of voice calls.
	srv.Listen = listen.New(st, hub, log)
	hub.SetListenProvider(srv.Listen)
	if sfu != nil {
		srv.Listen.SetCallGate(sfu)
		sfu.SetSessionHooks(srv.Listen)
	}

	// Web Push: VAPID keys live in the database, generated on first run.
	if sender, err := push.New(context.Background(), st, log, pushContact(cfg)); err != nil {
		log.Error("web push disabled", "err", err)
	} else {
		srv.Push = sender
		if sfu != nil {
			sfu.SetRingNotifier(srv.NotifyRing)
		}
	}

	// Built-in dynamic-DNS updater: keeps the hostname pointed at this
	// connection without a separate updater client. The runner lets the
	// admin settings UI reconfigure DDNS at runtime without a restart.
	ddnsRunner := ddns.NewRunner(log)
	defer ddnsRunner.Stop()
	srv.DDNS = ddnsRunner
	if cfg.DDNS.Enabled {
		domain := cfg.DDNS.Domain
		if domain == "" {
			domain = cfg.TLS.Domain
		}
		dcfg := ddns.Config{
			Provider: cfg.DDNS.Provider,
			Domain:   domain,
			Token:    cfg.DDNS.Token,
			Secret:   cfg.DDNS.Secret,
			URL:      cfg.DDNS.URL,
			Interval: time.Duration(cfg.DDNS.IntervalMinutes) * time.Minute,
		}
		if domain == "" {
			log.Error("ddns enabled but no domain set; updater not started")
		} else if err := ddns.Validate(dcfg); err != nil {
			log.Error("ddns enabled but misconfigured; updater not started", "err", err)
		} else {
			ddnsRunner.Set(dcfg)
		}
	}

	// LAN DNS (optional, toml-only): answers this server's domain with its
	// LAN IP for devices whose DNS points at this machine.
	lanDNS := landns.NewRunner(log)
	defer lanDNS.Stop()
	srv.LanDNS = lanDNS
	if cfg.LanDNS.Enabled {
		lcfg, err := landns.FromAppConfig(cfg.LanDNS, cfg.DDNS.Domain, cfg.TLS.Domain)
		if err == nil {
			err = lanDNS.Set(lcfg)
		}
		if err != nil {
			log.Error("lan dns not started", "err", err)
		}
	}

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

	// LAN access: a second domain pointing at this machine's LAN IP, with
	// its own DNS-01 certificate. Devices hitting the main domain through
	// the router's NAT loopback get redirected to it. Runtime-reconfigurable
	// from the admin settings UI.
	lanAccess := lanaccess.NewRunner(log)
	defer lanAccess.Stop()
	srv.LanAccess = lanAccess
	if cfg.LanAccess.Enabled {
		if !useACME {
			log.Error("lan_access requires HTTPS with a domain (tls mode auto/acme); not started")
		} else {
			token := cfg.LanAccess.Token
			if token == "" {
				token = cfg.DDNS.Token
			}
			secret := cfg.LanAccess.Secret
			if secret == "" {
				secret = cfg.DDNS.Secret
			}
			if err := lanAccess.Set(lanaccess.Config{
				Domain: cfg.LanAccess.Domain, Provider: cfg.DDNS.Provider,
				Token: token, Secret: secret, URL: cfg.DDNS.URL,
				LanIP: cfg.LanAccess.LanIP, DataDir: dataDir,
			}); err != nil {
				log.Error("lan access not started", "err", err)
			}
		}
	}

	// Hosted dynamic DNS: issue names under our zone to other Talaria
	// servers. Runtime-reconfigurable from the admin settings UI.
	srv.DDNSHost = ddnshost.New(st, log)
	if cfg.DDNSHost.Enabled {
		if err := srv.DDNSHost.Set(api.DDNSHostConfig(cfg)); err != nil {
			log.Error("hosted ddns not started", "err", err)
		}
	}

	handler := srv.Router()

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
		// SNI dispatch: the lan domain's DNS-01 cert when asked for it,
		// else autocert's HTTP-01 cert for the main domain.
		getCert := func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if lanAccess.Matches(hello.ServerName) {
				return lanAccess.Certificate()
			}
			return m.GetCertificate(hello)
		}
		tlsSrv := &http.Server{
			Addr:      ":" + strconv.Itoa(cfg.TLS.HTTPSPort),
			Handler:   handler,
			TLSConfig: &tls.Config{GetCertificate: getCert, MinVersion: tls.VersionTLS12},
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

	// First run: open the browser at the setup wizard (not when running as
	// a service; there is no desktop to open it on).
	if cfg.Server.OpenBrowser && !isWindowsService() {
		if done, _ := st.GetSetting(context.Background(), "setup_complete"); done != "true" {
			openBrowser(fmt.Sprintf("http://localhost:%d/setup", cfg.Server.HTTPPort))
		}
	}

	select {
	case err := <-errCh:
		log.Error("server error", "err", err)
		// Under the Windows SCM a non-zero exit triggers the recovery restart.
		defer os.Exit(1)
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
	fmt.Println("  Talaria is running!")
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

// pushContact is the VAPID subject push services may use to reach the
// operator: the ACME email when set, else the server's own https URL.
func pushContact(cfg *config.Config) string {
	if cfg.TLS.ACMEEmail != "" {
		return "mailto:" + cfg.TLS.ACMEEmail
	}
	if cfg.TLS.Domain != "" {
		return "https://" + cfg.TLS.Domain
	}
	return ""
}
