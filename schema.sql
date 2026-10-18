-- Compliance Workspace v1.1.0 — Cumulative SQLite Schema Reference
--
-- CATATAN PENTING:
-- Berkas ini disediakan HANYA sebagai referensi skema DDL kumulatif (snapshot sekilas)
-- untuk mempermudah audit struktural basis data secara menyeluruh.
-- Sumber kebenaran operasional (single source of truth) yang dieksekusi secara aktif
-- oleh aplikasi adalah berkas-berkas migrasi inkremental bernomor pada folder
-- 'internal/database/migrations/' (001_initial_schema.sql s/d 006_work_item_deletion_tombstone.sql)
-- yang disematkan via Go embed dan dicatat pada tabel 'schema_migrations'.
--
-- Store all instants in UTC RFC3339; dates (received_on/due_on/start_date/next_run_date) as YYYY-MM-DD.

PRAGMA foreign_keys = ON;
PRAGMA journal_mode = DELETE;
PRAGMA busy_timeout = 5000;

-- 1. Migration Version History
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- 2. Master Categories (v1.1.0)
CREATE TABLE IF NOT EXISTS categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL CHECK (length(trim(name)) > 0),
    description TEXT NOT NULL DEFAULT '',
    color TEXT NOT NULL DEFAULT '#4f46e5',
    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- 3. Work Items Master Table
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
    progress_percentage INTEGER NOT NULL DEFAULT 0
        CHECK (progress_percentage >= 0 AND progress_percentage <= 100),
    provenance TEXT NOT NULL DEFAULT 'unknown'
        CHECK (provenance IN ('manual', 'template', 'recurring', 'import', 'unknown')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    archived_at TEXT,
    deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_work_status_due ON work_items(status, due_on);
CREATE INDEX IF NOT EXISTS idx_work_received ON work_items(received_on);
CREATE INDEX IF NOT EXISTS idx_work_archived ON work_items(archived_at);
CREATE INDEX IF NOT EXISTS idx_work_items_deleted ON work_items(deleted_at);

-- 4. Subtasks & Checklists (v1.1.0)
CREATE TABLE IF NOT EXISTS subtasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_item_id INTEGER NOT NULL,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    is_completed INTEGER NOT NULL DEFAULT 0 CHECK (is_completed IN (0, 1)),
    due_on TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    FOREIGN KEY (work_item_id) REFERENCES work_items(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_subtasks_work_item ON subtasks(work_item_id, sort_order);

-- 5. Activity Logs (Audit Trail — Append-Only)
CREATE TABLE IF NOT EXISTS activity_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_item_id INTEGER NOT NULL,
    entry_type TEXT NOT NULL
        CHECK (entry_type IN ('created','activity','edited','status_changed','archived','restored','deleted')),
    description TEXT NOT NULL CHECK (length(trim(description)) > 0),
    field_name TEXT,
    old_value TEXT,
    new_value TEXT,
    occurred_at TEXT NOT NULL,
    recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    FOREIGN KEY (work_item_id) REFERENCES work_items(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_activity_item_time ON activity_logs(work_item_id, occurred_at, id);

-- Trigger protection: activity logs are strictly append-only
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

-- 6. Work Templates (v1.1.0)
CREATE TABLE IF NOT EXISTS work_templates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    title_pattern TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT 'General',
    priority TEXT NOT NULL DEFAULT 'normal'
        CHECK (priority IN ('low','normal','high','critical')),
    due_days_offset INTEGER NOT NULL DEFAULT 7,
    checklist_items_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- 7. Recurring Schedules (v1.1.0)
CREATE TABLE IF NOT EXISTS recurring_schedules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    template_id INTEGER REFERENCES work_templates(id) ON DELETE SET NULL,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT 'General',
    priority TEXT NOT NULL DEFAULT 'normal'
        CHECK (priority IN ('low','normal','high','critical')),
    frequency TEXT NOT NULL CHECK (frequency IN ('daily', 'weekly', 'monthly', 'quarterly', 'yearly')),
    interval_value INTEGER NOT NULL DEFAULT 1 CHECK (interval_value > 0),
    day_of_month INTEGER,
    day_of_week INTEGER,
    start_date TEXT NOT NULL,
    end_date TEXT,
    last_generated_date TEXT,
    next_run_date TEXT NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_recurring_active_next ON recurring_schedules(is_active, next_run_date);
CREATE UNIQUE INDEX IF NOT EXISTS uq_work_items_recurring_period ON work_items(requested_by, received_on) WHERE requested_by LIKE 'Jadwal Rutin #%';

-- 8. Data Import Batches & Items (v1.1.0)
CREATE TABLE IF NOT EXISTS import_batches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    batch_code TEXT UNIQUE NOT NULL,
    file_name TEXT NOT NULL,
    file_type TEXT NOT NULL CHECK (file_type IN ('csv', 'xlsx')),
    file_size INTEGER NOT NULL,
    total_rows INTEGER NOT NULL DEFAULT 0,
    imported_rows INTEGER NOT NULL DEFAULT 0,
    skipped_rows INTEGER NOT NULL DEFAULT 0,
    failed_rows INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL CHECK (status IN ('preview', 'completed', 'failed', 'rolled_back')),
    error_summary TEXT,
    column_mapping_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    completed_at TEXT
);

CREATE TABLE IF NOT EXISTS import_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    batch_id INTEGER NOT NULL REFERENCES import_batches(id) ON DELETE CASCADE,
    row_number INTEGER NOT NULL,
    raw_data_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('valid', 'invalid', 'imported', 'skipped', 'failed')),
    error_message TEXT,
    work_item_id INTEGER REFERENCES work_items(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_import_items_batch ON import_items(batch_id, status);
