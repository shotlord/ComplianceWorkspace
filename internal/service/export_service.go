package service

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
)

// ExportService menangani pembuatan laporan ekspor CSV yang aman dan kompatibel dengan Excel
type ExportService struct {
	workRepo     *repository.WorkItemRepository
	activityRepo *repository.ActivityLogRepository
}

func NewExportService(workRepo *repository.WorkItemRepository, activityRepo *repository.ActivityLogRepository) *ExportService {
	return &ExportService{
		workRepo:     workRepo,
		activityRepo: activityRepo,
	}
}

// SanitizeFormulaInjection mencegah CSV formula injection di Excel/Calc jika teks diawali =, +, -, @, \t, atau \r
func SanitizeFormulaInjection(s string) string {
	if s == "" {
		return ""
	}
	firstRaw := s[0]
	if firstRaw == '\t' || firstRaw == '\r' {
		return "'" + s
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	first := trimmed[0]
	if first == '=' || first == '+' || first == '-' || first == '@' || first == '\t' || first == '\r' {
		return "'" + s
	}
	return s
}

func getStatusIndonesian(status string) string {
	switch status {
	case domain.StatusNotStarted:
		return "Belum Mulai"
	case domain.StatusInProgress:
		return "Dalam Proses"
	case domain.StatusWaiting:
		return "Menunggu"
	case domain.StatusCompleted:
		return "Selesai"
	case domain.StatusCancelled:
		return "Dibatalkan"
	default:
		return status
	}
}

func getPriorityIndonesian(priority string) string {
	switch priority {
	case domain.PriorityLow:
		return "Rendah"
	case domain.PriorityNormal:
		return "Normal"
	case domain.PriorityHigh:
		return "Tinggi"
	case domain.PriorityCritical:
		return "Kritis"
	default:
		return priority
	}
}

func getEntryTypeIndonesian(entryType string) string {
	switch entryType {
	case domain.EntryTypeCreated:
		return "Pekerjaan Dibuat"
	case domain.EntryTypeActivity:
		return "Catatan Progres"
	case domain.EntryTypeEdited:
		return "Pembaruan Data"
	case domain.EntryTypeStatusChanged:
		return "Perubahan Status"
	case domain.EntryTypeArchived:
		return "Diarsipkan"
	case domain.EntryTypeRestored:
		return "Dipulihkan"
	default:
		return entryType
	}
}

// ExportWorkItemsCSV mengekspor data Work Register ke CSV ber-BOM UTF-8
func (s *ExportService) ExportWorkItemsCSV(ctx context.Context, w io.Writer, filter repository.WorkItemFilter) error {
	// Tulis UTF-8 BOM agar Microsoft Excel di Windows membaca encoding secara benar
	if _, err := w.Write([]byte("\xEF\xBB\xBF")); err != nil {
		return fmt.Errorf("gagal menulis UTF-8 BOM: %w", err)
	}

	csvWriter := csv.NewWriter(w)

	headers := []string{
		"Kode Pekerjaan",
		"Judul",
		"Kategori",
		"Pemberi Tugas",
		"Tanggal Diterima",
		"Tenggat Waktu",
		"Prioritas",
		"Status",
		"Tanggal Selesai (UTC)",
		"Tanggal Dibuat (UTC)",
		"Tanggal Diperbarui (UTC)",
		"Status Arsip",
		"Deskripsi",
	}

	if err := csvWriter.Write(headers); err != nil {
		return fmt.Errorf("gagal menulis header CSV: %w", err)
	}

	items, err := s.workRepo.List(ctx, filter)
	if err != nil {
		return fmt.Errorf("gagal membaca data pekerjaan untuk ekspor: %w", err)
	}

	for _, item := range items {
		dueStr := ""
		if item.DueOn != nil {
			dueStr = *item.DueOn
		}

		completedStr := ""
		if item.CompletedAt != nil {
			completedStr = item.CompletedAt.UTC().Format(time.RFC3339)
		}

		archiveStr := "Aktif"
		if item.ArchivedAt != nil {
			archiveStr = fmt.Sprintf("Diarsipkan (%s)", item.ArchivedAt.UTC().Format("2006-01-02"))
		}

		row := []string{
			SanitizeFormulaInjection(item.WorkCode),
			SanitizeFormulaInjection(item.Title),
			SanitizeFormulaInjection(item.Category),
			SanitizeFormulaInjection(item.RequestedBy),
			SanitizeFormulaInjection(item.ReceivedOn),
			SanitizeFormulaInjection(dueStr),
			SanitizeFormulaInjection(getPriorityIndonesian(item.Priority)),
			SanitizeFormulaInjection(getStatusIndonesian(item.Status)),
			SanitizeFormulaInjection(completedStr),
			SanitizeFormulaInjection(item.CreatedAt.UTC().Format(time.RFC3339)),
			SanitizeFormulaInjection(item.UpdatedAt.UTC().Format(time.RFC3339)),
			SanitizeFormulaInjection(archiveStr),
			SanitizeFormulaInjection(item.Description),
		}

		if err := csvWriter.Write(row); err != nil {
			return fmt.Errorf("gagal menulis baris CSV pekerjaan: %w", err)
		}
	}

	csvWriter.Flush()
	return csvWriter.Error()
}

// ExportActivityLogsCSV mengekspor data Activity Log ke CSV ber-BOM UTF-8
func (s *ExportService) ExportActivityLogsCSV(ctx context.Context, w io.Writer, workItemID int64) error {
	// Tulis UTF-8 BOM
	if _, err := w.Write([]byte("\xEF\xBB\xBF")); err != nil {
		return fmt.Errorf("gagal menulis UTF-8 BOM: %w", err)
	}

	csvWriter := csv.NewWriter(w)

	headers := []string{
		"ID Log",
		"Kode Pekerjaan",
		"Judul Pekerjaan",
		"Tipe Aktivitas",
		"Deskripsi Aktivitas",
		"Nama Kolom",
		"Nilai Lama",
		"Nilai Baru",
		"Waktu Kejadian (UTC)",
		"Waktu Dicatat (UTC)",
	}

	if err := csvWriter.Write(headers); err != nil {
		return fmt.Errorf("gagal menulis header CSV log: %w", err)
	}

	logs, err := s.activityRepo.ListAllWithWorkInfo(ctx, workItemID)
	if err != nil {
		return fmt.Errorf("gagal membaca data log untuk ekspor: %w", err)
	}

	for _, l := range logs {
		fieldName := ""
		if l.FieldName != nil {
			fieldName = *l.FieldName
		}
		oldVal := ""
		if l.OldValue != nil {
			oldVal = *l.OldValue
		}
		newVal := ""
		if l.NewValue != nil {
			newVal = *l.NewValue
		}

		row := []string{
			fmt.Sprintf("%d", l.ID),
			SanitizeFormulaInjection(l.WorkCode),
			SanitizeFormulaInjection(l.WorkTitle),
			SanitizeFormulaInjection(getEntryTypeIndonesian(l.EntryType)),
			SanitizeFormulaInjection(l.Description),
			SanitizeFormulaInjection(fieldName),
			SanitizeFormulaInjection(oldVal),
			SanitizeFormulaInjection(newVal),
			SanitizeFormulaInjection(l.OccurredAt.UTC().Format(time.RFC3339)),
			SanitizeFormulaInjection(l.RecordedAt.UTC().Format(time.RFC3339)),
		}

		if err := csvWriter.Write(row); err != nil {
			return fmt.Errorf("gagal menulis baris CSV log: %w", err)
		}
	}

	csvWriter.Flush()
	return csvWriter.Error()
}
