package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"compliance-workspace/internal/domain"
)

// RecurringRepository mengelola tabel recurring_schedules
type RecurringRepository struct {
	mu sync.RWMutex
	db *sql.DB
}

func NewRecurringRepository(db *sql.DB) *RecurringRepository {
	return &RecurringRepository{db: db}
}

func (r *RecurringRepository) SetDB(db *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
}

func (r *RecurringRepository) getDB() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db
}

// List mengambil seluruh jadwal pekerjaan berulang
func (r *RecurringRepository) List(ctx context.Context) ([]*domain.RecurringSchedule, error) {
	query := `
		SELECT
			id, template_id, title, description, category, priority,
			frequency, interval_value, day_of_month, day_of_week,
			start_date, end_date, last_generated_date, next_run_date,
			is_active, created_at, updated_at
		FROM recurring_schedules
		ORDER BY is_active DESC, next_run_date ASC;
	`
	rows, err := r.getDB().QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil daftar jadwal berulang: %w", err)
	}
	defer rows.Close()

	var list []*domain.RecurringSchedule
	for rows.Next() {
		s, err := scanRecurringSchedule(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, nil
}

// GetByID mengambil satu jadwal berulang berdasarkan ID
func (r *RecurringRepository) GetByID(ctx context.Context, id int64) (*domain.RecurringSchedule, error) {
	query := `
		SELECT
			id, template_id, title, description, category, priority,
			frequency, interval_value, day_of_month, day_of_week,
			start_date, end_date, last_generated_date, next_run_date,
			is_active, created_at, updated_at
		FROM recurring_schedules
		WHERE id = ?;
	`
	row := r.getDB().QueryRowContext(ctx, query, id)
	return scanRecurringSchedule(row)
}

// ListDueSchedules mengambil jadwal aktif yang tanggal jalannya <= asOfDate
func (r *RecurringRepository) ListDueSchedules(ctx context.Context, asOfDate string) ([]*domain.RecurringSchedule, error) {
	query := `
		SELECT
			id, template_id, title, description, category, priority,
			frequency, interval_value, day_of_month, day_of_week,
			start_date, end_date, last_generated_date, next_run_date,
			is_active, created_at, updated_at
		FROM recurring_schedules
		WHERE is_active = 1
		  AND next_run_date <= ?
		  AND (end_date IS NULL OR next_run_date <= end_date)
		ORDER BY next_run_date ASC;
	`
	rows, err := r.getDB().QueryContext(ctx, query, asOfDate)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil jadwal berulang yang jatuh tempo: %w", err)
	}
	defer rows.Close()

	var list []*domain.RecurringSchedule
	for rows.Next() {
		s, err := scanRecurringSchedule(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, nil
}

// Create menyimpan jadwal baru
func (r *RecurringRepository) Create(ctx context.Context, s *domain.RecurringSchedule) error {
	query := `
		INSERT INTO recurring_schedules (
			template_id, title, description, category, priority,
			frequency, interval_value, day_of_month, day_of_week,
			start_date, end_date, last_generated_date, next_run_date,
			is_active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	isActive := 0
	if s.IsActive {
		isActive = 1
	}

	createdAtStr := s.CreatedAt.UTC().Format(time.RFC3339Nano)
	updatedAtStr := s.UpdatedAt.UTC().Format(time.RFC3339Nano)

	res, err := r.getDB().ExecContext(ctx, query,
		s.TemplateID, s.Title, s.Description, s.Category, s.Priority,
		s.Frequency, s.IntervalValue, s.DayOfMonth, s.DayOfWeek,
		s.StartDate, s.EndDate, s.LastGeneratedDate, s.NextRunDate,
		isActive, createdAtStr, updatedAtStr,
	)
	if err != nil {
		return fmt.Errorf("gagal menyimpan jadwal berulang: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	s.ID = id
	return nil
}

// Update memperbarui jadwal berulang
func (r *RecurringRepository) Update(ctx context.Context, s *domain.RecurringSchedule) error {
	query := `
		UPDATE recurring_schedules SET
			template_id = ?, title = ?, description = ?, category = ?, priority = ?,
			frequency = ?, interval_value = ?, day_of_month = ?, day_of_week = ?,
			start_date = ?, end_date = ?, next_run_date = ?, is_active = ?, updated_at = ?
		WHERE id = ?;
	`
	isActive := 0
	if s.IsActive {
		isActive = 1
	}
	updatedAtStr := s.UpdatedAt.UTC().Format(time.RFC3339Nano)

	res, err := r.getDB().ExecContext(ctx, query,
		s.TemplateID, s.Title, s.Description, s.Category, s.Priority,
		s.Frequency, s.IntervalValue, s.DayOfMonth, s.DayOfWeek,
		s.StartDate, s.EndDate, s.NextRunDate, isActive, updatedAtStr, s.ID,
	)
	if err != nil {
		return fmt.Errorf("gagal mengupdate jadwal berulang: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("jadwal berulang dengan ID %d tidak ditemukan", s.ID)
	}
	return nil
}

// UpdateScheduleRunTx memperbarui tanggal generasi terakhir dan tanggal jadwal berikutnya dalam transaksi
func (r *RecurringRepository) UpdateScheduleRunTx(ctx context.Context, tx *sql.Tx, id int64, lastGeneratedDate, nextRunDate string) error {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	query := `
		UPDATE recurring_schedules SET
			last_generated_date = ?, next_run_date = ?, updated_at = ?
		WHERE id = ?;
	`
	res, err := tx.ExecContext(ctx, query, lastGeneratedDate, nextRunDate, nowStr, id)
	if err != nil {
		return fmt.Errorf("gagal memperbarui run date jadwal berulang: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("jadwal dengan ID %d tidak ditemukan", id)
	}
	return nil
}

// Delete menghapus jadwal berulang
func (r *RecurringRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.getDB().ExecContext(ctx, "DELETE FROM recurring_schedules WHERE id = ?;", id)
	if err != nil {
		return fmt.Errorf("gagal menghapus jadwal berulang: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("jadwal berulang dengan ID %d tidak ditemukan", id)
	}
	return nil
}

func scanRecurringSchedule(scanner rowScanner) (*domain.RecurringSchedule, error) {
	var s domain.RecurringSchedule
	var templateID sql.NullInt64
	var dayOfMonth, dayOfWeek sql.NullInt64
	var endDate, lastGenDate sql.NullString
	var isActive int
	var createdAtStr, updatedAtStr string

	err := scanner.Scan(
		&s.ID,
		&templateID,
		&s.Title,
		&s.Description,
		&s.Category,
		&s.Priority,
		&s.Frequency,
		&s.IntervalValue,
		&dayOfMonth,
		&dayOfWeek,
		&s.StartDate,
		&endDate,
		&lastGenDate,
		&s.NextRunDate,
		&isActive,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		return nil, err
	}

	if templateID.Valid {
		s.TemplateID = &templateID.Int64
	}
	if dayOfMonth.Valid {
		val := int(dayOfMonth.Int64)
		s.DayOfMonth = &val
	}
	if dayOfWeek.Valid {
		val := int(dayOfWeek.Int64)
		s.DayOfWeek = &val
	}
	if endDate.Valid {
		s.EndDate = &endDate.String
	}
	if lastGenDate.Valid {
		s.LastGeneratedDate = &lastGenDate.String
	}
	s.IsActive = (isActive == 1)

	tCreated, err := time.Parse(time.RFC3339Nano, createdAtStr)
	if err == nil {
		s.CreatedAt = tCreated.UTC()
	}
	tUpdated, err := time.Parse(time.RFC3339Nano, updatedAtStr)
	if err == nil {
		s.UpdatedAt = tUpdated.UTC()
	}
	return &s, nil
}
