package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

func TestDashboardViews(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	g := store.Group{Name: "Casa"}
	_ = e.st.SaveGroup(ctx, &g)
	conn := store.Connection{Name: "pve", URL: "https://10.0.0.2:8006", TokenID: "x@pve!t", SecretSealed: "s"}
	if err := e.st.SaveConnection(ctx, &conn); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.SyncGuests(ctx, conn.ID, []store.Guest{
		{VMID: 100, Kind: "lxc", Name: "cloudflared", Node: "pve1", Status: "running", IP: "10.0.0.100"},
		{VMID: 102, Kind: "lxc", Name: "zigbee", Node: "pve1", Status: "stopped"},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	guests, _ := e.st.ListGuests(ctx)
	var lxc100 int64
	for _, gu := range guests {
		if gu.VMID == 100 {
			lxc100 = gu.ID
		}
	}
	for _, sv := range []store.Service{
		{Name: "Portainer", InternalURL: "http://10.0.0.100:9000", Icon: "app:portainer", GroupID: g.ID, GuestID: lxc100},
		{Name: "Pihole", InternalURL: "http://10.0.0.3/admin", Icon: "app:pi-hole"},
		{Name: "Minha coisa", InternalURL: "http://10.0.0.4", Icon: "🏠"},
	} {
		sv := sv
		if err := e.st.SaveService(ctx, &sv); err != nil {
			t.Fatal(err)
		}
	}
	admin := e.login("admin@example.com")

	_, b := e.get(admin, "/")
	for _, want := range []string{`id="viewmenu"`, `href="/?view=infra"`, `href="/?view=type"`, `id="reorder"`, `data-gid="` + itoa(g.ID) + `"`} {
		if !strings.Contains(b, want) {
			t.Errorf("groups view lacks %q", want)
		}
	}

	pick := func(v string) string {
		r, _ := e.get(admin, "/?view="+v)
		if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/" {
			t.Fatalf("view=%s: %d %q", v, r.StatusCode, r.Header.Get("Location"))
		}
		_, body := e.get(admin, "/")
		return body
	}
	b = pick("infra")
	for _, want := range []string{"cloudflared", "pve1 · LXC 100 · 10.0.0.100", "Sem máquina", "Portainer", "Pihole", `class="dot running"`} {
		if !strings.Contains(b, want) {
			t.Errorf("infra view lacks %q", want)
		}
	}
	if strings.Contains(b, `id="reorder"`) || strings.Contains(b, "ghandle") {
		t.Error("infra view must not offer rearranging")
	}
	if strings.Index(b, "Portainer") > strings.Index(b, "Sem máquina") || strings.Index(b, "Pihole") < strings.Index(b, "Sem máquina") {
		t.Error("Portainer belongs to its machine, Pihole to 'Sem máquina'")
	}
	if strings.Contains(b, ">zigbee<") { // a machine without services is not shown
		t.Error("empty machine shown")
	}

	b = pick("type")
	for _, want := range []string{">Infraestrutura<", ">Rede<", ">Outros<", `data-gid="tinfra"`, `data-gid="tnetwork"`, `data-gid="tother"`} {
		if !strings.Contains(b, want) {
			t.Errorf("type view lacks %q", want)
		}
	}
	if strings.Contains(b, ">Mídia<") {
		t.Error("empty category shown")
	}

	if b = pick("nonsense"); !strings.Contains(b, `data-gid="tinfra"`) { // an invalid choice keeps the previous one
		t.Error("invalid view changed the mode")
	}
	b = pick("groups")
	if !strings.Contains(b, `id="reorder"`) {
		t.Error("groups view lost the rearrange button")
	}
}

func TestLoginLooksLikeTheDefaultDashboard(t *testing.T) {
	e := newEnv(t)
	_, b := e.get(e.client(), "/login")
	for _, want := range []string{`data-theme="dark"`, `class="has-bg"`, `href="/theme.css?v=`} {
		if !strings.Contains(b, want) {
			t.Errorf("login page lacks %q", want)
		}
	}
	r, css := e.get(e.client(), "/theme.css")
	if r.StatusCode != 200 || !strings.Contains(css, "url(/static/backgrounds/estrelas.webp)") || strings.Contains(r.Header.Get("Cache-Control"), "private") {
		t.Errorf("default theme.css: %d %s", r.StatusCode, css)
	}
}

func TestSearchLivesInTheHeader(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com")
	if _, b := e.get(admin, "/"); strings.Contains(b, `id="searchbox"`) || strings.Contains(b, `id="q"`) {
		t.Error("an empty dashboard has nothing to search")
	}
	sv := store.Service{Name: "A", InternalURL: "http://10.0.0.1"}
	_ = e.st.SaveService(context.Background(), &sv)
	_, b := e.get(admin, "/")
	h := strings.Index(b, "</header>")
	if i := strings.Index(b, `id="searchbox"`); i < 0 || i > h || !strings.Contains(b[:h], `id="searchbtn"`) || !strings.Contains(b[:h], `class="top hassearch"`) {
		t.Error("search box should be inside the header, with its icon button")
	}
	if strings.Index(b[h:], `id="q"`) >= 0 {
		t.Error("search input duplicated in the page body")
	}
}
