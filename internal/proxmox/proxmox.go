// Package proxmox is a small read-only client for the Proxmox VE API. It lists
// the VMs (qemu) and containers (lxc) of a node or cluster and, when possible,
// their IPv4 address. It needs only an API token with the PVEAuditor role.
package proxmox

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Guest kinds.
const (
	KindLXC  = "lxc"
	KindQEMU = "qemu"
)

// ErrAuth means the API rejected the token (HTTP 401/403).
var ErrAuth = errors.New("proxmox: token rejected (check token ID, secret and the PVEAuditor role)")

// Config describes how to reach one Proxmox server.
type Config struct {
	URL     string // https://host:8006
	TokenID string // user@realm!name
	Secret  string // token secret (UUID)
	// TLS: by default the certificate is verified normally. Fingerprint (SHA-256
	// of the server certificate, hex, with or without colons) pins it instead,
	// which is the safe way to trust Proxmox's default self-signed certificate.
	// Insecure skips verification entirely and should be a last resort.
	Fingerprint string
	Insecure    bool
	Timeout     time.Duration
}

// Guest is one VM or container.
type Guest struct {
	VMID     int
	Kind     string // KindLXC or KindQEMU
	Name     string
	Node     string
	Status   string // running, stopped, ...
	Template bool
	IP       string // best-effort IPv4, may be empty
}

// Client talks to one Proxmox server.
type Client struct {
	base string
	auth string
	hc   *http.Client
}

// NormalizeFingerprint upper-cases and strips separators, returning "" when the
// value is not a SHA-256 fingerprint.
func NormalizeFingerprint(fp string) string {
	fp = strings.ToUpper(strings.NewReplacer(":", "", " ", "", "-", "").Replace(strings.TrimSpace(fp)))
	if len(fp) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(fp); err != nil {
		return ""
	}
	return fp
}

// ValidateURL accepts only http(s) URLs with a host and without credentials,
// query or fragment, and returns the cleaned base (no trailing slash).
func ValidateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("proxmox: URL must look like https://host:8006")
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"), nil
}

// New builds a client.
func New(cfg Config) (*Client, error) {
	base, err := ValidateURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(cfg.TokenID, "!") || strings.ContainsAny(cfg.TokenID+cfg.Secret, " \r\n\t") || cfg.Secret == "" {
		return nil, errors.New("proxmox: token ID must look like user@realm!name and the secret must not be empty")
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.Fingerprint != "" {
		want := NormalizeFingerprint(cfg.Fingerprint)
		if want == "" {
			return nil, errors.New("proxmox: fingerprint must be a SHA-256 value (64 hex digits)")
		}
		tc.InsecureSkipVerify = true // replaced by the pin check below
		tc.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("no certificate presented")
			}
			sum := sha256.Sum256(raw[0])
			if got := strings.ToUpper(hex.EncodeToString(sum[:])); got != want {
				return fmt.Errorf("certificate fingerprint mismatch (server presented %s)", colon(got))
			}
			return nil
		}
	} else if cfg.Insecure {
		tc.InsecureSkipVerify = true
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		base: base + "/api2/json",
		auth: "PVEAPIToken=" + cfg.TokenID + "=" + cfg.Secret,
		hc: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig:     tc,
				DialContext:         (&net.Dialer{Timeout: 8 * time.Second}).DialContext,
				MaxIdleConnsPerHost: 4,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func colon(hexs string) string {
	var b strings.Builder
	for i := 0; i < len(hexs); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(hexs[i : i+2])
	}
	return b.String()
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("proxmox: %w", cleanErr(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("proxmox: reading response: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrAuth
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("proxmox: %s returned HTTP %d", path, resp.StatusCode)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("proxmox: %s did not return Proxmox JSON (is the URL right?)", path)
	}
	return json.Unmarshal(env.Data, out)
}

// cleanErr removes the request URL that net/http embeds in its errors.
func cleanErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

type resource struct {
	Type     string `json:"type"`
	VMID     int    `json:"vmid"`
	Name     string `json:"name"`
	Node     string `json:"node"`
	Status   string `json:"status"`
	Template int    `json:"template"`
}

// Guests returns every VM and container, with a best-effort IPv4 for the
// running ones. IP lookups never fail the call.
func (c *Client) Guests(ctx context.Context) ([]Guest, error) {
	var res []resource
	if err := c.get(ctx, "/cluster/resources?type=vm", &res); err != nil {
		return nil, err
	}
	var out []Guest
	for _, r := range res {
		if (r.Type != KindLXC && r.Type != KindQEMU) || r.VMID <= 0 || r.Node == "" {
			continue
		}
		out = append(out, Guest{VMID: r.VMID, Kind: r.Type, Name: r.Name, Node: r.Node,
			Status: r.Status, Template: r.Template == 1})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].VMID < out[j].VMID })

	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i := range out {
		if out[i].Status != "running" || out[i].Template {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(g *Guest) {
			defer wg.Done()
			defer func() { <-sem }()
			g.IP = c.guestIP(ctx, *g)
		}(&out[i])
	}
	wg.Wait()
	return out, nil
}

func (c *Client) guestIP(ctx context.Context, g Guest) string {
	base := "/nodes/" + url.PathEscape(g.Node) + "/" + g.Kind + "/" + strconv.Itoa(g.VMID)
	var cands []candidate
	switch g.Kind {
	case KindLXC:
		var ifs []struct {
			Name string `json:"name"`
			Inet string `json:"inet"`
		}
		if c.get(ctx, base+"/interfaces", &ifs) == nil {
			for _, i := range ifs {
				cands = append(cands, candidate{i.Name, i.Inet})
			}
		}
	case KindQEMU:
		var ag struct {
			Result []struct {
				Name string `json:"name"`
				IPs  []struct {
					IP   string `json:"ip-address"`
					Type string `json:"ip-address-type"`
				} `json:"ip-addresses"`
			} `json:"result"`
		}
		if c.get(ctx, base+"/agent/network-get-interfaces", &ag) == nil {
			for _, i := range ag.Result {
				for _, a := range i.IPs {
					if a.Type == "ipv4" {
						cands = append(cands, candidate{i.Name, a.IP})
					}
				}
			}
		}
	}
	return PickIP(cands)
}

type candidate struct{ iface, addr string }

var skipPrefixes = []string{"lo", "docker", "br-", "veth", "virbr", "cni", "flannel", "cali", "tailscale", "zt"}

// PickIP chooses the most useful IPv4 among interface addresses (CIDR or plain),
// ignoring loopback, link-local and container-bridge interfaces. Private
// addresses win over public ones.
func PickIP(cands []candidate) string {
	best := ""
	for _, c := range cands {
		skip := false
		for _, p := range skipPrefixes {
			if strings.HasPrefix(c.iface, p) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		addr := c.addr
		if i := strings.IndexByte(addr, '/'); i >= 0 {
			addr = addr[:i]
		}
		ip, err := netip.ParseAddr(addr)
		if err != nil || !ip.Is4() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			continue
		}
		if ip.IsPrivate() {
			return ip.String()
		}
		if best == "" {
			best = ip.String()
		}
	}
	return best
}
