// Package sqlstore implements store.Store on top of SQLite using the pure-Go
// modernc.org/sqlite driver (no CGO, so the binary stays static).
package sqlstore

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

//go:embed schema.sql
var schemaV1 string

//go:embed schema_v2.sql
var schemaV2 string

//go:embed schema_v3.sql
var schemaV3 string

//go:embed schema_v4.sql
var schemaV4 string

const splitMarker = "-- +split"

// migrations are applied in order; PRAGMA user_version records how many ran.
var migrations = []string{schemaV1, schemaV2, schemaV3, schemaV4}

// Store is the SQLite-backed store.
type Store struct{ db *sql.DB }

var _ store.Store = (*Store)(nil)

// Open opens (creating if needed) the database file and applies migrations.
func Open(path string) (*Store, error) {
	esc := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
	dsn := "file:" + esc + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, stmt := range strings.Split(migrations[i], splitMarker) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				tx.Rollback()
				return err
			}
		}
		// PRAGMA does not accept bound parameters.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func ts(t time.Time) int64     { return t.Unix() }
func fromTS(i int64) time.Time { return time.Unix(i, 0) }
func isNoRows(err error) bool  { return errors.Is(err, sql.ErrNoRows) }
func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
func isFK(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// affected converts "0 rows changed" into store.ErrNotFound.
func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// ---- settings ----

func (s *Store) GetSetting(ctx context.Context, name string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE name = ?`, name).Scan(&v)
	if isNoRows(err) {
		return "", store.ErrNotFound
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, name, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings(name, value) VALUES(?, ?)
		 ON CONFLICT(name) DO UPDATE SET value = excluded.value`, name, value)
	return err
}

// ---- users ----

type scanner interface{ Scan(dest ...any) error }

func scanUser(r scanner) (*store.User, error) {
	var u store.User
	var role string
	var created int64
	if err := r.Scan(&u.ID, &u.Email, &u.Name, &role, &created); err != nil {
		if isNoRows(err) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	u.Role = store.Role(role)
	u.CreatedAt = fromTS(created)
	return &u, nil
}

const userCols = `id, email, name, role, created_at`

func (s *Store) CreateUser(ctx context.Context, email, name string, role store.Role) (*store.User, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users(email, name, role, created_at) VALUES(?, ?, ?, ?)`,
		email, name, string(role), ts(now))
	if isUnique(err) {
		return nil, store.ErrExists
	}
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &store.User{ID: id, Email: email, Name: name, Role: role, CreatedAt: fromTS(ts(now))}, nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*store.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
}

func (s *Store) UserByID(ctx context.Context, id int64) (*store.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) ListUsers(ctx context.Context) ([]store.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUserName(ctx context.Context, id int64, name string) error {
	return affected(s.db.ExecContext(ctx, `UPDATE users SET name = ? WHERE id = ?`, name, id))
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id))
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&n)
	return n, err
}

// ---- login codes ----

func (s *Store) GetLoginCode(ctx context.Context, userID int64) (*store.LoginCode, error) {
	var c store.LoginCode
	var exp, win int64
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, code_hash, expires_at, attempts, window_start, sent
		 FROM login_codes WHERE user_id = ?`, userID).
		Scan(&c.UserID, &c.Hash, &exp, &c.Attempts, &win, &c.Sent)
	if isNoRows(err) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.ExpiresAt, c.WindowStart = fromTS(exp), fromTS(win)
	return &c, nil
}

func (s *Store) PutLoginCode(ctx context.Context, c store.LoginCode) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO login_codes(user_id, code_hash, expires_at, attempts, window_start, sent)
		 VALUES(?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		   code_hash = excluded.code_hash, expires_at = excluded.expires_at,
		   attempts = excluded.attempts, window_start = excluded.window_start,
		   sent = excluded.sent`,
		c.UserID, c.Hash, ts(c.ExpiresAt), c.Attempts, ts(c.WindowStart), c.Sent)
	if isFK(err) {
		return store.ErrNotFound
	}
	return err
}

func (s *Store) DeleteLoginCode(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_codes WHERE user_id = ?`, userID)
	return err
}

// ---- link tokens ----

func (s *Store) PutLinkToken(ctx context.Context, hash string, userID int64, expires time.Time) error {
	// Opportunistic cleanup of expired tokens.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM link_tokens WHERE expires_at < ?`, ts(time.Now())); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO link_tokens(token_hash, user_id, expires_at) VALUES(?, ?, ?)`,
		hash, userID, ts(expires))
	if isFK(err) {
		return store.ErrNotFound
	}
	return err
}

func (s *Store) TakeLinkToken(ctx context.Context, hash string, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var uid, exp int64
	err = tx.QueryRowContext(ctx,
		`SELECT user_id, expires_at FROM link_tokens WHERE token_hash = ?`, hash).Scan(&uid, &exp)
	if isNoRows(err) {
		return 0, store.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM link_tokens WHERE token_hash = ?`, hash); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if now.Unix() > exp {
		return 0, store.ErrNotFound
	}
	return uid, nil
}

// ---- devices ----

const deviceCols = `id, user_id, label, created_at, last_seen`

func scanDevice(r scanner) (*store.Device, error) {
	var d store.Device
	var created, seen int64
	if err := r.Scan(&d.ID, &d.UserID, &d.Label, &created, &seen); err != nil {
		if isNoRows(err) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	d.CreatedAt, d.LastSeen = fromTS(created), fromTS(seen)
	return &d, nil
}

func (s *Store) CreateDevice(ctx context.Context, userID int64, tokenHash, label string, now time.Time) (*store.Device, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO devices(user_id, token_hash, label, created_at, last_seen) VALUES(?, ?, ?, ?, ?)`,
		userID, tokenHash, label, ts(now), ts(now))
	if isFK(err) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &store.Device{ID: id, UserID: userID, Label: label, CreatedAt: fromTS(ts(now)), LastSeen: fromTS(ts(now))}, nil
}

func (s *Store) DeviceByHash(ctx context.Context, tokenHash string) (*store.Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE token_hash = ?`, tokenHash))
}

func (s *Store) TouchDevice(ctx context.Context, id int64, now time.Time) error {
	return affected(s.db.ExecContext(ctx, `UPDATE devices SET last_seen = ? WHERE id = ?`, ts(now), id))
}

func (s *Store) ListDevices(ctx context.Context, userID int64) ([]store.Device, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+deviceCols+` FROM devices WHERE user_id = ? ORDER BY last_seen DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDevice(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id))
}

// ---- groups ----

func (s *Store) ListGroups(ctx context.Context) ([]store.Group, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, position FROM svc_groups ORDER BY position, LOWER(name), id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Group
	for rows.Next() {
		var g store.Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Position); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) GetGroup(ctx context.Context, id int64) (*store.Group, error) {
	var g store.Group
	err := s.db.QueryRowContext(ctx, `SELECT id, name, position FROM svc_groups WHERE id = ?`, id).
		Scan(&g.ID, &g.Name, &g.Position)
	if isNoRows(err) {
		return nil, store.ErrNotFound
	}
	return &g, err
}

func (s *Store) SaveGroup(ctx context.Context, g *store.Group) error {
	if g.ID == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO svc_groups(name, position) VALUES(?, ?)`, g.Name, g.Position)
		if err != nil {
			return err
		}
		g.ID, err = res.LastInsertId()
		return err
	}
	return affected(s.db.ExecContext(ctx, `UPDATE svc_groups SET name = ?, position = ? WHERE id = ?`, g.Name, g.Position, g.ID))
}

func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM svc_groups WHERE id = ?`, id))
}

// ---- services ----

const serviceCols = `id, name, description, icon, internal_url, external_url, group_id, position`

func scanService(r scanner) (*store.Service, error) {
	var v store.Service
	var gid sql.NullInt64
	if err := r.Scan(&v.ID, &v.Name, &v.Description, &v.Icon, &v.InternalURL, &v.ExternalURL, &gid, &v.Position); err != nil {
		if isNoRows(err) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	v.GroupID = gid.Int64
	return &v, nil
}

func (s *Store) ListServices(ctx context.Context) ([]store.Service, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+serviceCols+` FROM services ORDER BY position, LOWER(name), id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Service
	for rows.Next() {
		v, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (s *Store) GetService(ctx context.Context, id int64) (*store.Service, error) {
	return scanService(s.db.QueryRowContext(ctx, `SELECT `+serviceCols+` FROM services WHERE id = ?`, id))
}

func (s *Store) SaveService(ctx context.Context, v *store.Service) error {
	if v.ID == 0 {
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO services(name, description, icon, internal_url, external_url, group_id, position)
			 VALUES(?, ?, ?, ?, ?, ?, ?)`,
			v.Name, v.Description, v.Icon, v.InternalURL, v.ExternalURL, nullID(v.GroupID), v.Position)
		if isFK(err) {
			return store.ErrNotFound
		}
		if err != nil {
			return err
		}
		v.ID, err = res.LastInsertId()
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE services SET name = ?, description = ?, icon = ?, internal_url = ?, external_url = ?,
		   group_id = ?, position = ? WHERE id = ?`,
		v.Name, v.Description, v.Icon, v.InternalURL, v.ExternalURL, nullID(v.GroupID), v.Position, v.ID)
	if isFK(err) {
		return store.ErrNotFound
	}
	return affected(res, err)
}

func (s *Store) DeleteService(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The guest that produced this service goes back to the "new" list.
	if _, err := tx.ExecContext(ctx,
		`UPDATE guests SET state = 'new', service_id = NULL WHERE service_id = ?`, id); err != nil {
		return err
	}
	if err := affected(tx.ExecContext(ctx, `DELETE FROM services WHERE id = ?`, id)); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- discovery: connections ----

const connCols = `id, name, url, token_id, secret, insecure, fingerprint, last_sync, last_error`

func scanConnection(r scanner) (*store.Connection, error) {
	var c store.Connection
	var insecure int
	var last int64
	if err := r.Scan(&c.ID, &c.Name, &c.URL, &c.TokenID, &c.SecretSealed, &insecure, &c.Fingerprint, &last, &c.LastError); err != nil {
		if isNoRows(err) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	c.Insecure = insecure != 0
	if last > 0 {
		c.LastSync = fromTS(last)
	}
	return &c, nil
}

func (s *Store) ListConnections(ctx context.Context) ([]store.Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+connCols+` FROM connections ORDER BY LOWER(name), id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Connection
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) GetConnection(ctx context.Context, id int64) (*store.Connection, error) {
	return scanConnection(s.db.QueryRowContext(ctx, `SELECT `+connCols+` FROM connections WHERE id = ?`, id))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) SaveConnection(ctx context.Context, c *store.Connection) error {
	if c.ID == 0 {
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO connections(name, url, token_id, secret, insecure, fingerprint) VALUES(?, ?, ?, ?, ?, ?)`,
			c.Name, c.URL, c.TokenID, c.SecretSealed, boolInt(c.Insecure), c.Fingerprint)
		if isUnique(err) {
			return store.ErrExists
		}
		if err != nil {
			return err
		}
		c.ID, err = res.LastInsertId()
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE connections SET name = ?, url = ?, token_id = ?, secret = ?, insecure = ?, fingerprint = ? WHERE id = ?`,
		c.Name, c.URL, c.TokenID, c.SecretSealed, boolInt(c.Insecure), c.Fingerprint, c.ID)
	if isUnique(err) {
		return store.ErrExists
	}
	return affected(res, err)
}

func (s *Store) DeleteConnection(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM connections WHERE id = ?`, id))
}

func (s *Store) SetConnectionSync(ctx context.Context, id int64, at time.Time, errMsg string) error {
	return affected(s.db.ExecContext(ctx,
		`UPDATE connections SET last_sync = ?, last_error = ? WHERE id = ?`, ts(at), errMsg, id))
}

// ---- discovery: guests ----

const guestCols = `id, conn_id, vmid, kind, name, node, status, ip, state, missing, service_id, first_seen, last_seen`

func scanGuest(r scanner) (*store.Guest, error) {
	var g store.Guest
	var missing int
	var state string
	var svc sql.NullInt64
	var first, last int64
	if err := r.Scan(&g.ID, &g.ConnID, &g.VMID, &g.Kind, &g.Name, &g.Node, &g.Status, &g.IP,
		&state, &missing, &svc, &first, &last); err != nil {
		if isNoRows(err) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	g.State, g.Missing, g.ServiceID = store.GuestState(state), missing != 0, svc.Int64
	g.FirstSeen, g.LastSeen = fromTS(first), fromTS(last)
	return &g, nil
}

func (s *Store) ListGuests(ctx context.Context) ([]store.Guest, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+guestCols+` FROM guests ORDER BY conn_id, node, vmid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Guest
	for rows.Next() {
		g, err := scanGuest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func (s *Store) GetGuest(ctx context.Context, id int64) (*store.Guest, error) {
	return scanGuest(s.db.QueryRowContext(ctx, `SELECT `+guestCols+` FROM guests WHERE id = ?`, id))
}

func (s *Store) SyncGuests(ctx context.Context, connID int64, found []store.Guest, now time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM connections WHERE id = ?`, connID).Scan(&exists); err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, store.ErrNotFound
	}
	// Everything starts as missing; what the server returned is cleared below.
	if _, err := tx.ExecContext(ctx, `UPDATE guests SET missing = 1 WHERE conn_id = ?`, connID); err != nil {
		return 0, err
	}
	added := 0
	for _, f := range found {
		res, err := tx.ExecContext(ctx,
			`UPDATE guests SET kind = ?, name = ?, node = ?, status = ?, ip = ?, missing = 0, last_seen = ?
			 WHERE conn_id = ? AND vmid = ?`,
			f.Kind, f.Name, f.Node, f.Status, f.IP, ts(now), connID, f.VMID)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO guests(conn_id, vmid, kind, name, node, status, ip, state, missing, first_seen, last_seen)
			 VALUES(?, ?, ?, ?, ?, ?, ?, 'new', 0, ?, ?)`,
			connID, f.VMID, f.Kind, f.Name, f.Node, f.Status, f.IP, ts(now), ts(now)); err != nil {
			return 0, err
		}
		added++
	}
	return added, tx.Commit()
}

func (s *Store) SetGuestState(ctx context.Context, id int64, state store.GuestState, serviceID int64) error {
	if !state.Valid() {
		return fmt.Errorf("invalid guest state %q", state)
	}
	var svc any
	if state == store.GuestApproved {
		svc = nullID(serviceID)
		if svc == nil {
			return store.ErrNotFound
		}
	}
	res, err := s.db.ExecContext(ctx, `UPDATE guests SET state = ?, service_id = ? WHERE id = ?`, string(state), svc, id)
	if isFK(err) {
		return store.ErrNotFound
	}
	return affected(res, err)
}

func (s *Store) DeleteGuest(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM guests WHERE id = ?`, id))
}

// ---- per-user appearance ----

func (s *Store) GetPrefs(ctx context.Context, userID int64) (store.Prefs, error) {
	p := store.DefaultPrefs(userID)
	var hasBg int
	err := s.db.QueryRowContext(ctx,
		`SELECT theme, accent, bg_dim, bg_type != '', bg_version, bg_preset FROM user_prefs WHERE user_id = ?`, userID).
		Scan(&p.Theme, &p.Accent, &p.BgDim, &hasBg, &p.BgVersion, &p.BgPreset)
	if isNoRows(err) {
		return p, nil
	}
	p.HasBg = hasBg != 0
	return p, err
}

func (s *Store) SavePrefs(ctx context.Context, p store.Prefs) error {
	if !store.ValidTheme(p.Theme) || p.BgDim < 0 || p.BgDim > 80 {
		return fmt.Errorf("invalid preferences")
	}
	if _, ok := store.NormalizeAccent(p.Accent); !ok {
		return fmt.Errorf("invalid accent colour")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_prefs(user_id, theme, accent, bg_dim) VALUES(?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET theme = excluded.theme, accent = excluded.accent, bg_dim = excluded.bg_dim`,
		p.UserID, p.Theme, p.Accent, p.BgDim)
	if isFK(err) {
		return store.ErrNotFound
	}
	return err
}

func (s *Store) GetBackground(ctx context.Context, userID int64) (string, []byte, error) {
	var ctype string
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT bg_type, bg_data FROM user_prefs WHERE user_id = ? AND bg_type != ''`, userID).
		Scan(&ctype, &data)
	if isNoRows(err) {
		return "", nil, store.ErrNotFound
	}
	return ctype, data, err
}

func (s *Store) SetBackground(ctx context.Context, userID int64, contentType string, data []byte, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_prefs(user_id, bg_type, bg_data, bg_version, bg_preset) VALUES(?, ?, ?, ?, '')
		 ON CONFLICT(user_id) DO UPDATE SET bg_type = excluded.bg_type, bg_data = excluded.bg_data, bg_version = excluded.bg_version, bg_preset = ''`,
		userID, contentType, data, at.UnixNano())
	if isFK(err) {
		return store.ErrNotFound
	}
	return err
}

func (s *Store) ClearBackground(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_prefs SET bg_type = '', bg_data = NULL, bg_version = 0, bg_preset = '' WHERE user_id = ?`, userID)
	return err
}

func (s *Store) SetBgPreset(ctx context.Context, userID int64, preset string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_prefs(user_id, bg_preset) VALUES(?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET bg_preset = excluded.bg_preset, bg_type = '', bg_data = NULL, bg_version = 0`,
		userID, preset)
	if isFK(err) {
		return store.ErrNotFound
	}
	return err
}
