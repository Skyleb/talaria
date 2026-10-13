// Package config loads and persists Talaria's TOML configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server    ServerConfig    `toml:"server"`
	TLS       TLSConfig       `toml:"tls"`
	DDNS      DDNSConfig      `toml:"ddns"`
	LanAccess LanAccessConfig `toml:"lan_access"`
	LanDNS    LanDNSConfig    `toml:"lan_dns"`
	DDNSHost  DDNSHostConfig  `toml:"ddns_host"`
	YouTube   YouTubeConfig   `toml:"youtube"`
	Gifs      GifsConfig      `toml:"gifs"`
	WebRTC    WebRTCConfig    `toml:"webrtc"`
	Net       NetConfig       `toml:"net"`

	// Path this config was loaded from (not serialized).
	path string `toml:"-"`
	// ResolvedDataDir is the absolute, verified-writable data dir (set at startup).
	ResolvedDataDir string `toml:"-"`
}

type ServerConfig struct {
	Name     string `toml:"name"`
	HTTPPort int    `toml:"http_port"`
	DataDir  string `toml:"data_dir"`
	// MaxUploadMB caps a single file upload.
	MaxUploadMB int64 `toml:"max_upload_mb"`
	// OpenBrowser launches the default browser on startup when setup is incomplete.
	OpenBrowser bool `toml:"open_browser"`
}

type TLSConfig struct {
	// Mode: "auto" (ACME when domain set, else plain HTTP), "acme", "selfsigned", "off".
	Mode      string `toml:"mode"`
	Domain    string `toml:"domain"`
	ACMEEmail string `toml:"acme_email"`
	// HTTPSPort is the TLS listener port (ACME also requires :80 reachable for HTTP-01).
	HTTPSPort int `toml:"https_port"`
}

// DDNSConfig makes Talaria keep a dynamic-DNS hostname updated itself —
// no separate updater client needed. With provider "duckdns", set token
// (from duckdns.org); with "porkbun" (a domain you own, DNS hosted at
// porkbun.com) set token to the API key and secret to the secret API key.
// tls.domain is used as the hostname unless domain is set here.
type DDNSConfig struct {
	Enabled  bool   `toml:"enabled"`
	Provider string `toml:"provider"` // "duckdns", "porkbun" or "custom"
	Domain   string `toml:"domain"`   // defaults to tls.domain
	Token    string `toml:"token"`    // DuckDNS token / Porkbun API key / custom {TOKEN}
	Secret   string `toml:"secret"`   // Porkbun secret API key
	URL      string `toml:"url"`      // custom provider: templates {DOMAIN} {TOKEN}
	// IntervalMinutes between updates (default 5).
	IntervalMinutes int `toml:"interval_minutes"`
}

// LanAccessConfig gives the server a second domain that resolves to its
// private LAN IP with its own Let's Encrypt certificate (DNS-01). Devices on
// the home network reaching the main domain through the router's (often
// broken) NAT loopback are redirected to it transparently — no router or
// per-device changes needed. The record and the TXT challenge go through
// the [ddns] provider (DuckDNS or Porkbun) and its credentials unless
// overridden here.
type LanAccessConfig struct {
	Enabled bool   `toml:"enabled"`
	Domain  string `toml:"domain"` // e.g. yourname-lan.duckdns.org or lan.talaria.gg
	Token   string `toml:"token"`  // defaults to ddns.token
	Secret  string `toml:"secret"` // defaults to ddns.secret
	LanIP   string `toml:"lan_ip"` // defaults to auto-detect
}

// DDNSHostConfig turns this server into a dynamic-DNS provider for other
// Talaria installations: they claim a name under zone (e.g. "talaria.gg"
// gives skyle.talaria.gg) and keep it pointed at their connection through
// this server, which publishes the records via Porkbun. Credentials default
// to the [ddns] Porkbun keys. Names a client can take are limited by the
// built-in reserved list plus this server's own hostnames.
type DDNSHostConfig struct {
	Enabled bool   `toml:"enabled"`
	Zone    string `toml:"zone"`    // names are issued as <name>.<zone>; must be in the Porkbun domain
	APIKey  string `toml:"api_key"` // defaults to ddns.token
	Secret  string `toml:"secret"`  // defaults to ddns.secret
	// MaxNames caps how many names may be claimed in total (default 1000).
	MaxNames int `toml:"max_names"`
}

// LanDNSConfig embeds a small DNS forwarder that answers queries for this
// server's domain with its LAN IP (fixing routers with broken NAT loopback)
// and forwards everything else upstream. Point the router's DHCP DNS at this
// machine to apply it to every device on the network.
type LanDNSConfig struct {
	Enabled  bool   `toml:"enabled"`
	ListenIP string `toml:"listen_ip"` // default: all interfaces
	Port     int    `toml:"port"`      // default 53
	Upstream string `toml:"upstream"`  // comma-separated resolvers
	AnswerIP string `toml:"answer_ip"` // default: auto-detect LAN IP
	TTL      int    `toml:"ttl_seconds"`
}

// YouTubeConfig: search works without a key by reading YouTube's results
// page; an API key switches to the official Data API (sturdier, quota-bound).
type YouTubeConfig struct {
	APIKey string `toml:"api_key"`
}

// GifsConfig: GIF search in the composer uses GIPHY, which needs a (free)
// developer key. Without one the GIF button explains that. (Tenor stopped
// taking new API clients in January 2026.)
type GifsConfig struct {
	GiphyAPIKey string `toml:"giphy_api_key"`
}

// WebRTCConfig is written now so Phase 2 (voice/video SFU) is purely additive.
type WebRTCConfig struct {
	UDPPort    int    `toml:"udp_port"`
	EnableTURN bool   `toml:"enable_turn"`
	TURNPort   int    `toml:"turn_port"`
	PublicIP   string `toml:"public_ip"`
	// Upload caps handed to clients for their top simulcast layer.
	MaxCamKbps    int `toml:"max_cam_kbps"`    // default 12000
	MaxScreenKbps int `toml:"max_screen_kbps"` // default 20000
	// MaxDownlinkKbps bounds the per-viewer bandwidth estimate.
	MaxDownlinkKbps int `toml:"max_downlink_kbps"` // default 40000
}

type NetConfig struct {
	TryUPnP bool `toml:"try_upnp"`
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Name:        "Talaria",
			HTTPPort:    8080,
			DataDir:     "./data",
			MaxUploadMB: 500,
			OpenBrowser: true,
		},
		TLS: TLSConfig{
			Mode:      "auto",
			HTTPSPort: 443,
		},
		DDNS: DDNSConfig{
			Provider:        "duckdns",
			IntervalMinutes: 5,
		},
		DDNSHost: DDNSHostConfig{MaxNames: 1000},
		LanDNS: LanDNSConfig{
			Port:     53,
			Upstream: "1.1.1.1, 8.8.8.8",
			TTL:      60,
		},
		WebRTC: WebRTCConfig{
			UDPPort:         50000,
			TURNPort:        3478,
			MaxCamKbps:      12000,
			MaxScreenKbps:   20000,
			MaxDownlinkKbps: 40000,
		},
		Net: NetConfig{TryUPnP: true},
	}
}

// Load reads the config at path, creating it with defaults if absent.
func Load(path string) (*Config, error) {
	cfg := Default()
	cfg.path = path
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		return cfg, nil
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(c.path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

func (c *Config) Path() string { return c.path }

// DefaultPath returns talaria.toml next to the executable (portable mode).
func DefaultPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "talaria.toml"
	}
	return filepath.Join(filepath.Dir(exe), "talaria.toml")
}

// ResolveDataDir resolves the configured data dir (relative paths are relative
// to the executable, keeping portable mode intact) and verifies writability,
// falling back to the OS-local app-data dir when the location is read-only.
func (c *Config) ResolveDataDir() (string, error) {
	dir := c.Server.DataDir
	if !filepath.IsAbs(dir) {
		exe, err := os.Executable()
		if err == nil {
			dir = filepath.Join(filepath.Dir(exe), dir)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err == nil {
		probe := filepath.Join(dir, ".write-probe")
		if err := os.WriteFile(probe, []byte("ok"), 0o644); err == nil {
			os.Remove(probe)
			return dir, nil
		}
	}
	// Read-only install location (zip mount, Program Files): fall back.
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("data dir %s not writable and no user config dir: %w", dir, err)
	}
	fallback := filepath.Join(base, "Talaria", "data")
	if err := os.MkdirAll(fallback, 0o755); err != nil {
		return "", err
	}
	return fallback, nil
}
