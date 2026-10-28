package proxmox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const tokenID, secret = "homeview@pve!dash", "11111111-2222-3333-4444-555555555555"

// fakePVE mimics the handful of endpoints HomeView uses.
func fakePVE(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	wrap := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "PVEAPIToken="+tokenID+"="+secret {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/api2/json/cluster/resources", wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") == "" {
			w.Write([]byte(`{"data":[
			 {"type":"node","node":"pve1","status":"online","cpu":0.23,"maxcpu":8,"mem":8000,"maxmem":32000,"disk":10,"maxdisk":100,"uptime":99},
			 {"type":"node","node":"pve2","status":"offline"},
			 {"type":"storage","node":"pve1","storage":"local-lvm","status":"available","disk":40,"maxdisk":100},
			 {"type":"storage","node":"pve1","storage":"nas","shared":1,"status":"available","disk":500,"maxdisk":1000},
			 {"type":"storage","node":"pve1","storage":"gone","status":"unknown","disk":0,"maxdisk":0},
			 {"type":"lxc","vmid":103,"name":"docker","node":"pve1","status":"running","cpu":0.5,"maxcpu":2,"mem":100,"maxmem":512,"disk":6,"maxdisk":8,"netin":10,"netout":20,"uptime":5},
			 {"type":"qemu","vmid":9000,"name":"tpl","node":"pve1","template":1}
			]}`))
			return
		}
		w.Write([]byte(`{"data":[
		 {"type":"qemu","vmid":100,"name":"haos","node":"pve1","status":"running","template":0},
		 {"type":"lxc","vmid":103,"name":"docker","node":"pve1","status":"running"},
		 {"type":"lxc","vmid":104,"name":"off","node":"pve1","status":"stopped"},
		 {"type":"qemu","vmid":9000,"name":"tpl","node":"pve1","status":"stopped","template":1},
		 {"type":"storage","id":"storage/pve1/local","node":"pve1"},
		 {"type":"qemu","vmid":0,"name":"bad","node":"pve1"}
		]}`))
	}))
	mux.HandleFunc("/api2/json/nodes/pve1/lxc/103/interfaces", wrap(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"name":"lo","inet":"127.0.0.1/8"},{"name":"eth0","inet":"192.168.77.23/24"}]}`))
	}))
	mux.HandleFunc("/api2/json/nodes/pve1/qemu/100/agent/network-get-interfaces", wrap(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"result":[
		 {"name":"lo","ip-addresses":[{"ip-address":"127.0.0.1","ip-address-type":"ipv4"}]},
		 {"name":"docker0","ip-addresses":[{"ip-address":"172.17.0.1","ip-address-type":"ipv4"}]},
		 {"name":"eth0","ip-addresses":[{"ip-address":"fe80::1","ip-address-type":"ipv6"},{"ip-address":"192.168.77.10","ip-address-type":"ipv4"}]}]}}`))
	}))
	mux.HandleFunc("/api2/json/cluster/status", wrap(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"type":"cluster","name":"lab"},
		 {"type":"node","name":"pve1","ip":"192.168.77.2"},{"type":"node","name":"pve2","ip":""},{"type":"quorum"}]}`))
	}))
	return httptest.NewTLSServer(mux)
}

func fp(srv *httptest.Server) string {
	sum := sha256.Sum256(srv.Certificate().Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func TestGuests(t *testing.T) {
	srv := fakePVE(t)
	defer srv.Close()
	c, err := New(Config{URL: srv.URL, TokenID: tokenID, Secret: secret, Insecure: true})
	if err != nil {
		t.Fatal(err)
	}
	gs, err := c.Guests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 4 {
		t.Fatalf("want 4 guests (storage and vmid 0 ignored), got %+v", gs)
	}
	by := map[int]Guest{}
	for _, g := range gs {
		by[g.VMID] = g
	}
	if g := by[100]; g.Kind != KindQEMU || g.Name != "haos" || g.IP != "192.168.77.10" {
		t.Errorf("vm 100: %+v", g)
	}
	if g := by[103]; g.Kind != KindLXC || g.IP != "192.168.77.23" || g.Node != "pve1" {
		t.Errorf("lxc 103: %+v", g)
	}
	if g := by[104]; g.IP != "" || g.Status != "stopped" {
		t.Errorf("stopped guest should have no IP lookup: %+v", g)
	}
	if !by[9000].Template {
		t.Errorf("template flag lost: %+v", by[9000])
	}
}

func TestTLSModes(t *testing.T) {
	srv := fakePVE(t)
	defer srv.Close()
	ctx := context.Background()
	try := func(cfg Config) error {
		cfg.URL, cfg.TokenID, cfg.Secret = srv.URL, tokenID, secret
		c, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Guests(ctx)
		return err
	}
	if err := try(Config{}); err == nil {
		t.Error("self-signed certificate accepted without pin or insecure flag")
	}
	if err := try(Config{Fingerprint: fp(srv)}); err != nil {
		t.Errorf("matching pin rejected: %v", err)
	}
	colons := colon(fp(srv))
	if err := try(Config{Fingerprint: strings.ToLower(colons)}); err != nil {
		t.Errorf("pin with colons/lower case rejected: %v", err)
	}
	if err := try(Config{Fingerprint: strings.Repeat("AB", 32)}); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("wrong pin accepted: %v", err)
	}
}

func TestAuthAndErrors(t *testing.T) {
	srv := fakePVE(t)
	defer srv.Close()
	c, _ := New(Config{URL: srv.URL, TokenID: tokenID, Secret: "wrong", Insecure: true})
	if _, err := c.Guests(context.Background()); !errors.Is(err, ErrAuth) {
		t.Errorf("err = %v", err)
	}
	// The secret must never leak into errors.
	c, _ = New(Config{URL: "https://127.0.0.1:1", TokenID: tokenID, Secret: secret, Insecure: true})
	_, err := c.Guests(context.Background())
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("err = %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	good := Config{URL: "https://pve:8006", TokenID: tokenID, Secret: secret}
	if _, err := New(good); err != nil {
		t.Fatal(err)
	}
	bad := []Config{
		{URL: "ftp://pve", TokenID: tokenID, Secret: secret},
		{URL: "https://u:p@pve", TokenID: tokenID, Secret: secret},
		{URL: "https://pve:8006?x=1", TokenID: tokenID, Secret: secret},
		{URL: "pve:8006", TokenID: tokenID, Secret: secret},
		{URL: good.URL, TokenID: "root@pam", Secret: secret},
		{URL: good.URL, TokenID: tokenID, Secret: ""},
		{URL: good.URL, TokenID: tokenID, Secret: "a b"},
		{URL: good.URL, TokenID: tokenID, Secret: secret, Fingerprint: "xyz"},
	}
	for i, cfg := range bad {
		if _, err := New(cfg); err == nil {
			t.Errorf("case %d accepted: %+v", i, cfg)
		}
	}
	if u, _ := ValidateURL(" https://pve:8006/ "); u != "https://pve:8006" {
		t.Errorf("ValidateURL = %q", u)
	}
}

func TestPickIP(t *testing.T) {
	cases := []struct {
		in   []candidate
		want string
	}{
		{nil, ""},
		{[]candidate{{"lo", "127.0.0.1/8"}}, ""},
		{[]candidate{{"docker0", "172.17.0.1/16"}, {"eth0", "10.0.0.5/24"}}, "10.0.0.5"},
		{[]candidate{{"eth0", "169.254.1.1/16"}}, ""},
		{[]candidate{{"eth1", "203.0.113.9/24"}, {"eth0", "192.168.1.9/24"}}, "192.168.1.9"},
		{[]candidate{{"eth1", "203.0.113.9"}}, "203.0.113.9"},
		{[]candidate{{"eth0", "garbage"}, {"eth0", "::1"}}, ""},
	}
	for _, c := range cases {
		if got := PickIP(c.in); got != c.want {
			t.Errorf("PickIP(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeFingerprint(t *testing.T) {
	f := strings.Repeat("ab", 32)
	if NormalizeFingerprint(colon(strings.ToUpper(f))) != strings.ToUpper(f) {
		t.Error("colon form not normalised")
	}
	for _, s := range []string{"", "abc", strings.Repeat("zz", 32), f + "00"} {
		if NormalizeFingerprint(s) != "" {
			t.Errorf("accepted %q", s)
		}
	}
}

func TestNodes(t *testing.T) {
	srv := fakePVE(t)
	defer srv.Close()
	c, _ := New(Config{URL: srv.URL, TokenID: tokenID, Secret: secret, Insecure: true})
	ns, err := c.Nodes(context.Background())
	if err != nil || len(ns) != 2 || ns[0] != (Node{"pve1", "192.168.77.2"}) || ns[1].Name != "pve2" || ns[1].IP != "" {
		t.Fatalf("nodes = %+v, %v", ns, err)
	}
}

func TestMetrics(t *testing.T) {
	srv := fakePVE(t)
	defer srv.Close()
	c, _ := New(Config{URL: srv.URL, TokenID: tokenID, Secret: secret, Insecure: true})
	m, err := c.Metrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Nodes) != 2 || m.Nodes[0].Name != "pve1" || !m.Nodes[0].Online || m.Nodes[0].CPU != 0.23 ||
		m.Nodes[0].MaxMem != 32000 || m.Nodes[1].Online {
		t.Fatalf("nodes = %+v", m.Nodes)
	}
	if len(m.Storages) != 2 || m.Storages[0].Name != "local-lvm" || m.Storages[1].Name != "nas" || !m.Storages[1].Shared {
		t.Fatalf("storages (unavailable ones are dropped) = %+v", m.Storages)
	}
	if len(m.Guests) != 1 || m.Guests[0].VMID != 103 || !m.Guests[0].Running || m.Guests[0].NetOut != 20 {
		t.Fatalf("guests (templates are dropped) = %+v", m.Guests)
	}
}
