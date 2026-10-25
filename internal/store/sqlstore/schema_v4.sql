-- HomeView schema, version 4: bundled wallpapers. '' means none; an uploaded image (bg_type) wins over it.

ALTER TABLE user_prefs ADD COLUMN bg_preset TEXT NOT NULL DEFAULT 'oceano';
