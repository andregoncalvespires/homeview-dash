-- HomeView schema, version 8: sub-groups, per-group access, service type and Proxmox node.
-- parent_id is one level deep (enforced by the application). A restricted group is only
-- visible to administrators and the users listed in group_users.

ALTER TABLE svc_groups ADD COLUMN parent_id INTEGER REFERENCES svc_groups(id) ON DELETE SET NULL;
-- +split
ALTER TABLE svc_groups ADD COLUMN restricted INTEGER NOT NULL DEFAULT 0;
-- +split
CREATE TABLE IF NOT EXISTS group_users (
  group_id INTEGER NOT NULL REFERENCES svc_groups(id) ON DELETE CASCADE,
  user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, user_id)
);
-- +split
ALTER TABLE services ADD COLUMN type TEXT NOT NULL DEFAULT '';
-- +split
ALTER TABLE services ADD COLUMN node TEXT NOT NULL DEFAULT '';
