-- HomeView schema, version 9: Proxmox figures.
-- px_access is set by an administrator (what a user may see); px_prefs is the
-- user's own choice among what is allowed. Missing rows mean "no access" and
-- "defaults" respectively.

CREATE TABLE IF NOT EXISTS px_access (
  user_id  INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  nodes    INTEGER NOT NULL DEFAULT 0,
  machines INTEGER NOT NULL DEFAULT 0,
  alerts   INTEGER NOT NULL DEFAULT 0
);
-- +split
CREATE TABLE IF NOT EXISTS px_prefs (
  user_id  INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  nodes    INTEGER NOT NULL DEFAULT 1,
  machines INTEGER NOT NULL DEFAULT 1,
  alerts   INTEGER NOT NULL DEFAULT 1,
  cpu      INTEGER NOT NULL DEFAULT 1,
  mem      INTEGER NOT NULL DEFAULT 1,
  disk     INTEGER NOT NULL DEFAULT 1,
  net      INTEGER NOT NULL DEFAULT 0,
  warn     INTEGER NOT NULL DEFAULT 75,
  crit     INTEGER NOT NULL DEFAULT 90,
  refresh  INTEGER NOT NULL DEFAULT 30
);
