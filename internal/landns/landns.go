// Package landns embeds a tiny LAN DNS forwarder: it answers queries for
// Talaria's own domain(s) with the server's LAN IP and forwards everything
// else upstream. It exists because many home routers mishandle NAT loopback
// (hairpinning), leaving a DDNS domain broken or stalling from inside the
// LAN. Pointing the router's DHCP DNS at this machine fixes every device at
// once with no per-device setup.
package landns

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	ListenIP  string // empty = all interfaces
	Port      int    // 53 in production; tests may use 0 for a random port
	Names     []string
	AnswerIP  net.IP // IPv4 returned for Names; nil = auto-detect LAN IP
	Upstreams []string
	TTL       uint32 // seconds; answers for Names only
}

const forwardTimeout = 3 * time.Second

// DetectLanIP returns this machine's outbound LAN IPv4 (no packets are sent).
func DetectLanIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.To4()
	}
	return nil
}

// NormalizeUpstreams parses a comma-separated resolver list, appending :53
// where no port is given.
func NormalizeUpstreams(s string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(part); err != nil {
			part = net.JoinHostPort(part, "53")
		}
		host, _, err := net.SplitHostPort(part)
		if err != nil || host == "" {
			return nil, fmt.Errorf("invalid upstream %q", part)
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return nil, errors.New("no upstream resolvers configured")
	}
	return out, nil
}

// Runner owns at most one running DNS server whose config can be swapped at
// runtime (the admin settings UI reconfigures LAN DNS without a restart).
type Runner struct {
	log     *slog.Logger
	mu      sync.Mutex
	srv     *server
	lastErr string
}

func NewRunner(log *slog.Logger) *Runner { return &Runner{log: log} }

// Set stops any current server and starts one with cfg. The returned error
// (also kept for Status) is typically a failure to bind port 53.
func (r *Runner) Set(cfg Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.srv != nil {
		r.srv.close()
		r.srv = nil
	}
	r.lastErr = ""
	if len(cfg.Names) == 0 {
		r.lastErr = "no domain to answer for"
		return errors.New(r.lastErr)
	}
	if cfg.AnswerIP == nil {
		cfg.AnswerIP = DetectLanIP()
		if cfg.AnswerIP == nil {
			r.lastErr = "could not detect this machine's LAN IP"
			return errors.New(r.lastErr)
		}
	}
	if cfg.TTL == 0 {
		cfg.TTL = 60
	}
	if err := checkUpstreamLoop(cfg); err != nil {
		r.lastErr = err.Error()
		return err
	}
	srv, err := startServer(cfg, r.log)
	if err != nil {
		r.lastErr = err.Error()
		return err
	}
	r.srv = srv
	r.log.Info("lan dns running", "addr", srv.udp.LocalAddr().String(),
		"names", strings.Join(cfg.Names, ","), "answer", cfg.AnswerIP.String(),
		"upstreams", strings.Join(cfg.Upstreams, ","))
	return nil
}

// Stop halts the server until the next Set.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.srv != nil {
		r.srv.close()
		r.srv = nil
	}
	r.lastErr = ""
}

// Status reports whether the server is running and the last start error.
func (r *Runner) Status() (active bool, lastErr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.srv != nil, r.lastErr
}

// Addr returns the bound UDP address (tests use this with Port 0).
func (r *Runner) Addr() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.srv == nil {
		return ""
	}
	return r.srv.udp.LocalAddr().String()
}

// checkUpstreamLoop rejects upstreams that point back at this server, which
// would forward queries to ourselves forever.
func checkUpstreamLoop(cfg Config) error {
	port := strconv.Itoa(cfg.Port)
	local := map[string]bool{"127.0.0.1": true, "::1": true, "localhost": true}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				local[ipn.IP.String()] = true
			}
		}
	}
	for _, up := range cfg.Upstreams {
		host, upPort, err := net.SplitHostPort(up)
		if err == nil && upPort == port && local[host] {
			return fmt.Errorf("upstream %s points at this server (loop)", up)
		}
	}
	return nil
}

type server struct {
	cfg    Config
	names  map[string]struct{}
	udp    *net.UDPConn
	tcp    net.Listener
	log    *slog.Logger
	closed atomic.Bool
}

func startServer(cfg Config, log *slog.Logger) (*server, error) {
	addr := net.JoinHostPort(cfg.ListenIP, strconv.Itoa(cfg.Port))
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udp, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("bind udp %s: %w", addr, err)
	}
	// TCP on the same port the UDP listener actually got (matters for Port 0).
	tcpAddr := net.JoinHostPort(cfg.ListenIP, strconv.Itoa(udp.LocalAddr().(*net.UDPAddr).Port))
	tcp, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		udp.Close()
		return nil, fmt.Errorf("bind tcp %s: %w", tcpAddr, err)
	}
	s := &server{cfg: cfg, names: map[string]struct{}{}, udp: udp, tcp: tcp, log: log}
	for _, n := range cfg.Names {
		n = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n), "."))
		if n != "" {
			s.names[n] = struct{}{}
		}
	}
	go s.serveUDP()
	go s.serveTCP()
	return s, nil
}

func (s *server) close() {
	s.closed.Store(true)
	s.udp.Close()
	s.tcp.Close()
}

func (s *server) serveUDP() {
	buf := make([]byte, 4096)
	for {
		n, raddr, err := s.udp.ReadFromUDP(buf)
		if err != nil {
			if s.closed.Load() {
				return
			}
			continue
		}
		q := make([]byte, n)
		copy(q, buf[:n])
		go func(q []byte, raddr *net.UDPAddr) {
			if resp := s.handle(q, false); resp != nil {
				s.udp.WriteToUDP(resp, raddr)
			}
		}(q, raddr)
	}
}

func (s *server) serveTCP() {
	for {
		conn, err := s.tcp.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			continue
		}
		go s.serveTCPConn(conn)
	}
}

// serveTCPConn speaks RFC 1035 TCP framing: each message is prefixed with a
// 2-byte big-endian length.
func (s *server) serveTCPConn(conn net.Conn) {
	defer conn.Close()
	for {
		conn.SetDeadline(time.Now().Add(15 * time.Second))
		var lenBuf [2]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}
		msgLen := int(binary.BigEndian.Uint16(lenBuf[:]))
		if msgLen == 0 || msgLen > 65535 {
			return
		}
		q := make([]byte, msgLen)
		if _, err := io.ReadFull(conn, q); err != nil {
			return
		}
		resp := s.handle(q, true)
		if resp == nil {
			return
		}
		out := make([]byte, 2+len(resp))
		binary.BigEndian.PutUint16(out, uint16(len(resp)))
		copy(out[2:], resp)
		if _, err := conn.Write(out); err != nil {
			return
		}
	}
}

// handle answers queries for configured names locally and forwards the rest.
func (s *server) handle(q []byte, viaTCP bool) []byte {
	name, qtype, qEnd, ok := parseQuestion(q)
	if ok {
		if _, match := s.names[name]; match {
			// A gets the LAN IP; other types (AAAA, HTTPS, ...) get an empty
			// NOERROR so clients fall back to A instead of the public path.
			if qtype == 1 {
				return makeResponse(q, qEnd, s.cfg.AnswerIP.To4(), s.cfg.TTL)
			}
			return makeResponse(q, qEnd, nil, s.cfg.TTL)
		}
	}
	var resp []byte
	var err error
	if viaTCP {
		resp, err = forwardTCP(q, s.cfg.Upstreams)
	} else {
		resp, err = forwardUDP(q, s.cfg.Upstreams)
	}
	if err != nil {
		s.log.Debug("lan dns forward failed", "err", err)
		return nil
	}
	return resp
}

// parseQuestion extracts the first question's name (lowercase, no trailing
// dot) and type. ok is false for anything unusual; callers then forward the
// raw query untouched.
func parseQuestion(m []byte) (name string, qtype uint16, qEnd int, ok bool) {
	if len(m) < 12 || binary.BigEndian.Uint16(m[4:6]) != 1 {
		return
	}
	i := 12
	var sb strings.Builder
	for {
		if i >= len(m) {
			return
		}
		l := int(m[i])
		if l == 0 {
			i++
			break
		}
		if l > 63 { // compression pointers don't appear in queries we answer
			return
		}
		i++
		if i+l > len(m) || sb.Len()+l > 253 {
			return
		}
		if sb.Len() > 0 {
			sb.WriteByte('.')
		}
		sb.Write(m[i : i+l])
		i += l
	}
	if i+4 > len(m) {
		return
	}
	return strings.ToLower(sb.String()), binary.BigEndian.Uint16(m[i : i+2]), i + 4, true
}

// makeResponse builds an authoritative reply to the query in q. With a
// non-nil IPv4 answer it carries one A record; otherwise an empty NOERROR.
func makeResponse(q []byte, qEnd int, answer net.IP, ttl uint32) []byte {
	if answer != nil {
		answer = answer.To4()
	}
	resp := make([]byte, qEnd, qEnd+16)
	copy(resp, q[:qEnd])
	resp[2] = q[2]&0x79 | 0x84 // keep opcode+RD; set QR+AA; clear TC
	resp[3] = 0x80             // RA set, rcode NOERROR
	binary.BigEndian.PutUint16(resp[4:6], 1) // QDCOUNT
	var ancount uint16
	if answer != nil {
		ancount = 1
	}
	binary.BigEndian.PutUint16(resp[6:8], ancount)
	binary.BigEndian.PutUint16(resp[8:10], 0)  // NSCOUNT
	binary.BigEndian.PutUint16(resp[10:12], 0) // ARCOUNT (drops any OPT)
	if answer != nil {
		resp = append(resp, 0xC0, 0x0C, 0, 1, 0, 1) // name ptr, type A, class IN
		var ttlb [4]byte
		binary.BigEndian.PutUint32(ttlb[:], ttl)
		resp = append(resp, ttlb[:]...)
		resp = append(resp, 0, 4)
		resp = append(resp, answer...)
	}
	return resp
}

func forwardUDP(query []byte, upstreams []string) ([]byte, error) {
	for _, up := range upstreams {
		conn, err := net.DialTimeout("udp", up, forwardTimeout)
		if err != nil {
			continue
		}
		conn.SetDeadline(time.Now().Add(forwardTimeout))
		if _, err := conn.Write(query); err != nil {
			conn.Close()
			continue
		}
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		conn.Close()
		if err == nil && n >= 12 {
			return buf[:n], nil
		}
	}
	return nil, errors.New("all upstream resolvers failed")
}

func forwardTCP(query []byte, upstreams []string) ([]byte, error) {
	for _, up := range upstreams {
		conn, err := net.DialTimeout("tcp", up, forwardTimeout)
		if err != nil {
			continue
		}
		conn.SetDeadline(time.Now().Add(forwardTimeout))
		out := make([]byte, 2+len(query))
		binary.BigEndian.PutUint16(out, uint16(len(query)))
		copy(out[2:], query)
		if _, err := conn.Write(out); err != nil {
			conn.Close()
			continue
		}
		var lenBuf [2]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			conn.Close()
			continue
		}
		resp := make([]byte, binary.BigEndian.Uint16(lenBuf[:]))
		_, err = io.ReadFull(conn, resp)
		conn.Close()
		if err == nil && len(resp) >= 12 {
			return resp, nil
		}
	}
	return nil, errors.New("all upstream resolvers failed")
}
