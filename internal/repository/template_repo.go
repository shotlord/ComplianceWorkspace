package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"compliance-workspace/internal/domain"
)

// WorkTemplateRepository mengelola tabel work_templates
type WorkTemplateRepository struct {
	mu sync.RWMutex
	db *sql.DB
}

func NewWorkTemplateRepository(db *sql.DB) *WorkTemplateRepository {
	return &WorkTemplateRepository{db: db}
}

func (r *WorkTemplateRepository) SetDB(db *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
}

func (r *WorkTemplateRepository) getDB() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db
}

// List mengambil seluruh template pekerjaan
func (r *WorkTemplateRepository) List(ctx context.Context) ([]*domain.WorkTemplate, error) {
	query := `
		SELECT id, name, title_pattern, description, category, priority, due_days_offset, checklist_items_json, created_at, updated_at
		FROM work_templates
		ORDER BY name ASC;
	`
	rows, err := r.getDB().QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil daftar template: %w", err)
	}
	defer rows.Close()

	var list []*domain.WorkTemplate
	for rows.Next() {
		tpl, err := scanWorkTemplate(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, tpl)
	}
	return list, nil
}

// GetByID mengambil template berdasarkan ID
func (r *WorkTemplateRepository) GetByID(ctx context.Context, id int64) (*domain.WorkTemplate, error) {
	query := `
		SELECT id, name, title_pattern, description, category, priority, due_days_offset, checklist_items_json, created_at, updated_at
		FROM work_templates
		WHERE id = ?;
	`
	row := r.getDB().QueryRowContext(ctx, query, id)
	return scanWorkTemplate(row)
}

// Create menambahkan template baru
func (r *WorkTemplateRepository) Create(ctx context.Context, tpl *domain.WorkTemplate) error {
	query := `
		INSERT INTO work_templates (
			name, title_pattern, description, category, priority,
			due_days_offset, checklist_items_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	checklistJSON, err := json.Marshal(tpl.ChecklistItems)
	if err != nil {
		checklistJSON = []byte("[]")
	}

	createdAtStr := tpl.CreatedAt.UTC().Format(time.RFC3339Nano)
	updatedAtStr := tpl.UpdatedAt.UTC().Format(time.RFC3339Nano)

	res, err := r.getDB().ExecContext(ctx, query,
		tpl.Name, tpl.TitlePattern, tpl.Description, tpl.Category, tpl.Priority,
		tpl.DueDaysOffset, string(checklistJSON), createdAtStr, updatedAtStr,
	)
	if err != nil {
		return fmt.Errorf("gagal menyimpan template: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	tpl.ID = id
	return nil
}

// Update memperbarui data template
func (r *WorkTemplateRepository) Update(ctx context.Context, tpl *domain.WorkTemplate) error {
	query := `
		UPDATE work_templates SET
			name = ?, title_pattern = ?, description = ?, category = ?,
			priority = ?, due_days_offset = ?, checklist_items_json = ?, updated_at = ?
		WHERE id = ?;
	`
	checklistJSON, err := json.Marshal(tpl.ChecklistItems)
	if err != nil {
		checklistJSON = []byte("[]")
	}
	updatedAtStr := tpl.UpdatedAt.UTC().Format(time.RFC3339Nano)

	res, err := r.getDB().ExecContext(ctx, query,
		tpl.Name, tpl.TitlePattern, tpl.Description, tpl.Category,
		tpl.Priority, tpl.DueDaysOffset, string(checklistJSON), updatedAtStr, tpl.ID,
	)
	if err != nil {
		return fmt.Errorf("gagal mengupdate template: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("template dengan ID %d tidak ditemukan", tpl.ID)
	}
	return nil
}

// Delete menghapus template
func (r *WorkTemplateRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.getDB().ExecContext(ctx, "DELETE FROM work_templates WHERE id = ?;", id)
	if err != nil {
		return fmt.Errorf("gagal menghapus template: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("template dengan ID %d tidak ditemukan", id)
	}
	return nil
}

func scanWorkTemplate(scanner rowScanner) (*domain.WorkTemplate, error) {
	var tpl domain.WorkTemplate
	var checklistJSON string
	var createdAtStr, updatedAtStr string

	err := scanner.Scan(
		&tpl.ID,
		&tpl.Name,
		&tpl.TitlePattern,
		&tpl.Description,
		&tpl.Category,
		&tpl.Priority,
		&tpl.DueDaysOffset,
		&checklistJSON,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		return nil, err
	}

	if checklistJSON != "" {
		_ = json.Unmarshal([]byte(checklistJSON), &tpl.ChecklistItems)
	}
	if tpl.ChecklistItems == nil {
		tpl.ChecklistItems = []string{}
	}

	tCreated, err := time.Parse(time.RFC3339Nano, createdAtStr)
	if err == nil {
		tpl.CreatedAt = tCreated.UTC()
	}
	tUpdated, err := time.Parse(time.RFC3339Nano, updatedAtStr)
	if err == nil {
		tpl.UpdatedAt = tUpdated.UTC()
	}
	return &tpl, nil
}
