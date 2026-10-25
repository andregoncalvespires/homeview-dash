// Package store defines HomeView's data model and the storage interface.
//
// Two implementations exist: sqlstore (production, SQLite) and MemStore
// (in-memory, used by tests).
package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	// ErrNotFound is returned when a row does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrExists is returned when a unique constraint is violated.
	ErrExists = errors.New("store: already exists")
)

// Role is the permission level of a user.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleUser }

// User is a pre-registered person who may sign in.
type User struct {
	ID        int64
	Email     string
	Name      string
	Role      Role
	CreatedAt time.Time
}

// IsAdmin reports whether the user is an administrator.
func (u *User) IsAdmin() bool { return u != nil && u.Role == RoleAdmin }

// Device is a browser/device that has been authorised for a user.
type Device struct {
	ID        int64
	UserID    int64
	Label     string
	CreatedAt time.Time
	LastSeen  time.Time
}

// LoginCode is the single active e-mail verification code of a user.
type LoginCode struct {
	UserID      int64
	Hash        string
	ExpiresAt   time.Time
	Attempts    int       // wrong guesses so far
	WindowStart time.Time // start of the rate-limit window
	Sent        int       // codes sent inside the window
}

// Group is a named section of the dashboard.
type Group struct {
	ID       int64
	Name     string
	Position int
}

// Service is one link shown on the dashboard.
type Service struct {
	ID          int64
	Name        string
	Description string
	Icon        string // emoji/short text, or an http(s) image URL
	InternalURL string
	ExternalURL string
	GroupID     int64 // 0 means "no group"
	Position    int
}

// Connection is a discovery source: one Proxmox server or cluster. The API
// token secret is kept encrypted (see package secretbox); the store never sees
// the plaintext.
type Connection struct {
	ID           int64
	Name         string
	URL          string
	TokenID      string
	SecretSealed string
	Insecure     bool   // skip TLS verification
	Fingerprint  string // pinned SHA-256 certificate fingerprint, "" if unused
	LastSync     time.Time
	LastError    string // "" when the last sync worked
}

// GuestState is where a discovered VM/container is in the approval flow.
type GuestState string

const (
	GuestNew      GuestState = "new"      // found, waiting for an admin decision
	GuestApproved GuestState = "approved" // became a Service
	GuestIgnored  GuestState = "ignored"  // admin does not want it on the dashboard
)

// Valid reports whether g is a known state.
func (g GuestState) Valid() bool {
	return g == GuestNew || g == GuestApproved || g == GuestIgnored
}

// Guest is a VM or container found through a Connection.
type Guest struct {
	ID        int64
	ConnID    int64
	VMID      int
	Kind      string // "lxc" or "qemu"
	Name      string
	Node      string
	Status    string // running, stopped, ...
	IP        string // best effort, may be empty
	State     GuestState
	Missing   bool  // not returned by the last successful sync
	ServiceID int64 // set when State == GuestApproved
	FirstSeen time.Time
	LastSeen  time.Time
}

// Store is the persistence interface used by the rest of the application.
type Store interface {
	Close() error

	// Settings (key/value).
	GetSetting(ctx context.Context, name string) (string, error)
	SetSetting(ctx context.Context, name, value string) error

	// Users. Emails must already be normalised with NormalizeEmail.
	CreateUser(ctx context.Context, email, name string, role Role) (*User, error)
	UserByEmail(ctx context.Context, email string) (*User, error)
	UserByID(ctx context.Context, id int64) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)
	UpdateUserName(ctx context.Context, id int64, name string) error
	DeleteUser(ctx context.Context, id int64) error
	CountAdmins(ctx context.Context) (int, error)

	// E-mail login codes (one active code per user).
	GetLoginCode(ctx context.Context, userID int64) (*LoginCode, error)
	PutLoginCode(ctx context.Context, c LoginCode) error
	DeleteLoginCode(ctx context.Context, userID int64) error

	// One-time login links (created from the CLI).
	PutLinkToken(ctx context.Context, hash string, userID int64, expires time.Time) error
	// TakeLinkToken atomically consumes a token and returns its user id.
	TakeLinkToken(ctx context.Context, hash string, now time.Time) (int64, error)

	// Devices.
	CreateDevice(ctx context.Context, userID int64, tokenHash, label string, now time.Time) (*Device, error)
	DeviceByHash(ctx context.Context, tokenHash string) (*Device, error)
	TouchDevice(ctx context.Context, id int64, now time.Time) error
	ListDevices(ctx context.Context, userID int64) ([]Device, error)
	DeleteDevice(ctx context.Context, id int64) error

	// Groups.
	ListGroups(ctx context.Context) ([]Group, error)
	GetGroup(ctx context.Context, id int64) (*Group, error)
	SaveGroup(ctx context.Context, g *Group) error // insert when ID == 0
	DeleteGroup(ctx context.Context, id int64) error

	// Services.
	ListServices(ctx context.Context) ([]Service, error)
	GetService(ctx context.Context, id int64) (*Service, error)
	SaveService(ctx context.Context, s *Service) error // insert when ID == 0
	// DeleteService also sends the guest that produced it back to GuestNew.
	DeleteService(ctx context.Context, id int64) error

	// Discovery connections.
	ListConnections(ctx context.Context) ([]Connection, error)
	GetConnection(ctx context.Context, id int64) (*Connection, error)
	SaveConnection(ctx context.Context, c *Connection) error // insert when ID == 0; ErrExists on duplicate name
	DeleteConnection(ctx context.Context, id int64) error    // also removes its guests (services stay)
	// SetConnectionSync records the outcome of a sync attempt.
	SetConnectionSync(ctx context.Context, id int64, at time.Time, errMsg string) error

	// Discovered guests.
	ListGuests(ctx context.Context) ([]Guest, error) // ordered by connection, node, vmid
	GetGuest(ctx context.Context, id int64) (*Guest, error)
	// SyncGuests merges a successful discovery result: unknown guests are added
	// as GuestNew, known ones are refreshed (decisions are kept), and guests of
	// the connection that were not returned are flagged Missing. It returns how
	// many guests were new.
	SyncGuests(ctx context.Context, connID int64, found []Guest, now time.Time) (added int, err error)
	// SetGuestState changes the decision. serviceID is required (and must
	// exist) for GuestApproved and is cleared otherwise.
	SetGuestState(ctx context.Context, id int64, state GuestState, serviceID int64) error
	DeleteGuest(ctx context.Context, id int64) error
}

// NormalizeEmail trims and lower-cases an address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// CleanText trims s and reports whether it is acceptable user-visible text:
// valid UTF-8, no control characters, and at most max characters. Text that
// fails the UTF-8 check usually comes from a terminal with the wrong encoding
// (e.g. a backspace that erased only half of an accented letter).
func CleanText(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return s, false
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return s, false
		}
	}
	return s, true
}
