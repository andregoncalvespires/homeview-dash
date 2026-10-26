-- HomeView schema, version 6: new defaults (dark theme, starry-sky wallpaper).
-- SQLite cannot change a column default in place, so the table is rebuilt.
-- Rows still on the previous defaults (auto theme, Ocean wallpaper) move to the new ones.

CREATE TABLE user_prefs_new (
  user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  theme      TEXT NOT NULL DEFAULT 'dark' CHECK (theme IN ('auto', 'light', 'dark')),
  accent     TEXT NOT NULL DEFAULT '',
  bg_dim     INTEGER NOT NULL DEFAULT 35 CHECK (bg_dim BETWEEN 0 AND 80),
  bg_type    TEXT NOT NULL DEFAULT '',
  bg_data    BLOB,
  bg_version INTEGER NOT NULL DEFAULT 0,
  bg_preset  TEXT NOT NULL DEFAULT 'estrelas'
);
-- +split
INSERT INTO user_prefs_new(user_id, theme, accent, bg_dim, bg_type, bg_data, bg_version, bg_preset)
SELECT user_id,
       CASE WHEN theme = 'auto' THEN 'dark' ELSE theme END,
       accent, bg_dim, bg_type, bg_data, bg_version,
       CASE WHEN bg_preset = 'oceano' THEN 'estrelas' ELSE bg_preset END
FROM user_prefs;
-- +split
DROP TABLE user_prefs;
-- +split
ALTER TABLE user_prefs_new RENAME TO user_prefs;
