package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"compliance-workspace/internal/domain"
)

// SubtaskRepository mengelola penyimpanan subtask / checklist item
type SubtaskRepository struct {
	mu sync.RWMutex
	db *sql.DB
}

func NewSubtaskRepository(db *sql.DB) *SubtaskRepository {
	return &SubtaskRepository{db: db}
}

// SetDB memperbarui instance database secara thread-safe
func (r *SubtaskRepository) SetDB(db *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
}

func (r *SubtaskRepository) getDB() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db
}

// ListByWorkItemID mengambil seluruh subtask untuk satu pekerjaan
func (r *SubtaskRepository) ListByWorkItemID(ctx context.Context, workItemID int64) ([]*domain.Subtask, error) {
	query := `
		SELECT id, work_item_id, title, is_completed, due_on, sort_order, created_at, updated_at
		FROM subtasks
		WHERE work_item_id = ?
		ORDER BY sort_order ASC, id ASC;
	`
	rows, err := r.getDB().QueryContext(ctx, query, workItemID)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil daftar subtask: %w", err)
	}
	defer rows.Close()

	var list []*domain.Subtask
	for rows.Next() {
		sub, err := scanSubtask(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sub)
	}
	return list, nil
}

// ListByWorkItemIDTx mengambil seluruh subtask di dalam transaksi
func (r *SubtaskRepository) ListByWorkItemIDTx(ctx context.Context, tx *sql.Tx, workItemID int64) ([]*domain.Subtask, error) {
	query := `
		SELECT id, work_item_id, title, is_completed, due_on, sort_order, created_at, updated_at
		FROM subtasks
		WHERE work_item_id = ?
		ORDER BY sort_order ASC, id ASC;
	`
	rows, err := tx.QueryContext(ctx, query, workItemID)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil daftar subtask: %w", err)
	}
	defer rows.Close()

	var list []*domain.Subtask
	for rows.Next() {
		sub, err := scanSubtask(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sub)
	}
	return list, nil
}

// GetByID mengambil satu subtask berdasarkan ID
func (r *SubtaskRepository) GetByID(ctx context.Context, id int64) (*domain.Subtask, error) {
	query := `
		SELECT id, work_item_id, title, is_completed, due_on, sort_order, created_at, updated_at
		FROM subtasks
		WHERE id = ?;
	`
	row := r.getDB().QueryRowContext(ctx, query, id)
	return scanSubtask(row)
}

// GetByIDTx mengambil satu subtask di dalam transaksi
func (r *SubtaskRepository) GetByIDTx(ctx context.Context, tx *sql.Tx, id int64) (*domain.Subtask, error) {
	query := `
		SELECT id, work_item_id, title, is_completed, due_on, sort_order, created_at, updated_at
		FROM subtasks
		WHERE id = ?;
	`
	row := tx.QueryRowContext(ctx, query, id)
	return scanSubtask(row)
}

// CreateTx menyimpan subtask baru di dalam transaksi
func (r *SubtaskRepository) CreateTx(ctx context.Context, tx *sql.Tx, sub *domain.Subtask) error {
	query := `
		INSERT INTO subtasks (work_item_id, title, is_completed, due_on, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`
	isComp := 0
	if sub.IsCompleted {
		isComp = 1
	}

	createdAtStr := sub.CreatedAt.UTC().Format(time.RFC3339Nano)
	updatedAtStr := sub.UpdatedAt.UTC().Format(time.RFC3339Nano)

	res, err := tx.ExecContext(ctx, query,
		sub.WorkItemID, sub.Title, isComp, sub.DueOn, sub.SortOrder,
		createdAtStr, updatedAtStr,
	)
	if err != nil {
		return fmt.Errorf("gagal mengeksekusi insert subtask: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("gagal mendapatkan ID subtask: %w", err)
	}
	sub.ID = id
	return nil
}

// UpdateTx memperbarui data subtask di dalam transaksi
func (r *SubtaskRepository) UpdateTx(ctx context.Context, tx *sql.Tx, sub *domain.Subtask) error {
	query := `
		UPDATE subtasks SET
			title = ?, is_completed = ?, due_on = ?, sort_order = ?, updated_at = ?
		WHERE id = ?;
	`
	isComp := 0
	if sub.IsCompleted {
		isComp = 1
	}
	updatedAtStr := sub.UpdatedAt.UTC().Format(time.RFC3339Nano)

	res, err := tx.ExecContext(ctx, query,
		sub.Title, isComp, sub.DueOn, sub.SortOrder, updatedAtStr, sub.ID,
	)
	if err != nil {
		return fmt.Errorf("gagal mengupdate subtask: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("subtask dengan ID %d tidak ditemukan", sub.ID)
	}
	return nil
}

// DeleteTx menghapus subtask di dalam transaksi
func (r *SubtaskRepository) DeleteTx(ctx context.Context, tx *sql.Tx, id int64) error {
	res, err := tx.ExecContext(ctx, "DELETE FROM subtasks WHERE id = ?;", id)
	if err != nil {
		return fmt.Errorf("gagal menghapus subtask: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("subtask dengan ID %d tidak ditemukan", id)
	}
	return nil
}

// CountAndCompletedCountTx menghitung total subtask dan jumlah yang telah selesai di dalam transaksi
func (r *SubtaskRepository) CountAndCompletedCountTx(ctx context.Context, tx *sql.Tx, workItemID int64) (total int, completed int, err error) {
	query := `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN is_completed = 1 THEN 1 ELSE 0 END), 0)
		FROM subtasks
		WHERE work_item_id = ?;
	`
	err = tx.QueryRowContext(ctx, query, workItemID).Scan(&total, &completed)
	if err != nil {
		return 0, 0, fmt.Errorf("gagal menghitung progres subtask: %w", err)
	}
	return total, completed, nil
}

func scanSubtask(scanner rowScanner) (*domain.Subtask, error) {
	var sub domain.Subtask
	var isComp int
	var dueOn sql.NullString
	var createdAtStr, updatedAtStr string

	err := scanner.Scan(
		&sub.ID,
		&sub.WorkItemID,
		&sub.Title,
		&isComp,
		&dueOn,
		&sub.SortOrder,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		return nil, err
	}

	sub.IsCompleted = (isComp == 1)
	if dueOn.Valid {
		sub.DueOn = &dueOn.String
	}

	tCreated, err := time.Parse(time.RFC3339Nano, createdAtStr)
	if err == nil {
		sub.CreatedAt = tCreated.UTC()
	}
	tUpdated, err := time.Parse(time.RFC3339Nano, updatedAtStr)
	if err == nil {
		sub.UpdatedAt = tUpdated.UTC()
	}

	return &sub, nil
}
