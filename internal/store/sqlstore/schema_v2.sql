-- HomeView schema, version 2: Proxmox discovery (connections and guests).

CREATE TABLE IF NOT EXISTS connections (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL UNIQUE,
  url         TEXT NOT NULL,
  token_id    TEXT NOT NULL,
  secret      TEXT NOT NULL,
  insecure    INTEGER NOT NULL DEFAULT 0,
  fingerprint TEXT NOT NULL DEFAULT '',
  last_sync   INTEGER NOT NULL DEFAULT 0,
  last_error  TEXT NOT NULL DEFAULT ''
);
-- +split
CREATE TABLE IF NOT EXISTS guests (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  conn_id    INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
  vmid       INTEGER NOT NULL,
  kind       TEXT NOT NULL,
  name       TEXT NOT NULL,
  node       TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT '',
  ip         TEXT NOT NULL DEFAULT '',
  state      TEXT NOT NULL DEFAULT 'new' CHECK (state IN ('new', 'approved', 'ignored')),
  missing    INTEGER NOT NULL DEFAULT 0,
  service_id INTEGER REFERENCES services(id) ON DELETE SET NULL,
  first_seen INTEGER NOT NULL,
  last_seen  INTEGER NOT NULL,
  UNIQUE (conn_id, vmid)
);
-- +split
CREATE INDEX IF NOT EXISTS idx_guests_service ON guests(service_id);
