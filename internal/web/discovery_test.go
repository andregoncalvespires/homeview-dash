package web

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/andregoncalvespires/homeview-dash/internal/catalog"
	"github.com/andregoncalvespires/homeview-dash/internal/discovery"
	"github.com/andregoncalvespires/homeview-dash/internal/proxmox"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

type fakePVE struct {
	guests []proxmox.Guest
	nodes  []proxmox.Node
	err    error
}

func (f *fakePVE) Guests(context.Context) ([]proxmox.Guest, error) { return f.guests, f.err }
func (f *fakePVE) Nodes(context.Context) ([]proxmox.Node, error)   { return f.nodes, nil }

const pveSecret = "very-secret-token-value"

func discoveryEnv(t *testing.T) (*env, *fakePVE) {
	t.Helper()
	e := newEnv(t)
	f := &fakePVE{guests: []proxmox.Guest{
		{VMID: 100, Kind: "qemu", Name: "haos", Node: "pve1", Status: "running", IP: "192.168.1.10"},
		{VMID: 103, Kind: "lxc", Name: "docker", Node: "pve1", Status: "stopped"},
		{VMID: 104, Kind: "lxc", Name: "scratch", Node: "pve2", Status: "running", IP: "192.168.1.14"},
	}, nodes: []proxmox.Node{{Name: "pve1", IP: "192.168.1.2"}, {Name: "pve2", IP: "192.168.1.3"}}}
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
		"name": {"Meu Docker"}, "new_group": {"pve1"}})
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
			"external_url": {"https://ha.example.com"}, "new_group": {group}, "icon": {"🏠"}}
		r, _ := e.post(admin, "/admin/discovery/approve", v)
		if r.StatusCode != 303 || r.Header.Get("Location") != "/admin/discovery?ok=msg.guest_approved" {
			t.Fatalf("approve %s: %d %q", name, r.StatusCode, r.Header.Get("Location"))
		}
		return &v
	}
	approve(id100, "Home Assistant", "http://192.168.1.10:8123", "pve1")
	approve(id103, "Docker", "http://192.168.1.11:9000", "PVE1") // typed name matches case-insensitively
	groups, _ := e.st.ListGroups(ctx)
	if len(groups) != 1 || groups[0].Name != "pve1" {
		t.Fatalf("groups: %+v", groups)
	}
	svcs, _ := e.st.ListServices(ctx)
	if len(svcs) != 2 || svcs[0].GroupID != groups[0].ID {
		t.Fatalf("services: %+v", svcs)
	}
	g100, _ := e.st.GetGuest(ctx, id100)
	if g100.State != store.GuestApproved || g100.Services != 1 {
		t.Fatalf("guest state: %+v", g100)
	}

	// A machine can host more services: a second one is added, linked to the same guest.
	approve(id100, "Zigbee2MQTT", "http://192.168.1.10:8080", "pve1")
	approve(id100, "Z2M garagem", "http://192.168.1.10:8081", "pve1")
	if g, _ := e.st.GetGuest(ctx, id100); g.Services != 3 || g.State != store.GuestApproved {
		t.Fatalf("after extra services: %+v", g)
	}
	if sv, _ := e.st.ListServices(ctx); len(sv) != 4 {
		t.Errorf("services: %+v", sv)
	}
	_, pageHTML := e.get(admin, "/admin/discovery")
	for _, want := range []string{"Zigbee2MQTT", "Z2M garagem", `data-addr="192.168.1.10"`, "Adicionar outro serviço", `class="app-model"`} {
		if !strings.Contains(pageHTML, want) {
			t.Errorf("discovery page lacks %q", want)
		}
	}
	// The services page offers machines and Proxmox hosts as origins.
	_, svcPage := e.get(admin, "/admin/services")
	for _, want := range []string{"haos · VM 100 · 192.168.1.10", `data-addr="192.168.1.10"`, "Proxmox pve1 · 192.168.1.2", `value="h:192.168.1.3"`} {
		if !strings.Contains(svcPage, want) {
			t.Errorf("services page lacks %q", want)
		}
	}
	// A manual service picked from that list links to the machine; a host address does not.
	post := func(origin, name string, want int) {
		v := url.Values{"csrf": {tok}, "name": {name}, "internal_url": {"http://192.168.1.10:1234"}, "origin": {origin}}
		if r, _ := e.post(admin, "/admin/services/save", v); r.StatusCode != want {
			t.Fatalf("save %s: %d, want %d", name, r.StatusCode, want)
		}
	}
	post("g:"+strconv.FormatInt(id100, 10), "Linked", 303)
	post("h:192.168.1.2", "OnHost", 303)
	post("g:99999", "Dangling", 400) // the form is shown again with an error
	var linked, onHost, dangling *store.Service
	all, _ := e.st.ListServices(ctx)
	for i := range all {
		switch all[i].Name {
		case "Linked":
			linked = &all[i]
		case "OnHost":
			onHost = &all[i]
		case "Dangling":
			dangling = &all[i]
		}
	}
	if linked == nil || linked.GuestID != id100 || onHost == nil || onHost.GuestID != 0 {
		t.Fatalf("origin handling: %+v %+v", linked, onHost)
	}
	if dangling != nil {
		t.Errorf("service pointing at an unknown machine was saved: %+v", dangling)
	}
	for _, sv := range []*store.Service{linked, onHost} {
		e.post(admin, "/admin/services/delete", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(sv.ID, 10)}})
	}
	if ns, _ := e.st.ListNodes(ctx); len(ns) != 2 || ns[0].Addr != "192.168.1.2" {
		t.Errorf("hosts were not stored: %+v", ns)
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

	// Removing every service puts the guest back in the "new" list.
	for _, sv := range func() []store.Service { l, _ := e.st.ListServices(ctx); return l }() {
		if sv.GuestID == id100 {
			e.post(admin, "/admin/services/delete", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(sv.ID, 10)}})
		}
	}
	if g, _ := e.st.GetGuest(ctx, id100); g.State != store.GuestNew || g.Services != 0 {
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

func TestCatalogIconsAreEmbedded(t *testing.T) {
	for _, a := range catalog.All() {
		b, err := assets.ReadFile("static/icons/" + a.File)
		if err != nil || len(b) < 100 {
			t.Errorf("%s: icon %s missing from the embedded files (%v)", a.ID, a.File, err)
		}
	}
}

func TestCatalogInServiceAndDiscoveryForms(t *testing.T) {
	e, f := discoveryEnv(t)
	f.guests = []proxmox.Guest{
		{VMID: 100, Kind: "qemu", Name: "HomeAssistant", Node: "pve1", Status: "running", IP: "192.168.1.10"},
		{VMID: 101, Kind: "lxc", Name: "pihole", Node: "pve1", Status: "running", IP: "192.168.1.2"},
		{VMID: 102, Kind: "lxc", Name: "scratch", Node: "pve1", Status: "running", IP: "192.168.1.3"},
	}
	ctx := context.Background()
	admin := e.login("admin@example.com")
	tok := e.csrf(admin)
	e.post(admin, "/admin/connections/save", validConnForm(tok))

	// Recognised guests get icon + usual port; unknown ones keep the plain default.
	_, b := e.get(admin, "/admin/discovery")
	for _, want := range []string{
		`value="http://192.168.1.10:8123"`, `value="app:home-assistant"`, "Reconhecido como",
		`value="http://192.168.1.2/admin"`, `value="app:pi-hole"`, `src="/static/icons/pi-hole.svg"`,
		`value="http://192.168.1.3"`, `value="📦"`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("discovery page lacks %q", want)
		}
	}

	// The service form offers the catalog and keeps the selection when editing.
	_, b = e.get(admin, "/admin/services")
	for _, want := range []string{`<select class="app-model"`, `value="grafana"`, `data-port="3000"`, `value="zigbee2mqtt"`} {
		if !strings.Contains(b, want) {
			t.Errorf("services form lacks %q", want)
		}
	}
	form := url.Values{"csrf": {tok}, "name": {"Grafana"}, "icon": {"app:grafana"}, "internal_url": {"http://10.0.0.9:3000"}}
	if r, _ := e.post(admin, "/admin/services/save", form); r.StatusCode != 303 {
		t.Fatalf("save with app icon: %d", r.StatusCode)
	}
	list, _ := e.st.ListServices(ctx)
	_, b = e.get(admin, "/admin/services?edit="+strconv.FormatInt(list[0].ID, 10))
	if !strings.Contains(b, `value="grafana" data-name="Grafana"`) || !strings.Contains(b, " selected>Grafana") {
		t.Errorf("edit form does not preselect the application:\n%s", b)
	}
	// ...and the dashboard shows the embedded icon, never the raw reference.
	_, b = e.get(admin, "/")
	if !strings.Contains(b, `src="/static/icons/grafana.svg"`) || strings.Contains(b, "app:grafana") {
		t.Errorf("dashboard icon:\n%s", b)
	}
	// Unknown catalog references are rejected.
	form.Set("icon", "app:does-not-exist")
	if r, _ := e.post(admin, "/admin/services/save", form); r.StatusCode != 400 {
		t.Errorf("unknown app icon accepted: %d", r.StatusCode)
	}
}

func TestGroupPickingInBothForms(t *testing.T) {
	e, f := discoveryEnv(t)
	ctx := context.Background()
	admin := e.login("admin@example.com")
	tok := e.csrf(admin)
	existing := &store.Group{Name: "pve1", Position: 1}
	other := &store.Group{Name: "Media", Position: 2}
	e.st.SaveGroup(ctx, existing)
	e.st.SaveGroup(ctx, other)
	e.post(admin, "/admin/connections/save", validConnForm(tok))

	// Discovery: the list shows every existing group, and the one named like the
	// node is preselected instead of proposing to create a duplicate.
	_, b := e.get(admin, "/admin/discovery")
	for _, g := range []*store.Group{existing, other} {
		if !strings.Contains(b, `<option value="`+strconv.FormatInt(g.ID, 10)+`"`) {
			t.Errorf("group %q missing from the list", g.Name)
		}
	}
	if !strings.Contains(b, `<option value="`+strconv.FormatInt(existing.ID, 10)+`" selected>pve1`) {
		t.Error("group named like the node is not preselected")
	}
	if !strings.Contains(b, `<label>Ou criar um novo grupo<input name="new_group" value="" `) {
		t.Error("new-group field should start empty when the node group exists")
	}
	if strings.Contains(b, "<datalist") {
		t.Error("the old free-text/datalist group field is still there")
	}
	// pve2 has no group yet: it proposes creating it.
	if !strings.Contains(b, `name="new_group" value="pve2"`) {
		t.Error("missing proposal to create the node's group")
	}

	// Picking an existing group from the list works.
	id104 := guestID(t, e, 104)
	r, _ := e.post(admin, "/admin/discovery/approve", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id104, 10)},
		"name": {"Scratch"}, "internal_url": {"http://192.168.1.14:8080"}, "group_id": {strconv.FormatInt(other.ID, 10)}})
	if r.StatusCode != 303 {
		t.Fatalf("approve with listed group: %d", r.StatusCode)
	}
	if sv, _ := e.st.ListServices(ctx); len(sv) != 1 || sv[0].GroupID != other.ID {
		t.Fatalf("service group: %+v", sv)
	}
	// A typed name wins over the list and creates the group.
	id100 := guestID(t, e, 100)
	e.post(admin, "/admin/discovery/approve", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id100, 10)},
		"name": {"HA"}, "internal_url": {"http://192.168.1.10:8123"}, "group_id": {strconv.FormatInt(other.ID, 10)}, "new_group": {"Casa Inteligente"}})
	groups, _ := e.st.ListGroups(ctx)
	if len(groups) != 3 {
		t.Fatalf("groups: %+v", groups)
	}
	sv, _ := e.st.ListServices(ctx)
	var ha store.Service
	for _, x := range sv {
		if x.Name == "HA" {
			ha = x
		}
	}
	if g, _ := e.st.GetGroup(ctx, ha.GroupID); g == nil || g.Name != "Casa Inteligente" {
		t.Errorf("typed group not used: %+v", ha)
	}
	// A validation error keeps what the admin picked/typed.
	id103 := guestID(t, e, 103)
	_, b = e.post(admin, "/admin/discovery/approve", url.Values{"csrf": {tok}, "id": {strconv.FormatInt(id103, 10)},
		"name": {"Docker"}, "group_id": {strconv.FormatInt(other.ID, 10)}, "new_group": {"Rascunho"}})
	if !strings.Contains(b, `name="new_group" value="Rascunho"`) || !strings.Contains(b, `value="`+strconv.FormatInt(other.ID, 10)+`" selected`) {
		t.Error("form lost the group choice after an error")
	}

	// Services form: same pattern, and it can create a group on the fly.
	_, b = e.get(admin, "/admin/services")
	if !strings.Contains(b, `<select name="group_id"`) || !strings.Contains(b, `name="new_group"`) {
		t.Error("services form lacks list + new group field")
	}
	form := url.Values{"csrf": {tok}, "name": {"Grafana"}, "internal_url": {"http://10.0.0.9:3000"}, "new_group": {"Monitoramento"}}
	if r, _ := e.post(admin, "/admin/services/save", form); r.StatusCode != 303 {
		t.Fatalf("service with new group: %d", r.StatusCode)
	}
	groups, _ = e.st.ListGroups(ctx)
	var created *store.Group
	for i := range groups {
		if groups[i].Name == "Monitoramento" {
			created = &groups[i]
		}
	}
	if created == nil {
		t.Fatalf("group not created: %+v", groups)
	}
	// Reusing the name (any case) does not duplicate it.
	form.Set("name", "Prometheus")
	form.Set("new_group", "monitoramento")
	e.post(admin, "/admin/services/save", form)
	if after, _ := e.st.ListGroups(ctx); len(after) != len(groups) {
		t.Errorf("duplicate group created: %+v", after)
	}
	// Failure keeps the typed text; invalid text is refused.
	form.Set("internal_url", "")
	form.Set("external_url", "")
	form.Set("new_group", "Meu Grupo")
	if r, b := e.post(admin, "/admin/services/save", form); r.StatusCode != 400 || !strings.Contains(b, `name="new_group" value="Meu Grupo"`) {
		t.Errorf("failed save lost the new group text: %d", r.StatusCode)
	}
	form.Set("internal_url", "http://a.example.com")
	form.Set("new_group", "Ru\xc3im")
	if r, _ := e.post(admin, "/admin/services/save", form); r.StatusCode != 400 {
		t.Errorf("broken group name accepted: %d", r.StatusCode)
	}
	_ = f
}

func TestNavLinksAreMutedAndMarkCurrentPage(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com")
	_, b := e.get(admin, "/admin/groups")
	if !strings.Contains(b, `<a href="/admin/groups" aria-current="page">`) || strings.Contains(b, `<a href="/admin/users" aria-current`) {
		t.Errorf("current page not marked:\n%s", b)
	}
	r, body := e.get(admin, "/static/app.css")
	if r.StatusCode != 200 || !strings.Contains(body, ".top nav a { color: var(--muted); }") {
		t.Error("nav links are not in the muted colour")
	}
}
