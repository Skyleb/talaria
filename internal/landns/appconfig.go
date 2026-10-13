package landns

import (
	"fmt"
	"net"
	"strings"

	"github.com/skyle/talaria/internal/config"
)

// FromAppConfig translates the TOML/admin-UI settings into a runner Config.
// names are the domains to answer for (empty entries are dropped).
func FromAppConfig(c config.LanDNSConfig, names ...string) (Config, error) {
	ups, err := NormalizeUpstreams(c.Upstream)
	if err != nil {
		return Config{}, err
	}
	var answer net.IP
	if s := strings.TrimSpace(c.AnswerIP); s != "" {
		answer = net.ParseIP(s)
		if answer == nil || answer.To4() == nil {
			return Config{}, fmt.Errorf("answer IP %q is not a valid IPv4 address", s)
		}
	}
	port := c.Port
	if port == 0 {
		port = 53
	}
	ttl := c.TTL
	if ttl <= 0 {
		ttl = 60
	}
	var clean []string
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			clean = append(clean, n)
		}
	}
	if len(clean) == 0 {
		return Config{}, fmt.Errorf("LAN DNS needs a domain (set one under HTTPS or DDNS)")
	}
	return Config{
		ListenIP:  strings.TrimSpace(c.ListenIP),
		Port:      port,
		Names:     clean,
		AnswerIP:  answer,
		Upstreams: ups,
		TTL:       uint32(ttl),
	}, nil
}
