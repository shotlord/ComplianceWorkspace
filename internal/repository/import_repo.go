package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"compliance-workspace/internal/domain"
)

// ImportRepository mengelola persistensi import_batches dan import_items
type ImportRepository struct {
	mu sync.RWMutex
	db *sql.DB
}

func NewImportRepository(db *sql.DB) *ImportRepository {
	return &ImportRepository{db: db}
}

func (r *ImportRepository) SetDB(db *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
}

func (r *ImportRepository) getDB() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db
}

// CreateBatchTx menyimpan entri import_batches di dalam transaksi
func (r *ImportRepository) CreateBatchTx(ctx context.Context, tx *sql.Tx, b *domain.ImportBatch) error {
	query := `
		INSERT INTO import_batches (
			batch_code, file_name, file_type, file_size, total_rows,
			imported_rows, skipped_rows, failed_rows, status, error_summary,
			column_mapping_json, created_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	createdAtStr := b.CreatedAt.UTC().Format(time.RFC3339Nano)
	var completedAtStr *string
	if b.CompletedAt != nil {
		s := b.CompletedAt.UTC().Format(time.RFC3339Nano)
		completedAtStr = &s
	}

	res, err := tx.ExecContext(ctx, query,
		b.BatchCode, b.FileName, b.FileType, b.FileSize, b.TotalRows,
		b.ImportedRows, b.SkippedRows, b.FailedRows, b.Status, b.ErrorSummary,
		b.ColumnMappingJSON, createdAtStr, completedAtStr,
	)
	if err != nil {
		return fmt.Errorf("gagal membuat batch impor: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	b.ID = id
	return nil
}

// UpdateBatchTx memperbarui status dan ringkasan batch di dalam transaksi
func (r *ImportRepository) UpdateBatchTx(ctx context.Context, tx *sql.Tx, b *domain.ImportBatch) error {
	query := `
		UPDATE import_batches SET
			total_rows = ?, imported_rows = ?, skipped_rows = ?, failed_rows = ?,
			status = ?, error_summary = ?, completed_at = ?
		WHERE id = ?
	`
	var completedAtStr *string
	if b.CompletedAt != nil {
		s := b.CompletedAt.UTC().Format(time.RFC3339Nano)
		completedAtStr = &s
	}

	_, err := tx.ExecContext(ctx, query,
		b.TotalRows, b.ImportedRows, b.SkippedRows, b.FailedRows,
		b.Status, b.ErrorSummary, completedAtStr, b.ID,
	)
	if err != nil {
		return fmt.Errorf("gagal memperbarui batch impor: %w", err)
	}
	return nil
}

// CreateItemTx menyimpan entri import_items di dalam transaksi
func (r *ImportRepository) CreateItemTx(ctx context.Context, tx *sql.Tx, item *domain.ImportItem) error {
	query := `
		INSERT INTO import_items (
			batch_id, row_number, work_item_id, status, action_type,
			error_message, raw_data_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	createdAtStr := item.CreatedAt.UTC().Format(time.RFC3339Nano)

	res, err := tx.ExecContext(ctx, query,
		item.BatchID, item.RowNumber, item.WorkItemID, item.Status, item.ActionType,
		item.ErrorMessage, item.RawDataJSON, createdAtStr,
	)
	if err != nil {
		return fmt.Errorf("gagal menyimpan rincian item impor baris %d: %w", item.RowNumber, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	item.ID = id
	return nil
}

// ListBatches mengambil daftar batch impor terurut terbaru
func (r *ImportRepository) ListBatches(ctx context.Context, limit, offset int) ([]*domain.ImportBatch, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT
			id, batch_code, file_name, file_type, file_size, total_rows,
			imported_rows, skipped_rows, failed_rows, status, error_summary,
			column_mapping_json, created_at, completed_at
		FROM import_batches
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`
	rows, err := r.getDB().QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil riwayat batch impor: %w", err)
	}
	defer rows.Close()

	var batches []*domain.ImportBatch
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		batches = append(batches, b)
	}
	return batches, nil
}

// GetBatchByID mengambil satu batch berdasarkan ID
func (r *ImportRepository) GetBatchByID(ctx context.Context, id int64) (*domain.ImportBatch, error) {
	query := `
		SELECT
			id, batch_code, file_name, file_type, file_size, total_rows,
			imported_rows, skipped_rows, failed_rows, status, error_summary,
			column_mapping_json, created_at, completed_at
		FROM import_batches
		WHERE id = ?
	`
	row := r.getDB().QueryRowContext(ctx, query, id)
	return scanBatch(row)
}

// GetBatchItems mengambil seluruh baris item rincian impor suatu batch
func (r *ImportRepository) GetBatchItems(ctx context.Context, batchID int64) ([]*domain.ImportItem, error) {
	query := `
		SELECT
			id, batch_id, row_number, work_item_id, status, action_type,
			error_message, raw_data_json, created_at
		FROM import_items
		WHERE batch_id = ?
		ORDER BY row_number ASC
	`
	rows, err := r.getDB().QueryContext(ctx, query, batchID)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil rincian item batch %d: %w", batchID, err)
	}
	defer rows.Close()

	var items []*domain.ImportItem
	for rows.Next() {
		var item domain.ImportItem
		var workItemID sql.NullInt64
		var errMessage, rawDataJSON sql.NullString
		var createdAtStr string

		if err := rows.Scan(
			&item.ID, &item.BatchID, &item.RowNumber, &workItemID, &item.Status, &item.ActionType,
			&errMessage, &rawDataJSON, &createdAtStr,
		); err != nil {
			return nil, err
		}

		if workItemID.Valid {
			item.WorkItemID = &workItemID.Int64
		}
		if errMessage.Valid {
			item.ErrorMessage = errMessage.String
		}
		if rawDataJSON.Valid {
			item.RawDataJSON = rawDataJSON.String
		}
		if t, err := time.Parse(time.RFC3339Nano, createdAtStr); err == nil {
			item.CreatedAt = t.UTC()
		}

		items = append(items, &item)
	}
	return items, nil
}

// GetCreatedWorkItemIDsByBatch mengambil daftar ID pekerjaan yang berhasil dibuat oleh batch tertentu
func (r *ImportRepository) GetCreatedWorkItemIDsByBatch(ctx context.Context, tx *sql.Tx, batchID int64) ([]int64, error) {
	query := `
		SELECT work_item_id
		FROM import_items
		WHERE batch_id = ? AND status = 'created' AND work_item_id IS NOT NULL
	`
	var rows *sql.Rows
	var err error
	if tx != nil {
		rows, err = tx.QueryContext(ctx, query, batchID)
	} else {
		rows, err = r.getDB().QueryContext(ctx, query, batchID)
	}
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil id pekerjaan batch %d: %w", batchID, err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// SetBatchStatusTx memperbarui status batch (misal saat kompensasi / rollback)
func (r *ImportRepository) SetBatchStatusTx(ctx context.Context, tx *sql.Tx, batchID int64, status string) error {
	query := "UPDATE import_batches SET status = ? WHERE id = ?"
	res, err := tx.ExecContext(ctx, query, status, batchID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("batch dengan ID %d tidak ditemukan", batchID)
	}
	return nil
}

func scanBatch(scanner rowScanner) (*domain.ImportBatch, error) {
	var b domain.ImportBatch
	var errorSummary sql.NullString
	var createdAtStr string
	var completedAtStr sql.NullString

	err := scanner.Scan(
		&b.ID, &b.BatchCode, &b.FileName, &b.FileType, &b.FileSize, &b.TotalRows,
		&b.ImportedRows, &b.SkippedRows, &b.FailedRows, &b.Status, &errorSummary,
		&b.ColumnMappingJSON, &createdAtStr, &completedAtStr,
	)
	if err != nil {
		return nil, err
	}

	if errorSummary.Valid {
		b.ErrorSummary = errorSummary.String
	}
	if t, err := time.Parse(time.RFC3339Nano, createdAtStr); err == nil {
		b.CreatedAt = t.UTC()
	}
	if completedAtStr.Valid {
		if t, err := time.Parse(time.RFC3339Nano, completedAtStr.String); err == nil {
			tUTC := t.UTC()
			b.CompletedAt = &tUTC
		}
	}
	b.CreatedCount = b.ImportedRows
	b.SkippedCount = b.SkippedRows
	b.FailedCount = b.FailedRows

	return &b, nil
}
