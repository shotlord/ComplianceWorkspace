package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
)

// WorkItemService mengkoordinasikan logika bisnis pekerjaan dan activity log dalam transaksi atomik
type WorkItemService struct {
	mu           sync.RWMutex
	db           *sql.DB
	workRepo     *repository.WorkItemRepository
	activityRepo *repository.ActivityLogRepository
}

func NewWorkItemService(db *sql.DB, workRepo *repository.WorkItemRepository, activityRepo *repository.ActivityLogRepository) *WorkItemService {
	return &WorkItemService{
		db:           db,
		workRepo:     workRepo,
		activityRepo: activityRepo,
	}
}

// SetDB memperbarui referensi database secara thread-safe (misal setelah restore)
func (s *WorkItemService) SetDB(db *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
	s.workRepo.SetDB(db)
	s.activityRepo.SetDB(db)
}

// GetDB mengambil referensi database aktif saat ini
func (s *WorkItemService) GetDB() *sql.DB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db
}

// CreateWorkItem membuat pekerjaan baru dan mencatat activity log 'created' dalam 1 transaksi
func (s *WorkItemService) CreateWorkItem(ctx context.Context, in domain.CreateWorkItemInput) (*domain.WorkItem, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("validasi gagal: %w", err)
	}

	now := time.Now().UTC()

	// Parse tahun dari received_on untuk kode pekerjaan (CMP-YYYY-NNNNNN)
	year := now.Year()
	parts := strings.Split(in.ReceivedOn, "-")
	if len(parts) == 3 {
		if y, err := strconv.Atoi(parts[0]); err == nil && y >= 2000 && y <= 2100 {
			year = y
		}
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal memulai transaksi: %w", err)
	}
	defer tx.Rollback()

	code, err := s.workRepo.GenerateNextWorkCode(ctx, tx, year)
	if err != nil {
		return nil, err
	}

	item := &domain.WorkItem{
		WorkCode:    code,
		Title:       in.Title,
		Description: in.Description,
		Category:    in.Category,
		RequestedBy: in.RequestedBy,
		ReceivedOn:  in.ReceivedOn,
		DueOn:       in.DueOn,
		Priority:   in.Priority,
		Status:     domain.StatusNotStarted,
		Provenance: domain.ProvenanceManual,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.workRepo.CreateTx(ctx, tx, item); err != nil {
		return nil, err
	}

	// Catat log aktivitas pembuatan
	desc := fmt.Sprintf("Pekerjaan dibuat dengan kode %s (%s)", item.WorkCode, item.Title)
	log := &domain.ActivityLog{
		WorkItemID:  item.ID,
		EntryType:   domain.EntryTypeCreated,
		Description: desc,
		OccurredAt:  now,
		RecordedAt:  now,
	}
	if err := s.activityRepo.CreateTx(ctx, tx, log); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("gagal commit transaksi pembuatan pekerjaan: %w", err)
	}

	return item, nil
}

// UpdateWorkItem memperbarui data pekerjaan dan mencatat perubahan ke log dalam 1 transaksi
func (s *WorkItemService) UpdateWorkItem(ctx context.Context, id int64, in domain.UpdateWorkItemInput) (*domain.WorkItem, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("validasi gagal: %w", err)
	}

	now := time.Now().UTC()

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal memulai transaksi: %w", err)
	}
	defer tx.Rollback()

	existing, err := s.workRepo.GetByIDTx(ctx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("pekerjaan tidak ditemukan: %w", err)
	}

	if existing.ArchivedAt != nil {
		return nil, errors.New("pekerjaan yang diarsipkan tidak dapat diubah; pulihkan terlebih dahulu")
	}

	type diff struct {
		fieldName string
		oldVal    string
		newVal    string
		desc      string
		isStatus  bool
	}
	var diffs []diff

	if existing.Title != in.Title {
		diffs = append(diffs, diff{
			fieldName: "title",
			oldVal:    existing.Title,
			newVal:    in.Title,
			desc:      fmt.Sprintf("Judul diubah dari '%s' menjadi '%s'", existing.Title, in.Title),
		})
		existing.Title = in.Title
	}

	if existing.Description != in.Description {
		diffs = append(diffs, diff{
			fieldName: "description",
			oldVal:    existing.Description,
			newVal:    in.Description,
			desc:      "Deskripsi pekerjaan diperbarui",
		})
		existing.Description = in.Description
	}

	if existing.Category != in.Category {
		diffs = append(diffs, diff{
			fieldName: "category",
			oldVal:    existing.Category,
			newVal:    in.Category,
			desc:      fmt.Sprintf("Kategori diubah dari '%s' menjadi '%s'", existing.Category, in.Category),
		})
		existing.Category = in.Category
	}

	if existing.RequestedBy != in.RequestedBy {
		diffs = append(diffs, diff{
			fieldName: "requested_by",
			oldVal:    existing.RequestedBy,
			newVal:    in.RequestedBy,
			desc:      fmt.Sprintf("Pemberi tugas diubah dari '%s' menjadi '%s'", existing.RequestedBy, in.RequestedBy),
		})
		existing.RequestedBy = in.RequestedBy
	}

	if existing.ReceivedOn != in.ReceivedOn {
		diffs = append(diffs, diff{
			fieldName: "received_on",
			oldVal:    existing.ReceivedOn,
			newVal:    in.ReceivedOn,
			desc:      fmt.Sprintf("Tanggal diterima diubah dari '%s' menjadi '%s'", existing.ReceivedOn, in.ReceivedOn),
		})
		existing.ReceivedOn = in.ReceivedOn
	}

	oldDue := ""
	if existing.DueOn != nil {
		oldDue = *existing.DueOn
	}
	newDue := ""
	if in.DueOn != nil {
		newDue = *in.DueOn
	}
	if oldDue != newDue {
		diffs = append(diffs, diff{
			fieldName: "due_on",
			oldVal:    oldDue,
			newVal:    newDue,
			desc:      fmt.Sprintf("Tenggat waktu diubah dari '%s' menjadi '%s'", oldDue, newDue),
		})
		existing.DueOn = in.DueOn
	}

	if existing.Priority != in.Priority {
		diffs = append(diffs, diff{
			fieldName: "priority",
			oldVal:    existing.Priority,
			newVal:    in.Priority,
			desc:      fmt.Sprintf("Prioritas diubah dari '%s' menjadi '%s'", existing.Priority, in.Priority),
		})
		existing.Priority = in.Priority
	}

	if existing.Status != in.Status {
		diffs = append(diffs, diff{
			fieldName: "status",
			oldVal:    existing.Status,
			newVal:    in.Status,
			desc:      fmt.Sprintf("Status diubah dari '%s' menjadi '%s'", existing.Status, in.Status),
			isStatus:  true,
		})
		existing.Status = in.Status

		if in.Status == domain.StatusCompleted {
			existing.CompletedAt = &now
		} else if existing.CompletedAt != nil && in.Status != domain.StatusCompleted {
			existing.CompletedAt = nil
		}

		// Sesuaikan progress_percentage jika pekerjaan tidak memiliki subtask
		var subtaskCount int
		_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM subtasks WHERE work_item_id = ?", id).Scan(&subtaskCount)
		if subtaskCount == 0 {
			existing.ProgressPercentage = domain.CalculateProgress(0, 0, in.Status)
		}
	}

	// Jika tidak ada perubahan, langsung kembalikan data lama
	if len(diffs) == 0 {
		return existing, nil
	}

	existing.UpdatedAt = now
	if err := s.workRepo.UpdateTx(ctx, tx, existing); err != nil {
		return nil, err
	}

	for _, d := range diffs {
		entryType := domain.EntryTypeEdited
		if d.isStatus {
			entryType = domain.EntryTypeStatusChanged
		}

		oldV := d.oldVal
		newV := d.newVal
		fName := d.fieldName

		log := &domain.ActivityLog{
			WorkItemID:  existing.ID,
			EntryType:   entryType,
			Description: d.desc,
			FieldName:   &fName,
			OldValue:    &oldV,
			NewValue:    &newV,
			OccurredAt:  now,
			RecordedAt:  now,
		}
		if err := s.activityRepo.CreateTx(ctx, tx, log); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("gagal commit transaksi pembaruan pekerjaan: %w", err)
	}

	return existing, nil
}

// ArchiveWorkItem mengarsipkan pekerjaan dan mencatat log 'archived' dalam 1 transaksi
func (s *WorkItemService) ArchiveWorkItem(ctx context.Context, id int64) (*domain.WorkItem, error) {
	now := time.Now().UTC()

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal memulai transaksi: %w", err)
	}
	defer tx.Rollback()

	item, err := s.workRepo.GetByIDTx(ctx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("pekerjaan tidak ditemukan: %w", err)
	}

	if item.ArchivedAt != nil {
		return item, nil // Sudah diarsipkan sebelumnya
	}

	item.ArchivedAt = &now
	item.UpdatedAt = now

	if err := s.workRepo.UpdateTx(ctx, tx, item); err != nil {
		return nil, err
	}

	log := &domain.ActivityLog{
		WorkItemID:  item.ID,
		EntryType:   domain.EntryTypeArchived,
		Description: fmt.Sprintf("Pekerjaan '%s' diarsipkan", item.Title),
		OccurredAt:  now,
		RecordedAt:  now,
	}
	if err := s.activityRepo.CreateTx(ctx, tx, log); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("gagal commit transaksi pengarsipan: %w", err)
	}

	return item, nil
}

// RestoreWorkItem memulihkan pekerjaan dari arsip dan mencatat log 'restored' dalam 1 transaksi
func (s *WorkItemService) RestoreWorkItem(ctx context.Context, id int64) (*domain.WorkItem, error) {
	now := time.Now().UTC()

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal memulai transaksi: %w", err)
	}
	defer tx.Rollback()

	item, err := s.workRepo.GetByIDTx(ctx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("pekerjaan tidak ditemukan: %w", err)
	}

	if item.ArchivedAt == nil {
		return item, nil // Belum pernah diarsipkan
	}

	item.ArchivedAt = nil
	item.UpdatedAt = now

	if err := s.workRepo.UpdateTx(ctx, tx, item); err != nil {
		return nil, err
	}

	log := &domain.ActivityLog{
		WorkItemID:  item.ID,
		EntryType:   domain.EntryTypeRestored,
		Description: fmt.Sprintf("Pekerjaan '%s' dipulihkan dari arsip", item.Title),
		OccurredAt:  now,
		RecordedAt:  now,
	}
	if err := s.activityRepo.CreateTx(ctx, tx, log); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("gagal commit transaksi pemulihan arsip: %w", err)
	}

	return item, nil
}

// AddActivity mencatat progres manual pada pekerjaan dalam 1 transaksi
func (s *WorkItemService) AddActivity(ctx context.Context, workItemID int64, in domain.AddActivityInput) (*domain.ActivityLog, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("validasi gagal: %w", err)
	}

	now := time.Now().UTC()
	occurredAt := now
	if in.OccurredAt != nil && *in.OccurredAt != "" {
		if t, err := time.Parse(time.RFC3339, *in.OccurredAt); err == nil {
			occurredAt = t.UTC()
		}
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal memulai transaksi: %w", err)
	}
	defer tx.Rollback()

	item, err := s.workRepo.GetByIDTx(ctx, tx, workItemID)
	if err != nil {
		return nil, fmt.Errorf("pekerjaan tidak ditemukan: %w", err)
	}

	// Update waktu updated_at pada pekerjaan induk
	item.UpdatedAt = now
	if err := s.workRepo.UpdateTx(ctx, tx, item); err != nil {
		return nil, err
	}

	log := &domain.ActivityLog{
		WorkItemID:  workItemID,
		EntryType:   domain.EntryTypeActivity,
		Description: in.Description,
		OccurredAt:  occurredAt,
		RecordedAt:  now,
	}
	if err := s.activityRepo.CreateTx(ctx, tx, log); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("gagal commit transaksi penambahan log: %w", err)
	}

	return log, nil
}

// GetWorkItem mengambil detail pekerjaan
func (s *WorkItemService) GetWorkItem(ctx context.Context, id int64) (*domain.WorkItem, error) {
	return s.workRepo.GetByID(ctx, id)
}

// ListWorkItems mengambil daftar pekerjaan sesuai kriteria filter
func (s *WorkItemService) ListWorkItems(ctx context.Context, filter repository.WorkItemFilter) ([]*domain.WorkItem, error) {
	return s.workRepo.List(ctx, filter)
}

// ListActivities mengambil seluruh riwayat aktivitas pekerjaan
func (s *WorkItemService) ListActivities(ctx context.Context, workItemID int64) ([]*domain.ActivityLog, error) {
	// Verifikasi apakah pekerjaan ada
	if _, err := s.workRepo.GetByID(ctx, workItemID); err != nil {
		return nil, fmt.Errorf("pekerjaan tidak ditemukan: %w", err)
	}
	return s.activityRepo.ListByWorkItemID(ctx, workItemID)
}
