package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Status pekerjaan
const (
	StatusNotStarted = "not_started"
	StatusInProgress = "in_progress"
	StatusWaiting    = "waiting"
	StatusCompleted  = "completed"
	StatusCancelled  = "cancelled"
)

// Prioritas pekerjaan
const (
	PriorityLow      = "low"
	PriorityNormal   = "normal"
	PriorityHigh     = "high"
	PriorityCritical = "critical"
)

// Tipe entri riwayat aktivitas
const (
	EntryTypeCreated       = "created"
	EntryTypeActivity      = "activity"
	EntryTypeEdited        = "edited"
	EntryTypeStatusChanged = "status_changed"
	EntryTypeArchived      = "archived"
	EntryTypeRestored      = "restored"
)

// Asal/Provenance pekerjaan (v1.1.0 Batch 06)
const (
	ProvenanceManual    = "manual"
	ProvenanceTemplate  = "template"
	ProvenanceRecurring = "recurring"
	ProvenanceImport    = "import"
	ProvenanceUnknown   = "unknown"
)

// IsValidProvenance memeriksa apakah nilai provenance valid
func IsValidProvenance(p string) bool {
	switch p {
	case ProvenanceManual, ProvenanceTemplate, ProvenanceRecurring, ProvenanceImport, ProvenanceUnknown:
		return true
	default:
		return false
	}
}

// WorkItem mewakili entitas pekerjaan dalam sistem
type WorkItem struct {
	ID                    int64      `json:"id"`
	WorkCode              string     `json:"work_code"`
	Title                 string     `json:"title"`
	Description           string     `json:"description"`
	Category              string     `json:"category"`
	RequestedBy           string     `json:"requested_by"`
	ReceivedOn            string     `json:"received_on"` // YYYY-MM-DD
	DueOn                 *string    `json:"due_on"`      // YYYY-MM-DD opsional
	Priority              string     `json:"priority"`
	Status                string     `json:"status"`
	ProgressPercentage    int        `json:"progress_percentage"` // 0-100%
	SubtaskCount          int        `json:"subtask_count"`
	CompletedSubtaskCount int        `json:"completed_subtask_count"`
	Provenance            string     `json:"provenance"` // manual, recurring, import, template
	CompletedAt           *time.Time `json:"completed_at"` // UTC RFC3339 opsional
	CreatedAt             time.Time  `json:"created_at"`   // UTC RFC3339
	UpdatedAt             time.Time  `json:"updated_at"`   // UTC RFC3339
	ArchivedAt            *time.Time `json:"archived_at"`  // UTC RFC3339 opsional
}

// DefaultLocation mewakili zona waktu standar Asia/Jakarta (WIB)
var DefaultLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.FixedZone("WIB", 7*3600)
	}
	return loc
}()

// GetCurrentLocalDate mengembalikan tanggal kalender lokal saat ini dalam format YYYY-MM-DD
func GetCurrentLocalDate() string {
	return time.Now().In(DefaultLocation).Format("2006-01-02")
}

// ActivityLog mewakili catatan log append-only untuk setiap pekerjaan
type ActivityLog struct {
	ID          int64     `json:"id"`
	WorkItemID  int64     `json:"work_item_id"`
	EntryType   string    `json:"entry_type"`
	Description string    `json:"description"`
	FieldName   *string   `json:"field_name,omitempty"`
	OldValue    *string   `json:"old_value,omitempty"`
	NewValue    *string   `json:"new_value,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"` // UTC RFC3339
	RecordedAt  time.Time `json:"recorded_at"` // UTC RFC3339
}

// CreateWorkItemInput mewakili payload pembuatan pekerjaan baru
type CreateWorkItemInput struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	RequestedBy string  `json:"requested_by"`
	ReceivedOn  string  `json:"received_on"` // YYYY-MM-DD
	DueOn       *string `json:"due_on"`      // YYYY-MM-DD opsional
	Priority    string  `json:"priority"`
}

// UpdateWorkItemInput mewakili payload pembaruan data pekerjaan
type UpdateWorkItemInput struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	RequestedBy string  `json:"requested_by"`
	ReceivedOn  string  `json:"received_on"` // YYYY-MM-DD
	DueOn       *string `json:"due_on"`      // YYYY-MM-DD opsional
	Priority    string  `json:"priority"`
	Status      string  `json:"status"`
}

// AddActivityInput mewakili payload penambahan catatan progres manual
type AddActivityInput struct {
	Description string  `json:"description"`
	OccurredAt  *string `json:"occurred_at"` // opsional RFC3339, default waktu sekarang
}

// Validasi status
func IsValidStatus(s string) bool {
	switch s {
	case StatusNotStarted, StatusInProgress, StatusWaiting, StatusCompleted, StatusCancelled:
		return true
	default:
		return false
	}
}

// Validasi prioritas
func IsValidPriority(p string) bool {
	switch p {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityCritical:
		return true
	default:
		return false
	}
}

// Validasi format tanggal YYYY-MM-DD
func ValidateDateYYYYMMDD(d string) error {
	if d == "" {
		return errors.New("tanggal tidak boleh kosong")
	}
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return fmt.Errorf("format tanggal harus YYYY-MM-DD: %w", err)
	}
	if t.Format("2006-01-02") != d {
		return errors.New("tanggal tidak valid")
	}
	return nil
}

// Validasi input pembuatan pekerjaan
func (in *CreateWorkItemInput) Validate() error {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return errors.New("judul pekerjaan (title) wajib diisi")
	}
	if in.Category == "" {
		in.Category = "General"
	}
	in.Category = strings.TrimSpace(in.Category)

	if in.Priority == "" {
		in.Priority = PriorityNormal
	}
	if !IsValidPriority(in.Priority) {
		return fmt.Errorf("prioritas tidak valid: %s (pilihan: low, normal, high, critical)", in.Priority)
	}

	if err := ValidateDateYYYYMMDD(in.ReceivedOn); err != nil {
		return fmt.Errorf("tanggal diterima (received_on) tidak valid: %w", err)
	}

	if in.DueOn != nil && strings.TrimSpace(*in.DueOn) != "" {
		trimmedDue := strings.TrimSpace(*in.DueOn)
		if err := ValidateDateYYYYMMDD(trimmedDue); err != nil {
			return fmt.Errorf("tenggat waktu (due_on) tidak valid: %w", err)
		}
		in.DueOn = &trimmedDue
	} else {
		in.DueOn = nil
	}

	in.Description = strings.TrimSpace(in.Description)
	in.RequestedBy = strings.TrimSpace(in.RequestedBy)
	return nil
}

// Validasi input pembaruan pekerjaan
func (in *UpdateWorkItemInput) Validate() error {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return errors.New("judul pekerjaan (title) wajib diisi")
	}
	if in.Category == "" {
		in.Category = "General"
	}
	in.Category = strings.TrimSpace(in.Category)

	if !IsValidPriority(in.Priority) {
		return fmt.Errorf("prioritas tidak valid: %s", in.Priority)
	}
	if !IsValidStatus(in.Status) {
		return fmt.Errorf("status tidak valid: %s", in.Status)
	}

	if err := ValidateDateYYYYMMDD(in.ReceivedOn); err != nil {
		return fmt.Errorf("tanggal diterima (received_on) tidak valid: %w", err)
	}

	if in.DueOn != nil && strings.TrimSpace(*in.DueOn) != "" {
		trimmedDue := strings.TrimSpace(*in.DueOn)
		if err := ValidateDateYYYYMMDD(trimmedDue); err != nil {
			return fmt.Errorf("tenggat waktu (due_on) tidak valid: %w", err)
		}
		in.DueOn = &trimmedDue
	} else {
		in.DueOn = nil
	}

	in.Description = strings.TrimSpace(in.Description)
	in.RequestedBy = strings.TrimSpace(in.RequestedBy)
	return nil
}

// Validasi input penambahan aktivitas manual
func (in *AddActivityInput) Validate() error {
	in.Description = strings.TrimSpace(in.Description)
	if in.Description == "" {
		return errors.New("deskripsi aktivitas wajib diisi")
	}
	if in.OccurredAt != nil && strings.TrimSpace(*in.OccurredAt) != "" {
		trimmed := strings.TrimSpace(*in.OccurredAt)
		_, err := time.Parse(time.RFC3339, trimmed)
		if err != nil {
			return fmt.Errorf("waktu kejadian (occurred_at) harus dalam format RFC3339 (contoh: 2026-10-17T12:00:00Z): %w", err)
		}
		in.OccurredAt = &trimmed
	}
	return nil
}

// Subtask mewakili item checklist pada suatu pekerjaan
type Subtask struct {
	ID          int64     `json:"id"`
	WorkItemID  int64     `json:"work_item_id"`
	Title       string    `json:"title"`
	IsCompleted bool      `json:"is_completed"`
	DueOn       *string   `json:"due_on,omitempty"` // YYYY-MM-DD opsional
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"` // UTC RFC3339
	UpdatedAt   time.Time `json:"updated_at"` // UTC RFC3339
}

// CreateSubtaskInput mewakili payload penambahan subtask
type CreateSubtaskInput struct {
	Title     string  `json:"title"`
	DueOn     *string `json:"due_on,omitempty"`
	SortOrder int     `json:"sort_order,omitempty"`
}

func (in *CreateSubtaskInput) Validate() error {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return errors.New("judul subtask (title) wajib diisi")
	}
	if in.DueOn != nil && strings.TrimSpace(*in.DueOn) != "" {
		trimmed := strings.TrimSpace(*in.DueOn)
		if err := ValidateDateYYYYMMDD(trimmed); err != nil {
			return fmt.Errorf("tenggat subtask tidak valid: %w", err)
		}
		in.DueOn = &trimmed
	} else {
		in.DueOn = nil
	}
	return nil
}

// UpdateSubtaskInput mewakili payload pembaruan subtask
type UpdateSubtaskInput struct {
	Title       *string `json:"title,omitempty"`
	IsCompleted *bool   `json:"is_completed,omitempty"`
	DueOn       *string `json:"due_on,omitempty"`
	SortOrder   *int    `json:"sort_order,omitempty"`
}

func (in *UpdateSubtaskInput) Validate() error {
	if in.Title != nil {
		trimmed := strings.TrimSpace(*in.Title)
		if trimmed == "" {
			return errors.New("judul subtask tidak boleh kosong")
		}
		in.Title = &trimmed
	}
	if in.DueOn != nil && strings.TrimSpace(*in.DueOn) != "" {
		trimmed := strings.TrimSpace(*in.DueOn)
		if err := ValidateDateYYYYMMDD(trimmed); err != nil {
			return fmt.Errorf("tenggat subtask tidak valid: %w", err)
		}
		in.DueOn = &trimmed
	}
	return nil
}

// Category mewakili master kategori penugasan
type Category struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Color       string    `json:"color"`
	IsDefault   bool      `json:"is_default"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateCategoryInput mewakili payload pembuatan kategori baru
type CreateCategoryInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

func (in *CreateCategoryInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("nama kategori wajib diisi")
	}
	in.Description = strings.TrimSpace(in.Description)
	in.Color = strings.TrimSpace(in.Color)
	if in.Color == "" {
		in.Color = "#4f46e5"
	}
	return nil
}

// UpdateCategoryInput mewakili payload pembaruan kategori
type UpdateCategoryInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	IsActive    *bool  `json:"is_active,omitempty"`
}

func (in *UpdateCategoryInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("nama kategori wajib diisi")
	}
	in.Description = strings.TrimSpace(in.Description)
	in.Color = strings.TrimSpace(in.Color)
	if in.Color == "" {
		in.Color = "#4f46e5"
	}
	return nil
}

// WorkTemplate mewakili template baku pekerjaan kepatuhan
type WorkTemplate struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	TitlePattern   string    `json:"title_pattern"`
	Description    string    `json:"description"`
	Category       string    `json:"category"`
	Priority       string    `json:"priority"`
	DueDaysOffset  int       `json:"due_days_offset"`
	ChecklistItems []string  `json:"checklist_items"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CreateWorkTemplateInput mewakili payload pembuatan template pekerjaan
type CreateWorkTemplateInput struct {
	Name           string   `json:"name"`
	TitlePattern   string   `json:"title_pattern"`
	Description    string   `json:"description"`
	Category       string   `json:"category"`
	Priority       string   `json:"priority"`
	DueDaysOffset  int      `json:"due_days_offset"`
	ChecklistItems []string `json:"checklist_items"`
}

func (in *CreateWorkTemplateInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("nama template wajib diisi")
	}
	in.TitlePattern = strings.TrimSpace(in.TitlePattern)
	if in.TitlePattern == "" {
		return errors.New("pola judul pekerjaan (title_pattern) wajib diisi")
	}
	in.Category = strings.TrimSpace(in.Category)
	if in.Category == "" {
		in.Category = "General"
	}
	in.Priority = strings.TrimSpace(in.Priority)
	if in.Priority == "" {
		in.Priority = PriorityNormal
	}
	if !IsValidPriority(in.Priority) {
		return fmt.Errorf("prioritas tidak valid: %s", in.Priority)
	}
	if in.DueDaysOffset < 0 {
		in.DueDaysOffset = 0
	}
	var cleanChecklist []string
	for _, item := range in.ChecklistItems {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			cleanChecklist = append(cleanChecklist, trimmed)
		}
	}
	in.ChecklistItems = cleanChecklist
	return nil
}

// UpdateWorkTemplateInput mewakili payload pembaruan template pekerjaan
type UpdateWorkTemplateInput struct {
	Name           string   `json:"name"`
	TitlePattern   string   `json:"title_pattern"`
	Description    string   `json:"description"`
	Category       string   `json:"category"`
	Priority       string   `json:"priority"`
	DueDaysOffset  int      `json:"due_days_offset"`
	ChecklistItems []string `json:"checklist_items"`
}

func (in *UpdateWorkTemplateInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("nama template wajib diisi")
	}
	in.TitlePattern = strings.TrimSpace(in.TitlePattern)
	if in.TitlePattern == "" {
		return errors.New("pola judul pekerjaan (title_pattern) wajib diisi")
	}
	in.Category = strings.TrimSpace(in.Category)
	if in.Category == "" {
		in.Category = "General"
	}
	in.Priority = strings.TrimSpace(in.Priority)
	if in.Priority == "" {
		in.Priority = PriorityNormal
	}
	if !IsValidPriority(in.Priority) {
		return fmt.Errorf("prioritas tidak valid: %s", in.Priority)
	}
	if in.DueDaysOffset < 0 {
		in.DueDaysOffset = 0
	}
	var cleanChecklist []string
	for _, item := range in.ChecklistItems {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			cleanChecklist = append(cleanChecklist, trimmed)
		}
	}
	in.ChecklistItems = cleanChecklist
	return nil
}

// ApplyTemplateInput mewakili parameter kustomisasi opsional saat membuat pekerjaan dari template
type ApplyTemplateInput struct {
	ReceivedOn  string  `json:"received_on"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	RequestedBy string  `json:"requested_by"`
	DueOn       *string `json:"due_on"`
	Priority    string  `json:"priority"`
}

// RecurringSchedule mewakili jadwal pekerjaan berulang
type RecurringSchedule struct {
	ID                 int64     `json:"id"`
	TemplateID         *int64    `json:"template_id,omitempty"`
	Title              string    `json:"title"`
	Description        string    `json:"description"`
	Category           string    `json:"category"`
	Priority           string    `json:"priority"`
	Frequency          string    `json:"frequency"` // daily, weekly, monthly, quarterly, yearly
	IntervalValue      int       `json:"interval_value"`
	DayOfMonth         *int      `json:"day_of_month,omitempty"`
	DayOfWeek          *int      `json:"day_of_week,omitempty"`
	StartDate          string    `json:"start_date"` // YYYY-MM-DD
	EndDate            *string   `json:"end_date,omitempty"`
	LastGeneratedDate  *string   `json:"last_generated_date,omitempty"`
	NextRunDate        string    `json:"next_run_date"` // YYYY-MM-DD
	IsActive           bool      `json:"is_active"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// CreateRecurringScheduleInput mewakili payload pembuatan jadwal berulang
type CreateRecurringScheduleInput struct {
	TemplateID    *int64  `json:"template_id,omitempty"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Category      string  `json:"category"`
	Priority      string  `json:"priority"`
	Frequency     string  `json:"frequency"`
	IntervalValue int     `json:"interval_value"`
	DayOfMonth    *int    `json:"day_of_month,omitempty"`
	DayOfWeek     *int    `json:"day_of_week,omitempty"`
	StartDate     string  `json:"start_date"`
	EndDate       *string `json:"end_date,omitempty"`
}

func (in *CreateRecurringScheduleInput) Validate() error {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return errors.New("judul jadwal berulang wajib diisi")
	}
	in.Category = strings.TrimSpace(in.Category)
	if in.Category == "" {
		in.Category = "General"
	}
	in.Priority = strings.TrimSpace(in.Priority)
	if in.Priority == "" {
		in.Priority = PriorityNormal
	}
	if !IsValidPriority(in.Priority) {
		return fmt.Errorf("prioritas tidak valid: %s", in.Priority)
	}

	validFreq := map[string]bool{
		"daily": true, "weekly": true, "monthly": true, "quarterly": true, "yearly": true,
	}
	if !validFreq[in.Frequency] {
		return fmt.Errorf("frekuensi tidak valid: %s (pilihan: daily, weekly, monthly, quarterly, yearly)", in.Frequency)
	}
	if in.IntervalValue <= 0 {
		in.IntervalValue = 1
	}

	if err := ValidateDateYYYYMMDD(in.StartDate); err != nil {
		return fmt.Errorf("tanggal mulai (start_date) tidak valid: %w", err)
	}
	if in.EndDate != nil && strings.TrimSpace(*in.EndDate) != "" {
		trimmedEnd := strings.TrimSpace(*in.EndDate)
		if err := ValidateDateYYYYMMDD(trimmedEnd); err != nil {
			return fmt.Errorf("tanggal selesai (end_date) tidak valid: %w", err)
		}
		if trimmedEnd < in.StartDate {
			return errors.New("tanggal selesai tidak boleh mendahului tanggal mulai")
		}
		in.EndDate = &trimmedEnd
	} else {
		in.EndDate = nil
	}
	return nil
}

// UpdateRecurringScheduleInput mewakili payload pembaruan jadwal berulang
type UpdateRecurringScheduleInput struct {
	TemplateID    *int64  `json:"template_id,omitempty"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Category      string  `json:"category"`
	Priority      string  `json:"priority"`
	Frequency     string  `json:"frequency"`
	IntervalValue int     `json:"interval_value"`
	DayOfMonth    *int    `json:"day_of_month,omitempty"`
	DayOfWeek     *int    `json:"day_of_week,omitempty"`
	StartDate     string  `json:"start_date"`
	EndDate       *string `json:"end_date,omitempty"`
	IsActive      *bool   `json:"is_active,omitempty"`
}

func (in *UpdateRecurringScheduleInput) Validate() error {
	// Jika hanya mengubah status aktif/nonaktif (toggle is_active)
	if strings.TrimSpace(in.Title) == "" && strings.TrimSpace(in.Frequency) == "" && in.IsActive != nil {
		return nil
	}

	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return errors.New("judul jadwal berulang wajib diisi")
	}
	in.Category = strings.TrimSpace(in.Category)
	if in.Category == "" {
		in.Category = "General"
	}
	in.Priority = strings.TrimSpace(in.Priority)
	if in.Priority == "" {
		in.Priority = PriorityNormal
	}
	if !IsValidPriority(in.Priority) {
		return fmt.Errorf("prioritas tidak valid: %s", in.Priority)
	}

	validFreq := map[string]bool{
		"daily": true, "weekly": true, "monthly": true, "quarterly": true, "yearly": true,
	}
	if !validFreq[in.Frequency] {
		return fmt.Errorf("frekuensi tidak valid: %s (pilihan: daily, weekly, monthly, quarterly, yearly)", in.Frequency)
	}
	if in.IntervalValue <= 0 {
		in.IntervalValue = 1
	}

	if err := ValidateDateYYYYMMDD(in.StartDate); err != nil {
		return fmt.Errorf("tanggal mulai (start_date) tidak valid: %w", err)
	}
	if in.EndDate != nil && strings.TrimSpace(*in.EndDate) != "" {
		trimmedEnd := strings.TrimSpace(*in.EndDate)
		if err := ValidateDateYYYYMMDD(trimmedEnd); err != nil {
			return fmt.Errorf("tanggal selesai (end_date) tidak valid: %w", err)
		}
		if trimmedEnd < in.StartDate {
			return errors.New("tanggal selesai tidak boleh mendahului tanggal mulai")
		}
		in.EndDate = &trimmedEnd
	} else {
		in.EndDate = nil
	}
	return nil
}

// CalculateProgress menghitung persentase penyelesaian (0 - 100%)
// Aturan v1.1.0:
// 1. Jika ada subtask (total > 0): progress = round((completed * 100) / total).
//    Catatan: Status pekerjaan utama (status) TIDAK otomatis berubah menjadi 'completed'.
// 2. Jika tidak ada subtask (total == 0):
//    - completed: 100%
//    - in_progress / waiting: 50%
//    - not_started / cancelled: 0%
func CalculateProgress(totalSubtasks, completedSubtasks int, status string) int {
	if totalSubtasks > 0 {
		if completedSubtasks <= 0 {
			return 0
		}
		if completedSubtasks >= totalSubtasks {
			return 100
		}
		return int((float64(completedSubtasks) / float64(totalSubtasks)) * 100)
	}
	switch status {
	case StatusCompleted:
		return 100
	case StatusInProgress, StatusWaiting:
		return 50
	default:
		return 0
	}
}
