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

// TaskManagementService mengkoordinasikan subtask, checklist, kategori, template, dan recurring tasks
type TaskManagementService struct {
	mu           sync.Mutex
	db           *sql.DB
	workRepo     *repository.WorkItemRepository
	activityRepo *repository.ActivityLogRepository
	subtaskRepo  *repository.SubtaskRepository
	categoryRepo *repository.CategoryRepository
	templateRepo *repository.WorkTemplateRepository
	recurRepo    *repository.RecurringRepository
}

func NewTaskManagementService(
	db *sql.DB,
	workRepo *repository.WorkItemRepository,
	activityRepo *repository.ActivityLogRepository,
	subtaskRepo *repository.SubtaskRepository,
	categoryRepo *repository.CategoryRepository,
	templateRepo *repository.WorkTemplateRepository,
	recurRepo *repository.RecurringRepository,
) *TaskManagementService {
	return &TaskManagementService{
		db:           db,
		workRepo:     workRepo,
		activityRepo: activityRepo,
		subtaskRepo:  subtaskRepo,
		categoryRepo: categoryRepo,
		templateRepo: templateRepo,
		recurRepo:    recurRepo,
	}
}

func (s *TaskManagementService) SetDB(db *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
	s.workRepo.SetDB(db)
	s.activityRepo.SetDB(db)
	s.subtaskRepo.SetDB(db)
	s.categoryRepo.SetDB(db)
	s.templateRepo.SetDB(db)
	s.recurRepo.SetDB(db)
}

func (s *TaskManagementService) getDB() *sql.DB {
	return s.db
}

// -----------------------------------------------------------------------------
// Subtask & Checklist Methods (Atomik dengan Activity Log & Progres)
// -----------------------------------------------------------------------------

// ListSubtasks mengambil seluruh checklist item dari suatu pekerjaan
func (s *TaskManagementService) ListSubtasks(ctx context.Context, workItemID int64) ([]*domain.Subtask, error) {
	return s.subtaskRepo.ListByWorkItemID(ctx, workItemID)
}

// AddSubtask menambahkan subtask baru, menghitung ulang progres, dan mencatat log
func (s *TaskManagementService) AddSubtask(ctx context.Context, workItemID int64, in domain.CreateSubtaskInput) (*domain.Subtask, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("validasi gagal: %w", err)
	}

	tx, err := s.getDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal memulai transaksi: %w", err)
	}
	defer tx.Rollback()

	workItem, err := s.workRepo.GetByIDTx(ctx, tx, workItemID)
	if err != nil {
		return nil, fmt.Errorf("pekerjaan tidak ditemukan: %w", err)
	}
	if workItem.ArchivedAt != nil {
		return nil, errors.New("tidak dapat menambah subtask pada pekerjaan yang diarsipkan")
	}

	// Tentukan sort_order jika belum dispesifikasikan
	existing, _ := s.subtaskRepo.ListByWorkItemIDTx(ctx, tx, workItemID)
	sortOrder := len(existing) + 1
	if in.SortOrder > 0 {
		sortOrder = in.SortOrder
	}

	now := time.Now().UTC()
	sub := &domain.Subtask{
		WorkItemID:  workItemID,
		Title:       in.Title,
		IsCompleted: false,
		DueOn:       in.DueOn,
		SortOrder:   sortOrder,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.subtaskRepo.CreateTx(ctx, tx, sub); err != nil {
		return nil, err
	}

	// Hitung ulang progres
	total, completed, err := s.subtaskRepo.CountAndCompletedCountTx(ctx, tx, workItemID)
	if err != nil {
		return nil, err
	}
	newProgress := domain.CalculateProgress(total, completed, workItem.Status)
	if err := s.workRepo.UpdateProgressTx(ctx, tx, workItemID, newProgress); err != nil {
		return nil, err
	}

	// Catat Activity Log append-only
	desc := fmt.Sprintf("Subtask ditambahkan: '%s' (progres: %d%%)", sub.Title, newProgress)
	act := &domain.ActivityLog{
		WorkItemID:  workItemID,
		EntryType:   domain.EntryTypeActivity,
		Description: desc,
		OccurredAt:  now,
		RecordedAt:  now,
	}
	if err := s.activityRepo.CreateTx(ctx, tx, act); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("gagal commit penambahan subtask: %w", err)
	}

	return sub, nil
}

// ToggleSubtask mengubah status penyelesaian subtask dan menghitung ulang progres
func (s *TaskManagementService) ToggleSubtask(ctx context.Context, subtaskID int64, isCompleted bool) (*domain.Subtask, error) {
	tx, err := s.getDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal memulai transaksi: %w", err)
	}
	defer tx.Rollback()

	sub, err := s.subtaskRepo.GetByIDTx(ctx, tx, subtaskID)
	if err != nil {
		return nil, fmt.Errorf("subtask tidak ditemukan: %w", err)
	}

	workItem, err := s.workRepo.GetByIDTx(ctx, tx, sub.WorkItemID)
	if err != nil {
		return nil, fmt.Errorf("pekerjaan terkait tidak ditemukan: %w", err)
	}
	if workItem.ArchivedAt != nil {
		return nil, errors.New("tidak dapat memperbarui subtask pada pekerjaan yang diarsipkan")
	}

	sub.IsCompleted = isCompleted
	sub.UpdatedAt = time.Now().UTC()
	if err := s.subtaskRepo.UpdateTx(ctx, tx, sub); err != nil {
		return nil, err
	}

	// Hitung ulang progres (tidak otomatis mengubah status pekerjaan utama)
	total, completed, err := s.subtaskRepo.CountAndCompletedCountTx(ctx, tx, sub.WorkItemID)
	if err != nil {
		return nil, err
	}
	newProgress := domain.CalculateProgress(total, completed, workItem.Status)
	if err := s.workRepo.UpdateProgressTx(ctx, tx, sub.WorkItemID, newProgress); err != nil {
		return nil, err
	}

	statusText := "belum selesai"
	if isCompleted {
		statusText = "selesai"
	}
	desc := fmt.Sprintf("Subtask '%s' ditandai %s (progres: %d%%)", sub.Title, statusText, newProgress)
	act := &domain.ActivityLog{
		WorkItemID:  sub.WorkItemID,
		EntryType:   domain.EntryTypeActivity,
		Description: desc,
		OccurredAt:  sub.UpdatedAt,
		RecordedAt:  sub.UpdatedAt,
	}
	if err := s.activityRepo.CreateTx(ctx, tx, act); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("gagal commit toggle subtask: %w", err)
	}

	return sub, nil
}

// UpdateSubtask memperbarui atribut subtask
func (s *TaskManagementService) UpdateSubtask(ctx context.Context, subtaskID int64, in domain.UpdateSubtaskInput) (*domain.Subtask, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("validasi gagal: %w", err)
	}

	tx, err := s.getDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal memulai transaksi: %w", err)
	}
	defer tx.Rollback()

	sub, err := s.subtaskRepo.GetByIDTx(ctx, tx, subtaskID)
	if err != nil {
		return nil, fmt.Errorf("subtask tidak ditemukan: %w", err)
	}

	workItem, err := s.workRepo.GetByIDTx(ctx, tx, sub.WorkItemID)
	if err != nil {
		return nil, fmt.Errorf("pekerjaan terkait tidak ditemukan: %w", err)
	}
	if workItem.ArchivedAt != nil {
		return nil, errors.New("tidak dapat mengupdate subtask pada pekerjaan yang diarsipkan")
	}

	if in.Title != nil {
		sub.Title = *in.Title
	}
	if in.DueOn != nil {
		sub.DueOn = in.DueOn
	}
	if in.SortOrder != nil {
		sub.SortOrder = *in.SortOrder
	}
	if in.IsCompleted != nil {
		sub.IsCompleted = *in.IsCompleted
	}
	sub.UpdatedAt = time.Now().UTC()

	if err := s.subtaskRepo.UpdateTx(ctx, tx, sub); err != nil {
		return nil, err
	}

	// Hitung ulang progres
	total, completed, err := s.subtaskRepo.CountAndCompletedCountTx(ctx, tx, sub.WorkItemID)
	if err != nil {
		return nil, err
	}
	newProgress := domain.CalculateProgress(total, completed, workItem.Status)
	if err := s.workRepo.UpdateProgressTx(ctx, tx, sub.WorkItemID, newProgress); err != nil {
		return nil, err
	}

	desc := fmt.Sprintf("Subtask '%s' diperbarui", sub.Title)
	act := &domain.ActivityLog{
		WorkItemID:  sub.WorkItemID,
		EntryType:   domain.EntryTypeActivity,
		Description: desc,
		OccurredAt:  sub.UpdatedAt,
		RecordedAt:  sub.UpdatedAt,
	}
	if err := s.activityRepo.CreateTx(ctx, tx, act); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("gagal commit update subtask: %w", err)
	}

	return sub, nil
}

// DeleteSubtask menghapus subtask, menghitung ulang progres, dan mencatat log
func (s *TaskManagementService) DeleteSubtask(ctx context.Context, subtaskID int64) error {
	tx, err := s.getDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("gagal memulai transaksi: %w", err)
	}
	defer tx.Rollback()

	sub, err := s.subtaskRepo.GetByIDTx(ctx, tx, subtaskID)
	if err != nil {
		return fmt.Errorf("subtask tidak ditemukan: %w", err)
	}

	workItem, err := s.workRepo.GetByIDTx(ctx, tx, sub.WorkItemID)
	if err != nil {
		return fmt.Errorf("pekerjaan terkait tidak ditemukan: %w", err)
	}
	if workItem.ArchivedAt != nil {
		return errors.New("tidak dapat menghapus subtask pada pekerjaan yang diarsipkan")
	}

	if err := s.subtaskRepo.DeleteTx(ctx, tx, subtaskID); err != nil {
		return err
	}

	// Hitung ulang progres sisa subtask
	total, completed, err := s.subtaskRepo.CountAndCompletedCountTx(ctx, tx, sub.WorkItemID)
	if err != nil {
		return err
	}
	newProgress := domain.CalculateProgress(total, completed, workItem.Status)
	if err := s.workRepo.UpdateProgressTx(ctx, tx, sub.WorkItemID, newProgress); err != nil {
		return err
	}

	now := time.Now().UTC()
	desc := fmt.Sprintf("Subtask '%s' dihapus (progres: %d%%)", sub.Title, newProgress)
	act := &domain.ActivityLog{
		WorkItemID:  sub.WorkItemID,
		EntryType:   domain.EntryTypeActivity,
		Description: desc,
		OccurredAt:  now,
		RecordedAt:  now,
	}
	if err := s.activityRepo.CreateTx(ctx, tx, act); err != nil {
		return nil
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("gagal commit penghapusan subtask: %w", err)
	}

	return nil
}

// -----------------------------------------------------------------------------
// Category Management Methods
// -----------------------------------------------------------------------------

func (s *TaskManagementService) ListCategories(ctx context.Context, onlyActive bool) ([]*domain.Category, error) {
	return s.categoryRepo.List(ctx, onlyActive)
}

func (s *TaskManagementService) CreateCategory(ctx context.Context, in domain.CreateCategoryInput) (*domain.Category, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("validasi gagal: %w", err)
	}

	// Pemeriksaan duplikasi nama kategori tanpa membedakan huruf besar/kecil (case-insensitive)
	existing, err := s.categoryRepo.GetByName(ctx, in.Name)
	if err == nil && existing != nil {
		if existing.IsActive {
			return nil, fmt.Errorf("kategori dengan nama '%s' sudah terdaftar", in.Name)
		}
		// Jika ada kategori lama yang dinonaktifkan dengan nama yang sama, aktifkan kembali
		existing.Description = in.Description
		existing.Color = in.Color
		existing.IsActive = true
		if err := s.categoryRepo.Update(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	}

	cat := &domain.Category{
		Name:        in.Name,
		Description: in.Description,
		Color:       in.Color,
		IsDefault:   false,
		IsActive:    true,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.categoryRepo.Create(ctx, cat); err != nil {
		return nil, err
	}
	return cat, nil
}

func (s *TaskManagementService) UpdateCategory(ctx context.Context, id int64, in domain.UpdateCategoryInput) (*domain.Category, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("validasi gagal: %w", err)
	}

	existing, err := s.categoryRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("kategori tidak ditemukan: %w", err)
	}

	// Cegah duplikasi nama dengan kategori lain (case-insensitive)
	duplicate, err := s.categoryRepo.GetByName(ctx, in.Name)
	if err == nil && duplicate != nil && duplicate.ID != existing.ID {
		return nil, fmt.Errorf("nama kategori '%s' sudah digunakan oleh kategori lain", in.Name)
	}

	// Kategori sistem yang wajib dipertahankan harus dilindungi dari pengubahan nama dan penonaktifan
	if existing.IsDefault {
		if strings.TrimSpace(strings.ToLower(in.Name)) != strings.TrimSpace(strings.ToLower(existing.Name)) {
			return nil, errors.New("nama kategori default sistem dilindungi dan tidak dapat diubah")
		}
		if in.IsActive != nil && !*in.IsActive {
			return nil, errors.New("kategori default sistem dilindungi dan tidak dapat dinonaktifkan")
		}
	}

	oldName := existing.Name
	existing.Name = in.Name
	existing.Description = in.Description
	existing.Color = in.Color
	if in.IsActive != nil {
		existing.IsActive = *in.IsActive
	}

	tx, err := s.getDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal memulai transaksi pembaruan kategori: %w", err)
	}
	defer tx.Rollback()

	if err := s.categoryRepo.UpdateTx(ctx, tx, existing); err != nil {
		return nil, err
	}

	// Jika nama kategori diperbarui, sinkronkan seluruh referensi
	// (work_items, work_templates, recurring_schedules) secara transaksional (CASE 6).
	// Log audit historis (activity_logs) tetap dipertahankan tanpa perubahan.
	if oldName != in.Name {
		if _, err := tx.ExecContext(ctx, "UPDATE work_items SET category = ? WHERE category = ?;", in.Name, oldName); err != nil {
			return nil, fmt.Errorf("gagal menyelaraskan kategori pada pekerjaan: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE work_templates SET category = ? WHERE category = ?;", in.Name, oldName); err != nil {
			return nil, fmt.Errorf("gagal menyelaraskan kategori pada template: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE recurring_schedules SET category = ? WHERE category = ?;", in.Name, oldName); err != nil {
			return nil, fmt.Errorf("gagal menyelaraskan kategori pada jadwal berulang: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("gagal commit pembaruan kategori: %w", err)
	}
	return existing, nil
}

func (s *TaskManagementService) DeleteCategory(ctx context.Context, id int64) error {
	tx, err := s.getDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("gagal memulai transaksi: %w", err)
	}
	defer tx.Rollback()

	// 1. Ambil data kategori untuk memeriksa keberadaan dan status default
	var catName string
	var isDefault int
	queryCat := "SELECT name, is_default FROM categories WHERE id = ?;"
	if err := tx.QueryRowContext(ctx, queryCat, id).Scan(&catName, &isDefault); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("kategori dengan ID %d tidak ditemukan", id)
		}
		return fmt.Errorf("gagal memeriksa kategori: %w", err)
	}

	// 2. Kategori sistem bawaan (default) dilindungi dan tidak boleh dihapus
	if isDefault == 1 {
		return errors.New("kategori default sistem dilindungi dan tidak dapat dihapus")
	}

	// 3. Periksa referensi pada work_items (termasuk pekerjaan yang diarsipkan)
	var workCount int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_items WHERE category = ?;", catName).Scan(&workCount); err != nil {
		return fmt.Errorf("gagal memeriksa referensi pekerjaan: %w", err)
	}

	// 4. Periksa referensi pada work_templates
	var templateCount int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_templates WHERE category = ?;", catName).Scan(&templateCount); err != nil {
		return fmt.Errorf("gagal memeriksa referensi template: %w", err)
	}

	// 5. Periksa referensi pada recurring_schedules
	var recurringCount int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM recurring_schedules WHERE category = ?;", catName).Scan(&recurringCount); err != nil {
		return fmt.Errorf("gagal memeriksa referensi jadwal berulang: %w", err)
	}

	// 6. Tolak penghapusan apabila masih ada referensi yang menggunakan kategori ini
	if workCount > 0 || templateCount > 0 || recurringCount > 0 {
		var parts []string
		if workCount > 0 {
			parts = append(parts, fmt.Sprintf("%d pekerjaan", workCount))
		}
		if templateCount > 0 {
			parts = append(parts, fmt.Sprintf("%d template", templateCount))
		}
		if recurringCount > 0 {
			parts = append(parts, fmt.Sprintf("%d jadwal berulang", recurringCount))
		}

		var usageStr string
		if len(parts) == 1 {
			usageStr = parts[0]
		} else if len(parts) == 2 {
			usageStr = fmt.Sprintf("%s dan %s", parts[0], parts[1])
		} else {
			usageStr = fmt.Sprintf("%s, dan %s", strings.Join(parts[:len(parts)-1], ", "), parts[len(parts)-1])
		}

		return fmt.Errorf("Kategori tidak dapat dihapus karena masih digunakan oleh %s. Nonaktifkan kategori atau pindahkan referensinya terlebih dahulu.", usageStr)
	}

	// 7. Lakukan hard delete pada baris kategori
	if err := s.categoryRepo.DeleteTx(ctx, tx, id); err != nil {
		return err
	}

	return tx.Commit()
}

// -----------------------------------------------------------------------------
// Work Templates Methods
// -----------------------------------------------------------------------------

func (s *TaskManagementService) ListTemplates(ctx context.Context) ([]*domain.WorkTemplate, error) {
	return s.templateRepo.List(ctx)
}

func (s *TaskManagementService) GetTemplate(ctx context.Context, id int64) (*domain.WorkTemplate, error) {
	return s.templateRepo.GetByID(ctx, id)
}

func (s *TaskManagementService) CreateTemplate(ctx context.Context, in domain.CreateWorkTemplateInput) (*domain.WorkTemplate, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("validasi gagal: %w", err)
	}

	now := time.Now().UTC()
	tpl := &domain.WorkTemplate{
		Name:           in.Name,
		TitlePattern:   in.TitlePattern,
		Description:    in.Description,
		Category:       in.Category,
		Priority:       in.Priority,
		DueDaysOffset:  in.DueDaysOffset,
		ChecklistItems: in.ChecklistItems,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.templateRepo.Create(ctx, tpl); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s *TaskManagementService) UpdateTemplate(ctx context.Context, id int64, in domain.UpdateWorkTemplateInput) (*domain.WorkTemplate, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("validasi gagal: %w", err)
	}

	existing, err := s.templateRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("template tidak ditemukan: %w", err)
	}

	existing.Name = in.Name
	existing.TitlePattern = in.TitlePattern
	existing.Description = in.Description
	existing.Category = in.Category
	existing.Priority = in.Priority
	existing.DueDaysOffset = in.DueDaysOffset
	existing.ChecklistItems = in.ChecklistItems
	existing.UpdatedAt = time.Now().UTC()

	if err := s.templateRepo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *TaskManagementService) DeleteTemplate(ctx context.Context, id int64) error {
	return s.templateRepo.Delete(ctx, id)
}

// ApplyTemplate membuat pekerjaan baru langsung dari template dan menyertakan checklist bawaan secara atomik
func (s *TaskManagementService) ApplyTemplate(ctx context.Context, templateID int64, receivedOn string) (*domain.WorkItem, error) {
	return s.ApplyTemplateWithInput(ctx, templateID, domain.ApplyTemplateInput{ReceivedOn: receivedOn})
}

// ApplyTemplateWithInput membuat pekerjaan baru dari template dengan parameter kustomisasi opsional secara atomik
func (s *TaskManagementService) ApplyTemplateWithInput(ctx context.Context, templateID int64, in domain.ApplyTemplateInput) (*domain.WorkItem, error) {
	tpl, err := s.templateRepo.GetByID(ctx, templateID)
	if err != nil {
		return nil, fmt.Errorf("template pekerjaan tidak ditemukan: %w", err)
	}

	receivedOn := in.ReceivedOn
	if err := domain.ValidateDateYYYYMMDD(receivedOn); err != nil {
		receivedOn = domain.GetCurrentLocalDate()
	}

	now := time.Now().UTC()
	year := now.Year()
	parts := strings.Split(receivedOn, "-")
	if len(parts) == 3 {
		if y, err := strconv.Atoi(parts[0]); err == nil && y >= 2000 && y <= 2100 {
			year = y
		}
	}

	// Hitung due_on: gunakan in.DueOn jika disuplai; jika tidak, hitung receivedOn + due_days_offset
	var dueOn *string
	if in.DueOn != nil && strings.TrimSpace(*in.DueOn) != "" {
		trimmed := strings.TrimSpace(*in.DueOn)
		if err := domain.ValidateDateYYYYMMDD(trimmed); err == nil {
			dueOn = &trimmed
		}
	}
	if dueOn == nil && tpl.DueDaysOffset >= 0 {
		recDate, err := time.Parse("2006-01-02", receivedOn)
		if err == nil {
			calculatedDue := recDate.AddDate(0, 0, tpl.DueDaysOffset).Format("2006-01-02")
			dueOn = &calculatedDue
		}
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = tpl.TitlePattern
	}

	desc := strings.TrimSpace(in.Description)
	if desc == "" {
		desc = tpl.Description
	}

	category := strings.TrimSpace(in.Category)
	if category == "" {
		category = tpl.Category
	}

	priority := strings.TrimSpace(in.Priority)
	if priority == "" || !domain.IsValidPriority(priority) {
		priority = tpl.Priority
	}

	requestedBy := strings.TrimSpace(in.RequestedBy)
	if requestedBy == "" {
		requestedBy = fmt.Sprintf("Template: %s", tpl.Name)
	}

	tx, err := s.getDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal memulai transaksi: %w", err)
	}
	defer tx.Rollback()

	code, err := s.workRepo.GenerateNextWorkCode(ctx, tx, year)
	if err != nil {
		return nil, err
	}

	item := &domain.WorkItem{
		WorkCode:           code,
		Title:              title,
		Description:        desc,
		Category:           category,
		RequestedBy:        requestedBy,
		ReceivedOn:         receivedOn,
		DueOn:              dueOn,
		Priority:           priority,
		Status:             domain.StatusNotStarted,
		ProgressPercentage: 0,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := s.workRepo.CreateTx(ctx, tx, item); err != nil {
		return nil, err
	}

	// Buat subtask checklist sesuai template
	for i, checkItem := range tpl.ChecklistItems {
		sub := &domain.Subtask{
			WorkItemID:  item.ID,
			Title:       checkItem,
			IsCompleted: false,
			SortOrder:   i + 1,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.subtaskRepo.CreateTx(ctx, tx, sub); err != nil {
			return nil, err
		}
	}

	// Catat log
	actDesc := fmt.Sprintf("Pekerjaan dibuat dari template '%s' dengan kode %s (%d subtask)", tpl.Name, item.WorkCode, len(tpl.ChecklistItems))
	act := &domain.ActivityLog{
		WorkItemID:  item.ID,
		EntryType:   domain.EntryTypeCreated,
		Description: actDesc,
		OccurredAt:  now,
		RecordedAt:  now,
	}
	if err := s.activityRepo.CreateTx(ctx, tx, act); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("gagal commit apply template: %w", err)
	}

	return item, nil
}

// -----------------------------------------------------------------------------
// Recurring Tasks Generator (Idempotency, Offline Protection)
// -----------------------------------------------------------------------------

func (s *TaskManagementService) ListRecurringSchedules(ctx context.Context) ([]*domain.RecurringSchedule, error) {
	return s.recurRepo.List(ctx)
}

func (s *TaskManagementService) GetRecurringSchedule(ctx context.Context, id int64) (*domain.RecurringSchedule, error) {
	return s.recurRepo.GetByID(ctx, id)
}

func (s *TaskManagementService) CreateRecurringSchedule(ctx context.Context, in domain.CreateRecurringScheduleInput) (*domain.RecurringSchedule, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("validasi gagal: %w", err)
	}

	now := time.Now().UTC()
	// Tentukan next_run_date awal = start_date
	nextRunDate := in.StartDate

	sch := &domain.RecurringSchedule{
		TemplateID:    in.TemplateID,
		Title:         in.Title,
		Description:   in.Description,
		Category:      in.Category,
		Priority:      in.Priority,
		Frequency:     in.Frequency,
		IntervalValue: in.IntervalValue,
		DayOfMonth:    in.DayOfMonth,
		DayOfWeek:     in.DayOfWeek,
		StartDate:     in.StartDate,
		EndDate:       in.EndDate,
		NextRunDate:   nextRunDate,
		IsActive:      true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.recurRepo.Create(ctx, sch); err != nil {
		return nil, err
	}
	return sch, nil
}

func (s *TaskManagementService) UpdateRecurringSchedule(ctx context.Context, id int64, in domain.UpdateRecurringScheduleInput) (*domain.RecurringSchedule, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("validasi gagal: %w", err)
	}

	existing, err := s.recurRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("jadwal berulang tidak ditemukan: %w", err)
	}

	// Jika hanya mengubah status aktif/nonaktif (toggle is_active)
	if strings.TrimSpace(in.Title) == "" && strings.TrimSpace(in.Frequency) == "" && in.IsActive != nil {
		existing.IsActive = *in.IsActive
		existing.UpdatedAt = time.Now().UTC()
		if err := s.recurRepo.Update(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	}

	existing.TemplateID = in.TemplateID
	existing.Title = in.Title
	existing.Description = in.Description
	existing.Category = in.Category
	existing.Priority = in.Priority
	existing.Frequency = in.Frequency
	existing.IntervalValue = in.IntervalValue
	existing.DayOfMonth = in.DayOfMonth
	existing.DayOfWeek = in.DayOfWeek
	existing.StartDate = in.StartDate
	existing.EndDate = in.EndDate
	if in.IsActive != nil {
		existing.IsActive = *in.IsActive
	}
	existing.UpdatedAt = time.Now().UTC()

	if err := s.recurRepo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *TaskManagementService) DeleteRecurringSchedule(ctx context.Context, id int64) error {
	return s.recurRepo.Delete(ctx, id)
}

// CalculateNextRunDate menghitung tanggal berikutnya berdasarkan frekuensi dan interval
func CalculateNextRunDate(currentDate string, frequency string, interval int) (string, error) {
	t, err := time.Parse("2006-01-02", currentDate)
	if err != nil {
		return "", err
	}
	if interval <= 0 {
		interval = 1
	}

	var next time.Time
	switch frequency {
	case "daily":
		next = t.AddDate(0, 0, interval)
	case "weekly":
		next = t.AddDate(0, 0, interval*7)
	case "monthly":
		next = t.AddDate(0, interval, 0)
	case "quarterly":
		next = t.AddDate(0, interval*3, 0)
	case "yearly":
		next = t.AddDate(interval, 0, 0)
	default:
		next = t.AddDate(0, 0, interval)
	}
	return next.Format("2006-01-02"), nil
}

type RecurringEvaluationResult struct {
	TotalSchedules int                 `json:"total_schedules"`
	DueSchedules   int                 `json:"due_schedules"`
	CreatedItems   []*domain.WorkItem  `json:"created_items"`
	SkippedDetails []RecurringSkipInfo `json:"skipped_details"`
}

type RecurringSkipInfo struct {
	ScheduleID  int64  `json:"schedule_id"`
	Title       string `json:"title"`
	Reason      string `json:"reason"`
	NextRunDate string `json:"next_run_date"`
}

// TriggerRecurringGeneration mengevaluasi jadwal yang jatuh tempo dan menghasilkan pekerjaan baru secara IDEMPOTENT.
func (s *TaskManagementService) TriggerRecurringGeneration(ctx context.Context, asOfDate string) ([]*domain.WorkItem, error) {
	rep, err := s.TriggerRecurringWithReport(ctx, asOfDate)
	if err != nil {
		return nil, err
	}
	return rep.CreatedItems, nil
}

// TriggerRecurringWithReport mengevaluasi jadwal yang jatuh tempo dan menghasilkan laporan evaluasi lengkap beserta alasan skip.
func (s *TaskManagementService) TriggerRecurringWithReport(ctx context.Context, asOfDate string) (*RecurringEvaluationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if asOfDate == "" {
		asOfDate = domain.GetCurrentLocalDate()
	}

	allSchedules, err := s.recurRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil daftar jadwal berulang: %w", err)
	}

	result := &RecurringEvaluationResult{
		TotalSchedules: len(allSchedules),
		CreatedItems:   []*domain.WorkItem{},
		SkippedDetails: []RecurringSkipInfo{},
	}

	for _, sch := range allSchedules {
		if !sch.IsActive {
			result.SkippedDetails = append(result.SkippedDetails, RecurringSkipInfo{
				ScheduleID:  sch.ID,
				Title:       sch.Title,
				Reason:      "Jadwal nonaktif",
				NextRunDate: sch.NextRunDate,
			})
			continue
		}

		if sch.NextRunDate > asOfDate {
			result.SkippedDetails = append(result.SkippedDetails, RecurringSkipInfo{
				ScheduleID:  sch.ID,
				Title:       sch.Title,
				Reason:      fmt.Sprintf("Belum jatuh tempo (jadwal berikutnya: %s)", sch.NextRunDate),
				NextRunDate: sch.NextRunDate,
			})
			continue
		}

		if sch.EndDate != nil && strings.TrimSpace(*sch.EndDate) != "" && sch.NextRunDate > *sch.EndDate {
			result.SkippedDetails = append(result.SkippedDetails, RecurringSkipInfo{
				ScheduleID:  sch.ID,
				Title:       sch.Title,
				Reason:      fmt.Sprintf("Jadwal sudah melewati tanggal selesai (%s)", *sch.EndDate),
				NextRunDate: sch.NextRunDate,
			})
			continue
		}

		// Teridentifikasi sebagai jatuh tempo
		result.DueSchedules++

		// Kebijakan CASE 4: Generator tidak menciptakan pekerjaan baru menggunakan kategori nonaktif
		if cat, err := s.categoryRepo.GetByName(ctx, sch.Category); err == nil && cat != nil && !cat.IsActive {
			result.SkippedDetails = append(result.SkippedDetails, RecurringSkipInfo{
				ScheduleID:  sch.ID,
				Title:       sch.Title,
				Reason:      fmt.Sprintf("Kategori '%s' sedang nonaktif", sch.Category),
				NextRunDate: sch.NextRunDate,
			})
			continue
		}

		// Idempotency Check 1: Jika last_generated_date persis sama dengan next_run_date, lewati
		if sch.LastGeneratedDate != nil && *sch.LastGeneratedDate == sch.NextRunDate {
			result.SkippedDetails = append(result.SkippedDetails, RecurringSkipInfo{
				ScheduleID:  sch.ID,
				Title:       sch.Title,
				Reason:      fmt.Sprintf("Sudah diproses untuk tanggal %s", sch.NextRunDate),
				NextRunDate: sch.NextRunDate,
			})
			continue
		}

		// Idempotency Check 2: Pastikan tidak ada pekerjaan dengan requested_by = Jadwal #ID dan received_on = targetDate
		targetPeriodDate := sch.NextRunDate
		requestedByTag := fmt.Sprintf("Jadwal Rutin #%d", sch.ID)

		// Jika jadwal terhubung ke template, ambil checklist items sebelum membuka transaksi
		// untuk menghindari deadlock koneksi SQLite (MaxOpenConns=1)
		var templateChecklist []string
		if sch.TemplateID != nil {
			if tpl, err := s.templateRepo.GetByID(ctx, *sch.TemplateID); err == nil && tpl != nil {
				templateChecklist = tpl.ChecklistItems
			}
		}

		tx, err := s.getDB().BeginTx(ctx, nil)
		if err != nil {
			continue
		}

		var existingCount int
		_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_items WHERE requested_by = ? AND received_on = ?;", requestedByTag, targetPeriodDate).Scan(&existingCount)
		if existingCount > 0 {
			// Sudah pernah dibuat, cukup majukan tanggal jadwal berikutnya
			nextDate, _ := CalculateNextRunDate(targetPeriodDate, sch.Frequency, sch.IntervalValue)
			// Jika masih tertinggal jauh di belakang asOfDate (karena offline lama), majukan hingga melampaui asOfDate
			for nextDate <= asOfDate {
				cand, err := CalculateNextRunDate(nextDate, sch.Frequency, sch.IntervalValue)
				if err != nil || cand <= nextDate {
					break
				}
				nextDate = cand
			}
			_ = s.recurRepo.UpdateScheduleRunTx(ctx, tx, sch.ID, targetPeriodDate, nextDate)
			_ = tx.Commit()
			result.SkippedDetails = append(result.SkippedDetails, RecurringSkipInfo{
				ScheduleID:  sch.ID,
				Title:       sch.Title,
				Reason:      fmt.Sprintf("Pekerjaan sudah ada untuk periode %s (jadwal dimajukan ke %s)", targetPeriodDate, nextDate),
				NextRunDate: nextDate,
			})
			continue
		}

		now := time.Now().UTC()
		year := now.Year()
		parts := strings.Split(targetPeriodDate, "-")
		if len(parts) == 3 {
			if y, err := strconv.Atoi(parts[0]); err == nil && y >= 2000 && y <= 2100 {
				year = y
			}
		}

		code, err := s.workRepo.GenerateNextWorkCode(ctx, tx, year)
		if err != nil {
			_ = tx.Rollback()
			continue
		}

		itemTitle := fmt.Sprintf("%s (%s)", sch.Title, targetPeriodDate)
		item := &domain.WorkItem{
			WorkCode:           code,
			Title:              itemTitle,
			Description:        sch.Description,
			Category:           sch.Category,
			RequestedBy:        requestedByTag,
			ReceivedOn:         targetPeriodDate,
			DueOn:              &targetPeriodDate,
			Priority:           sch.Priority,
			Status:             domain.StatusNotStarted,
			ProgressPercentage: 0,
			CreatedAt:          now,
			UpdatedAt:          now,
		}

		if err := s.workRepo.CreateTx(ctx, tx, item); err != nil {
			_ = tx.Rollback()
			continue
		}

		// Jika jadwal terhubung ke template, sertakan subtask checklist
		for i, checkItem := range templateChecklist {
			sub := &domain.Subtask{
				WorkItemID:  item.ID,
				Title:       checkItem,
				IsCompleted: false,
				SortOrder:   i + 1,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			_ = s.subtaskRepo.CreateTx(ctx, tx, sub)
		}

		// Catat log aktivitas
		desc := fmt.Sprintf("Pekerjaan dibuat otomatis oleh jadwal berulang '%s' untuk periode %s", sch.Title, targetPeriodDate)
		act := &domain.ActivityLog{
			WorkItemID:  item.ID,
			EntryType:   domain.EntryTypeCreated,
			Description: desc,
			OccurredAt:  now,
			RecordedAt:  now,
		}
		_ = s.activityRepo.CreateTx(ctx, tx, act)

		// Hitung next_run_date berikutnya
		nextDate, _ := CalculateNextRunDate(targetPeriodDate, sch.Frequency, sch.IntervalValue)
		// Kebijakan Offline Recovery: Jika aplikasi lama mati, jangan spam antrean, majukan nextDate hingga melewati asOfDate
		for nextDate <= asOfDate {
			cand, err := CalculateNextRunDate(nextDate, sch.Frequency, sch.IntervalValue)
			if err != nil || cand <= nextDate {
				break
			}
			nextDate = cand
		}

		// Perbarui tanggal run pada tabel recurring_schedules
		if err := s.recurRepo.UpdateScheduleRunTx(ctx, tx, sch.ID, targetPeriodDate, nextDate); err != nil {
			_ = tx.Rollback()
			continue
		}

		if err := tx.Commit(); err == nil {
			result.CreatedItems = append(result.CreatedItems, item)
		}
	}

	return result, nil
}
