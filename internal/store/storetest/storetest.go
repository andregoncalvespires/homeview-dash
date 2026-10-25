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
