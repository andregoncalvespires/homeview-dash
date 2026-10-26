-- HomeView schema, version 7: icons uploaded for services. Services refer to them as "upload:<id>".
-- The hash makes identical images share one row.

CREATE TABLE IF NOT EXISTS icons (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  sha256       TEXT NOT NULL UNIQUE,
  content_type TEXT NOT NULL,
  data         BLOB NOT NULL,
  created_at   INTEGER NOT NULL
);
