package tests

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"strings"
	"testing"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
	"compliance-workspace/internal/service"
)

func TestCSVExportSanitizationAndBOM(t *testing.T) {
	// 1. Uji Sanitasi Formula Injection Unit Test
	riskyInputs := []struct {
		input    string
		expected string
	}{
		{"=SUM(A1:A10)", "'=SUM(A1:A10)"},
		{"+1234567890", "'+1234567890"},
		{"-500000", "'-500000"},
		{"@HYPERLINK(\"http://evil.com\")", "'@HYPERLINK(\"http://evil.com\")"},
		{"\tTabIndent", "'\tTabIndent"},
		{"\rCarriageReturn", "'\rCarriageReturn"},
		{"Kepatuhan Normal", "Kepatuhan Normal"},
		{"", ""},
		{"   ", ""},
	}

	for _, tc := range riskyInputs {
		actual := service.SanitizeFormulaInjection(tc.input)
		if actual != tc.expected {
			t.Errorf("Sanitasi formula injection gagal untuk '%s': diharapkan '%s', didapat '%s'", tc.input, tc.expected, actual)
		}
	}

	// 2. Uji Integrasi Ekspor CSV Work Register dengan UTF-8 BOM
	svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Masukkan pekerjaan dengan formula berisiko
	item1, err := svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "=cmd|'/C calc'!A0",
		Description: "@SUM(1,2)",
		Category:    "+FormulaCategory",
		RequestedBy: "-MinusRequester",
		ReceivedOn:  "2026-10-17",
		Priority:    domain.PriorityHigh,
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan uji: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(svc.GetDB())
	activityRepo := repository.NewActivityLogRepository(svc.GetDB())
	exportSvc := service.NewExportService(workRepo, activityRepo)

	var buf bytes.Buffer
	if err := exportSvc.ExportWorkItemsCSV(ctx, &buf, repository.WorkItemFilter{IncludeArchived: true}); err != nil {
		t.Fatalf("Gagal ekspor CSV pekerjaan: %v", err)
	}

	csvBytes := buf.Bytes()

	// Verifikasi 3 byte pertama adalah UTF-8 BOM (\xEF\xBB\xBF)
	if len(csvBytes) < 3 || csvBytes[0] != 0xEF || csvBytes[1] != 0xBB || csvBytes[2] != 0xBF {
		t.Fatalf("File CSV tidak diawali dengan UTF-8 BOM (0xEF, 0xBB, 0xBF)")
	}

	// Baca isi CSV setelah BOM
	reader := csv.NewReader(bytes.NewReader(csvBytes[3:]))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("Gagal membaca hasil CSV: %v", err)
	}

	if len(records) < 2 {
		t.Fatalf("CSV harus memiliki minimal baris header dan 1 baris data, didapat %d", len(records))
	}

	header := records[0]
	if header[0] != "Kode Pekerjaan" || header[1] != "Judul" {
		t.Errorf("Header CSV tidak sesuai: %v", header)
	}

	row := records[1]
	// Pastikan data tersanitasi
	if row[0] != item1.WorkCode {
		t.Errorf("Kode pekerjaan salah: %s", row[0])
	}
	if row[1] != "'=cmd|'/C calc'!A0" {
		t.Errorf("Judul pekerjaan tidak disanitasi: %s", row[1])
	}
	if row[2] != "'+FormulaCategory" {
		t.Errorf("Kategori tidak disanitasi: %s", row[2])
	}
	if row[3] != "'-MinusRequester" {
		t.Errorf("Pemberi tugas tidak disanitasi: %s", row[3])
	}
	if row[12] != "'@SUM(1,2)" {
		t.Errorf("Deskripsi tidak disanitasi: %s", row[12])
	}
}

func TestCSVExportEndpoints(t *testing.T) {
	_, baseURL, cleanup := setupTestServer(t)
	defer cleanup()

	client := &http.Client{}

	// 1. Uji GET /api/export/work-items
	respWork, err := client.Get(baseURL + "/api/export/work-items")
	if err != nil {
		t.Fatalf("Request /api/export/work-items gagal: %v", err)
	}
	defer respWork.Body.Close()

	if respWork.StatusCode != http.StatusOK {
		t.Errorf("Ekspektasi 200 OK untuk ekspor pekerjaan, didapat: %d", respWork.StatusCode)
	}

	ctWork := respWork.Header.Get("Content-Type")
	if !strings.Contains(ctWork, "text/csv") {
		t.Errorf("Content-Type harus text/csv, didapat: %s", ctWork)
	}

	cdWork := respWork.Header.Get("Content-Disposition")
	if !strings.Contains(cdWork, "attachment; filename=\"compliance_work_register_") {
		t.Errorf("Content-Disposition tidak sesuai: %s", cdWork)
	}

	// 2. Uji GET /api/export/activity-logs
	respAct, err := client.Get(baseURL + "/api/export/activity-logs")
	if err != nil {
		t.Fatalf("Request /api/export/activity-logs gagal: %v", err)
	}
	defer respAct.Body.Close()

	if respAct.StatusCode != http.StatusOK {
		t.Errorf("Ekspektasi 200 OK untuk ekspor log aktivitas, didapat: %d", respAct.StatusCode)
	}

	ctAct := respAct.Header.Get("Content-Type")
	if !strings.Contains(ctAct, "text/csv") {
		t.Errorf("Content-Type log harus text/csv, didapat: %s", ctAct)
	}

	cdAct := respAct.Header.Get("Content-Disposition")
	if !strings.Contains(cdAct, "attachment; filename=\"compliance_activity_logs_") {
		t.Errorf("Content-Disposition log tidak sesuai: %s", cdAct)
	}

	// Verifikasi BOM pada respons HTTP
	bodyBytes, _ := io.ReadAll(respWork.Body)
	if len(bodyBytes) >= 3 {
		if bodyBytes[0] != 0xEF || bodyBytes[1] != 0xBB || bodyBytes[2] != 0xBF {
			t.Errorf("Respons HTTP ekspor pekerjaan tidak memiliki UTF-8 BOM")
		}
	}
}
