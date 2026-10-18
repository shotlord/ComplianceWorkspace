package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"compliance-workspace/internal/domain"
)

// CategoryRepository mengelola operasi master tabel categories
type CategoryRepository struct {
	mu sync.RWMutex
	db *sql.DB
}

func NewCategoryRepository(db *sql.DB) *CategoryRepository {
	return &CategoryRepository{db: db}
}

func (r *CategoryRepository) SetDB(db *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
}

func (r *CategoryRepository) getDB() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db
}

// List mengambil daftar kategori, dengan opsi filter hanya kategori aktif
func (r *CategoryRepository) List(ctx context.Context, onlyActive bool) ([]*domain.Category, error) {
	query := `
		SELECT id, name, description, color, is_default, is_active, created_at
		FROM categories
	`
	if onlyActive {
		query += " WHERE is_active = 1"
	}
	query += " ORDER BY is_default DESC, name ASC;"

	rows, err := r.getDB().QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil daftar kategori: %w", err)
	}
	defer rows.Close()

	var list []*domain.Category
	for rows.Next() {
		cat, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, cat)
	}
	return list, nil
}

// GetByID mengambil kategori berdasarkan ID
func (r *CategoryRepository) GetByID(ctx context.Context, id int64) (*domain.Category, error) {
	query := `
		SELECT id, name, description, color, is_default, is_active, created_at
		FROM categories
		WHERE id = ?;
	`
	row := r.getDB().QueryRowContext(ctx, query, id)
	return scanCategory(row)
}

// GetByName mengambil kategori berdasarkan nama secara case-insensitive
func (r *CategoryRepository) GetByName(ctx context.Context, name string) (*domain.Category, error) {
	query := `
		SELECT id, name, description, color, is_default, is_active, created_at
		FROM categories
		WHERE LOWER(trim(name)) = LOWER(trim(?));
	`
	row := r.getDB().QueryRowContext(ctx, query, name)
	return scanCategory(row)
}

// GetByNameTx mengambil kategori berdasarkan nama secara case-insensitive dalam konteks transaksi
func (r *CategoryRepository) GetByNameTx(ctx context.Context, tx *sql.Tx, name string) (*domain.Category, error) {
	query := `
		SELECT id, name, description, color, is_default, is_active, created_at
		FROM categories
		WHERE LOWER(trim(name)) = LOWER(trim(?));
	`
	row := tx.QueryRowContext(ctx, query, name)
	return scanCategory(row)
}

// IsUsedByWorkItems memeriksa apakah nama kategori telah digunakan pada tabel work_items
func (r *CategoryRepository) IsUsedByWorkItems(ctx context.Context, categoryName string) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM work_items WHERE LOWER(trim(category)) = LOWER(trim(?));`
	err := r.getDB().QueryRowContext(ctx, query, categoryName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Create menambahkan kategori baru dengan status aktif
func (r *CategoryRepository) Create(ctx context.Context, cat *domain.Category) error {
	query := `
		INSERT INTO categories (name, description, color, is_default, is_active, created_at)
		VALUES (?, ?, ?, ?, ?, ?);
	`
	isDef := 0
	if cat.IsDefault {
		isDef = 1
	}
	isActive := 1
	if !cat.IsActive {
		isActive = 0
	}
	createdAtStr := cat.CreatedAt.UTC().Format(time.RFC3339Nano)

	res, err := r.getDB().ExecContext(ctx, query, cat.Name, cat.Description, cat.Color, isDef, isActive, createdAtStr)
	if err != nil {
		return fmt.Errorf("gagal menyimpan kategori: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	cat.ID = id
	cat.IsActive = true
	return nil
}

// CreateTx menambahkan kategori baru dalam konteks transaksi yang diberikan
func (r *CategoryRepository) CreateTx(ctx context.Context, tx *sql.Tx, cat *domain.Category) error {
	query := `
		INSERT INTO categories (name, description, color, is_default, is_active, created_at)
		VALUES (?, ?, ?, ?, ?, ?);
	`
	isDef := 0
	if cat.IsDefault {
		isDef = 1
	}
	isActive := 1
	if !cat.IsActive {
		isActive = 0
	}
	createdAtStr := cat.CreatedAt.UTC().Format(time.RFC3339Nano)

	res, err := tx.ExecContext(ctx, query, cat.Name, cat.Description, cat.Color, isDef, isActive, createdAtStr)
	if err != nil {
		return fmt.Errorf("gagal menyimpan kategori: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	cat.ID = id
	cat.IsActive = true
	return nil
}

// Update memperbarui data kategori dan status aktif
func (r *CategoryRepository) Update(ctx context.Context, cat *domain.Category) error {
	query := `
		UPDATE categories SET
			name = ?, description = ?, color = ?, is_active = ?
		WHERE id = ?;
	`
	isActive := 1
	if !cat.IsActive {
		isActive = 0
	}

	res, err := r.getDB().ExecContext(ctx, query, cat.Name, cat.Description, cat.Color, isActive, cat.ID)
	if err != nil {
		return fmt.Errorf("gagal mengupdate kategori: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("kategori dengan ID %d tidak ditemukan", cat.ID)
	}
	return nil
}

// UpdateTx memperbarui kategori dalam konteks transaksi yang diberikan
func (r *CategoryRepository) UpdateTx(ctx context.Context, tx *sql.Tx, cat *domain.Category) error {
	query := `
		UPDATE categories SET
			name = ?, description = ?, color = ?, is_active = ?
		WHERE id = ?;
	`
	isActive := 1
	if !cat.IsActive {
		isActive = 0
	}

	res, err := tx.ExecContext(ctx, query, cat.Name, cat.Description, cat.Color, isActive, cat.ID)
	if err != nil {
		return fmt.Errorf("gagal mengupdate kategori: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("kategori dengan ID %d tidak ditemukan", cat.ID)
	}
	return nil
}

// Delete menghapus baris kategori secara permanen dari database
func (r *CategoryRepository) Delete(ctx context.Context, id int64) error {
	cat, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if cat.IsDefault {
		return errors.New("kategori default sistem dilindungi dan tidak dapat dihapus")
	}

	res, err := r.getDB().ExecContext(ctx, "DELETE FROM categories WHERE id = ?;", id)
	if err != nil {
		return fmt.Errorf("gagal menghapus kategori: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("kategori dengan ID %d tidak ditemukan", id)
	}
	return nil
}

// DeleteTx menghapus baris kategori secara permanen dalam transaksi yang diberikan
func (r *CategoryRepository) DeleteTx(ctx context.Context, tx *sql.Tx, id int64) error {
	res, err := tx.ExecContext(ctx, "DELETE FROM categories WHERE id = ?;", id)
	if err != nil {
		return fmt.Errorf("gagal menghapus kategori: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("kategori dengan ID %d tidak ditemukan", id)
	}
	return nil
}

func scanCategory(scanner rowScanner) (*domain.Category, error) {
	var cat domain.Category
	var isDef, isActive int
	var createdAtStr string

	err := scanner.Scan(
		&cat.ID,
		&cat.Name,
		&cat.Description,
		&cat.Color,
		&isDef,
		&isActive,
		&createdAtStr,
	)
	if err != nil {
		return nil, err
	}

	cat.IsDefault = (isDef == 1)
	cat.IsActive = (isActive == 1)
	tCreated, err := time.Parse(time.RFC3339Nano, createdAtStr)
	if err == nil {
		cat.CreatedAt = tCreated.UTC()
	}
	return &cat, nil
}
