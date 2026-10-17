-- Compliance Workspace v1.1.0 — incremental migration
-- Enhancements: Subtasks, Categories, Work Templates, Recurring Tasks, and Data Import Center.
-- Store all instants in UTC RFC3339; dates (received_on/due_on/start_date/next_run_date) as YYYY-MM-DD.

-- 1. Penambahan kolom progress pada tabel work_items (Non-destructive, default 0%)
ALTER TABLE work_items ADD COLUMN progress_percentage INTEGER NOT NULL DEFAULT 0
    CHECK (progress_percentage >= 0 AND progress_percentage <= 100);

-- 2. Tabel Master Kategori
CREATE TABLE IF NOT EXISTS categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL CHECK (length(trim(name)) > 0),
    description TEXT NOT NULL DEFAULT '',
    color TEXT NOT NULL DEFAULT '#4f46e5',
    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Seed kategori kepatuhan standar v1.1.0
INSERT OR IGNORE INTO categories (name, description, color, is_default) VALUES
    ('General', 'Kategori umum penugasan kepatuhan', '#64748b', 1),
    ('Pelaporan Regulator', 'Laporan kepatuhan OJK, BI, PPATK, dan instansi pengawas', '#2563eb', 0),
    ('Kepatuhan Internal', 'Review kebijakan, SOP, dan audit kepatuhan internal', '#0d9488', 0),
    ('Audit & Pengawasan', 'Tindak lanjut audit eksternal dan pengawasan operasional', '#d97706', 0),
    ('Manajemen Risiko', 'Penilaian risiko kepatuhan dan mitigasi fraud', '#dc2626', 0);

-- Migrasi kategori unik yang sudah ada di database v1.0.0 agar otomatis tercatat di master categories
INSERT OR IGNORE INTO categories (name)
SELECT DISTINCT trim(category) FROM work_items WHERE length(trim(category)) > 0;

-- 3. Tabel Subtask & Checklist per Pekerjaan
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

-- 4. Tabel Template Pekerjaan Kepatuhan
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

-- 5. Tabel Tugas Kepatuhan Berulang (Recurring Tasks)
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

-- 6. Tabel Riwayat Batch Impor (Data Import Center)
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

-- 7. Tabel Rincian Baris Hasil Impor (Traceability, Audit & Compensatory Rollback)
CREATE TABLE IF NOT EXISTS import_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    batch_id INTEGER NOT NULL,
    row_number INTEGER NOT NULL,
    work_item_id INTEGER,
    status TEXT NOT NULL CHECK (status IN ('created', 'updated', 'skipped', 'failed')),
    action_type TEXT NOT NULL DEFAULT 'insert',
    error_message TEXT,
    raw_data_json TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    FOREIGN KEY (batch_id) REFERENCES import_batches(id) ON DELETE CASCADE,
    FOREIGN KEY (work_item_id) REFERENCES work_items(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_import_items_batch ON import_items(batch_id, row_number);
