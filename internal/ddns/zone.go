package ddns

import (
	"fmt"
	"strings"
)

// twoLevelSuffixes are common public suffixes made of two labels, so that
// "lan.example.co.uk" splits into zone "example.co.uk" rather than "co.uk".
var twoLevelSuffixes = map[string]bool{
	"co.uk": true, "org.uk": true, "me.uk": true, "ltd.uk": true,
	"com.au": true, "net.au": true, "org.au": true, "id.au": true,
	"co.nz": true, "net.nz": true, "org.nz": true,
	"co.jp": true, "ne.jp": true, "or.jp": true,
	"com.br": true, "net.br": true, "org.br": true,
	"co.za": true, "com.mx": true, "co.in": true, "co.kr": true,
	"com.ar": true, "com.tr": true, "com.sg": true, "com.hk": true,
}

// SplitZone splits a hostname into the registrable zone and the subdomain
// label(s) inside it: "lan.talaria.gg" → ("talaria.gg", "lan"); the apex
// itself yields an empty subdomain.
func SplitZone(host string) (zone, sub string, err error) {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), ".")
	labels := strings.Split(host, ".")
	n := len(labels)
	if n < 2 || host == "" {
		return "", "", fmt.Errorf("%q is not a domain name", host)
	}
	zoneLabels := 2
	if twoLevelSuffixes[labels[n-2]+"."+labels[n-1]] {
		zoneLabels = 3
	}
	if n < zoneLabels {
		return "", "", fmt.Errorf("%q is a public suffix, not a domain", host)
	}
	zone = strings.Join(labels[n-zoneLabels:], ".")
	sub = strings.Join(labels[:n-zoneLabels], ".")
	return zone, sub, nil
}
