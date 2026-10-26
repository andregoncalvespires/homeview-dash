package web

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

func seedLayout(t *testing.T, e *env) (g1, g2 store.Group, svc map[string]store.Service) {
	t.Helper()
	ctx := context.Background()
	g1, g2 = store.Group{Name: "Casa", Position: 0}, store.Group{Name: "Rede", Position: 1}
	for _, g := range []*store.Group{&g1, &g2} {
		if err := e.st.SaveGroup(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	svc = map[string]store.Service{}
	for i, d := range []struct {
		name string
		gid  int64
	}{{"A", g1.ID}, {"B", g1.ID}, {"C", g2.ID}, {"D", 0}} {
		s := store.Service{Name: d.name, InternalURL: "http://" + strings.ToLower(d.name) + ".lan", GroupID: d.gid, Position: i}
		if err := e.st.SaveService(ctx, &s); err != nil {
			t.Fatal(err)
		}
		svc[d.name] = s
	}
	return
}

func TestLayoutSaving(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	g1, g2, s := seedLayout(t, e)
	admin, ana := e.login("admin@example.com"), e.login("ana@example.com")
	tok, anaTok := e.csrf(admin), e.csrf(ana)
	post := func(c interface{}, token, layout string) int {
		cl := admin
		if c == "ana" {
			cl = ana
		}
		r, _ := e.post(cl, "/admin/layout", url.Values{"csrf": {token}, "layout": {layout}})
		return r.StatusCode
	}
	j := func(format string, a ...any) string { return fmt.Sprintf(format, a...) }

	// Rules for who may call it.
	if c := post("ana", anaTok, "[]"); c != 403 {
		t.Errorf("non-admin: %d", c)
	}
	if c := post("admin", "bad", "[]"); c != 403 {
		t.Errorf("bad csrf: %d", c)
	}
	for _, bad := range []string{"", "nope", `{"a":1}`, `[{"group":"x"}]`} {
		if c := post("admin", tok, bad); c != 400 {
			t.Errorf("layout %q: %d", bad, c)
		}
	}

	// Swap the groups and move B into the second one, D into the first.
	layout := j(`[{"group":%d,"services":[%d,%d]},{"group":%d,"services":[%d,%d]},{"group":0,"services":[]}]`,
		g2.ID, s["C"].ID, s["B"].ID, g1.ID, s["D"].ID, s["A"].ID)
	if c := post("admin", tok, layout); c != 200 {
		t.Fatalf("save: %d", c)
	}
	groups, _ := e.st.ListGroups(ctx)
	if len(groups) != 2 || groups[0].ID != g2.ID || groups[1].ID != g1.ID || groups[0].Name != "Rede" {
		t.Fatalf("groups: %+v", groups)
	}
	svcs, _ := e.st.ListServices(ctx)
	var order []string
	byName := map[string]store.Service{}
	for _, sv := range svcs {
		order = append(order, sv.Name)
		byName[sv.Name] = sv
	}
	if strings.Join(order, "") != "CBDA" || byName["B"].GroupID != g2.ID || byName["D"].GroupID != g1.ID || byName["A"].GroupID != g1.ID {
		t.Errorf("services: %v %+v", order, byName)
	}
	// Names and links are untouched.
	if byName["B"].Name != "B" || byName["B"].InternalURL != "http://b.lan" {
		t.Errorf("service changed: %+v", byName["B"])
	}

	// The dashboard follows that order.
	_, b := e.get(admin, "/")
	if i, k := strings.Index(b, "Rede"), strings.Index(b, "Casa"); i < 0 || k < 0 || i > k {
		t.Error("dashboard does not show the new group order")
	}
	if strings.Index(b, `data-sid="`+fmt.Sprint(s["C"].ID)+`"`) > strings.Index(b, `data-sid="`+fmt.Sprint(s["B"].ID)+`"`) {
		t.Error("dashboard does not show the new card order")
	}

	// Moving a card out of every group puts it under "other"; unknown ids are ignored.
	layout = j(`[{"group":%d,"services":[%d,999999]},{"group":424242,"services":[%d]},{"group":0,"services":[%d]}]`,
		g1.ID, s["A"].ID, s["B"].ID, s["C"].ID)
	if c := post("admin", tok, layout); c != 200 {
		t.Fatalf("save 2: %d", c)
	}
	if got, _ := e.st.GetService(ctx, s["C"].ID); got.GroupID != 0 {
		t.Errorf("C should be ungrouped: %+v", got)
	}
	if got, _ := e.st.GetService(ctx, s["B"].ID); got.GroupID != g2.ID { // its group in the payload does not exist: left alone
		t.Errorf("B moved to an unknown group: %+v", got)
	}
	if gs, _ := e.st.ListGroups(ctx); len(gs) != 2 {
		t.Errorf("groups changed: %+v", gs)
	}
}

func TestDashboardRearrangeControls(t *testing.T) {
	e := newEnv(t)
	seedLayout(t, e)
	empty := store.Group{Name: "Vazio", Position: 9}
	if err := e.st.SaveGroup(context.Background(), &empty); err != nil {
		t.Fatal(err)
	}
	admin, ana := e.login("admin@example.com"), e.login("ana@example.com")

	_, b := e.get(admin, "/")
	for _, want := range []string{`id="reorder"`, `data-save="/admin/layout"`, `data-label-done="Concluir"`, `class="ghandle"`, `class="cols"`, "Vazio", `group group-empty`} {
		if !strings.Contains(b, want) {
			t.Errorf("admin dashboard lacks %q", want)
		}
	}
	_, b = e.get(ana, "/")
	for _, bad := range []string{`id="reorder"`, "ghandle", "Vazio", "/admin/layout"} {
		if strings.Contains(b, bad) {
			t.Errorf("regular user sees %q", bad)
		}
	}
	if !strings.Contains(b, `class="cols"`) || !strings.Contains(b, `data-sid=`) {
		t.Error("regular user lost the columns")
	}
}
