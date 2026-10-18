-- Compliance Workspace v1.1.0 — incremental migration 004
-- Add is_active column to categories table for soft-delete and deactivation support

ALTER TABLE categories ADD COLUMN is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1));
