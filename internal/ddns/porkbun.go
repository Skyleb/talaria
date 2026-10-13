package ddns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Porkbun talks to porkbun.com's DNS API (JSON over POST). It manages
// records in a domain you own, so it covers both the dynamic A record and
// the TXT records Let's Encrypt's DNS-01 challenge needs. Enable "API
// access" for the domain in the Porkbun dashboard and create an API key
// pair (key + secret) to use it.
type Porkbun struct {
	APIKey string
	Secret string
	// BaseURL overrides the API endpoint (tests). Default porkbunBaseURL.
	BaseURL string
	// IPURL overrides the IPv4-only ping endpoint used to learn this
	// connection's public IP (tests). Default porkbunIPv4URL.
	IPURL  string
	Client *http.Client
}

const (
	porkbunBaseURL = "https://api.porkbun.com/api/json/v3"
	porkbunIPv4URL = "https://api-ipv4.porkbun.com/api/json/v3/ping"
	// porkbunTTL is the minimum TTL Porkbun accepts.
	porkbunTTL = "600"
)

type porkbunRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

type porkbunResponse struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	YourIP  string          `json:"yourIp"`
	Records []porkbunRecord `json:"records"`
}

// PublicIPv4 asks Porkbun which IPv4 address this connection comes from.
func (p *Porkbun) PublicIPv4(ctx context.Context) (string, error) {
	u := p.IPURL
	if u == "" {
		u = porkbunIPv4URL
	}
	resp, err := p.post(ctx, u, nil)
	if err != nil {
		return "", err
	}
	if resp.YourIP == "" {
		return "", errors.New("porkbun ping returned no IP")
	}
	return resp.YourIP, nil
}

// SetRecord makes host's records of the given type consist of exactly one
// record with content. It is a no-op when the record already matches.
func (p *Porkbun) SetRecord(ctx context.Context, host, typ, content string) error {
	zone, sub, err := SplitZone(host)
	if err != nil {
		return err
	}
	existing, err := p.retrieve(ctx, zone, typ, sub)
	if err != nil {
		return err
	}
	if len(existing) == 1 && existing[0].Content == content {
		return nil
	}
	if len(existing) > 0 {
		_, err = p.post(ctx, p.url("dns/editByNameType", zone, typ, sub), map[string]string{
			"content": content, "ttl": porkbunTTL,
		})
		return err
	}
	_, err = p.post(ctx, p.url("dns/create", zone), map[string]string{
		"name": sub, "type": typ, "content": content, "ttl": porkbunTTL,
	})
	return err
}

// DeleteRecord removes every record of the given type at host.
func (p *Porkbun) DeleteRecord(ctx context.Context, host, typ string) error {
	zone, sub, err := SplitZone(host)
	if err != nil {
		return err
	}
	existing, err := p.retrieve(ctx, zone, typ, sub)
	if err != nil || len(existing) == 0 {
		return err
	}
	_, err = p.post(ctx, p.url("dns/deleteByNameType", zone, typ, sub), nil)
	return err
}

// SetTXT implements TXTSetter: the DNS-01 record lives at _acme-challenge.<domain>.
func (p *Porkbun) SetTXT(ctx context.Context, domain, txt string) error {
	host := "_acme-challenge." + strings.ToLower(domain)
	if txt == "" {
		return p.DeleteRecord(ctx, host, "TXT")
	}
	return p.SetRecord(ctx, host, "TXT", txt)
}

func (p *Porkbun) retrieve(ctx context.Context, zone, typ, sub string) ([]porkbunRecord, error) {
	resp, err := p.post(ctx, p.url("dns/retrieveByNameType", zone, typ, sub), nil)
	if err != nil {
		return nil, err
	}
	return resp.Records, nil
}

// url joins the base URL with path segments, dropping a trailing empty
// subdomain segment (Porkbun addresses the zone apex by omitting it).
func (p *Porkbun) url(parts ...string) string {
	base := p.BaseURL
	if base == "" {
		base = porkbunBaseURL
	}
	if n := len(parts); n > 0 && parts[n-1] == "" {
		parts = parts[:n-1]
	}
	return base + "/" + strings.Join(parts, "/")
}

func (p *Porkbun) post(ctx context.Context, url string, fields map[string]string) (*porkbunResponse, error) {
	body := map[string]string{"apikey": p.APIKey, "secretapikey": p.Secret}
	for k, v := range fields {
		body[k] = v
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out porkbunResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("porkbun: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out.Status != "SUCCESS" {
		msg := out.Message
		if msg == "" {
			msg = fmt.Sprintf("status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("porkbun: %s", msg)
	}
	return &out, nil
}
