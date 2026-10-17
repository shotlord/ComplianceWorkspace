package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"compliance-workspace/internal/domain"
)

// ActivityLogWithWorkInfo menggabungkan data log dengan informasi kode & judul pekerjaan untuk ekspor
type ActivityLogWithWorkInfo struct {
	domain.ActivityLog
	WorkCode  string `json:"work_code"`
	WorkTitle string `json:"work_title"`
}

// ActivityLogRepository mengelola penyimpanan riwayat aktivitas pekerjaan
type ActivityLogRepository struct {
	mu sync.RWMutex
	db *sql.DB
}

func NewActivityLogRepository(db *sql.DB) *ActivityLogRepository {
	return &ActivityLogRepository{db: db}
}

// SetDB memperbarui instance database secara thread-safe (misal setelah restore)
func (r *ActivityLogRepository) SetDB(db *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
}

func (r *ActivityLogRepository) getDB() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db
}

// CreateTx menyisipkan catatan aktivitas ke dalam transaksi
func (r *ActivityLogRepository) CreateTx(ctx context.Context, tx *sql.Tx, log *domain.ActivityLog) error {
	query := `
		INSERT INTO activity_logs (
			work_item_id, entry_type, description, field_name,
			old_value, new_value, occurred_at, recorded_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	occurredAtStr := log.OccurredAt.UTC().Format(time.RFC3339Nano)
	recordedAtStr := log.RecordedAt.UTC().Format(time.RFC3339Nano)

	res, err := tx.ExecContext(ctx, query,
		log.WorkItemID, log.EntryType, log.Description, log.FieldName,
		log.OldValue, log.NewValue, occurredAtStr, recordedAtStr,
	)
	if err != nil {
		return fmt.Errorf("gagal menyisipkan log aktivitas: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("gagal mendapatkan ID log aktivitas: %w", err)
	}
	log.ID = id
	return nil
}

// ListByWorkItemID mengambil semua log aktivitas untuk sebuah pekerjaan (urut kronologis)
func (r *ActivityLogRepository) ListByWorkItemID(ctx context.Context, workItemID int64) ([]*domain.ActivityLog, error) {
	query := `
		SELECT
			id, work_item_id, entry_type, description,
			field_name, old_value, new_value, occurred_at, recorded_at
		FROM activity_logs
		WHERE work_item_id = ?
		ORDER BY occurred_at ASC, id ASC
	`
	rows, err := r.getDB().QueryContext(ctx, query, workItemID)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil log aktivitas untuk pekerjaan %d: %w", workItemID, err)
	}
	defer rows.Close()

	var logs []*domain.ActivityLog
	for rows.Next() {
		var l domain.ActivityLog
		var fieldName, oldValue, newValue sql.NullString
		var occurredAtStr, recordedAtStr string

		err := rows.Scan(
			&l.ID,
			&l.WorkItemID,
			&l.EntryType,
			&l.Description,
			&fieldName,
			&oldValue,
			&newValue,
			&occurredAtStr,
			&recordedAtStr,
		)
		if err != nil {
			return nil, fmt.Errorf("gagal memindai baris log aktivitas: %w", err)
		}

		if fieldName.Valid {
			l.FieldName = &fieldName.String
		}
		if oldValue.Valid {
			l.OldValue = &oldValue.String
		}
		if newValue.Valid {
			l.NewValue = &newValue.String
		}

		if t, err := time.Parse(time.RFC3339Nano, occurredAtStr); err == nil {
			l.OccurredAt = t.UTC()
		}
		if t, err := time.Parse(time.RFC3339Nano, recordedAtStr); err == nil {
			l.RecordedAt = t.UTC()
		}

		logs = append(logs, &l)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterasi baris log aktivitas: %w", err)
	}

	return logs, nil
}

// ListAllWithWorkInfo mengambil log aktivitas dengan join informasi kode & judul pekerjaan (untuk CSV export)
func (r *ActivityLogRepository) ListAllWithWorkInfo(ctx context.Context, workItemID int64) ([]*ActivityLogWithWorkInfo, error) {
	var query string
	var args []interface{}

	if workItemID > 0 {
		query = `
			SELECT
				a.id, a.work_item_id, a.entry_type, a.description,
				a.field_name, a.old_value, a.new_value, a.occurred_at, a.recorded_at,
				w.work_code, w.title
			FROM activity_logs a
			JOIN work_items w ON a.work_item_id = w.id
			WHERE a.work_item_id = ?
			ORDER BY a.occurred_at ASC, a.id ASC
		`
		args = append(args, workItemID)
	} else {
		query = `
			SELECT
				a.id, a.work_item_id, a.entry_type, a.description,
				a.field_name, a.old_value, a.new_value, a.occurred_at, a.recorded_at,
				w.work_code, w.title
			FROM activity_logs a
			JOIN work_items w ON a.work_item_id = w.id
			ORDER BY a.occurred_at ASC, a.id ASC
		`
	}

	rows, err := r.getDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil log aktivitas dengan info pekerjaan: %w", err)
	}
	defer rows.Close()

	var logs []*ActivityLogWithWorkInfo
	for rows.Next() {
		var l ActivityLogWithWorkInfo
		var fieldName, oldValue, newValue sql.NullString
		var occurredAtStr, recordedAtStr string
		var workCode, workTitle sql.NullString

		err := rows.Scan(
			&l.ID,
			&l.WorkItemID,
			&l.EntryType,
			&l.Description,
			&fieldName,
			&oldValue,
			&newValue,
			&occurredAtStr,
			&recordedAtStr,
			&workCode,
			&workTitle,
		)
		if err != nil {
			return nil, fmt.Errorf("gagal memindai baris log aktivitas join: %w", err)
		}

		if fieldName.Valid {
			l.FieldName = &fieldName.String
		}
		if oldValue.Valid {
			l.OldValue = &oldValue.String
		}
		if newValue.Valid {
			l.NewValue = &newValue.String
		}
		if workCode.Valid {
			l.WorkCode = workCode.String
		}
		if workTitle.Valid {
			l.WorkTitle = workTitle.String
		}

		if t, err := time.Parse(time.RFC3339Nano, occurredAtStr); err == nil {
			l.OccurredAt = t.UTC()
		}
		if t, err := time.Parse(time.RFC3339Nano, recordedAtStr); err == nil {
			l.RecordedAt = t.UTC()
		}

		logs = append(logs, &l)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterasi baris log aktivitas: %w", err)
	}

	return logs, nil
}
