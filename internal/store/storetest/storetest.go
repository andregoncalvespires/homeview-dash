// Package storetest holds a conformance suite that every store.Store
// implementation must pass. It runs against MemStore locally and against the
// SQLite store in CI.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

// Run executes the suite. newStore must return a fresh, empty store.
func Run(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Run("settings", func(t *testing.T) { testSettings(t, newStore(t)) })
	t.Run("users", func(t *testing.T) { testUsers(t, newStore(t)) })
	t.Run("login codes", func(t *testing.T) { testCodes(t, newStore(t)) })
	t.Run("link tokens", func(t *testing.T) { testLinks(t, newStore(t)) })
	t.Run("devices", func(t *testing.T) { testDevices(t, newStore(t)) })
	t.Run("groups and services", func(t *testing.T) { testCatalog(t, newStore(t)) })
	t.Run("discovery", func(t *testing.T) { testDiscovery(t, newStore(t)) })
	t.Run("preferences", func(t *testing.T) { testPrefs(t, newStore(t)) })
	t.Run("icons", func(t *testing.T) { testIcons(t, newStore(t)) })
}

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func isErr(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("got error %v, want %v", got, want)
	}
}

func testSettings(t *testing.T, s store.Store) {
	_, err := s.GetSetting(ctx, "k")
	isErr(t, err, store.ErrNotFound)
	must(t, s.SetSetting(ctx, "k", "v1"))
	must(t, s.SetSetting(ctx, "k", "v2"))
	v, err := s.GetSetting(ctx, "k")
	must(t, err)
	if v != "v2" {
		t.Fatalf("setting = %q", v)
	}
}

func testUsers(t *testing.T, s store.Store) {
	u, err := s.CreateUser(ctx, "a@example.com", "Ann", store.RoleAdmin)
	must(t, err)
	if u.ID == 0 || !u.IsAdmin() {
		t.Fatalf("bad user %+v", u)
	}
	_, err = s.CreateUser(ctx, "a@example.com", "Dup", store.RoleUser)
	isErr(t, err, store.ErrExists)
	_, err = s.CreateUser(ctx, "b@example.com", "Bob", store.RoleUser)
	must(t, err)

	got, err := s.UserByEmail(ctx, "a@example.com")
	must(t, err)
	if got.Name != "Ann" || got.Role != store.RoleAdmin {
		t.Fatalf("UserByEmail = %+v", got)
	}
	_, err = s.UserByEmail(ctx, "nobody@example.com")
	isErr(t, err, store.ErrNotFound)
	byID, err := s.UserByID(ctx, u.ID)
	must(t, err)
	if byID.Email != "a@example.com" {
		t.Fatalf("UserByID = %+v", byID)
	}

	must(t, s.UpdateUserName(ctx, u.ID, "Ann Maria"))
	isErr(t, s.UpdateUserName(ctx, 9999, "x"), store.ErrNotFound)
	if renamed, err := s.UserByID(ctx, u.ID); err != nil || renamed.Name != "Ann Maria" {
		t.Fatalf("UpdateUserName: %+v %v", renamed, err)
	}

	list, err := s.ListUsers(ctx)
	must(t, err)
	if len(list) != 2 || list[0].Email != "a@example.com" {
		t.Fatalf("ListUsers = %+v", list)
	}
	n, err := s.CountAdmins(ctx)
	must(t, err)
	if n != 1 {
		t.Fatalf("CountAdmins = %d", n)
	}

	// Deleting a user removes their devices, code and link tokens.
	now := time.Now()
	_, err = s.CreateDevice(ctx, u.ID, "hash-1", "x", now)
	must(t, err)
	must(t, s.PutLoginCode(ctx, store.LoginCode{UserID: u.ID, Hash: "h", ExpiresAt: now.Add(time.Minute), WindowStart: now}))
	must(t, s.PutLinkToken(ctx, "link-1", u.ID, now.Add(time.Minute)))
	must(t, s.DeleteUser(ctx, u.ID))
	isErr(t, s.DeleteUser(ctx, u.ID), store.ErrNotFound)
	_, err = s.DeviceByHash(ctx, "hash-1")
	isErr(t, err, store.ErrNotFound)
	_, err = s.GetLoginCode(ctx, u.ID)
	isErr(t, err, store.ErrNotFound)
	_, err = s.TakeLinkToken(ctx, "link-1", now)
	isErr(t, err, store.ErrNotFound)
}

func testCodes(t *testing.T, s store.Store) {
	u, err := s.CreateUser(ctx, "a@example.com", "Ann", store.RoleUser)
	must(t, err)
	_, err = s.GetLoginCode(ctx, u.ID)
	isErr(t, err, store.ErrNotFound)

	now := time.Now().Truncate(time.Second)
	c := store.LoginCode{UserID: u.ID, Hash: "h1", ExpiresAt: now.Add(10 * time.Minute), Attempts: 0, WindowStart: now, Sent: 1}
	must(t, s.PutLoginCode(ctx, c))
	c.Hash, c.Attempts, c.Sent = "h2", 2, 3
	must(t, s.PutLoginCode(ctx, c)) // upsert
	got, err := s.GetLoginCode(ctx, u.ID)
	must(t, err)
	if got.Hash != "h2" || got.Attempts != 2 || got.Sent != 3 || !got.ExpiresAt.Equal(c.ExpiresAt) || !got.WindowStart.Equal(now) {
		t.Fatalf("code = %+v", got)
	}
	must(t, s.DeleteLoginCode(ctx, u.ID))
	_, err = s.GetLoginCode(ctx, u.ID)
	isErr(t, err, store.ErrNotFound)
}

func testLinks(t *testing.T, s store.Store) {
	u, err := s.CreateUser(ctx, "a@example.com", "Ann", store.RoleAdmin)
	must(t, err)
	now := time.Now()
	must(t, s.PutLinkToken(ctx, "ok", u.ID, now.Add(time.Minute)))
	must(t, s.PutLinkToken(ctx, "old", u.ID, now.Add(-time.Minute)))

	id, err := s.TakeLinkToken(ctx, "ok", now)
	must(t, err)
	if id != u.ID {
		t.Fatalf("user id = %d", id)
	}
	_, err = s.TakeLinkToken(ctx, "ok", now) // single use
	isErr(t, err, store.ErrNotFound)
	_, err = s.TakeLinkToken(ctx, "old", now) // expired
	isErr(t, err, store.ErrNotFound)
	_, err = s.TakeLinkToken(ctx, "missing", now)
	isErr(t, err, store.ErrNotFound)
}

func testDevices(t *testing.T, s store.Store) {
	u, err := s.CreateUser(ctx, "a@example.com", "Ann", store.RoleUser)
	must(t, err)
	t0 := time.Now().Truncate(time.Second)

	d1, err := s.CreateDevice(ctx, u.ID, "h1", "Firefox/Linux", t0)
	must(t, err)
	_, err = s.CreateDevice(ctx, u.ID, "h2", "Chrome/Android", t0.Add(time.Minute))
	must(t, err)
	_, err = s.CreateDevice(ctx, 9999, "h3", "ghost", t0)
	isErr(t, err, store.ErrNotFound)

	got, err := s.DeviceByHash(ctx, "h1")
	must(t, err)
	if got.ID != d1.ID || got.Label != "Firefox/Linux" || got.UserID != u.ID {
		t.Fatalf("device = %+v", got)
	}
	_, err = s.DeviceByHash(ctx, "nope")
	isErr(t, err, store.ErrNotFound)

	must(t, s.TouchDevice(ctx, d1.ID, t0.Add(time.Hour)))
	list, err := s.ListDevices(ctx, u.ID)
	must(t, err)
	if len(list) != 2 || list[0].ID != d1.ID { // most recently seen first
		t.Fatalf("ListDevices = %+v", list)
	}
	must(t, s.DeleteDevice(ctx, d1.ID))
	isErr(t, s.DeleteDevice(ctx, d1.ID), store.ErrNotFound)
	isErr(t, s.TouchDevice(ctx, d1.ID, t0), store.ErrNotFound)
	list, err = s.ListDevices(ctx, u.ID)
	must(t, err)
	if len(list) != 1 {
		t.Fatalf("ListDevices after delete = %+v", list)
	}
}

func testCatalog(t *testing.T, s store.Store) {
	g1 := &store.Group{Name: "Proxmox", Position: 2}
	g2 := &store.Group{Name: "Media", Position: 1}
	must(t, s.SaveGroup(ctx, g1))
	must(t, s.SaveGroup(ctx, g2))
	if g1.ID == 0 || g2.ID == 0 || g1.ID == g2.ID {
		t.Fatalf("group ids %d %d", g1.ID, g2.ID)
	}
	groups, err := s.ListGroups(ctx)
	must(t, err)
	if len(groups) != 2 || groups[0].Name != "Media" {
		t.Fatalf("ListGroups = %+v", groups)
	}
	g1.Name = "Proxmox VE"
	must(t, s.SaveGroup(ctx, g1))
	got, err := s.GetGroup(ctx, g1.ID)
	must(t, err)
	if got.Name != "Proxmox VE" {
		t.Fatalf("GetGroup = %+v", got)
	}
	isErr(t, s.SaveGroup(ctx, &store.Group{ID: 9999, Name: "x"}), store.ErrNotFound)

	a := &store.Service{Name: "Frigate", Description: "NVR", Icon: "📹", InternalURL: "http://10.0.0.5:5000", ExternalURL: "https://frigate.example.com", GroupID: g1.ID, Position: 1}
	b := &store.Service{Name: "Alone", InternalURL: "http://10.0.0.6"}
	must(t, s.SaveService(ctx, a))
	must(t, s.SaveService(ctx, b))
	isErr(t, s.SaveService(ctx, &store.Service{Name: "Bad", GroupID: 9999}), store.ErrNotFound)

	list, err := s.ListServices(ctx)
	must(t, err)
	if len(list) != 2 {
		t.Fatalf("ListServices = %+v", list)
	}
	// position 0 sorts before position 1
	if list[0].Name != "Alone" || list[0].GroupID != 0 || list[1].GroupID != g1.ID {
		t.Fatalf("ListServices order/groups = %+v", list)
	}
	a.Name = "Frigate NVR"
	a.GroupID = 0
	must(t, s.SaveService(ctx, a))
	sv, err := s.GetService(ctx, a.ID)
	must(t, err)
	if sv.Name != "Frigate NVR" || sv.GroupID != 0 || sv.ExternalURL != "https://frigate.example.com" {
		t.Fatalf("GetService = %+v", sv)
	}
	a.GroupID = g1.ID
	must(t, s.SaveService(ctx, a))

	// Deleting a group keeps its services, now ungrouped.
	must(t, s.DeleteGroup(ctx, g1.ID))
	isErr(t, s.DeleteGroup(ctx, g1.ID), store.ErrNotFound)
	sv, err = s.GetService(ctx, a.ID)
	must(t, err)
	if sv.GroupID != 0 {
		t.Fatalf("service still grouped: %+v", sv)
	}
	must(t, s.DeleteService(ctx, a.ID))
	isErr(t, s.DeleteService(ctx, a.ID), store.ErrNotFound)
	_, err = s.GetService(ctx, a.ID)
	isErr(t, err, store.ErrNotFound)
}

func testDiscovery(t *testing.T, s store.Store) {
	now := time.Now().Truncate(time.Second)

	c := &store.Connection{Name: "casa", URL: "https://pve:8006", TokenID: "u@pve!t", SecretSealed: "v1:abc", Fingerprint: "AA"}
	must(t, s.SaveConnection(ctx, c))
	if c.ID == 0 {
		t.Fatal("no connection id")
	}
	isErr(t, s.SaveConnection(ctx, &store.Connection{Name: "casa", URL: "https://x", TokenID: "a!b", SecretSealed: "s"}), store.ErrExists)
	other := &store.Connection{Name: "sitio", URL: "https://pve2:8006", TokenID: "u@pve!t", SecretSealed: "v1:def", Insecure: true}
	must(t, s.SaveConnection(ctx, other))
	other.Name = "casa" // renaming onto an existing name must fail too
	isErr(t, s.SaveConnection(ctx, other), store.ErrExists)
	isErr(t, s.SaveConnection(ctx, &store.Connection{ID: 9999, Name: "ghost", URL: "u", TokenID: "a!b", SecretSealed: "s"}), store.ErrNotFound)

	c.URL = "https://pve.lan:8006"
	must(t, s.SaveConnection(ctx, c))
	got, err := s.GetConnection(ctx, c.ID)
	must(t, err)
	if got.URL != "https://pve.lan:8006" || got.SecretSealed != "v1:abc" || got.Insecure || got.Fingerprint != "AA" || !got.LastSync.IsZero() {
		t.Fatalf("GetConnection = %+v", got)
	}
	must(t, s.SetConnectionSync(ctx, c.ID, now, "boom"))
	got, _ = s.GetConnection(ctx, c.ID)
	if !got.LastSync.Equal(now) || got.LastError != "boom" {
		t.Fatalf("sync status = %+v", got)
	}
	isErr(t, s.SetConnectionSync(ctx, 9999, now, ""), store.ErrNotFound)
	list, err := s.ListConnections(ctx)
	must(t, err)
	if len(list) != 2 || list[0].Name != "casa" || !list[1].Insecure {
		t.Fatalf("ListConnections = %+v", list)
	}

	// First sync: everything is new.
	found := []store.Guest{
		{VMID: 100, Kind: "qemu", Name: "haos", Node: "pve1", Status: "running", IP: "10.0.0.10"},
		{VMID: 103, Kind: "lxc", Name: "docker", Node: "pve1", Status: "running", IP: "10.0.0.23"},
		{VMID: 104, Kind: "lxc", Name: "old", Node: "pve1", Status: "stopped"},
	}
	added, err := s.SyncGuests(ctx, c.ID, found, now)
	must(t, err)
	if added != 3 {
		t.Fatalf("added = %d", added)
	}
	_, err = s.SyncGuests(ctx, 9999, found, now)
	isErr(t, err, store.ErrNotFound)
	guests, err := s.ListGuests(ctx)
	must(t, err)
	if len(guests) != 3 || guests[0].VMID != 100 || guests[0].State != store.GuestNew || guests[0].Missing ||
		!guests[0].FirstSeen.Equal(now) || guests[1].IP != "10.0.0.23" {
		t.Fatalf("ListGuests = %+v", guests)
	}

	// Approval is derived from services: 103 gets two, 104 is ignored.
	svc := &store.Service{Name: "docker", InternalURL: "http://10.0.0.23", GuestID: guests[1].ID}
	must(t, s.SaveService(ctx, svc))
	svc2 := &store.Service{Name: "portainer", InternalURL: "http://10.0.0.23:9000", GuestID: guests[1].ID}
	must(t, s.SaveService(ctx, svc2))
	isErr(t, s.SaveService(ctx, &store.Service{Name: "x", InternalURL: "http://x", GuestID: 9999}), store.ErrNotFound)
	if got, _ := s.GetService(ctx, svc.ID); got.GuestID != guests[1].ID {
		t.Fatalf("service guest link = %+v", got)
	}
	if err := s.SetGuestState(ctx, guests[1].ID, store.GuestApproved); err == nil {
		t.Fatal("GuestApproved accepted")
	}
	isErr(t, s.SetGuestState(ctx, 9999, store.GuestIgnored), store.ErrNotFound)
	must(t, s.SetGuestState(ctx, guests[2].ID, store.GuestIgnored))
	g103, _ := s.GetGuest(ctx, guests[1].ID)
	g104, _ := s.GetGuest(ctx, guests[2].ID)
	if g103.State != store.GuestApproved || g103.Services != 2 || g104.State != store.GuestIgnored || g104.Services != 0 {
		t.Fatalf("decisions: %+v %+v", g103, g104)
	}

	// Second sync: refreshed data keeps decisions; 100 vanished; 200 is new.
	later := now.Add(time.Hour)
	_, err = s.SyncGuests(ctx, other.ID, nil, later) // another connection is untouched
	must(t, err)
	added, err = s.SyncGuests(ctx, c.ID, []store.Guest{
		{VMID: 103, Kind: "lxc", Name: "docker2", Node: "pve2", Status: "stopped", IP: ""},
		{VMID: 104, Kind: "lxc", Name: "old", Node: "pve1", Status: "stopped"},
		{VMID: 104, Kind: "lxc", Name: "old", Node: "pve1", Status: "stopped"}, // duplicate in one result
		{VMID: 200, Kind: "qemu", Name: "new", Node: "pve1", Status: "running"},
	}, later)
	must(t, err)
	if added != 1 {
		t.Fatalf("added = %d", added)
	}
	g103, _ = s.GetGuest(ctx, g103.ID)
	if g103.Name != "docker2" || g103.Node != "pve2" || g103.Status != "stopped" || g103.IP != "" ||
		g103.State != store.GuestApproved || g103.Services != 2 || g103.Missing || !g103.LastSeen.Equal(later) || !g103.FirstSeen.Equal(now) {
		t.Fatalf("refreshed guest = %+v", g103)
	}
	g100, _ := s.GetGuest(ctx, guests[0].ID)
	if !g100.Missing || g100.State != store.GuestNew {
		t.Fatalf("vanished guest = %+v", g100)
	}
	if all, _ := s.ListGuests(ctx); len(all) != 4 {
		t.Fatalf("guest count = %d", len(all))
	}

	// A guest that comes back is no longer missing.
	_, err = s.SyncGuests(ctx, c.ID, []store.Guest{{VMID: 100, Kind: "qemu", Name: "haos", Node: "pve1", Status: "running"}}, later)
	must(t, err)
	g100, _ = s.GetGuest(ctx, g100.ID)
	if g100.Missing {
		t.Fatal("returning guest still missing")
	}

	// Deleting one of two services keeps the guest approved; deleting the last sends it back to "new".
	must(t, s.DeleteService(ctx, svc.ID))
	if g103, _ = s.GetGuest(ctx, g103.ID); g103.State != store.GuestApproved || g103.Services != 1 {
		t.Fatalf("guest after first delete = %+v", g103)
	}
	must(t, s.DeleteService(ctx, svc2.ID))
	g103, _ = s.GetGuest(ctx, g103.ID)
	if g103.State != store.GuestNew || g103.Services != 0 {
		t.Fatalf("guest after service delete = %+v", g103)
	}
	isErr(t, s.DeleteService(ctx, svc.ID), store.ErrNotFound)

	// Deleting a guest leaves its services in place, unlinked.
	orphan := &store.Service{Name: "orphan", InternalURL: "http://o.lan", GuestID: g103.ID}
	must(t, s.SaveService(ctx, orphan))
	must(t, s.DeleteGuest(ctx, g103.ID))
	isErr(t, s.DeleteGuest(ctx, g103.ID), store.ErrNotFound)
	if got, err := s.GetService(ctx, orphan.ID); err != nil || got.GuestID != 0 {
		t.Fatalf("service after guest delete = %+v, %v", got, err)
	}
	must(t, s.DeleteService(ctx, orphan.ID))

	// Proxmox hosts: replaced per connection, listed in order.
	must(t, s.SetNodes(ctx, c.ID, []store.Node{{Name: "pve2", Addr: "10.0.0.2"}, {Name: "pve1", Addr: "10.0.0.1"}}))
	must(t, s.SetNodes(ctx, other.ID, []store.Node{{Name: "solo"}}))
	ns, _ := s.ListNodes(ctx)
	if len(ns) != 3 || ns[0].Name != "pve1" || ns[0].Addr != "10.0.0.1" || ns[0].ConnID != c.ID || ns[2].Name != "solo" {
		t.Fatalf("nodes = %+v", ns)
	}
	must(t, s.SetNodes(ctx, c.ID, []store.Node{{Name: "pve1", Addr: "10.0.0.9"}}))
	if ns, _ = s.ListNodes(ctx); len(ns) != 2 || ns[0].Addr != "10.0.0.9" {
		t.Fatalf("nodes after replace = %+v", ns)
	}
	isErr(t, s.SetNodes(ctx, 9999, nil), store.ErrNotFound)

	// Deleting a connection removes its guests but never its services.
	keep := &store.Service{Name: "kept", InternalURL: "http://x.lan"}
	must(t, s.SaveService(ctx, keep))
	g200, _ := s.ListGuests(ctx)
	keep.GuestID = g200[len(g200)-1].ID
	must(t, s.SaveService(ctx, keep))
	must(t, s.DeleteConnection(ctx, c.ID))
	isErr(t, s.DeleteConnection(ctx, c.ID), store.ErrNotFound)
	if all, _ := s.ListGuests(ctx); len(all) != 0 {
		t.Fatalf("guests survived their connection: %+v", all)
	}
	if _, err := s.GetService(ctx, keep.ID); err != nil {
		t.Fatalf("service lost with connection: %v", err)
	}
	_, err = s.GetConnection(ctx, c.ID)
	isErr(t, err, store.ErrNotFound)
	if ns, _ = s.ListNodes(ctx); len(ns) != 1 || ns[0].Name != "solo" {
		t.Fatalf("nodes survived their connection: %+v", ns)
	}
}

func testIcons(t *testing.T, s store.Store) {
	at := time.Now()
	png := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	a, err := s.SaveIcon(ctx, "image/png", png, at)
	must(t, err)
	b, err := s.SaveIcon(ctx, "image/png", append([]byte(nil), png...), at)
	must(t, err)
	if a == 0 || a != b {
		t.Fatalf("identical images got ids %d and %d", a, b)
	}
	c, err := s.SaveIcon(ctx, "image/svg+xml", []byte("<svg/>"), at)
	must(t, err)
	if c == a {
		t.Fatal("different images share an id")
	}
	ct, data, err := s.GetIcon(ctx, a)
	must(t, err)
	if ct != "image/png" || string(data) != string(png) {
		t.Fatalf("icon = %q %v", ct, data)
	}
	_, _, err = s.GetIcon(ctx, 9999)
	isErr(t, err, store.ErrNotFound)
	must(t, s.DeleteIcon(ctx, a))
	isErr(t, s.DeleteIcon(ctx, a), store.ErrNotFound)
	if _, _, err := s.GetIcon(ctx, c); err != nil {
		t.Fatal("deleting one icon removed another")
	}
	// A deleted image can be uploaded again and gets a fresh id.
	d, err := s.SaveIcon(ctx, "image/png", png, at)
	must(t, err)
	if d == a {
		t.Fatalf("id %d was reused", d)
	}
}

func testPrefs(t *testing.T, s store.Store) {
	u, err := s.CreateUser(ctx, "a@example.com", "Ann", store.RoleUser)
	must(t, err)

	// Defaults until something is saved.
	p, err := s.GetPrefs(ctx, u.ID)
	must(t, err)
	if p != store.DefaultPrefs(u.ID) || p.Theme != "dark" || p.BgPreset != "estrelas" || p.BgDim != store.DefaultBgDim || p.HasBg {
		t.Fatalf("defaults = %+v", p)
	}
	if _, _, err := s.GetBackground(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("background before upload: %v", err)
	}

	must(t, s.SavePrefs(ctx, store.Prefs{UserID: u.ID, Theme: "dark", Accent: "#7c3aed", BgDim: 50}))
	p, _ = s.GetPrefs(ctx, u.ID)
	if p.Theme != "dark" || p.Accent != "#7c3aed" || p.BgDim != 50 || p.HasBg {
		t.Fatalf("saved = %+v", p)
	}
	isErr(t, s.SavePrefs(ctx, store.Prefs{UserID: 9999, Theme: "dark", BgDim: 10}), store.ErrNotFound)
	if err := s.SavePrefs(ctx, store.Prefs{UserID: u.ID, Theme: "neon", BgDim: 10}); err == nil {
		t.Error("invalid theme accepted")
	}
	if err := s.SavePrefs(ctx, store.Prefs{UserID: u.ID, Theme: "dark", Accent: "red", BgDim: 10}); err == nil {
		t.Error("invalid accent accepted")
	}
	if err := s.SavePrefs(ctx, store.Prefs{UserID: u.ID, Theme: "dark", BgDim: 95}); err == nil {
		t.Error("out-of-range dim accepted")
	}

	// Background: stored as given, saving other prefs keeps it, clearing keeps the rest.
	at := time.Now()
	img := []byte{0x89, 'P', 'N', 'G', 1, 2, 3, 0, 255}
	must(t, s.SetBackground(ctx, u.ID, "image/png", img, at))
	isErr(t, s.SetBackground(ctx, 9999, "image/png", img, at), store.ErrNotFound)
	ct, data, err := s.GetBackground(ctx, u.ID)
	must(t, err)
	if ct != "image/png" || string(data) != string(img) {
		t.Fatalf("background = %q %v", ct, data)
	}
	p, _ = s.GetPrefs(ctx, u.ID)
	if !p.HasBg || p.BgVersion != at.UnixNano() || p.Theme != "dark" {
		t.Fatalf("after upload = %+v", p)
	}
	must(t, s.SavePrefs(ctx, store.Prefs{UserID: u.ID, Theme: "light", BgDim: 20}))
	if _, d, err := s.GetBackground(ctx, u.ID); err != nil || len(d) != len(img) {
		t.Fatalf("SavePrefs lost the background: %v", err)
	}
	must(t, s.SetBackground(ctx, u.ID, "image/jpeg", []byte{0xff, 0xd8, 0xff}, at.Add(time.Second)))
	if ct, _, _ := s.GetBackground(ctx, u.ID); ct != "image/jpeg" {
		t.Fatalf("replace: %q", ct)
	}
	must(t, s.ClearBackground(ctx, u.ID))
	must(t, s.ClearBackground(ctx, u.ID)) // idempotent
	must(t, s.ClearBackground(ctx, 9999)) // unknown user: nothing to do
	if _, _, err := s.GetBackground(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("background after clear: %v", err)
	}
	if p, _ = s.GetPrefs(ctx, u.ID); p.HasBg || p.ShowsBackground() || p.Theme != "light" || p.BgDim != 20 {
		t.Fatalf("after clear = %+v", p)
	}

	// Bundled wallpapers: the default applies until chosen; an upload replaces it and vice versa.
	u3, _ := s.CreateUser(ctx, "c@example.com", "Cy", store.RoleUser)
	if p, _ = s.GetPrefs(ctx, u3.ID); p.BgPreset != store.DefaultBgPreset || !p.ShowsBackground() || p.HasBg {
		t.Fatalf("default wallpaper = %+v", p)
	}
	must(t, s.SetBgPreset(ctx, u3.ID, "dunas")) // first write creates the row
	if p, _ = s.GetPrefs(ctx, u3.ID); p.BgPreset != "dunas" || p.Theme != "dark" || p.BgDim != store.DefaultBgDim {
		t.Fatalf("preset = %+v", p)
	}
	must(t, s.SetBackground(ctx, u3.ID, "image/png", img, at))
	if p, _ = s.GetPrefs(ctx, u3.ID); !p.HasBg || p.BgPreset != "" {
		t.Fatalf("upload should replace the preset: %+v", p)
	}
	must(t, s.SetBgPreset(ctx, u3.ID, "floresta"))
	if p, _ = s.GetPrefs(ctx, u3.ID); p.HasBg || p.BgPreset != "floresta" {
		t.Fatalf("preset should replace the upload: %+v", p)
	}
	if _, _, err := s.GetBackground(ctx, u3.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("upload survived a preset: %v", err)
	}
	must(t, s.SavePrefs(ctx, store.Prefs{UserID: u3.ID, Theme: "dark", BgDim: 10}))
	if p, _ = s.GetPrefs(ctx, u3.ID); p.BgPreset != "floresta" {
		t.Fatalf("SavePrefs lost the preset: %+v", p)
	}
	must(t, s.SetBgPreset(ctx, u3.ID, "")) // none
	if p, _ = s.GetPrefs(ctx, u3.ID); p.ShowsBackground() {
		t.Fatalf("none = %+v", p)
	}
	isErr(t, s.SetBgPreset(ctx, 9999, "dunas"), store.ErrNotFound)

	// A background can be the very first thing a user sets.
	u2, _ := s.CreateUser(ctx, "b@example.com", "Bob", store.RoleUser)
	must(t, s.SetBackground(ctx, u2.ID, "image/webp", []byte("RIFFxxxxWEBP"), at))
	if p, _ = s.GetPrefs(ctx, u2.ID); !p.HasBg || p.Theme != "dark" || p.BgDim != store.DefaultBgDim {
		t.Fatalf("first-write defaults = %+v", p)
	}

	// Deleting the user deletes their preferences.
	must(t, s.DeleteUser(ctx, u2.ID))
	if _, _, err := s.GetBackground(ctx, u2.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("background survived the user: %v", err)
	}
}
