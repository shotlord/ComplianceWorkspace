-- Compliance Workspace v1.1.0 — incremental migration 005
-- Add structured provenance column to work_items table

ALTER TABLE work_items ADD COLUMN provenance TEXT NOT NULL DEFAULT 'unknown' CHECK (provenance IN ('manual', 'template', 'recurring', 'import', 'unknown'));

-- Ensure dependent tables exist for backfill queries (guarantees safety on partial/mock test schemas)
CREATE TABLE IF NOT EXISTS activity_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_item_id INTEGER NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    entry_type TEXT NOT NULL,
    description TEXT NOT NULL,
    field_name TEXT,
    old_value TEXT,
    new_value TEXT,
    occurred_at TEXT NOT NULL,
    recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS import_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    batch_id INTEGER NOT NULL,
    row_number INTEGER NOT NULL,
    work_item_id INTEGER REFERENCES work_items(id) ON DELETE SET NULL,
    status TEXT NOT NULL
);

-- Backfill existing data using immutable activity logs and import relations
-- 1. Import items: items that were created via import batches
UPDATE work_items
SET provenance = 'import'
WHERE id IN (
    SELECT work_item_id FROM import_items WHERE work_item_id IS NOT NULL AND status IN ('created', 'inserted')
);

-- 2. Recurring items: items created by recurring schedule generators
UPDATE work_items
SET provenance = 'recurring'
WHERE id IN (
    SELECT work_item_id FROM activity_logs
    WHERE entry_type = 'created'
      AND (description LIKE 'Pekerjaan dibuat otomatis oleh jadwal berulang%' OR description LIKE 'Pekerjaan dibentuk secara otomatis oleh Jadwal Rutin%')
)
AND provenance = 'unknown';

-- 3. Template items: items created from work templates
UPDATE work_items
SET provenance = 'template'
WHERE id IN (
    SELECT work_item_id FROM activity_logs
    WHERE entry_type = 'created'
      AND (description LIKE 'Pekerjaan dibuat dari template%' OR description LIKE 'Pekerjaan dibuat dari Template%')
)
AND provenance = 'unknown';

-- 4. Manual items: items created manually via form
UPDATE work_items
SET provenance = 'manual'
WHERE id IN (
    SELECT work_item_id FROM activity_logs
    WHERE entry_type = 'created'
      AND description LIKE 'Pekerjaan dibuat dengan kode %'
)
AND provenance = 'unknown';
