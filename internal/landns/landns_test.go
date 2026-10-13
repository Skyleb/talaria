package landns

import (
	"encoding/binary"
	"log/slog"
	"net"
	"testing"
	"time"
)

// buildQuery assembles a minimal DNS query for name/qtype with the given ID.
func buildQuery(id uint16, name string, qtype uint16) []byte {
	q := make([]byte, 12)
	binary.BigEndian.PutUint16(q[0:2], id)
	q[2] = 0x01 // RD
	binary.BigEndian.PutUint16(q[4:6], 1)
	for _, label := range splitLabels(name) {
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0)
	var tail [4]byte
	binary.BigEndian.PutUint16(tail[0:2], qtype)
	binary.BigEndian.PutUint16(tail[2:4], 1) // IN
	return append(q, tail[:]...)
}

func splitLabels(name string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			if i > start {
				out = append(out, name[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// fakeUpstream answers every query with an empty NOERROR response carrying a
// recognizable extra byte so tests can tell forwarded answers apart.
func fakeUpstream(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, raddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			resp := make([]byte, n)
			copy(resp, buf[:n])
			resp[2] |= 0x80 // QR
			conn.WriteToUDP(resp, raddr)
		}
	}()
	return conn.LocalAddr().String()
}

func startTestRunner(t *testing.T, upstream string) *Runner {
	t.Helper()
	r := NewRunner(slog.Default())
	err := r.Set(Config{
		ListenIP:  "127.0.0.1",
		Port:      0,
		Names:     []string{"Chat.Example.ORG."},
		AnswerIP:  net.IPv4(192, 168, 1, 99),
		Upstreams: []string{upstream},
		TTL:       60,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	return r
}

func exchange(t *testing.T, addr string, query []byte) []byte {
	t.Helper()
	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(query); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf[:n]
}

func TestAnswersOwnDomainWithLanIP(t *testing.T) {
	r := startTestRunner(t, fakeUpstream(t))
	resp := exchange(t, r.Addr(), buildQuery(0x1234, "chat.example.org", 1))

	if got := binary.BigEndian.Uint16(resp[0:2]); got != 0x1234 {
		t.Fatalf("ID = %#x, want 0x1234", got)
	}
	if resp[2]&0x80 == 0 || resp[2]&0x04 == 0 {
		t.Fatalf("flags %#x: want QR and AA set", resp[2])
	}
	if an := binary.BigEndian.Uint16(resp[6:8]); an != 1 {
		t.Fatalf("ANCOUNT = %d, want 1", an)
	}
	ip := net.IP(resp[len(resp)-4:])
	if ip.String() != "192.168.1.99" {
		t.Fatalf("answer = %s, want 192.168.1.99", ip)
	}
}

func TestAAAAForOwnDomainIsEmptyNoError(t *testing.T) {
	r := startTestRunner(t, fakeUpstream(t))
	resp := exchange(t, r.Addr(), buildQuery(7, "chat.example.org", 28))

	if rcode := resp[3] & 0x0F; rcode != 0 {
		t.Fatalf("rcode = %d, want NOERROR", rcode)
	}
	if an := binary.BigEndian.Uint16(resp[6:8]); an != 0 {
		t.Fatalf("ANCOUNT = %d, want 0", an)
	}
}

func TestForwardsOtherDomains(t *testing.T) {
	r := startTestRunner(t, fakeUpstream(t))
	resp := exchange(t, r.Addr(), buildQuery(42, "example.com", 1))

	if got := binary.BigEndian.Uint16(resp[0:2]); got != 42 {
		t.Fatalf("forwarded ID = %d, want 42", got)
	}
	if resp[2]&0x04 != 0 {
		t.Fatal("forwarded response should not have AA set by us")
	}
}

func TestUpstreamLoopRejected(t *testing.T) {
	r := NewRunner(slog.Default())
	err := r.Set(Config{
		ListenIP:  "127.0.0.1",
		Port:      5391,
		Names:     []string{"chat.example.org"},
		AnswerIP:  net.IPv4(192, 168, 1, 99),
		Upstreams: []string{"127.0.0.1:5391"},
	})
	if err == nil {
		r.Stop()
		t.Fatal("expected loop error")
	}
}

func TestNormalizeUpstreams(t *testing.T) {
	got, err := NormalizeUpstreams(" 1.1.1.1 , 192.168.1.1:53 ")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.1.1.1:53", "192.168.1.1:53"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if _, err := NormalizeUpstreams(" , "); err == nil {
		t.Fatal("expected error for empty list")
	}
}

// answeringUpstream responds to every A query with the given IP.
func answeringUpstream(t *testing.T, ip net.IP) string {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, raddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if _, _, qEnd, ok := parseQuestion(buf[:n]); ok {
				conn.WriteToUDP(makeResponse(buf[:n], qEnd, ip, 60), raddr)
			}
		}
	}()
	return conn.LocalAddr().String()
}

func TestResolveABypassesOSResolver(t *testing.T) {
	up := answeringUpstream(t, net.IPv4(203, 0, 113, 7))
	ips, err := ResolveA("chat.example.org", []string{up})
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || ips[0].String() != "203.0.113.7" {
		t.Fatalf("got %v, want [203.0.113.7]", ips)
	}
}
