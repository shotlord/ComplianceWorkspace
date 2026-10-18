-- Compliance Workspace v1.1.0 — incremental migration 006
-- Add deleted_at column for audit-compliant permanent deletion (tombstone)

ALTER TABLE work_items ADD COLUMN deleted_at TEXT;

CREATE INDEX IF NOT EXISTS idx_work_items_deleted ON work_items(deleted_at);
