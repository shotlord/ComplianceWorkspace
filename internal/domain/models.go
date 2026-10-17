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

// WorkItem mewakili entitas pekerjaan dalam sistem
type WorkItem struct {
	ID          int64      `json:"id"`
	WorkCode    string     `json:"work_code"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	RequestedBy string     `json:"requested_by"`
	ReceivedOn  string     `json:"received_on"` // YYYY-MM-DD
	DueOn       *string    `json:"due_on"`      // YYYY-MM-DD opsional
	Priority    string     `json:"priority"`
	Status      string     `json:"status"`
	CompletedAt *time.Time `json:"completed_at"` // UTC RFC3339 opsional
	CreatedAt   time.Time  `json:"created_at"`   // UTC RFC3339
	UpdatedAt   time.Time  `json:"updated_at"`   // UTC RFC3339
	ArchivedAt  *time.Time `json:"archived_at"`  // UTC RFC3339 opsional
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
