package landns

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"strings"
)

// ResolveA queries upstream resolvers directly on the wire for the domain's
// IPv4 addresses, deliberately bypassing the OS resolver — hosts-file entries
// and split-horizon DNS on the server machine must not influence the answer
// (hairpin detection in lan access depends on the domain's true public IP).
func ResolveA(domain string, upstreams []string) ([]net.IP, error) {
	query, err := buildAQuery(domain)
	if err != nil {
		return nil, err
	}
	resp, err := forwardUDP(query, upstreams)
	if err != nil {
		return nil, err
	}
	if len(resp) >= 12 && resp[2]&0x02 != 0 { // truncated: retry over TCP
		if resp, err = forwardTCP(query, upstreams); err != nil {
			return nil, err
		}
	}
	if len(resp) < 12 || resp[0] != query[0] || resp[1] != query[1] {
		return nil, errors.New("mismatched DNS response")
	}
	return parseAAnswers(resp)
}

func buildAQuery(domain string) ([]byte, error) {
	domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")
	if domain == "" {
		return nil, errors.New("empty domain")
	}
	q := make([]byte, 12, 12+len(domain)+6)
	rand.Read(q[0:2]) // ID
	q[2] = 0x01       // RD
	binary.BigEndian.PutUint16(q[4:6], 1)
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, errors.New("invalid domain label")
		}
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0, 0, 1, 0, 1) // root, type A, class IN
	return q, nil
}

// skipName advances past a (possibly compressed) DNS name starting at i.
func skipName(m []byte, i int) (int, bool) {
	for {
		if i >= len(m) {
			return 0, false
		}
		l := int(m[i])
		switch {
		case l == 0:
			return i + 1, true
		case l&0xC0 == 0xC0: // compression pointer ends the name
			return i + 2, true
		default:
			i += 1 + l
		}
	}
}

func parseAAnswers(m []byte) ([]net.IP, error) {
	qdcount := int(binary.BigEndian.Uint16(m[4:6]))
	ancount := int(binary.BigEndian.Uint16(m[6:8]))
	i := 12
	var ok bool
	for range qdcount {
		if i, ok = skipName(m, i); !ok || i+4 > len(m) {
			return nil, errors.New("malformed DNS question")
		}
		i += 4
	}
	var ips []net.IP
	for range ancount {
		if i, ok = skipName(m, i); !ok || i+10 > len(m) {
			return nil, errors.New("malformed DNS answer")
		}
		rtype := binary.BigEndian.Uint16(m[i : i+2])
		rdlen := int(binary.BigEndian.Uint16(m[i+8 : i+10]))
		i += 10
		if i+rdlen > len(m) {
			return nil, errors.New("malformed DNS rdata")
		}
		if rtype == 1 && rdlen == 4 {
			ips = append(ips, net.IP(append([]byte(nil), m[i:i+4]...)))
		}
		i += rdlen
	}
	return ips, nil
}
