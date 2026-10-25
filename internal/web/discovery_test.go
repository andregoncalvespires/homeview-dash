package web

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/andregoncalvespires/homeview-dash/internal/discovery"
	"github.com/andregoncalvespires/homeview-dash/internal/proxmox"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

type fakePVE struct {
	guests []proxmox.Guest
	err    error
}

func (f *fakePVE) Guests(context.Context) ([]proxmox.Guest, error) { return f.guests, f.err }

const pveSecret = "very-secret-token-value"

func discoveryEnv(t *testing.T) (*env, *fakePVE) {
	t.Helper()
	e := newEnv(t)
	f := &fakePVE{guests: []proxmox.Guest{
		{VMID: 100, Kind: "qemu", Name: "haos", Node: "pve1", Status: "running", IP: "192.168.1.10"},
		{VMID: 103, Kind: "lxc", Name: "docker", Node: "pve1", Status: "stopped"},
		{VMID: 104, Kind: "lxc", Name: "scratch", Node: "pve2", Status: "running", IP: "192.168.1.14"},
	}}
	d := discovery.New(e.st, []byte("0123456789abcdef0123456789abcdef"), nil)
	d.NewClient = func(proxmox.Config) (discovery.Client, error) { return f, nil }
	e.web.SetDiscovery(d)
	return e, f
}

func validConnForm(csrf string) url.Values {
	return url.Values{"csrf": {csrf}, "name": {"Casa"}, "url": {"https://192.168.1.2:8006"},
		"token_id": {"homeview@pve!dashboard"}, "secret": {pveSecret}}
}

func guestID(t *testing.T, e *env, vmid int) int64 {
	t.Helper()
	gs, _ := e.st.ListGuests(context.Background())
	for _, g := range gs {
		if g.VMID == vmid {
			return g.ID
		}
	}
	t.Fatalf("guest %d not found", vmid)
	return 0
}

func TestDiscoveryFlow(t *testing.T) {
	e, f := discoveryEnv(t)
	ctx := context.Background()
	admin := e.login("admin@example.com")
	tok := e.csrf(admin)

	// Only admins reach these screens and actions.
	ana := e.login("ana@example.com")
	anaTok := e.csrf(ana)
	for _, p := range []string{"/admin/discovery", "/admin/connections"} {
		if r, _ := e.get(ana, p); r.StatusCode != 403 {
			t.Errorf("GET %s as user: %d", p, r.StatusCode)
		}
	}
	for _, p := range []string{"/admin/connections/save", "/admin/discovery/sync", "/admin/discovery/approve"} {
		if r, _ := e.post(ana, p, validConnForm(anaTok)); r.StatusCode != 403 {
			t.Errorf("POST %s as user: %d", p, r.StatusCode)
		}
		if r, _ := e.post(admin, p, url.Values{}); r.StatusCode != 403 { // no CSRF token
			t.Errorf("POST %s without csrf: %d", p, r.StatusCode)
		}
	}

	// Nothing configured yet.
	if r, b := e.get(admin, "/admin/discovery"); r.StatusCode != 200 || !strings.Contains(b, "/admin/connections") || strings.Contains(b, "Sincronizar agora") {
		t.Errorf("empty discovery page: %d", r.StatusCode)
	}

	// A failing connection test stores nothing and explains why.
	f.err = proxmox.ErrAuth
	r, b := e.post(admin, "/admin/connections/save", validConnForm(tok))
	if r.StatusCode != 400 || !strings.Contains(b, "Nada foi salvo") || !strings.Contains(b, "token rejected") {
		t.Errorf("failed test: %d\n%s", r.StatusCode, b)
	}
	if strings.Contains(b, pveSecret) {
		t.Error("secret echoed back in the form")
	}
	if cs, _ := e.st.ListConnections(ctx); len(cs) != 0 {
		t.Fatalf("connection stored: %+v", cs)
	}
	f.err = nil

	// Invalid fields are explained.
	bad := validConnForm(tok)
	bad.Set("token_id", "root@pam")
	if r, b := e.post(admin, "/admin/connections/save", bad); r.StatusCode != 400 || !strings.Contains(b, "usuário@realm!nome") {
		t.Errorf("bad token id: %d", r.StatusCode)
	}

	// Success: connection stored with an encrypted secret and guests read.
	r, _ = e.post(admin, "/admin/connections/save", validConnForm(tok))
	if r.StatusCode != 303 || r.Header.Get("Location") != "/admin/discovery?ok=msg.conn_saved" {
		t.Fatalf("save: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	cs, _ := e.st.ListConnections(ctx)
	if len(cs) != 1 || strings.Contains(cs[0].SecretSealed, pveSecret) {
		t.Fatalf("stored connection: %+v", cs)
	}
	for _, p := range []string{"/admin/connections", "/admin/connections?edit=" + strconv.FormatInt(cs[0].ID, 10), "/admin/discovery"} {
		_, b := e.get(admin, p)
		if strings.Contains(b, pveSecret) || strings.Contains(b, cs[0].SecretSealed) {
			t.Errorf("%s leaks the token secret", p)
		}
	}
	if r, _ := e.post(admin, "/admin/connections/save", validConnForm(tok)); r.StatusCode != 400 { // same name
		t.Errorf("duplicate name: %d", r.StatusCode)
	}

	_, b = e.get(admin, "/admin/discovery")
	for _, want := range []string{"haos", "docker", "scratch", "192.168.1.10", "http://192.168.1.10"} {
		if !strings.Contains(b, want) {
			t.Errorf("discovery page lacks %q", want)
		}
	}

	// Approving without any URL is refused and keeps the form open with the input.
	id100, id103, id104 := guestID(t, e, 100), guestID(t, e, 103), guestID(t, e, 104)
	r, b = e.post(admin, "/admin/discovery/approve", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id103, 10)},
		"name": {"Meu Docker"}, "group": {"pve1"}})
	if r.StatusCode != 400 || !strings.Contains(b, "pelo menos uma URL") || !strings.Contains(b, `value="Meu Docker"`) || !strings.Contains(b, "<details class=\"approve\" open>") {
		t.Errorf("approve without url: %d", r.StatusCode)
	}
	if sv, _ := e.st.ListServices(ctx); len(sv) != 0 {
		t.Fatalf("service created despite error: %+v", sv)
	}
	// Broken encoding is refused too.
	if r, _ := e.post(admin, "/admin/discovery/approve", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id100, 10)},
		"name": {"ha\xc3os"}, "internal_url": {"http://192.168.1.10:8123"}}); r.StatusCode != 400 {
		t.Errorf("broken name: %d", r.StatusCode)
	}

	// Approve haos into the "pve1" group (created on the fly), then a second guest into the same one.
	approve := func(id int64, name, internal, group string) *url.Values {
		v := url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id, 10)}, "name": {name}, "internal_url": {internal},
			"external_url": {"https://ha.example.com"}, "group": {group}, "icon": {"🏠"}}
		r, _ := e.post(admin, "/admin/discovery/approve", v)
		if r.StatusCode != 303 || r.Header.Get("Location") != "/admin/discovery?ok=msg.guest_approved" {
			t.Fatalf("approve %s: %d %q", name, r.StatusCode, r.Header.Get("Location"))
		}
		return &v
	}
	approve(id100, "Home Assistant", "http://192.168.1.10:8123", "pve1")
	approve(id103, "Docker", "http://192.168.1.11:9000", "PVE1") // case-insensitive group match
	groups, _ := e.st.ListGroups(ctx)
	if len(groups) != 1 || groups[0].Name != "pve1" {
		t.Fatalf("groups: %+v", groups)
	}
	svcs, _ := e.st.ListServices(ctx)
	if len(svcs) != 2 || svcs[0].GroupID != groups[0].ID {
		t.Fatalf("services: %+v", svcs)
	}
	g100, _ := e.st.GetGuest(ctx, id100)
	if g100.State != store.GuestApproved || g100.ServiceID == 0 {
		t.Fatalf("guest state: %+v", g100)
	}

	// A second approval of the same guest (double click) creates nothing.
	v := url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id100, 10)}, "name": {"Dup"}, "internal_url": {"http://x.lan"}}
	if r, _ := e.post(admin, "/admin/discovery/approve", v); r.Header.Get("Location") != "/admin/discovery?err=err.guest_state" {
		t.Errorf("double approve: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	if sv, _ := e.st.ListServices(ctx); len(sv) != 2 {
		t.Errorf("duplicate service: %+v", sv)
	}
	// Approved guests cannot be ignored behind the service's back.
	if r, _ := e.post(admin, "/admin/discovery/ignore", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id100, 10)}}); r.Header.Get("Location") != "/admin/discovery?err=err.guest_state" {
		t.Errorf("ignore approved: %q", r.Header.Get("Location"))
	}

	// The dashboard shows the services with their live status dot.
	_, b = e.get(ana, "/")
	if !strings.Contains(b, "Home Assistant") || !strings.Contains(b, `class="dot running"`) || !strings.Contains(b, `class="dot stopped"`) {
		t.Errorf("dashboard lacks services/status dots:\n%s", b)
	}

	// Ignore and restore.
	e.post(admin, "/admin/discovery/ignore", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id104, 10)}})
	if g, _ := e.st.GetGuest(ctx, id104); g.State != store.GuestIgnored {
		t.Fatalf("ignore: %+v", g)
	}
	e.post(admin, "/admin/discovery/restore", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id104, 10)}})
	if g, _ := e.st.GetGuest(ctx, id104); g.State != store.GuestNew {
		t.Fatalf("restore: %+v", g)
	}

	// A guest that disappears from Proxmox is flagged, then can be forgotten.
	f.guests = f.guests[:2]
	if r, _ := e.post(admin, "/admin/discovery/sync", url.Values{"csrf": {tok}}); r.Header.Get("Location") != "/admin/discovery?ok=msg.synced" {
		t.Fatalf("sync: %q", r.Header.Get("Location"))
	}
	if g, _ := e.st.GetGuest(ctx, id104); !g.Missing {
		t.Fatalf("not flagged missing: %+v", g)
	}
	_, b = e.get(admin, "/admin/discovery")
	if !strings.Contains(b, "Não existem mais no Proxmox (1)") {
		t.Error("missing section not shown")
	}
	if r, _ := e.post(admin, "/admin/discovery/forget", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id100, 10)}}); r.Header.Get("Location") != "/admin/discovery?err=err.guest_state" {
		t.Errorf("forgetting a present/approved guest: %q", r.Header.Get("Location"))
	}
	e.post(admin, "/admin/discovery/forget", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id104, 10)}})
	if _, err := e.st.GetGuest(ctx, id104); err == nil {
		t.Error("forgotten guest still exists")
	}

	// A failing sync is reported and its reason is kept on the connection.
	f.err = proxmox.ErrAuth
	if r, _ := e.post(admin, "/admin/discovery/sync", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(cs[0].ID, 10)}}); r.Header.Get("Location") != "/admin/discovery?err=err.sync_failed" {
		t.Errorf("failed sync: %q", r.Header.Get("Location"))
	}
	if _, b := e.get(admin, "/admin/connections"); !strings.Contains(b, "token rejected") {
		t.Error("sync error not shown on the connection")
	}
	f.err = nil

	// Removing the service puts its guest back in the "new" list.
	e.post(admin, "/admin/services/delete", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(g100.ServiceID, 10)}})
	if g, _ := e.st.GetGuest(ctx, id100); g.State != store.GuestNew {
		t.Errorf("guest after service delete: %+v", g)
	}

	// Editing keeps the stored secret when the field is left empty.
	edit := validConnForm(tok)
	edit.Set("id", strconv.FormatInt(cs[0].ID, 10))
	edit.Set("secret", "")
	edit.Set("name", "Casa 2")
	if r, _ := e.post(admin, "/admin/connections/save", edit); r.StatusCode != 303 {
		t.Errorf("edit: %d", r.StatusCode)
	}
	if c, _ := e.st.GetConnection(ctx, cs[0].ID); c.Name != "Casa 2" || c.SecretSealed != cs[0].SecretSealed {
		t.Errorf("edited connection: %+v", c)
	}

	// Deleting the connection drops its guests; remaining services stay.
	e.post(admin, "/admin/connections/delete", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(cs[0].ID, 10)}})
	if gs, _ := e.st.ListGuests(ctx); len(gs) != 0 {
		t.Errorf("guests left: %+v", gs)
	}
	if sv, _ := e.st.ListServices(ctx); len(sv) != 1 {
		t.Errorf("services after connection delete: %+v", sv)
	}
}
