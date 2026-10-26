package web

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

func TestYAMLEditor(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	g := store.Group{Name: "Casa", Position: 0}
	if err := e.st.SaveGroup(ctx, &g); err != nil {
		t.Fatal(err)
	}
	a := store.Service{Name: "HA", InternalURL: "http://10.0.0.5:8123", GroupID: g.ID, GuestID: 0}
	b := store.Service{Name: "Solto", ExternalURL: "https://solto.example.com"}
	for _, s := range []*store.Service{&a, &b} {
		if err := e.st.SaveService(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	admin, ana := e.login("admin@example.com"), e.login("ana@example.com")

	if resp, _ := e.get(ana, "/admin/yaml"); resp.StatusCode != 403 && resp.StatusCode != 404 {
		t.Errorf("non-admin got %d", resp.StatusCode)
	}
	resp, body := e.get(admin, "/admin/yaml/export")
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), "homeview.yml") || !strings.Contains(body, `name: "HA"`) {
		t.Fatalf("export: %d %s", resp.StatusCode, body)
	}
	_, page := e.get(admin, "/admin/yaml")
	tok := e.csrf(admin)
	post := func(text, action string) (int, string) {
		r, b := e.post(admin, "/admin/yaml", url.Values{"csrf": {tok}, "yaml": {text}, "action": {action}})
		return r.StatusCode, b
	}
	_ = page

	// Exporting and applying unchanged changes nothing.
	if c, _ := post(body, "apply"); c != 200 && c != 303 {
		t.Fatalf("apply same: %d", c)
	}
	if got, _ := e.st.GetService(ctx, a.ID); got == nil || got.GroupID != g.ID {
		t.Fatalf("service changed: %+v", got)
	}

	// Check reports without writing.
	edit := strings.Replace(body, `name: "HA"`, `name: "Home Assistant"`, 1)
	c, out := post(edit, "check")
	if c != 200 || !strings.Contains(out, "1 alterados") && !strings.Contains(out, "1 changed") {
		t.Fatalf("check: %d %s", c, out)
	}
	if got, _ := e.st.GetService(ctx, a.ID); got.Name != "HA" {
		t.Fatal("check wrote")
	}

	// New group, new service, move, and removal of what is absent.
	src := `groups:
  - name: Rede
    services:
      - id: ` + itoa(b.ID) + `
        name: "Solto"
        external_url: https://solto.example.com
      - name: Novo
        internal_url: http://192.168.0.1
ungrouped:
  - name: "Home Assistant"
    internal_url: http://10.0.0.5:8123
`
	if c, _ := post(src, "apply"); c != 200 && c != 303 {
		t.Fatalf("apply: %d", c)
	}
	gs, _ := e.st.ListGroups(ctx)
	if len(gs) != 1 || gs[0].Name != "Rede" {
		t.Fatalf("groups: %+v", gs)
	}
	svcs, _ := e.st.ListServices(ctx)
	by := map[string]store.Service{}
	for _, s := range svcs {
		by[s.Name] = s
	}
	if len(svcs) != 3 || by["Solto"].ID != b.ID || by["Solto"].GroupID != gs[0].ID || by["Novo"].GroupID != gs[0].ID || by["Home Assistant"].GroupID != 0 {
		t.Fatalf("services: %+v", svcs)
	}
	if by["Novo"].Position <= by["Solto"].Position {
		t.Errorf("order lost: %+v", by)
	}

	// Bad files change nothing and explain why.
	for _, bad := range []string{
		"groups:\n  - name: A\n    colour: x\n",
		"ungrouped:\n  - name: X\n    internal_url: javascript:alert(1)\n",
		"ungrouped:\n  - name: X\n",
		"ungrouped:\n  - name: X\n    internal_url: http://a:1\n    icon: upload:99999\n",
		"groups:\n  - name: A\n  - name: a\n",
	} {
		before, _ := e.st.ListServices(ctx)
		c, out := post(bad, "apply")
		after, _ := e.st.ListServices(ctx)
		if c != 400 || len(before) != len(after) || !strings.Contains(out, `class="flash err"`) {
			t.Errorf("bad %q: %d", bad, c)
		}
	}
}
