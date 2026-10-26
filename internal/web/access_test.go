package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

func seedAccess(t *testing.T, e *env) (casa, kitchen, open store.Group, ana, bob *store.User) {
	t.Helper()
	ctx := context.Background()
	var err error
	if ana, err = e.st.UserByEmail(ctx, "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	if bob, err = e.st.CreateUser(ctx, "bob@example.com", "Bob", store.RoleUser); err != nil {
		t.Fatal(err)
	}
	casa = store.Group{Name: "Casa", Position: 1, Restricted: true, UserIDs: []int64{ana.ID}}
	open = store.Group{Name: "Publico", Position: 3}
	for _, g := range []*store.Group{&casa, &open} {
		if err := e.st.SaveGroup(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	kitchen = store.Group{Name: "Cozinha", Position: 2, ParentID: casa.ID}
	if err := e.st.SaveGroup(ctx, &kitchen); err != nil {
		t.Fatal(err)
	}
	for _, sv := range []store.Service{
		{Name: "SegredoCasa", InternalURL: "http://10.0.0.1", GroupID: casa.ID, Icon: "app:grafana"},
		{Name: "SegredoCozinha", InternalURL: "http://10.0.0.2", GroupID: kitchen.ID},
		{Name: "ServicoPublico", InternalURL: "http://10.0.0.3", GroupID: open.ID},
		{Name: "SemGrupo", InternalURL: "http://10.0.0.4"},
		{Name: "NoHost", InternalURL: "http://10.0.0.5:8006", Node: "pve1", Type: "network"},
	} {
		sv := sv
		if err := e.st.SaveService(ctx, &sv); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func TestGroupAccessHidesServicesInEveryView(t *testing.T) {
	e := newEnv(t)
	seedAccess(t, e)
	// bob has no entry in newEnv: sign him in through the same helper.
	admin, ana, bob := e.login("admin@example.com"), e.login("ana@example.com"), e.login("bob@example.com")

	get := func(cl, v string) string {
		client := map[string]*clientT{"admin": {admin}, "ana": {ana}, "bob": {bob}}[cl].c
		r, _ := e.get(client, "/?view="+v)
		if r.StatusCode != 303 {
			t.Fatalf("view %s: %d", v, r.StatusCode)
		}
		_, b := e.get(client, "/")
		return b
	}
	for _, v := range []string{"groups", "infra", "type"} {
		b := get("bob", v)
		for _, hidden := range []string{"SegredoCasa", "SegredoCozinha"} {
			if strings.Contains(b, hidden) {
				t.Errorf("bob sees %s in the %s view", hidden, v)
			}
		}
		for _, shown := range []string{"ServicoPublico", "SemGrupo"} {
			if !strings.Contains(b, shown) {
				t.Errorf("bob misses %s in the %s view", shown, v)
			}
		}
		for _, who := range []string{"ana", "admin"} {
			b := get(who, v)
			for _, shown := range []string{"SegredoCasa", "SegredoCozinha", "ServicoPublico"} {
				if !strings.Contains(b, shown) {
					t.Errorf("%s misses %s in the %s view", who, shown, v)
				}
			}
		}
	}
	b := get("ana", "groups")
	if !strings.Contains(b, "Casa › Cozinha") {
		t.Error("sub-group should be shown as Casa › Cozinha")
	}
	// Host service: its own section by node; the type override beats the catalog.
	b = get("bob", "infra")
	if !strings.Contains(b, "Proxmox pve1") {
		t.Error("infra view lacks the node section")
	}
	b = get("bob", "type")
	if i, j := strings.Index(b, "NoHost"), strings.Index(b, ">Rede<"); i < 0 || j < 0 || i < j {
		t.Error("NoHost should sit under Rede (type override)")
	}
}

type clientT struct{ c *http.Client }

func TestGroupAdminForm(t *testing.T) {
	e := newEnv(t)
	casa, kitchen, _, ana, bob := seedAccess(t, e)
	ctx := context.Background()
	admin, user := e.login("admin@example.com"), e.login("ana@example.com")
	tok := e.csrf(admin)
	post := func(v url.Values) string {
		v.Set("csrf", tok)
		r, _ := e.post(admin, "/admin/groups/save", v)
		return r.Header.Get("Location")
	}
	if r, _ := e.post(user, "/admin/groups/save", url.Values{"name": {"x"}}); r.StatusCode != 403 && r.StatusCode != 404 {
		t.Errorf("non-admin saved a group: %d", r.StatusCode)
	}
	if loc := post(url.Values{"id": {itoa(kitchen.ID)}, "name": {"Cozinha"}, "parent_id": {itoa(casa.ID)}, "restricted": {"1"}, "users": {itoa(bob.ID), "999999", itoa(ana.ID)}}); !strings.Contains(loc, "ok=") {
		t.Fatalf("save: %s", loc)
	}
	got, _ := e.st.GetGroup(ctx, kitchen.ID)
	if !got.Restricted || got.ParentID != casa.ID || len(got.UserIDs) != 2 {
		t.Fatalf("kitchen = %+v", got)
	}
	// Casa has a sub-group, so it cannot become a sub-group itself; a sub-group cannot be a parent.
	if loc := post(url.Values{"id": {itoa(casa.ID)}, "name": {"Casa"}, "parent_id": {itoa(kitchen.ID)}}); !strings.Contains(loc, "err=err.parent") {
		t.Errorf("nesting under a sub-group: %s", loc)
	}
	if loc := post(url.Values{"name": {"Novo"}, "parent_id": {itoa(kitchen.ID)}}); !strings.Contains(loc, "err=err.parent") {
		t.Errorf("new group under a sub-group: %s", loc)
	}
	if loc := post(url.Values{"id": {itoa(casa.ID)}, "name": {"Casa"}, "parent_id": {itoa(casa.ID)}}); !strings.Contains(loc, "err=err.parent") {
		t.Errorf("own parent: %s", loc)
	}
	// Unticking "restricted" opens the group and forgets the list.
	post(url.Values{"id": {itoa(kitchen.ID)}, "name": {"Cozinha"}, "parent_id": {itoa(casa.ID)}, "users": {itoa(bob.ID)}})
	if got, _ = e.st.GetGroup(ctx, kitchen.ID); got.Restricted || len(got.UserIDs) != 0 {
		t.Errorf("kitchen after unrestrict = %+v", got)
	}
	_, page := e.get(admin, "/admin/groups")
	for _, want := range []string{"Casa", "Cozinha", "Quem vê", `name="restricted"`, "bob@example.com"} {
		if !strings.Contains(page, want) {
			t.Errorf("groups page lacks %q", want)
		}
	}
	if strings.Contains(page, "admin@example.com</span>") {
		t.Error("administrators should not be offered in the access list")
	}
}

func TestYAMLSubgroupsAndAccess(t *testing.T) {
	e := newEnv(t)
	casa, kitchen, _, ana, _ := seedAccess(t, e)
	ctx := context.Background()
	admin := e.login("admin@example.com")
	_, out := e.get(admin, "/admin/yaml/export")
	for _, want := range []string{"restricted: true", `"ana@example.com"`, "groups:", `name: "Cozinha"`, `type: "network"`} {
		if !strings.Contains(out, want) {
			t.Errorf("export lacks %q:\n%s", want, out)
		}
	}
	tok := e.csrf(admin)
	post := func(text string) (int, string) {
		r, b := e.post(admin, "/admin/yaml", url.Values{"csrf": {tok}, "yaml": {text}, "action": {"apply"}})
		return r.StatusCode, b
	}
	// Exported text applies cleanly and changes nothing.
	if c, _ := post(out); c != 303 {
		t.Fatalf("re-apply: %d", c)
	}
	g, _ := e.st.GetGroup(ctx, kitchen.ID)
	c2, _ := e.st.GetGroup(ctx, casa.ID)
	if g.ParentID != casa.ID || !c2.Restricted || len(c2.UserIDs) != 1 || c2.UserIDs[0] != ana.ID {
		t.Fatalf("round trip changed groups: %+v %+v", g, c2)
	}
	// Unknown user is refused; a new sub-group is created under its parent.
	if c, b := post("groups:\n  - name: A\n    restricted: true\n    users:\n      - nobody@example.com\n"); c != 400 || !strings.Contains(b, "nobody@example.com") {
		t.Errorf("unknown user: %d", c)
	}
	src := "groups:\n  - name: Casa\n    groups:\n      - name: Sala\n        services:\n          - name: TV\n            internal_url: http://tv:1\n"
	if c, _ := post(src); c != 303 {
		t.Fatalf("apply nested: %d", c)
	}
	gs, _ := e.st.ListGroups(ctx)
	if len(gs) != 2 {
		t.Fatalf("groups = %+v", gs)
	}
	var sala *store.Group
	for i := range gs {
		if gs[i].Name == "Sala" {
			sala = &gs[i]
		}
	}
	if sala == nil || sala.ParentID != casa.ID {
		t.Fatalf("Sala = %+v", sala)
	}
}

func TestServiceFormKeepsNodeAndType(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	conn := store.Connection{Name: "pve", URL: "https://10.0.0.2:8006", TokenID: "x@pve!t", SecretSealed: "s"}
	if err := e.st.SaveConnection(ctx, &conn); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetNodes(ctx, conn.ID, []store.Node{{Name: "pve1", Addr: "10.0.0.2"}}); err != nil {
		t.Fatal(err)
	}
	admin := e.login("admin@example.com")
	tok := e.csrf(admin)
	r, _ := e.post(admin, "/admin/services/save", url.Values{"csrf": {tok}, "name": {"Proxmox"}, "internal_url": {"https://10.0.0.2:8006"}, "origin": {"h:10.0.0.2"}, "type": {"infra"}})
	if r.StatusCode != 303 {
		t.Fatalf("save: %d", r.StatusCode)
	}
	svcs, _ := e.st.ListServices(ctx)
	if len(svcs) != 1 || svcs[0].Node != "pve1" || svcs[0].Type != "infra" || svcs[0].GuestID != 0 {
		t.Fatalf("service = %+v", svcs)
	}
	_, form := e.get(admin, "/admin/services?edit="+itoa(svcs[0].ID))
	if !strings.Contains(form, `value="h:10.0.0.2" data-addr="10.0.0.2" selected`) || !strings.Contains(form, `value="infra" selected`) {
		t.Error("edit form does not preselect the node and the type")
	}
	if r, _ = e.post(admin, "/admin/services/save", url.Values{"csrf": {tok}, "id": {itoa(svcs[0].ID)}, "name": {"Proxmox"}, "internal_url": {"https://10.0.0.2:8006"}, "type": {"bogus"}}); r.StatusCode != 400 {
		t.Errorf("bogus type accepted: %d", r.StatusCode)
	}
}
