// Package discovery keeps HomeView's list of VMs and containers up to date by
// asking each configured Proxmox connection, and manages those connections
// (validation, connection test, encrypted token storage).
package discovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	neturl "net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/andregoncalvespires/homeview-dash/internal/proxmox"
	"github.com/andregoncalvespires/homeview-dash/internal/secretbox"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

// Client is what the service needs from a Proxmox client (tests fake it).
type Client interface {
	Guests(ctx context.Context) ([]proxmox.Guest, error)
	Nodes(ctx context.Context) ([]proxmox.Node, error)
	Metrics(ctx context.Context) (proxmox.Metrics, error)
}

// InputError reports invalid form input; Key is an i18n message key.
type InputError struct{ Key string }

func (e *InputError) Error() string { return "invalid input: " + e.Key }

// ConnectError wraps a failed connection test; Err is safe to show (the token
// secret never appears in it).
type ConnectError struct{ Err error }

func (e *ConnectError) Error() string { return e.Err.Error() }
func (e *ConnectError) Unwrap() error { return e.Err }

// Service runs discovery.
type Service struct {
	st     store.Store
	secret []byte
	log    *slog.Logger

	// NewClient and Now can be replaced in tests.
	NewClient func(proxmox.Config) (Client, error)
	Now       func() time.Time

	mu sync.Mutex // one sync at a time

	mm    sync.Mutex // guards the metrics cache; held while refreshing
	cache map[int64]*Reading
	pm    sync.Mutex // guards prev (read concurrently per connection)
	prev  map[string]sample
}

// New builds a Service. secret is the server secret used to encrypt tokens.
func New(st store.Store, secret []byte, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		st: st, secret: secret, log: log,
		NewClient: func(c proxmox.Config) (Client, error) { return proxmox.New(c) },
		Now:       time.Now,
	}
}

// ConnInput is the form data of a connection.
type ConnInput struct {
	Name        string
	URL         string
	TokenID     string
	Secret      string // empty on edit = keep the stored one
	Fingerprint string
	Insecure    bool
}

func (in ConnInput) validate(requireSecret bool) (ConnInput, error) {
	var ok bool
	if in.Name, ok = store.CleanText(in.Name, 60); !ok || in.Name == "" {
		return in, &InputError{"err.name"}
	}
	u, err := proxmox.ValidateURL(in.URL)
	if err != nil {
		return in, &InputError{"err.conn_url"}
	}
	in.URL = u
	in.TokenID = strings.TrimSpace(in.TokenID)
	if !utf8.ValidString(in.TokenID) || !strings.Contains(in.TokenID, "!") || len(in.TokenID) > 120 ||
		strings.IndexFunc(in.TokenID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return in, &InputError{"err.conn_token_id"}
	}
	in.Secret = strings.TrimSpace(in.Secret)
	if (in.Secret == "" && requireSecret) || len(in.Secret) > 200 || !utf8.ValidString(in.Secret) ||
		strings.IndexFunc(in.Secret, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return in, &InputError{"err.conn_secret"}
	}
	if fp := strings.TrimSpace(in.Fingerprint); fp != "" {
		if in.Fingerprint = proxmox.NormalizeFingerprint(fp); in.Fingerprint == "" {
			return in, &InputError{"err.conn_fingerprint"}
		}
		in.Insecure = false // a pin is stricter, and wins
	}
	return in, nil
}

// SaveConnection validates the input, checks that the server answers with the
// given token and then stores it (creating when id == 0). The first guests are
// fetched right away. It returns *InputError, *ConnectError, store.ErrExists or
// store.ErrNotFound for the caller to translate.
func (s *Service) SaveConnection(ctx context.Context, id int64, in ConnInput) (*store.Connection, error) {
	var cur *store.Connection
	if id != 0 {
		var err error
		if cur, err = s.st.GetConnection(ctx, id); err != nil {
			return nil, err
		}
	}
	in, err := in.validate(cur == nil)
	if err != nil {
		return nil, err
	}
	secret := in.Secret
	sealed := ""
	if secret == "" { // editing without a new secret
		if secret, err = secretbox.Open(s.secret, cur.SecretSealed); err != nil {
			return nil, &InputError{"err.conn_secret"} // stored token unreadable: ask for it again
		}
		sealed = cur.SecretSealed
	} else if sealed, err = secretbox.Seal(s.secret, secret); err != nil {
		return nil, err
	}

	tctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	fr, err := s.fetch(tctx, in.URL, in.TokenID, secret, in.Fingerprint, in.Insecure)
	if err != nil {
		return nil, &ConnectError{Err: err}
	}

	c := &store.Connection{Name: in.Name, URL: in.URL, TokenID: in.TokenID, SecretSealed: sealed,
		Insecure: in.Insecure, Fingerprint: in.Fingerprint}
	if cur != nil {
		c.ID = cur.ID
	}
	if err := s.st.SaveConnection(ctx, c); err != nil {
		return nil, err
	}
	if _, err := s.store(ctx, c.ID, c.URL, fr); err != nil {
		s.log.Warn("first sync could not be stored", "connection", c.Name, "err", err)
	}
	return c, nil
}

type fetched struct {
	guests []proxmox.Guest
	nodes  []proxmox.Node // best effort: empty when the host list could not be read
}

func (s *Service) fetch(ctx context.Context, url, tokenID, secret, fp string, insecure bool) (fetched, error) {
	cl, err := s.NewClient(proxmox.Config{URL: url, TokenID: tokenID, Secret: secret, Fingerprint: fp, Insecure: insecure})
	if err != nil {
		return fetched{}, err
	}
	g, err := cl.Guests(ctx)
	if err != nil {
		return fetched{}, err
	}
	n, err := cl.Nodes(ctx)
	if err != nil {
		s.log.Debug("could not list Proxmox hosts", "err", err)
	}
	return fetched{g, n}, nil
}

// hostOf returns the host part of a Proxmox URL when it is usable as a
// service address (an IP or a plain host name), else "".
func hostOf(raw string) string {
	u, err := neturl.Parse(raw)
	if err != nil {
		return ""
	}
	h := u.Hostname()
	if ip := cleanIP(h); ip != "" {
		return ip
	}
	if h == "" || len(h) > 253 {
		return ""
	}
	for _, r := range h {
		if !(r == '.' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return ""
		}
	}
	return h
}

// store sanitises and saves a discovery result and records success.
func (s *Service) store(ctx context.Context, connID int64, connURL string, fr fetched) (int, error) {
	var gs []store.Guest
	for _, g := range fr.guests {
		if g.Template {
			continue
		}
		gs = append(gs, store.Guest{
			VMID: g.VMID, Kind: g.Kind,
			Name:   cleanLabel(g.Name, 80, fmt.Sprintf("%s-%d", g.Kind, g.VMID)),
			Node:   cleanLabel(g.Node, 80, "?"),
			Status: cleanLabel(g.Status, 20, "unknown"),
			IP:     cleanIP(g.IP),
		})
	}
	now := s.Now()
	added, err := s.st.SyncGuests(ctx, connID, gs, now)
	if err != nil {
		return 0, err
	}
	// Hosts are optional extras: a failure here never fails the sync. When the
	// cluster does not report an address and there is a single node, the URL
	// the admin typed is the best address there is. An empty list keeps the
	// previous hosts.
	if len(fr.nodes) > 0 {
		var ns []store.Node
		for _, n := range fr.nodes {
			addr := cleanIP(n.IP)
			if addr == "" && len(fr.nodes) == 1 {
				addr = hostOf(connURL)
			}
			ns = append(ns, store.Node{Name: cleanLabel(n.Name, 80, "?"), Addr: addr})
		}
		if err := s.st.SetNodes(ctx, connID, ns); err != nil {
			s.log.Warn("could not store Proxmox hosts", "connection", connID, "err", err)
		}
	}
	return added, s.st.SetConnectionSync(ctx, connID, now, "")
}

// Result summarises one sync.
type Result struct{ Total, Added int }

// Sync refreshes one connection. Failures are recorded on the connection (the
// UI shows them) and returned; known guests are left untouched in that case.
func (s *Service) Sync(ctx context.Context, connID int64) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncLocked(ctx, connID)
}

func (s *Service) syncLocked(ctx context.Context, connID int64) (Result, error) {
	c, err := s.st.GetConnection(ctx, connID)
	if err != nil {
		return Result{}, err
	}
	fail := func(err error) (Result, error) {
		msg, _ := store.CleanText(err.Error(), 300)
		if msg == "" {
			msg = "sync failed"
		}
		if e := s.st.SetConnectionSync(ctx, connID, s.Now(), msg); e != nil {
			s.log.Warn("could not record sync failure", "err", e)
		}
		return Result{}, err
	}
	secret, err := secretbox.Open(s.secret, c.SecretSealed)
	if err != nil {
		return fail(errors.New("cannot decrypt the stored token (was the server secret changed?); edit the connection and enter it again"))
	}
	tctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	fr, err := s.fetch(tctx, c.URL, c.TokenID, secret, c.Fingerprint, c.Insecure)
	if err != nil {
		return fail(err)
	}
	added, err := s.store(ctx, connID, c.URL, fr)
	if err != nil {
		return fail(err)
	}
	return Result{Total: len(fr.guests), Added: added}, nil
}

// SyncAll refreshes every connection, logging (not returning) failures.
func (s *Service) SyncAll(ctx context.Context) {
	conns, err := s.st.ListConnections(ctx)
	if err != nil {
		s.log.Error("discovery: list connections", "err", err)
		return
	}
	for _, c := range conns {
		if ctx.Err() != nil {
			return
		}
		res, err := s.Sync(ctx, c.ID)
		if err != nil {
			s.log.Warn("discovery sync failed", "connection", c.Name, "err", err)
			continue
		}
		s.log.Info("discovery sync", "connection", c.Name, "guests", res.Total, "new", res.Added)
	}
}

// Run syncs once shortly after start and then every interval until ctx ends.
// A non-positive interval disables the background loop.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.SyncAll(ctx)
			timer.Reset(interval)
		}
	}
}

// cleanLabel makes text from the Proxmox API safe to store and display.
func cleanLabel(s string, max int, fallback string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	if s == "" {
		return fallback
	}
	return s
}

func cleanIP(s string) string {
	ip, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return ""
	}
	return ip.String()
}
