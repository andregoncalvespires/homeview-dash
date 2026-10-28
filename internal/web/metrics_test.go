package web

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/andregoncalvespires/homeview-dash/internal/proxmox"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

func metricsEnv(t *testing.T) *env {
	t.Helper()
	e, f := discoveryEnv(t)
	f.metrics = proxmox.Metrics{
		Nodes: []proxmox.NodeMetrics{
			{Name: "pve1", Online: true, CPU: .23, MaxCPU: 8, Mem: 25 << 30, MaxMem: 32 << 30, Uptime: 90000},
			{Name: "pve2", Online: false},
		},
		Storages: []proxmox.StorageMetrics{
			{Node: "pve1", Name: "local-lvm", Used: 40, Total: 100},
			{Node: "pve1", Name: "nas", Shared: true, Used: 5, Total: 10},
			{Node: "pve2", Name: "nas", Shared: true, Used: 5, Total: 10},
		},
		Guests: []proxmox.GuestMetrics{
			{VMID: 100, Kind: "qemu", Name: "haos", Node: "pve1", Running: true, CPU: .1, MaxCPU: 2, Mem: 1, MaxMem: 2, MaxDisk: 32},
			{VMID: 103, Kind: "lxc", Name: "docker", Node: "pve1", Running: true, CPU: .95, MaxCPU: 2, Mem: 1, MaxMem: 2, Disk: 1, MaxDisk: 2},
		},
	}
	admin := e.login("admin@example.com")
	if r, b := e.post(admin, "/admin/connections/save", validConnForm(e.csrf(admin))); r.StatusCode != 303 {
		t.Fatalf("connection: %d %s", r.StatusCode, b)
	}
	return e
}

func apiPX(t *testing.T, e *env, email string) pxResponse {
	t.Helper()
	r, b := e.get(e.login(email), "/api/proxmox")
	if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("api: %d %v", r.StatusCode, r.Header)
	}
	var out pxResponse
	if err := json.Unmarshal([]byte(b), &out); err != nil {
		t.Fatalf("json: %v\n%s", err, b)
	}
	return out
}

func TestMetricsAreOffUntilAnAdminAllowsThem(t *testing.T) {
	e := metricsEnv(t)
	ana := e.login("ana@example.com")
	if r, b := e.get(ana, "/"); r.StatusCode != 200 || strings.Contains(b, `id="px"`) {
		t.Fatalf("a user without access must not get the Proxmox row (%d)", r.StatusCode)
	}
	if got := apiPX(t, e, "ana@example.com"); len(got.Conns) != 0 {
		t.Fatalf("api leaked data: %+v", got)
	}
	if r, b := e.get(ana, "/settings"); strings.Contains(b, "/settings/proxmox") {
		t.Fatalf("settings offers Proxmox options without access (%d)", r.StatusCode)
	}
	if r, _ := e.post(ana, "/settings/proxmox", url.Values{"csrf": {e.csrf(ana)}, "warn": {"70"}, "crit": {"90"}, "refresh": {"30"}}); r.StatusCode != 403 {
		t.Fatalf("saving prefs without access: %d", r.StatusCode)
	}
	if r, _ := e.post(ana, "/admin/users/proxmox", url.Values{"csrf": {e.csrf(ana)}, "id": {"2"}, "nodes": {"1"}}); r.StatusCode != 403 {
		t.Fatalf("a user must not grant access: %d", r.StatusCode)
	}
}

func TestAdminSeesEverything(t *testing.T) {
	e := metricsEnv(t)
	if _, b := e.get(e.login("admin@example.com"), "/"); !strings.Contains(b, `id="px"`) || !strings.Contains(b, `data-refresh="30"`) {
		t.Fatalf("admin dashboard lacks the Proxmox row")
	}
	got := apiPX(t, e, "admin@example.com")
	if len(got.Conns) != 1 || len(got.Conns[0].Nodes) != 2 {
		t.Fatalf("conns = %+v", got.Conns)
	}
	n := got.Conns[0].Nodes[0]
	if n.Name != "pve1" || !n.Online || n.CPU < 22.9 || n.CPU > 23.1 || len(n.Guests) != 2 {
		t.Fatalf("node = %+v", n)
	}
	// The shared storage appears on the first node only.
	if len(n.Storages) != 2 || len(got.Conns[0].Nodes[1].Storages) != 0 {
		t.Fatalf("storages: pve1=%+v pve2=%+v", n.Storages, got.Conns[0].Nodes[1].Storages)
	}
	if !got.Opts.Alerts || !got.Opts.Machines || got.Opts.Warn != 75 || got.Opts.Crit != 90 {
		t.Fatalf("opts = %+v", got.Opts)
	}
}

func TestAdminGrantsAccessPerKind(t *testing.T) {
	e := metricsEnv(t)
	admin := e.login("admin@example.com")
	tok := e.csrf(admin)
	ana, _ := e.st.UserByEmail(context.Background(), "ana@example.com")

	// Nodes only: no machines, no alert colours.
	if r, _ := e.post(admin, "/admin/users/proxmox", url.Values{"csrf": {tok}, "id": {itoa(ana.ID)}, "nodes": {"1"}}); r.StatusCode != 303 {
		t.Fatalf("grant: %d", r.StatusCode)
	}
	got := apiPX(t, e, "ana@example.com")
	if len(got.Conns) != 1 || len(got.Conns[0].Nodes[0].Guests) != 0 || got.Opts.Machines || got.Opts.Alerts {
		t.Fatalf("nodes-only access: %+v", got)
	}
	if _, b := e.get(e.login("ana@example.com"), "/"); !strings.Contains(b, `id="px"`) {
		t.Fatal("the row must appear once nodes are allowed")
	}

	// Machines and alerts too.
	e.post(admin, "/admin/users/proxmox", url.Values{"csrf": {tok}, "id": {itoa(ana.ID)}, "nodes": {"1"}, "machines": {"1"}, "alerts": {"1"}})
	got = apiPX(t, e, "ana@example.com")
	if len(got.Conns[0].Nodes[0].Guests) != 2 || !got.Opts.Machines || !got.Opts.Alerts {
		t.Fatalf("full access: %+v", got)
	}

	// The user can narrow it down but never widen it.
	ana2 := e.login("ana@example.com")
	r, _ := e.post(ana2, "/settings/proxmox", url.Values{"csrf": {e.csrf(ana2)}, "nodes": {"1"}, "cpu": {"1"}, "net": {"1"}, "warn": {"60"}, "crit": {"80"}, "refresh": {"15"}})
	if r.StatusCode != 303 || strings.Contains(r.Header.Get("Location"), "err=") {
		t.Fatalf("save prefs: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	got = apiPX(t, e, "ana@example.com")
	if got.Opts.Machines || got.Opts.Alerts || !got.Opts.Net || got.Opts.Warn != 60 || got.Opts.Refresh != 15 || len(got.Conns[0].Nodes[0].Guests) != 0 {
		t.Fatalf("prefs not applied: %+v", got.Opts)
	}
	if r, _ := e.post(ana2, "/settings/proxmox", url.Values{"csrf": {e.csrf(ana2)}, "warn": {"90"}, "crit": {"80"}, "refresh": {"15"}}); !strings.Contains(r.Header.Get("Location"), "err=err.px_range") {
		t.Fatalf("warn >= crit must be refused: %s", r.Header.Get("Location"))
	}
	if _, b := e.get(ana2, "/settings"); !strings.Contains(b, `action="/settings/proxmox"`) {
		t.Fatal("settings must offer the Proxmox options to an allowed user")
	}

	// Unticking everything removes the row again.
	e.post(admin, "/admin/users/proxmox", url.Values{"csrf": {tok}, "id": {itoa(ana.ID)}})
	if got = apiPX(t, e, "ana@example.com"); len(got.Conns) != 0 {
		t.Fatalf("revoked access still serves data: %+v", got)
	}
}

func TestMachinesFollowServiceVisibility(t *testing.T) {
	e := metricsEnv(t)
	ctx := context.Background()
	ana, _ := e.st.UserByEmail(ctx, "ana@example.com")
	e.st.SetMetricsAccess(ctx, store.MetricsAccess{UserID: ana.ID, Nodes: true, Machines: true})

	// The docker machine backs a service in a group Ana cannot see.
	secret := store.Group{Name: "Segredo", Position: 1, Restricted: true}
	if err := e.st.SaveGroup(ctx, &secret); err != nil {
		t.Fatal(err)
	}
	sv := store.Service{Name: "Docker", InternalURL: "http://192.168.1.13", GroupID: secret.ID, GuestID: guestID(t, e, 103)}
	if err := e.st.SaveService(ctx, &sv); err != nil {
		t.Fatal(err)
	}
	names := func(email string) []string {
		var out []string
		for _, g := range apiPX(t, e, email).Conns[0].Nodes[0].Guests {
			out = append(out, g.Name)
		}
		return out
	}
	if got := names("ana@example.com"); len(got) != 1 || got[0] != "haos" {
		t.Fatalf("ana sees %v, want only haos (docker is behind a group she cannot see)", got)
	}
	if got := names("admin@example.com"); len(got) != 2 {
		t.Fatalf("admin sees %v", got)
	}
	// Once she is listed on the group the machine shows up.
	secret.UserIDs = []int64{ana.ID}
	e.st.SaveGroup(ctx, &secret)
	if got := names("ana@example.com"); len(got) != 2 {
		t.Fatalf("ana sees %v after being granted the group", got)
	}
}

func TestMetricsConnectionError(t *testing.T) {
	e, f := discoveryEnv(t)
	admin := e.login("admin@example.com")
	e.post(admin, "/admin/connections/save", validConnForm(e.csrf(admin)))
	f.merr = proxmox.ErrAuth
	got := apiPX(t, e, "admin@example.com")
	if len(got.Conns) != 1 || got.Conns[0].Err == "" || len(got.Conns[0].Nodes) != 0 {
		t.Fatalf("a failing server must be reported, not hidden: %+v", got.Conns)
	}
}
