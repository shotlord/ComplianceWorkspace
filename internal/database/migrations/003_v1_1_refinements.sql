-- Compliance Workspace v1.1.0 — incremental refinements
-- Ensure unique index for recurring period exists for databases migrated incrementally

CREATE UNIQUE INDEX IF NOT EXISTS uq_work_items_recurring_period
ON work_items(requested_by, received_on)
WHERE requested_by LIKE 'Jadwal Rutin #%';
