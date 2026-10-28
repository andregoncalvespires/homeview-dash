package discovery

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/proxmox"
	"github.com/andregoncalvespires/homeview-dash/internal/secretbox"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

type fake struct {
	guests  []proxmox.Guest
	nodes   []proxmox.Node
	err     error
	metrics proxmox.Metrics
	merr    error
	cfgs    *[]proxmox.Config
}

func (f fake) Guests(context.Context) ([]proxmox.Guest, error)  { return f.guests, f.err }
func (f fake) Metrics(context.Context) (proxmox.Metrics, error) { return f.metrics, f.merr }
func (f fake) Nodes(context.Context) ([]proxmox.Node, error)    { return f.nodes, nil }

func newSvc(t *testing.T, f *fake) (*Service, *store.MemStore) {
	t.Helper()
	st := store.NewMemStore()
	s := New(st, []byte("server-secret"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	var cfgs []proxmox.Config
	f.cfgs = &cfgs
	s.NewClient = func(c proxmox.Config) (Client, error) {
		*f.cfgs = append(*f.cfgs, c)
		return *f, nil
	}
	s.Now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return s, st
}

var good = ConnInput{Name: "Casa", URL: "https://pve.lan:8006/", TokenID: "homeview@pve!dash", Secret: "s3cr3t-uuid"}

func TestSaveConnection(t *testing.T) {
	f := &fake{guests: []proxmox.Guest{
		{VMID: 100, Kind: "qemu", Name: "haos", Node: "pve1", Status: "running", IP: "10.0.0.10"},
		{VMID: 9000, Kind: "qemu", Name: "template", Node: "pve1", Template: true},
	}}
	s, st := newSvc(t, f)
	ctx := context.Background()

	c, err := s.SaveConnection(ctx, 0, good)
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "https://pve.lan:8006" || c.SecretSealed == "" || strings.Contains(c.SecretSealed, good.Secret) {
		t.Fatalf("stored connection: %+v", c)
	}
	if got, err := secretbox.Open([]byte("server-secret"), c.SecretSealed); err != nil || got != good.Secret {
		t.Fatalf("sealed secret does not round-trip: %q %v", got, err)
	}
	gs, _ := st.ListGuests(ctx)
	if len(gs) != 1 || gs[0].Name != "haos" || gs[0].State != store.GuestNew { // template skipped
		t.Fatalf("guests after save: %+v", gs)
	}
	if got, _ := st.GetConnection(ctx, c.ID); got.LastError != "" || got.LastSync.IsZero() {
		t.Fatalf("sync status: %+v", got)
	}
	if cfg := (*f.cfgs)[0]; cfg.Secret != good.Secret || cfg.URL != "https://pve.lan:8006" {
		t.Fatalf("client config: %+v", cfg)
	}

	// Duplicate name.
	if _, err := s.SaveConnection(ctx, 0, good); !errors.Is(err, store.ErrExists) {
		t.Errorf("duplicate: %v", err)
	}
	// Edit without a new secret keeps (and reuses) the stored one.
	edit := good
	edit.Secret, edit.Name = "", "Casa 2"
	c2, err := s.SaveConnection(ctx, c.ID, edit)
	if err != nil || c2.SecretSealed != c.SecretSealed || c2.Name != "Casa 2" {
		t.Fatalf("edit: %+v %v", c2, err)
	}
	if cfg := (*f.cfgs)[len(*f.cfgs)-1]; cfg.Secret != good.Secret {
		t.Errorf("edit did not reuse stored secret: %+v", cfg)
	}
	if _, err := s.SaveConnection(ctx, 9999, edit); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
}

func TestSaveConnectionValidation(t *testing.T) {
	f := &fake{}
	s, st := newSvc(t, f)
	mut := func(fn func(*ConnInput)) ConnInput { in := good; fn(&in); return in }
	cases := map[string]struct {
		in  ConnInput
		key string
	}{
		"name":        {mut(func(i *ConnInput) { i.Name = " " }), "err.name"},
		"broken name": {mut(func(i *ConnInput) { i.Name = "Ca\xc3sa" }), "err.name"},
		"url":         {mut(func(i *ConnInput) { i.URL = "pve:8006" }), "err.conn_url"},
		"token id":    {mut(func(i *ConnInput) { i.TokenID = "root@pam" }), "err.conn_token_id"},
		"token space": {mut(func(i *ConnInput) { i.TokenID = "a b!c" }), "err.conn_token_id"},
		"no secret":   {mut(func(i *ConnInput) { i.Secret = "" }), "err.conn_secret"},
		"secret ws":   {mut(func(i *ConnInput) { i.Secret = "a b" }), "err.conn_secret"},
		"fingerprint": {mut(func(i *ConnInput) { i.Fingerprint = "zz" }), "err.conn_fingerprint"},
	}
	for name, c := range cases {
		_, err := s.SaveConnection(context.Background(), 0, c.in)
		var ie *InputError
		if !errors.As(err, &ie) || ie.Key != c.key {
			t.Errorf("%s: err = %v, want key %s", name, err, c.key)
		}
	}
	if n, _ := st.ListConnections(context.Background()); len(n) != 0 {
		t.Errorf("invalid input stored: %+v", n)
	}
	// A pin wins over "insecure".
	fp := strings.Repeat("ab", 32)
	in := mut(func(i *ConnInput) { i.Fingerprint, i.Insecure = fp, true })
	c, err := s.SaveConnection(context.Background(), 0, in)
	if err != nil || c.Insecure || c.Fingerprint != strings.ToUpper(fp) {
		t.Errorf("pin handling: %+v %v", c, err)
	}
}

func TestFailedTestStoresNothing(t *testing.T) {
	f := &fake{err: proxmox.ErrAuth}
	s, st := newSvc(t, f)
	_, err := s.SaveConnection(context.Background(), 0, good)
	var ce *ConnectError
	if !errors.As(err, &ce) || !errors.Is(err, proxmox.ErrAuth) {
		t.Fatalf("err = %v", err)
	}
	if n, _ := st.ListConnections(context.Background()); len(n) != 0 {
		t.Errorf("connection stored despite failed test: %+v", n)
	}
}

func TestSync(t *testing.T) {
	f := &fake{guests: []proxmox.Guest{
		{VMID: 100, Kind: "qemu", Name: "haos", Node: "pve1", Status: "running", IP: "10.0.0.10"},
		{VMID: 103, Kind: "lxc", Name: "docker", Node: "pve1", Status: "running"},
	}}
	s, st := newSvc(t, f)
	ctx := context.Background()
	c, err := s.SaveConnection(ctx, 0, good)
	if err != nil {
		t.Fatal(err)
	}

	// New guest appears, one disappears.
	f.guests = []proxmox.Guest{
		{VMID: 103, Kind: "lxc", Name: "docker", Node: "pve1", Status: "stopped"},
		{VMID: 104, Kind: "lxc", Name: "nova\x00\xc3\xa7\xc3", Node: "pve1", Status: "running", IP: "not-an-ip"},
		{VMID: 105, Kind: "lxc", Name: "  ", Node: "", Status: ""},
	}
	res, err := s.Sync(ctx, c.ID)
	if err != nil || res.Total != 3 || res.Added != 2 {
		t.Fatalf("Sync = %+v, %v", res, err)
	}
	by := map[int]store.Guest{}
	gs, _ := st.ListGuests(ctx)
	for _, g := range gs {
		by[g.VMID] = g
	}
	if !by[100].Missing || by[103].Status != "stopped" {
		t.Errorf("missing/refresh: %+v %+v", by[100], by[103])
	}
	if by[104].Name != "novaç" || by[104].IP != "" {
		t.Errorf("sanitising: %+v", by[104])
	}
	if by[105].Name != "lxc-105" || by[105].Node != "?" || by[105].Status != "unknown" {
		t.Errorf("fallbacks: %+v", by[105])
	}

	// A failing sync records the error and leaves guests alone.
	f.err = errors.New("connection refused")
	if _, err := s.Sync(ctx, c.ID); err == nil {
		t.Fatal("expected error")
	}
	got, _ := st.GetConnection(ctx, c.ID)
	if got.LastError != "connection refused" {
		t.Errorf("LastError = %q", got.LastError)
	}
	after, _ := st.ListGuests(ctx)
	if len(after) != 4 {
		t.Fatalf("guests changed by failed sync: %+v", after)
	}
	for _, g := range after {
		if g.Missing != by[g.VMID].Missing || g.Status != by[g.VMID].Status {
			t.Errorf("guest %d changed by failed sync: %+v", g.VMID, g)
		}
	}
	f.err = nil
	if _, err := s.Sync(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetConnection(ctx, c.ID); got.LastError != "" {
		t.Errorf("error not cleared: %q", got.LastError)
	}
}

func TestSyncWithWrongServerSecret(t *testing.T) {
	f := &fake{}
	s, st := newSvc(t, f)
	c, err := s.SaveConnection(context.Background(), 0, good)
	if err != nil {
		t.Fatal(err)
	}
	other := New(st, []byte("different-secret"), nil)
	other.NewClient = s.NewClient
	if _, err := other.Sync(context.Background(), c.ID); err == nil || !strings.Contains(err.Error(), "decrypt") {
		t.Errorf("err = %v", err)
	}
	if got, _ := st.GetConnection(context.Background(), c.ID); !strings.Contains(got.LastError, "decrypt") {
		t.Errorf("LastError = %q", got.LastError)
	}
	// Editing without re-entering the token must ask for it again.
	e := good
	e.Secret = ""
	_, err = other.SaveConnection(context.Background(), c.ID, e)
	var ie *InputError
	if !errors.As(err, &ie) || ie.Key != "err.conn_secret" {
		t.Errorf("edit err = %v", err)
	}
}

func TestSyncAllAndRun(t *testing.T) {
	f := &fake{guests: []proxmox.Guest{{VMID: 1, Kind: "lxc", Name: "a", Node: "n", Status: "running"}}}
	s, st := newSvc(t, f)
	ctx := context.Background()
	if _, err := s.SaveConnection(ctx, 0, good); err != nil {
		t.Fatal(err)
	}
	f.guests = append(f.guests, proxmox.Guest{VMID: 2, Kind: "lxc", Name: "b", Node: "n"})
	s.SyncAll(ctx)
	if gs, _ := st.ListGuests(ctx); len(gs) != 2 {
		t.Errorf("SyncAll guests = %d", len(gs))
	}
	// interval <= 0 returns immediately; a cancelled context stops the loop.
	s.Run(ctx, 0)
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	done := make(chan struct{})
	go func() { s.Run(cctx, time.Hour); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on cancelled context")
	}
}

func TestNodeAddresses(t *testing.T) {
	ctx := context.Background()
	// A standalone server without a reported address falls back to the URL host.
	s, st := newSvc(t, &fake{guests: []proxmox.Guest{{VMID: 1, Kind: "lxc", Name: "a", Node: "pve"}}, nodes: []proxmox.Node{{Name: "pve"}}})
	if _, err := s.SaveConnection(ctx, 0, good); err != nil {
		t.Fatal(err)
	}
	ns, _ := st.ListNodes(ctx)
	if len(ns) != 1 || ns[0].Name != "pve" || ns[0].Addr != "pve.lan" {
		t.Fatalf("fallback: %+v", ns)
	}
	// With several nodes, only reported (and valid) addresses are kept.
	s, st = newSvc(t, &fake{nodes: []proxmox.Node{{Name: "a", IP: "10.0.0.1"}, {Name: "b", IP: "not an ip"}, {Name: "c"}}})
	if _, err := s.SaveConnection(ctx, 0, good); err != nil {
		t.Fatal(err)
	}
	ns, _ = st.ListNodes(ctx)
	if len(ns) != 3 || ns[0].Addr != "10.0.0.1" || ns[1].Addr != "" || ns[2].Addr != "" {
		t.Fatalf("multi: %+v", ns)
	}
	if got := hostOf("https://192.168.1.2:8006"); got != "192.168.1.2" {
		t.Errorf("hostOf ip = %q", got)
	}
	if got := hostOf("https://[fe80::1]:8006"); got == "pve" || strings.ContainsAny(got, "[]/ ") {
		t.Errorf("hostOf v6 = %q", got)
	}
}

func TestReadings(t *testing.T) {
	f := &fake{metrics: proxmox.Metrics{
		Nodes:  []proxmox.NodeMetrics{{Name: "pve1", Online: true, CPU: .2}},
		Guests: []proxmox.GuestMetrics{{VMID: 103, Kind: "lxc", Name: "docker", Node: "pve1", Running: true, NetIn: 1000, NetOut: 500}},
	}}
	s, _ := newSvc(t, f)
	ctx := context.Background()
	if _, err := s.SaveConnection(ctx, 0, good); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time { return now }

	rs := s.Readings(ctx, 10*time.Second)
	if len(rs) != 1 || rs[0].ConnName != "Casa" || rs[0].Err != "" || len(rs[0].Nodes) != 1 || rs[0].Guests[0].RateIn != 0 {
		t.Fatalf("first reading: %+v", rs)
	}

	// Within maxAge nothing is fetched again.
	n := len(*f.cfgs)
	s.Now = func() time.Time { return now.Add(5 * time.Second) }
	s.Readings(ctx, 10*time.Second)
	if len(*f.cfgs) != n {
		t.Fatal("a fresh reading must be served from the cache")
	}

	// Later: counters grew, so a rate appears.
	f.metrics.Guests = []proxmox.GuestMetrics{{VMID: 103, Kind: "lxc", Name: "docker", Node: "pve1", Running: true, NetIn: 3000, NetOut: 1500}}
	s.Now = func() time.Time { return now.Add(20 * time.Second) }
	rs = s.Readings(ctx, 10*time.Second)
	if g := rs[0].Guests[0]; g.RateIn != 100 || g.RateOut != 50 {
		t.Fatalf("rates = %v / %v, want 100 / 50", g.RateIn, g.RateOut)
	}

	// A failing server keeps the last figures and reports the error.
	f.merr = errors.New("boom")
	s.Now = func() time.Time { return now.Add(60 * time.Second) }
	rs = s.Readings(ctx, 10*time.Second)
	if rs[0].Err != "boom" || len(rs[0].Nodes) != 1 {
		t.Fatalf("stale reading expected: %+v", rs[0])
	}
}
