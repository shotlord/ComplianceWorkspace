-- Compliance Workspace v1.0 — initial SQLite schema
-- Store all instants in UTC RFC3339; dates (received_on/due_on) as YYYY-MM-DD.

CREATE TABLE IF NOT EXISTS work_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_code TEXT UNIQUE,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT 'General',
    requested_by TEXT,
    received_on TEXT NOT NULL,
    due_on TEXT,
    priority TEXT NOT NULL DEFAULT 'normal'
        CHECK (priority IN ('low','normal','high','critical')),
    status TEXT NOT NULL DEFAULT 'not_started'
        CHECK (status IN ('not_started','in_progress','waiting','completed','cancelled')),
    completed_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    archived_at TEXT
);

CREATE TABLE IF NOT EXISTS activity_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_item_id INTEGER NOT NULL,
    entry_type TEXT NOT NULL
        CHECK (entry_type IN ('created','activity','edited','status_changed','archived','restored')),
    description TEXT NOT NULL CHECK (length(trim(description)) > 0),
    field_name TEXT,
    old_value TEXT,
    new_value TEXT,
    occurred_at TEXT NOT NULL,
    recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    FOREIGN KEY (work_item_id) REFERENCES work_items(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_work_status_due ON work_items(status, due_on);
CREATE INDEX IF NOT EXISTS idx_work_received ON work_items(received_on);
CREATE INDEX IF NOT EXISTS idx_work_archived ON work_items(archived_at);
CREATE INDEX IF NOT EXISTS idx_activity_item_time ON activity_logs(work_item_id, occurred_at, id);

-- Application must never expose UPDATE/DELETE for activity_logs.
CREATE TRIGGER IF NOT EXISTS activity_logs_no_update
BEFORE UPDATE ON activity_logs
BEGIN
    SELECT RAISE(ABORT, 'Activity logs are append-only');
END;

CREATE TRIGGER IF NOT EXISTS activity_logs_no_delete
BEFORE DELETE ON activity_logs
BEGIN
    SELECT RAISE(ABORT, 'Activity logs are append-only');
END;
