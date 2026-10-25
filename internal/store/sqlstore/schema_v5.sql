-- HomeView schema, version 5: several services per discovered machine, and Proxmox hosts.
-- guests.service_id (v2) is no longer used; the link now lives on the service.

ALTER TABLE services ADD COLUMN guest_id INTEGER REFERENCES guests(id) ON DELETE SET NULL;
-- +split
UPDATE services SET guest_id = (SELECT g.id FROM guests g WHERE g.service_id = services.id);
-- +split
CREATE INDEX IF NOT EXISTS idx_services_guest ON services(guest_id);
-- +split
CREATE TABLE IF NOT EXISTS nodes (
  conn_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
  name    TEXT NOT NULL,
  addr    TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (conn_id, name)
);
