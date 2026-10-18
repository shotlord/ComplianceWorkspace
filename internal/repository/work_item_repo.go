package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"compliance-workspace/internal/domain"
)

// WorkItemFilter mendefinisikan kriteria penyaringan data pekerjaan
type WorkItemFilter struct {
	Status          string
	Priority        string
	Category        string
	Search          string
	IncludeArchived bool
	DueBefore       string // YYYY-MM-DD
	DueTodayOrPast  string // YYYY-MM-DD
	Provenance      string // manual, template, recurring, import, unknown
	SortBy          string
	SortOrder       string
}

// WorkItemRepository mengelola penyimpanan pekerjaan
type WorkItemRepository struct {
	mu sync.RWMutex
	db *sql.DB
}

func NewWorkItemRepository(db *sql.DB) *WorkItemRepository {
	return &WorkItemRepository{db: db}
}

// SetDB memperbarui instance database secara thread-safe (misal setelah restore)
func (r *WorkItemRepository) SetDB(db *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
}

func (r *WorkItemRepository) getDB() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db
}

// GenerateNextWorkCode membuat kode berikutnya dengan format CMP-YYYY-NNNNNN
func (r *WorkItemRepository) GenerateNextWorkCode(ctx context.Context, tx *sql.Tx, year int) (string, error) {
	prefix := fmt.Sprintf("CMP-%04d-", year)
	pattern := prefix + "%"

	query := `
		SELECT COALESCE(MAX(CAST(substr(work_code, 10, 6) AS INTEGER)), 0)
		FROM work_items
		WHERE work_code LIKE ?;
	`
	var maxNum int
	if err := tx.QueryRowContext(ctx, query, pattern).Scan(&maxNum); err != nil {
		return "", fmt.Errorf("gagal mengambil nomor urut terakhir pekerjaan: %w", err)
	}

	nextNum := maxNum + 1
	return fmt.Sprintf("CMP-%04d-%06d", year, nextNum), nil
}

// CreateTx menyimpan pekerjaan baru di dalam transaksi yang aktif
func (r *WorkItemRepository) CreateTx(ctx context.Context, tx *sql.Tx, item *domain.WorkItem) error {
	prov := item.Provenance
	if prov == "" {
		prov = domain.ProvenanceManual
	}
	item.Provenance = prov

	query := `
		INSERT INTO work_items (
			work_code, title, description, category, requested_by,
			received_on, due_on, priority, status, progress_percentage,
			provenance, completed_at, created_at, updated_at, archived_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	var completedAtStr *string
	if item.CompletedAt != nil {
		s := item.CompletedAt.UTC().Format(time.RFC3339Nano)
		completedAtStr = &s
	}

	createdAtStr := item.CreatedAt.UTC().Format(time.RFC3339Nano)
	updatedAtStr := item.UpdatedAt.UTC().Format(time.RFC3339Nano)

	var archivedAtStr *string
	if item.ArchivedAt != nil {
		s := item.ArchivedAt.UTC().Format(time.RFC3339Nano)
		archivedAtStr = &s
	}

	res, err := tx.ExecContext(ctx, query,
		item.WorkCode, item.Title, item.Description, item.Category, item.RequestedBy,
		item.ReceivedOn, item.DueOn, item.Priority, item.Status, item.ProgressPercentage,
		prov, completedAtStr, createdAtStr, updatedAtStr, archivedAtStr,
	)
	if err != nil {
		return fmt.Errorf("gagal mengeksekusi insert pekerjaan: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("gagal mendapatkan ID pekerjaan baru: %w", err)
	}
	item.ID = id
	return nil
}

// UpdateTx memperbarui data pekerjaan di dalam transaksi
func (r *WorkItemRepository) UpdateTx(ctx context.Context, tx *sql.Tx, item *domain.WorkItem) error {
	query := `
		UPDATE work_items SET
			title = ?, description = ?, category = ?, requested_by = ?,
			received_on = ?, due_on = ?, priority = ?, status = ?, progress_percentage = ?,
			completed_at = ?, updated_at = ?, archived_at = ?
		WHERE id = ?
	`
	var completedAtStr *string
	if item.CompletedAt != nil {
		s := item.CompletedAt.UTC().Format(time.RFC3339Nano)
		completedAtStr = &s
	}

	updatedAtStr := item.UpdatedAt.UTC().Format(time.RFC3339Nano)

	var archivedAtStr *string
	if item.ArchivedAt != nil {
		s := item.ArchivedAt.UTC().Format(time.RFC3339Nano)
		archivedAtStr = &s
	}

	res, err := tx.ExecContext(ctx, query,
		item.Title, item.Description, item.Category, item.RequestedBy,
		item.ReceivedOn, item.DueOn, item.Priority, item.Status, item.ProgressPercentage,
		completedAtStr, updatedAtStr, archivedAtStr, item.ID,
	)
	if err != nil {
		return fmt.Errorf("gagal mengupdate pekerjaan: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("pekerjaan dengan ID %d tidak ditemukan", item.ID)
	}

	return nil
}

// UpdateProgressTx memperbarui nilai progress_percentage pada pekerjaan
func (r *WorkItemRepository) UpdateProgressTx(ctx context.Context, tx *sql.Tx, id int64, progress int) error {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	query := "UPDATE work_items SET progress_percentage = ?, updated_at = ? WHERE id = ?"
	res, err := tx.ExecContext(ctx, query, progress, nowStr, id)
	if err != nil {
		return fmt.Errorf("gagal mengupdate progress_percentage: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("pekerjaan dengan ID %d tidak ditemukan", id)
	}
	return nil
}

// GetByID mengambil satu pekerjaan berdasarkan ID yang belum dihapus permanen
func (r *WorkItemRepository) GetByID(ctx context.Context, id int64) (*domain.WorkItem, error) {
	query := `
		SELECT
			id, work_code, title, description, category, requested_by,
			received_on, due_on, priority, status, progress_percentage, completed_at,
			created_at, updated_at, archived_at, deleted_at,
			(SELECT COUNT(*) FROM subtasks WHERE work_item_id = work_items.id) AS subtask_count,
			(SELECT COUNT(*) FROM subtasks WHERE work_item_id = work_items.id AND is_completed = 1) AS completed_subtask_count,
			work_items.provenance
		FROM work_items
		WHERE id = ? AND deleted_at IS NULL
	`
	row := r.getDB().QueryRowContext(ctx, query, id)
	return scanWorkItem(row)
}

// GetByIDTx mengambil satu pekerjaan di dalam transaksi yang belum dihapus permanen
func (r *WorkItemRepository) GetByIDTx(ctx context.Context, tx *sql.Tx, id int64) (*domain.WorkItem, error) {
	query := `
		SELECT
			id, work_code, title, description, category, requested_by,
			received_on, due_on, priority, status, progress_percentage, completed_at,
			created_at, updated_at, archived_at, deleted_at,
			(SELECT COUNT(*) FROM subtasks WHERE work_item_id = work_items.id) AS subtask_count,
			(SELECT COUNT(*) FROM subtasks WHERE work_item_id = work_items.id AND is_completed = 1) AS completed_subtask_count,
			work_items.provenance
		FROM work_items
		WHERE id = ? AND deleted_at IS NULL
	`
	row := tx.QueryRowContext(ctx, query, id)
	return scanWorkItem(row)
}

// GetByIDIncludingDeletedTx mengambil satu pekerjaan di dalam transaksi tanpa memfilter deleted_at
func (r *WorkItemRepository) GetByIDIncludingDeletedTx(ctx context.Context, tx *sql.Tx, id int64) (*domain.WorkItem, error) {
	query := `
		SELECT
			id, work_code, title, description, category, requested_by,
			received_on, due_on, priority, status, progress_percentage, completed_at,
			created_at, updated_at, archived_at, deleted_at,
			(SELECT COUNT(*) FROM subtasks WHERE work_item_id = work_items.id) AS subtask_count,
			(SELECT COUNT(*) FROM subtasks WHERE work_item_id = work_items.id AND is_completed = 1) AS completed_subtask_count,
			work_items.provenance
		FROM work_items
		WHERE id = ?
	`
	row := tx.QueryRowContext(ctx, query, id)
	return scanWorkItem(row)
}

// PermanentDeleteTx menandai pekerjaan sebagai terhapus permanen (tombstone) di dalam transaksi
func (r *WorkItemRepository) PermanentDeleteTx(ctx context.Context, tx *sql.Tx, id int64) error {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	query := `UPDATE work_items SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`
	res, err := tx.ExecContext(ctx, query, nowStr, nowStr, id)
	if err != nil {
		return fmt.Errorf("gagal menghapus permanen pekerjaan: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("pekerjaan dengan ID %d tidak ditemukan atau sudah terhapus", id)
	}
	return nil
}

// List mengambil daftar pekerjaan berdasarkan filter
func (r *WorkItemRepository) List(ctx context.Context, filter WorkItemFilter) ([]*domain.WorkItem, error) {
	var conditions []string
	var args []interface{}

	// Filter agar pekerjaan yang sudah dihapus permanen (tombstone) tidak pernah muncul
	conditions = append(conditions, "deleted_at IS NULL")

	if !filter.IncludeArchived {
		conditions = append(conditions, "archived_at IS NULL")
	}

	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}

	if filter.Priority != "" {
		conditions = append(conditions, "priority = ?")
		args = append(args, filter.Priority)
	}

	if filter.Category != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, filter.Category)
	}

	if filter.Provenance != "" {
		conditions = append(conditions, "work_items.provenance = ?")
		args = append(args, filter.Provenance)
	}

	if filter.Search != "" {
		likePattern := "%" + strings.TrimSpace(filter.Search) + "%"
		conditions = append(conditions, "(title LIKE ? OR description LIKE ? OR work_code LIKE ? OR requested_by LIKE ?)")
		args = append(args, likePattern, likePattern, likePattern, likePattern)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	sortColumn := "received_on"
	switch filter.SortBy {
	case "title":
		sortColumn = "title"
	case "due_on":
		sortColumn = "due_on"
	case "priority":
		sortColumn = "priority"
	case "status":
		sortColumn = "status"
	case "created_at":
		sortColumn = "created_at"
	}

	sortOrder := "DESC"
	if strings.ToUpper(filter.SortOrder) == "ASC" {
		sortOrder = "ASC"
	}

	query := fmt.Sprintf(`
		SELECT
			id, work_code, title, description, category, requested_by,
			received_on, due_on, priority, status, progress_percentage, completed_at,
			created_at, updated_at, archived_at, deleted_at,
			(SELECT COUNT(*) FROM subtasks WHERE work_item_id = work_items.id) AS subtask_count,
			(SELECT COUNT(*) FROM subtasks WHERE work_item_id = work_items.id AND is_completed = 1) AS completed_subtask_count,
			work_items.provenance
		FROM work_items
		%s
		ORDER BY %s %s, id DESC
	`, whereClause, sortColumn, sortOrder)

	rows, err := r.getDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil daftar pekerjaan: %w", err)
	}
	defer rows.Close()

	var items []*domain.WorkItem
	for rows.Next() {
		item, err := scanWorkItemFromRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterasi baris pekerjaan: %w", err)
	}

	return items, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func parseFlexibleTimestamp(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("gagal mem-parse timestamp: %s", s)
}

func scanWorkItem(scanner rowScanner) (*domain.WorkItem, error) {
	var item domain.WorkItem
	var workCode, requestedBy sql.NullString
	var dueOn sql.NullString
	var completedAtStr, createdAtStr, updatedAtStr, archivedAtStr, deletedAtStr sql.NullString
	var provenance sql.NullString

	err := scanner.Scan(
		&item.ID,
		&workCode,
		&item.Title,
		&item.Description,
		&item.Category,
		&requestedBy,
		&item.ReceivedOn,
		&dueOn,
		&item.Priority,
		&item.Status,
		&item.ProgressPercentage,
		&completedAtStr,
		&createdAtStr,
		&updatedAtStr,
		&archivedAtStr,
		&deletedAtStr,
		&item.SubtaskCount,
		&item.CompletedSubtaskCount,
		&provenance,
	)
	if err != nil {
		return nil, err
	}

	if provenance.Valid && provenance.String != "" {
		item.Provenance = provenance.String
	} else {
		item.Provenance = domain.ProvenanceUnknown
	}

	if workCode.Valid {
		item.WorkCode = workCode.String
	}
	if requestedBy.Valid {
		item.RequestedBy = requestedBy.String
	}
	if dueOn.Valid {
		item.DueOn = &dueOn.String
	}

	if completedAtStr.Valid && completedAtStr.String != "" {
		if t, err := parseFlexibleTimestamp(completedAtStr.String); err == nil {
			item.CompletedAt = &t
		}
	}

	if createdAtStr.Valid && createdAtStr.String != "" {
		if t, err := parseFlexibleTimestamp(createdAtStr.String); err == nil {
			item.CreatedAt = t
		}
	}

	if updatedAtStr.Valid && updatedAtStr.String != "" {
		if t, err := parseFlexibleTimestamp(updatedAtStr.String); err == nil {
			item.UpdatedAt = t
		}
	}

	if archivedAtStr.Valid && archivedAtStr.String != "" {
		if t, err := parseFlexibleTimestamp(archivedAtStr.String); err == nil {
			item.ArchivedAt = &t
		}
	}

	if deletedAtStr.Valid && deletedAtStr.String != "" {
		if t, err := parseFlexibleTimestamp(deletedAtStr.String); err == nil {
			item.DeletedAt = &t
		}
	}

	return &item, nil
}

func scanWorkItemFromRows(rows *sql.Rows) (*domain.WorkItem, error) {
	return scanWorkItem(rows)
}
