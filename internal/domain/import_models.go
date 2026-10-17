package domain

import (
	"strings"
	"time"
)

// Status batch impor
const (
	ImportStatusPreview    = "preview"
	ImportStatusCompleted  = "completed"
	ImportStatusFailed     = "failed"
	ImportStatusRolledBack = "rolled_back"
)

// Status baris item impor
const (
	ItemStatusCreated = "created"
	ItemStatusUpdated = "updated"
	ItemStatusSkipped = "skipped"
	ItemStatusFailed  = "failed"
)

// Batasan teknis impor data (Constraint v1.1.0)
const (
	MaxImportFileSize       = 10 * 1024 * 1024 // 10 MB
	MaxImportRows           = 5000             // 5.000 baris
	MaxXLSXWorksheets       = 20               // Maksimal 20 worksheet
	MaxXLSXCells            = 100000           // Maksimal 100.000 sel per file
	MaxXLSXUncompressedSize = 50 * 1024 * 1024 // 50 MB batas total ukuran terdekompresi
	MaxXLSXCompressionRatio = 50               // Rasio kompresi maksimal 50:1
)

// ImportBatch merepresentasikan catatan batch impor file
type ImportBatch struct {
	ID                int64      `json:"id"`
	BatchCode         string     `json:"batch_code"`
	FileName          string     `json:"file_name"`
	FileType          string     `json:"file_type"` // csv atau xlsx
	FileSize          int64      `json:"file_size"`
	TotalRows         int        `json:"total_rows"`
	ImportedRows      int        `json:"imported_rows"`
	SkippedRows       int        `json:"skipped_rows"`
	FailedRows        int        `json:"failed_rows"`
	CreatedCount      int        `json:"created_count"`
	SkippedCount      int        `json:"skipped_count"`
	FailedCount       int        `json:"failed_count"`
	Status            string     `json:"status"` // preview, completed, failed, rolled_back
	ErrorSummary      string     `json:"error_summary,omitempty"`
	ColumnMappingJSON string     `json:"column_mapping_json"`
	CreatedAt         time.Time  `json:"created_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

// ImportItem merepresentasikan rincian per baris hasil impor
type ImportItem struct {
	ID           int64     `json:"id"`
	BatchID      int64     `json:"batch_id"`
	RowNumber    int       `json:"row_number"`
	WorkItemID   *int64    `json:"work_item_id,omitempty"`
	Status       string    `json:"status"`      // created, updated, skipped, failed
	ActionType   string    `json:"action_type"` // insert, skip
	ErrorMessage string    `json:"error_message,omitempty"`
	RawDataJSON  string    `json:"raw_data_json,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// ImportPreviewRow merepresentasikan hasil parsing dan validasi satu baris sebelum commit
type ImportPreviewRow struct {
	RowNumber         int               `json:"row_number"`
	RawData           map[string]string `json:"raw_data"`
	Title             string            `json:"title"`
	Description       string            `json:"description"`
	Category          string            `json:"category"`
	RequestedBy       string            `json:"requested_by"`
	ReceivedOn        string            `json:"received_on"`
	DueOn             *string           `json:"due_on,omitempty"`
	Priority          string            `json:"priority"`
	Status            string            `json:"status"`
	IsValid           bool              `json:"is_valid"`
	ValidationErrors  []string          `json:"validation_errors,omitempty"`
	Errors            []string          `json:"errors,omitempty"` // alias kompatibilitas
	IsDuplicate       bool              `json:"is_duplicate"`
	DuplicateReason   string            `json:"duplicate_reason,omitempty"`
	MatchedWorkItemID *int64            `json:"matched_work_item_id,omitempty"`
	MatchedWorkCode   string            `json:"matched_work_code,omitempty"`
}

// ImportPreviewResult merepresentasikan hasil dry-run preview impor
type ImportPreviewResult struct {
	FileName          string             `json:"file_name"`
	FileType          string             `json:"file_type"`
	FileSize          int64              `json:"file_size"`
	TotalRows         int                `json:"total_rows"`
	ValidRows         int                `json:"valid_rows"`
	InvalidRows       int                `json:"invalid_rows"`
	ErrorRows         int                `json:"error_rows"` // alias kompatibilitas
	DuplicateRows     int                `json:"duplicate_rows"`
	DetectedHeaders   []string           `json:"detected_headers"`
	SuggestedMapping  map[string]string  `json:"suggested_mapping"`
	Worksheets        []string           `json:"worksheets,omitempty"`
	SelectedWorksheet string             `json:"selected_worksheet,omitempty"`
	Rows              []ImportPreviewRow `json:"rows"`        // seluruh baris preview
	SampleRows        []ImportPreviewRow `json:"sample_rows"` // alias kompatibilitas
}

// CommitImportRequest mewakili parameter saat pengguna mengeksekusi commit
type CommitImportRequest struct {
	FileName      string            `json:"file_name"`
	FileType      string            `json:"file_type"`
	Worksheet     string            `json:"worksheet,omitempty"`
	ColumnMapping map[string]string `json:"column_mapping"`
	DuplicateMode string            `json:"duplicate_mode"` // "skip" atau "create" (default: "skip")
}

// CommitImportResult mewakili ringkasan hasil commit impor
type CommitImportResult struct {
	BatchID      int64  `json:"batch_id"`
	BatchCode    string `json:"batch_code"`
	TotalRows    int    `json:"total_rows"`
	ImportedRows int    `json:"imported_rows"`
	SkippedRows  int    `json:"skipped_rows"`
	FailedRows   int    `json:"failed_rows"`
	CreatedCount int    `json:"created_count"`
	SkippedCount int    `json:"skipped_count"`
	FailedCount  int    `json:"failed_count"`
	Status       string `json:"status"`
	Message      string `json:"message"`
}

// SanitizeFormulaInjection mencegah CSV/Spreadsheet formula injection
func SanitizeFormulaInjection(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) > 0 {
		first := trimmed[0]
		if first == '=' || first == '+' || first == '-' || first == '@' || first == '\t' || first == '\r' {
			return "'" + trimmed
		}
	}
	return value
}
