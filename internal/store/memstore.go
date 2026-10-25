package store

import (
	"context"
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

func (m *MemStore) DeleteUser(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[id]; !ok {
		return ErrNotFound
	}
	delete(m.users, id)
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

var _ Store = (*MemStore)(nil)
