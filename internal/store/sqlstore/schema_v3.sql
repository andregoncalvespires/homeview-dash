-- HomeView schema, version 3: per-user appearance (theme, accent colour, background image).

CREATE TABLE IF NOT EXISTS user_prefs (
  user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  theme      TEXT NOT NULL DEFAULT 'auto' CHECK (theme IN ('auto', 'light', 'dark')),
  accent     TEXT NOT NULL DEFAULT '',
  bg_dim     INTEGER NOT NULL DEFAULT 35 CHECK (bg_dim BETWEEN 0 AND 80),
  bg_type    TEXT NOT NULL DEFAULT '',
  bg_data    BLOB,
  bg_version INTEGER NOT NULL DEFAULT 0
);
