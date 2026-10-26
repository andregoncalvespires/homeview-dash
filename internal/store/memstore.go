package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemStore is an in-memory Store used by tests.
type MemStore struct {
	mu       sync.Mutex
	next     int64
	settings map[string]string
	users    map[int64]*User
	codes    map[int64]LoginCode
	links    map[string]linkToken
	devices  map[int64]deviceRow
	groups   map[int64]*Group
	services map[int64]*Service
	conns    map[int64]*Connection
	guests   map[int64]*Guest
	nodes    map[int64][]Node
	icons    map[int64]*iconRow
	prefs    map[int64]*prefsRow
}

type prefsRow struct {
	Prefs
	bgType string
	bgData []byte
}

type linkToken struct {
	userID  int64
	expires time.Time
}

type deviceRow struct {
	Device
	hash string
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{
		settings: map[string]string{},
		users:    map[int64]*User{},
		codes:    map[int64]LoginCode{},
		links:    map[string]linkToken{},
		devices:  map[int64]deviceRow{},
		groups:   map[int64]*Group{},
		services: map[int64]*Service{},
		conns:    map[int64]*Connection{},
		guests:   map[int64]*Guest{},
		nodes:    map[int64][]Node{},
		icons:    map[int64]*iconRow{},
		prefs:    map[int64]*prefsRow{},
	}
}

func (m *MemStore) id() int64 { m.next++; return m.next }

func (m *MemStore) Close() error { return nil }

func (m *MemStore) GetSetting(_ context.Context, name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.settings[name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (m *MemStore) SetSetting(_ context.Context, name, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings[name] = value
	return nil
}

func (m *MemStore) CreateUser(_ context.Context, email, name string, role Role) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Email == email {
			return nil, ErrExists
		}
	}
	u := &User{ID: m.id(), Email: email, Name: name, Role: role, CreatedAt: time.Now()}
	m.users[u.ID] = u
	c := *u
	return &c, nil
}

func (m *MemStore) UserByEmail(_ context.Context, email string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Email == email {
			c := *u
			return &c, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemStore) UserByID(_ context.Context, id int64) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *u
	return &c, nil
}

func (m *MemStore) ListUsers(_ context.Context) ([]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

func (m *MemStore) UpdateUserName(_ context.Context, id int64, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	u.Name = name
	return nil
}

func (m *MemStore) DeleteUser(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[id]; !ok {
		return ErrNotFound
	}
	delete(m.users, id)
	delete(m.prefs, id)
	delete(m.codes, id)
	for h, l := range m.links {
		if l.userID == id {
			delete(m.links, h)
		}
	}
	for did, d := range m.devices {
		if d.UserID == id {
			delete(m.devices, did)
		}
	}
	return nil
}

func (m *MemStore) CountAdmins(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, u := range m.users {
		if u.Role == RoleAdmin {
			n++
		}
	}
	return n, nil
}

func (m *MemStore) GetLoginCode(_ context.Context, userID int64) (*LoginCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.codes[userID]
	if !ok {
		return nil, ErrNotFound
	}
	return &c, nil
}

func (m *MemStore) PutLoginCode(_ context.Context, c LoginCode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes[c.UserID] = c
	return nil
}

func (m *MemStore) DeleteLoginCode(_ context.Context, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.codes, userID)
	return nil
}

func (m *MemStore) PutLinkToken(_ context.Context, hash string, userID int64, expires time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links[hash] = linkToken{userID: userID, expires: expires}
	return nil
}

func (m *MemStore) TakeLinkToken(_ context.Context, hash string, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.links[hash]
	if !ok {
		return 0, ErrNotFound
	}
	delete(m.links, hash)
	if now.After(l.expires) {
		return 0, ErrNotFound
	}
	return l.userID, nil
}

func (m *MemStore) CreateDevice(_ context.Context, userID int64, tokenHash, label string, now time.Time) (*Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[userID]; !ok {
		return nil, ErrNotFound
	}
	d := deviceRow{Device: Device{ID: m.id(), UserID: userID, Label: label, CreatedAt: now, LastSeen: now}, hash: tokenHash}
	m.devices[d.ID] = d
	c := d.Device
	return &c, nil
}

func (m *MemStore) DeviceByHash(_ context.Context, tokenHash string) (*Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.devices {
		if d.hash == tokenHash {
			c := d.Device
			return &c, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemStore) TouchDevice(_ context.Context, id int64, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.LastSeen = now
	m.devices[id] = d
	return nil
}

func (m *MemStore) ListDevices(_ context.Context, userID int64) ([]Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Device
	for _, d := range m.devices {
		if d.UserID == userID {
			out = append(out, d.Device)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, nil
}

func (m *MemStore) DeleteDevice(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.devices[id]; !ok {
		return ErrNotFound
	}
	delete(m.devices, id)
	return nil
}

func (m *MemStore) ListGroups(_ context.Context) ([]Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Group, 0, len(m.groups))
	for _, g := range m.groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (m *MemStore) GetGroup(_ context.Context, id int64) (*Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *g
	return &c, nil
}

func (m *MemStore) SaveGroup(_ context.Context, g *Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g.ID == 0 {
		g.ID = m.id()
	} else if _, ok := m.groups[g.ID]; !ok {
		return ErrNotFound
	}
	c := *g
	m.groups[g.ID] = &c
	return nil
}

func (m *MemStore) DeleteGroup(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[id]; !ok {
		return ErrNotFound
	}
	delete(m.groups, id)
	for _, s := range m.services {
		if s.GroupID == id {
			s.GroupID = 0
		}
	}
	return nil
}

func (m *MemStore) ListServices(_ context.Context) ([]Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Service, 0, len(m.services))
	for _, s := range m.services {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (m *MemStore) GetService(_ context.Context, id int64) (*Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.services[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *s
	return &c, nil
}

func (m *MemStore) SaveService(_ context.Context, s *Service) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.GroupID != 0 {
		if _, ok := m.groups[s.GroupID]; !ok {
			return ErrNotFound
		}
	}
	if s.GuestID != 0 {
		if _, ok := m.guests[s.GuestID]; !ok {
			return ErrNotFound
		}
	}
	if s.ID == 0 {
		s.ID = m.id()
	} else if _, ok := m.services[s.ID]; !ok {
		return ErrNotFound
	}
	c := *s
	m.services[s.ID] = &c
	return nil
}

func (m *MemStore) DeleteService(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.services[id]; !ok {
		return ErrNotFound
	}
	delete(m.services, id)
	return nil
}

func (m *MemStore) ListConnections(_ context.Context) ([]Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Connection, 0, len(m.conns))
	for _, c := range m.conns {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *MemStore) GetConnection(_ context.Context, id int64) (*Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (m *MemStore) SaveConnection(_ context.Context, c *Connection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.conns {
		if o.ID != c.ID && o.Name == c.Name {
			return ErrExists
		}
	}
	if c.ID == 0 {
		c.ID = m.id()
	} else if _, ok := m.conns[c.ID]; !ok {
		return ErrNotFound
	}
	cp := *c
	m.conns[c.ID] = &cp
	return nil
}

func (m *MemStore) DeleteConnection(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.conns[id]; !ok {
		return ErrNotFound
	}
	delete(m.conns, id)
	delete(m.nodes, id)
	for gid, g := range m.guests {
		if g.ConnID == id {
			m.dropGuest(gid)
		}
	}
	return nil
}

func (m *MemStore) SetConnectionSync(_ context.Context, id int64, at time.Time, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[id]
	if !ok {
		return ErrNotFound
	}
	c.LastSync, c.LastError = at, errMsg
	return nil
}

func (m *MemStore) ListGuests(_ context.Context) ([]Guest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Guest, 0, len(m.guests))
	for _, g := range m.guests {
		out = append(out, m.guestView(g))
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ConnID != b.ConnID {
			return a.ConnID < b.ConnID
		}
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		return a.VMID < b.VMID
	})
	return out, nil
}

func (m *MemStore) GetGuest(_ context.Context, id int64) (*Guest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.guests[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := m.guestView(g)
	return &cp, nil
}

func (m *MemStore) SyncGuests(_ context.Context, connID int64, found []Guest, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.conns[connID]; !ok {
		return 0, ErrNotFound
	}
	seen := map[int]bool{}
	added := 0
	for _, f := range found {
		seen[f.VMID] = true
		var cur *Guest
		for _, g := range m.guests {
			if g.ConnID == connID && g.VMID == f.VMID {
				cur = g
				break
			}
		}
		if cur == nil {
			g := &Guest{ID: m.id(), ConnID: connID, VMID: f.VMID, Kind: f.Kind, Name: f.Name,
				Node: f.Node, Status: f.Status, IP: f.IP, State: GuestNew, FirstSeen: now, LastSeen: now}
			m.guests[g.ID] = g
			added++
			continue
		}
		cur.Kind, cur.Name, cur.Node, cur.Status, cur.IP = f.Kind, f.Name, f.Node, f.Status, f.IP
		cur.Missing, cur.LastSeen = false, now
	}
	for _, g := range m.guests {
		if g.ConnID == connID && !seen[g.VMID] {
			g.Missing = true
		}
	}
	return added, nil
}

func (m *MemStore) SetGuestState(_ context.Context, id int64, state GuestState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state != GuestNew && state != GuestIgnored {
		return fmt.Errorf("store: guest state %q cannot be set", state)
	}
	g, ok := m.guests[id]
	if !ok {
		return ErrNotFound
	}
	g.State = state
	return nil
}

// guestView returns a copy with the derived fields filled in. Caller holds mu.
func (m *MemStore) guestView(g *Guest) Guest {
	cp := *g
	for _, s := range m.services {
		if s.GuestID == g.ID {
			cp.Services++
		}
	}
	switch {
	case cp.State == GuestIgnored:
	case cp.Services > 0:
		cp.State = GuestApproved
	default:
		cp.State = GuestNew
	}
	return cp
}

func (m *MemStore) SetNodes(_ context.Context, connID int64, nodes []Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.conns[connID]; !ok {
		return ErrNotFound
	}
	m.nodes[connID] = append([]Node(nil), nodes...)
	for i := range m.nodes[connID] {
		m.nodes[connID][i].ConnID = connID
	}
	return nil
}

func (m *MemStore) ListNodes(_ context.Context) ([]Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Node
	for _, ns := range m.nodes {
		out = append(out, ns...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ConnID != out[j].ConnID {
			return out[i].ConnID < out[j].ConnID
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (m *MemStore) DeleteGuest(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.guests[id]; !ok {
		return ErrNotFound
	}
	m.dropGuest(id)
	return nil
}

// dropGuest removes a guest; its services stay, unlinked. Caller holds mu.
func (m *MemStore) dropGuest(id int64) {
	delete(m.guests, id)
	for _, s := range m.services {
		if s.GuestID == id {
			s.GuestID = 0
		}
	}
}

func (m *MemStore) prefsFor(userID int64) *prefsRow {
	r, ok := m.prefs[userID]
	if !ok {
		r = &prefsRow{Prefs: DefaultPrefs(userID)}
		m.prefs[userID] = r
	}
	return r
}

func (m *MemStore) GetPrefs(_ context.Context, userID int64) (Prefs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.prefs[userID]; ok {
		return r.Prefs, nil
	}
	return DefaultPrefs(userID), nil
}

func (m *MemStore) SavePrefs(_ context.Context, p Prefs) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[p.UserID]; !ok {
		return ErrNotFound
	}
	if !ValidTheme(p.Theme) || p.BgDim < 0 || p.BgDim > 80 {
		return errors.New("store: invalid preferences")
	}
	if _, ok := NormalizeAccent(p.Accent); !ok {
		return errors.New("store: invalid accent colour")
	}
	r := m.prefsFor(p.UserID)
	r.Theme, r.Accent, r.BgDim = p.Theme, p.Accent, p.BgDim
	return nil
}

func (m *MemStore) GetBackground(_ context.Context, userID int64) (string, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.prefs[userID]
	if !ok || !r.HasBg {
		return "", nil, ErrNotFound
	}
	return r.bgType, append([]byte(nil), r.bgData...), nil
}

func (m *MemStore) SetBackground(_ context.Context, userID int64, contentType string, data []byte, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[userID]; !ok {
		return ErrNotFound
	}
	r := m.prefsFor(userID)
	r.HasBg, r.bgType, r.bgData, r.BgVersion = true, contentType, append([]byte(nil), data...), at.UnixNano()
	r.BgPreset = ""
	return nil
}

func (m *MemStore) ClearBackground(_ context.Context, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.prefs[userID]; ok {
		r.HasBg, r.bgType, r.bgData, r.BgVersion = false, "", nil, 0
		r.BgPreset = ""
	}
	return nil
}

func (m *MemStore) SetBgPreset(_ context.Context, userID int64, preset string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[userID]; !ok {
		return ErrNotFound
	}
	r := m.prefsFor(userID)
	r.HasBg, r.bgType, r.bgData, r.BgVersion = false, "", nil, 0
	r.BgPreset = preset
	return nil
}

var _ Store = (*MemStore)(nil)

type iconRow struct {
	ct   string
	data []byte
}

func (m *MemStore) SaveIcon(_ context.Context, contentType string, data []byte, _ time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range m.icons {
		if r.ct == contentType && string(r.data) == string(data) {
			return id, nil
		}
	}
	id := m.id()
	m.icons[id] = &iconRow{ct: contentType, data: append([]byte(nil), data...)}
	return id, nil
}

func (m *MemStore) GetIcon(_ context.Context, id int64) (string, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.icons[id]
	if !ok {
		return "", nil, ErrNotFound
	}
	return r.ct, append([]byte(nil), r.data...), nil
}

func (m *MemStore) DeleteIcon(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.icons[id]; !ok {
		return ErrNotFound
	}
	delete(m.icons, id)
	return nil
}
