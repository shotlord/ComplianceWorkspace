package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"compliance-workspace/internal/database"
	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
	"compliance-workspace/internal/server"
	"compliance-workspace/internal/service"

	"github.com/xuri/excelize/v2"
)

func setupImportTestServer(t *testing.T) (*server.Server, string, *service.ImportService, *service.WorkItemService, func()) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_import.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB test: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := database.Migrate(ctx, db); err != nil {
		db.Close()
		t.Fatalf("Gagal migrasi DB test: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(db)
	actRepo := repository.NewActivityLogRepository(db)
	subRepo := repository.NewSubtaskRepository(db)
	catRepo := repository.NewCategoryRepository(db)
	tplRepo := repository.NewWorkTemplateRepository(db)
	recRepo := repository.NewRecurringRepository(db)
	importRepo := repository.NewImportRepository(db)

	workSvc := service.NewWorkItemService(db, workRepo, actRepo)
	exportSvc := service.NewExportService(workRepo, actRepo)
	backupSvc := service.NewBackupService(workSvc, nil, nil, dbPath, filepath.Join(tempDir, "backups"))
	taskSvc := service.NewTaskManagementService(db, workRepo, actRepo, subRepo, catRepo, tplRepo, recRepo)
	importSvc := service.NewImportService(db, importRepo, workRepo, actRepo, catRepo, backupSvc)

	srv, err := server.NewServer("127.0.0.1:0", workSvc, backupSvc, exportSvc, taskSvc, importSvc)
	if err != nil {
		db.Close()
		t.Fatalf("Gagal membuat server test: %v", err)
	}

	go func() {
		_ = srv.Start()
	}()

	addr := srv.Addr()
	for i := 0; i < 50; i++ {
		if addr != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
		addr = srv.Addr()
	}

	if addr == "" {
		t.Fatal("Server tidak kunjung bind port")
	}

	baseURL := fmt.Sprintf("http://%s", addr)

	cleanup := func() {
		ctxShut, cancelShut := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelShut()
		_ = srv.Shutdown(ctxShut)
		_ = db.Close()
	}

	return srv, baseURL, importSvc, workSvc, cleanup
}

func getImportTestClientWithCSRF(t *testing.T, baseURL string) (*http.Client, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("Gagal membuat cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/csrf-token", nil)
	if err != nil {
		t.Fatalf("Gagal membuat request CSRF: %v", err)
	}
	req.Header.Set("Origin", baseURL)

	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("Gagal memanggil endpoint CSRF: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("Status token CSRF bukan 200: %d", res.StatusCode)
	}

	var csrfResp struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&csrfResp); err != nil {
		t.Fatalf("Gagal mendecode respons CSRF: %v", err)
	}

	return client, csrfResp.CSRFToken
}

func createTestXLSXBytes(t *testing.T, sheetName string, rows [][]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()

	defaultSheet := "Sheet1"
	if sheetName != defaultSheet {
		f.NewSheet(sheetName)
		f.DeleteSheet(defaultSheet)
	}

	for rIdx, row := range rows {
		for cIdx, val := range row {
			cell, err := excelize.CoordinatesToCellName(cIdx+1, rIdx+1)
			if err != nil {
				t.Fatalf("Gagal hitung cell name: %v", err)
			}
			if err := f.SetCellValue(sheetName, cell, val); err != nil {
				t.Fatalf("Gagal set cell value: %v", err)
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("Gagal write xlsx to buffer: %v", err)
	}
	return buf.Bytes()
}

func sendMultipartRequest(t *testing.T, client *http.Client, url, method, csrfToken string, fields map[string]string, fileField, fileName string, fileBytes []byte) (*http.Response, error) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, err
		}
	}

	if fileField != "" && len(fileBytes) > 0 {
		part, err := writer.CreateFormFile(fileField, fileName)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(fileBytes); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrfToken)
	parts := strings.Split(url, "/api/")
	baseURL := parts[0]
	req.Header.Set("Origin", baseURL)

	return client.Do(req)
}

// 1. Test CSV Valid Tanpa BOM & Dengan UTF-8 BOM
func TestImportCSVWithAndWithoutBOM(t *testing.T) {
	_, baseURL, _, workSvc, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	csvContentWithoutBOM := "Judul Pekerjaan,Tanggal Diterima,Kategori,Pemohon,Batas Waktu,Prioritas,Status\n" +
		"Audit Anti-Pencucian Uang,2026-10-18,APU-PPT,OJK,2026-10-25,high,not_started\n" +
		"Review Formulir KYC Nasabah,2026-10-19,General,Divisi Operasional,2026-10-30,normal,in_progress\n"

	// 1a. Preview CSV tanpa BOM
	fields := map[string]string{
		"duplicate_mode": "skip",
	}
	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, fields, "file", "test_no_bom.csv", []byte(csvContentWithoutBOM))
	if err != nil {
		t.Fatalf("Gagal kirim preview CSV no BOM: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("Preview status bukan 200: %d, body: %s", res.StatusCode, string(body))
	}

	var previewRes domain.ImportPreviewResult
	if err := json.NewDecoder(res.Body).Decode(&previewRes); err != nil {
		t.Fatalf("Gagal decode preview response: %v", err)
	}

	if previewRes.TotalRows != 2 || previewRes.ValidRows != 2 {
		t.Errorf("Preview CSV no BOM tidak sesuai: total=%d, valid=%d", previewRes.TotalRows, previewRes.ValidRows)
	}

	// 1b. Commit CSV no BOM
	mappingJSON, _ := json.Marshal(previewRes.SuggestedMapping)
	commitFields := map[string]string{
		"mapping":        string(mappingJSON),
		"duplicate_mode": "skip",
	}
	resCommit, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken, commitFields, "file", "test_no_bom.csv", []byte(csvContentWithoutBOM))
	if err != nil {
		t.Fatalf("Gagal kirim commit CSV no BOM: %v", err)
	}
	defer resCommit.Body.Close()

	if resCommit.StatusCode != http.StatusCreated && resCommit.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resCommit.Body)
		t.Fatalf("Commit status bukan 200/201: %d, body: %s", resCommit.StatusCode, string(body))
	}

	var commitRes domain.CommitImportResult
	if err := json.NewDecoder(resCommit.Body).Decode(&commitRes); err != nil {
		t.Fatalf("Gagal decode commit response: %v", err)
	}

	if commitRes.ImportedRows != 2 {
		t.Errorf("Ekspektasi imported_rows=2, dapat: %d", commitRes.ImportedRows)
	}

	// 1c. CSV dengan UTF-8 BOM
	bomBytes := []byte{0xEF, 0xBB, 0xBF}
	csvContentWithBOM := append(bomBytes, []byte("Judul Pekerjaan,Tanggal Diterima,Kategori,Pemohon\nPengujian BOM UTF-8,2026-10-20,Audit,Internal\n")...)

	resBOM, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, fields, "file", "test_bom.csv", csvContentWithBOM)
	if err != nil {
		t.Fatalf("Gagal kirim preview CSV BOM: %v", err)
	}
	defer resBOM.Body.Close()

	if resBOM.StatusCode != http.StatusOK {
		t.Fatalf("Preview CSV BOM gagal: %d", resBOM.StatusCode)
	}

	var previewBOM domain.ImportPreviewResult
	if err := json.NewDecoder(resBOM.Body).Decode(&previewBOM); err != nil {
		t.Fatalf("Gagal decode BOM preview: %v", err)
	}

	if previewBOM.TotalRows != 1 || previewBOM.ValidRows != 1 {
		t.Errorf("BOM header parsing gagal: TotalRows=%d, ValidRows=%d, Headers=%v", previewBOM.TotalRows, previewBOM.ValidRows, previewBOM.DetectedHeaders)
	}

	// Verifikasi data masuk ke Work Register
	items, err := workSvc.ListWorkItems(context.Background(), repository.WorkItemFilter{})
	if err != nil {
		t.Fatalf("Gagal list work items: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("Jumlah pekerjaan di database bukan 2: %d", len(items))
	}
}

// 2. Test XLSX Valid dengan Custom Column Mapping
func TestImportXLSXWithCustomColumnMapping(t *testing.T) {
	_, baseURL, _, _, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	// Buat XLSX dengan header non-standar
	rows := [][]string{
		{"Topic Name", "Arrival Date", "Req Div", "Urgency", "Notes"},
		{"Pemeriksaan Transaksi Mencurigakan", "2026-10-21", "Divisi Kepatuhan", "critical", "Segera ditindaklanjuti"},
		{"Pelaporan Tahunan OJK", "2026-10-22", "Direksi", "high", "Dokumen telah siap"},
	}
	xlsxBytes := createTestXLSXBytes(t, "Sheet1", rows)

	// Pemetaan manual
	customMapping := map[string]string{
		"title":        "Topic Name",
		"received_on":  "Arrival Date",
		"requested_by": "Req Div",
		"priority":     "Urgency",
		"description":  "Notes",
	}
	mappingJSON, _ := json.Marshal(customMapping)

	commitFields := map[string]string{
		"mapping":        string(mappingJSON),
		"duplicate_mode": "skip",
	}

	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken, commitFields, "file", "custom_headers.xlsx", xlsxBytes)
	if err != nil {
		t.Fatalf("Gagal commit XLSX: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("Commit XLSX status bukan 200/201: %d, body: %s", res.StatusCode, string(body))
	}

	var commitRes domain.CommitImportResult
	if err := json.NewDecoder(res.Body).Decode(&commitRes); err != nil {
		t.Fatalf("Gagal decode commit response: %v", err)
	}

	if commitRes.ImportedRows != 2 {
		t.Errorf("ImportedRows XLSX bukan 2: %d", commitRes.ImportedRows)
	}
}

// 3. Test Validasi Baris (Field Wajib Kosong, Tanggal Salah, Priority Salah, Status Salah)
func TestImportValidationRejections(t *testing.T) {
	_, baseURL, _, _, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	csvContent := "Judul Pekerjaan,Tanggal Diterima,Prioritas,Status\n" +
		",2026-10-18,normal,not_started\n" + // Baris 2: Judul kosong
		"Judul Ada,18-10-2026,normal,not_started\n" + // Baris 3: Tanggal format salah (DD-MM-YYYY)
		"Judul Prioritas Salah,2026-10-18,super_urgent,not_started\n" + // Baris 4: Prioritas invalid
		"Judul Status Salah,2026-10-18,normal,on_hold\n" + // Baris 5: Status invalid
		"Baris Valid,2026-10-18,high,in_progress\n" // Baris 6: Valid

	fields := map[string]string{
		"duplicate_mode": "skip",
	}
	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, fields, "file", "invalid_rows.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("Gagal request preview: %v", err)
	}
	defer res.Body.Close()

	var preview domain.ImportPreviewResult
	if err := json.NewDecoder(res.Body).Decode(&preview); err != nil {
		t.Fatalf("Gagal decode preview: %v", err)
	}

	if preview.TotalRows != 5 {
		t.Errorf("Ekspektasi total 5 baris, dapat: %d", preview.TotalRows)
	}
	if preview.InvalidRows != 4 {
		t.Errorf("Ekspektasi 4 baris error, dapat: %d", preview.InvalidRows)
	}
	if preview.ValidRows != 1 {
		t.Errorf("Ekspektasi 1 baris valid, dapat: %d", preview.ValidRows)
	}

	// Periksa pesan validasi masing-masing
	if preview.SampleRows[0].IsValid || len(preview.SampleRows[0].Errors) == 0 {
		t.Errorf("Baris 1 harusnya error judul wajib diisi")
	}
	if preview.SampleRows[1].IsValid || len(preview.SampleRows[1].Errors) == 0 {
		t.Errorf("Baris 2 harusnya error format tanggal")
	}
	if preview.SampleRows[2].IsValid || len(preview.SampleRows[2].Errors) == 0 {
		t.Errorf("Baris 3 harusnya error prioritas")
	}
	if preview.SampleRows[3].IsValid || len(preview.SampleRows[3].Errors) == 0 {
		t.Errorf("Baris 4 harusnya error status")
	}
	if !preview.SampleRows[4].IsValid {
		t.Errorf("Baris 5 harusnya valid")
	}
}

// 4. Test Duplicate Detection: In-File & Database (Mode Skip vs Mode Create)
func TestImportDuplicateDetectionSkipAndCreate(t *testing.T) {
	_, baseURL, _, workSvc, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	// 4a. Buat 1 data awal di database
	ctx := context.Background()
	_, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pemeriksaan Kepatuhan Cabang Surabaya",
		RequestedBy: "Divisi Audit",
		ReceivedOn:  "2026-10-18",
		Priority:    "high",
	})
	if err != nil {
		t.Fatalf("Gagal buat initial work item: %v", err)
	}

	// 4b. Buat file CSV yang memiliki:
	// Baris 1: Duplikat dengan database (Title, RequestedBy, ReceivedOn sama)
	// Baris 2: Pekerjaan Baru A
	// Baris 3: Pekerjaan Baru A (In-file duplikat dari Baris 2)
	csvDuplicateContent := "Judul Pekerjaan,Tanggal Diterima,Pemberi Tugas,Prioritas\n" +
		"Pemeriksaan Kepatuhan Cabang Surabaya,2026-10-18,Divisi Audit,high\n" + // DB duplicate
		"Sosialisasi Aturan Perlindungan Data,2026-10-19,Legal,normal\n" + // Unique
		"Sosialisasi Aturan Perlindungan Data,2026-10-19,Legal,normal\n" // In-file duplicate

	// Preview dengan mode skip
	fieldsSkip := map[string]string{
		"duplicate_mode": "skip",
	}
	resPrev, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, fieldsSkip, "file", "dups.csv", []byte(csvDuplicateContent))
	if err != nil {
		t.Fatalf("Gagal preview dups: %v", err)
	}
	defer resPrev.Body.Close()

	var prevRes domain.ImportPreviewResult
	if err := json.NewDecoder(resPrev.Body).Decode(&prevRes); err != nil {
		t.Fatalf("Gagal decode preview dups: %v", err)
	}

	if prevRes.TotalRows != 3 || prevRes.DuplicateRows != 2 || prevRes.ValidRows != 1 {
		t.Errorf("Preview dups salah: total=%d, duplicate=%d, valid=%d", prevRes.TotalRows, prevRes.DuplicateRows, prevRes.ValidRows)
	}

	// Commit dengan mode SKIP
	mappingJSON, _ := json.Marshal(prevRes.SuggestedMapping)
	commitSkipFields := map[string]string{
		"mapping":        string(mappingJSON),
		"duplicate_mode": "skip",
	}
	resCommitSkip, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken, commitSkipFields, "file", "dups.csv", []byte(csvDuplicateContent))
	if err != nil {
		t.Fatalf("Gagal commit skip: %v", err)
	}
	defer resCommitSkip.Body.Close()

	var commitSkipRes domain.CommitImportResult
	if err := json.NewDecoder(resCommitSkip.Body).Decode(&commitSkipRes); err != nil {
		t.Fatalf("Gagal decode commit skip: %v", err)
	}

	if commitSkipRes.ImportedRows != 1 || commitSkipRes.SkippedRows != 2 {
		t.Errorf("Hasil commit skip tidak sesuai: created=%d (harus 1), skipped=%d (harus 2)", commitSkipRes.ImportedRows, commitSkipRes.SkippedRows)
	}

	// 4c. Sekarang uji mode CREATE dengan file yang sama
	commitCreateFields := map[string]string{
		"mapping":        string(mappingJSON),
		"duplicate_mode": "create",
	}
	resCommitCreate, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken, commitCreateFields, "file", "dups_create.csv", []byte(csvDuplicateContent))
	if err != nil {
		t.Fatalf("Gagal commit create: %v", err)
	}
	defer resCommitCreate.Body.Close()

	var commitCreateRes domain.CommitImportResult
	if err := json.NewDecoder(resCommitCreate.Body).Decode(&commitCreateRes); err != nil {
		t.Fatalf("Gagal decode commit create: %v", err)
	}

	// Dalam mode create, baris duplikat tetap dibuat (selama valid)
	if commitCreateRes.ImportedRows != 3 || commitCreateRes.SkippedRows != 0 {
		t.Errorf("Hasil commit create tidak sesuai: created=%d (harus 3), skipped=%d (harus 0)", commitCreateRes.ImportedRows, commitCreateRes.SkippedRows)
	}
}

func TestImportWorkRegisterExportDefaultsTitleToJudulAndSkipsExistingDuplicates(t *testing.T) {
	_, baseURL, _, workSvc, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	ctx := context.Background()
	existing := []domain.CreateWorkItemInput{
		{Title: "UAT_TEMPLATE - 0001", RequestedBy: "Project Owner", ReceivedOn: "2026-10-20", Priority: domain.PriorityNormal},
		{Title: "UAT_0001", RequestedBy: "Project Owner", ReceivedOn: "2026-10-20", Priority: domain.PriorityNormal},
	}
	for _, input := range existing {
		if _, err := workSvc.CreateWorkItem(ctx, input); err != nil {
			t.Fatalf("gagal menyiapkan pekerjaan existing %q: %v", input.Title, err)
		}
	}

	csvContent := "Kode Pekerjaan,Judul,Deskripsi,Kategori,Pemberi Tugas,Tanggal Diterima,Batas Waktu,Prioritas,Status,Progres\n" +
		"CMP-2026-000003,UAT_TEMPLATE - 0001,,General,Project Owner,2026-10-20,,normal,not_started,0\n" +
		"CMP-2026-000001,UAT_0001,,General,Project Owner,2026-10-20,,normal,not_started,0\n"

	previewResponse, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{"duplicate_mode": "skip"}, "file", "work-register.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("preview gagal: %v", err)
	}
	defer previewResponse.Body.Close()

	var preview domain.ImportPreviewResult
	if err := json.NewDecoder(previewResponse.Body).Decode(&preview); err != nil {
		t.Fatalf("decode preview gagal: %v", err)
	}
	if got := preview.SuggestedMapping["title"]; got != "Judul" {
		t.Fatalf("default title mapping salah: mendapat %q, ingin %q", got, "Judul")
	}
	if preview.ValidRows != 0 || preview.DuplicateRows != 2 || preview.InvalidRows != 0 {
		t.Fatalf("hasil preview salah: valid=%d duplicate=%d invalid=%d", preview.ValidRows, preview.DuplicateRows, preview.InvalidRows)
	}
	for i, row := range preview.Rows {
		if !row.IsDuplicate {
			t.Fatalf("baris %d harus terdeteksi sebagai duplikasi", i+1)
		}
		if row.Title != existing[i].Title {
			t.Fatalf("judul preview baris %d salah: mendapat %q, ingin %q", i+1, row.Title, existing[i].Title)
		}
	}

	mappingJSON, err := json.Marshal(preview.SuggestedMapping)
	if err != nil {
		t.Fatal(err)
	}
	commitResponse, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken, map[string]string{
		"mapping":        string(mappingJSON),
		"duplicate_mode": "skip",
	}, "file", "work-register.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("commit skip gagal: %v", err)
	}
	defer commitResponse.Body.Close()

	var commit domain.CommitImportResult
	if err := json.NewDecoder(commitResponse.Body).Decode(&commit); err != nil {
		t.Fatalf("decode commit gagal: %v", err)
	}
	if commit.ImportedRows != 0 || commit.SkippedRows != 2 {
		t.Fatalf("commit duplikasi harus tidak membuat data: imported=%d skipped=%d", commit.ImportedRows, commit.SkippedRows)
	}
	items, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != len(existing) {
		t.Fatalf("jumlah pekerjaan berubah setelah commit skip: mendapat %d, ingin %d", len(items), len(existing))
	}
}

func TestSuggestColumnMappingRequiresUnambiguousTitle(t *testing.T) {
	withoutTitle := service.SuggestColumnMapping([]string{"Kode Pekerjaan", "Tanggal Diterima"})
	if got := withoutTitle["title"]; got != "" {
		t.Fatalf("header identifier tidak boleh menjadi fallback title: %q", got)
	}

	ambiguousTitle := service.SuggestColumnMapping([]string{"Judul", "Title", "Tanggal Diterima"})
	if got := ambiguousTitle["title"]; got != "" {
		t.Fatalf("title ambigu harus meminta pemetaan manual, bukan memilih %q", got)
	}

	allHeaders := []string{"Judul", "Deskripsi", "Kategori", "Pemberi Tugas", "Tanggal Diterima", "Batas Waktu", "Prioritas", "Status"}
	mapping := service.SuggestColumnMapping(allHeaders)
	expected := map[string]string{
		"title":        "Judul",
		"description":  "Deskripsi",
		"category":     "Kategori",
		"requested_by": "Pemberi Tugas",
		"received_on":  "Tanggal Diterima",
		"due_on":       "Batas Waktu",
		"priority":     "Prioritas",
		"status":       "Status",
	}
	for field, want := range expected {
		if got := mapping[field]; got != want {
			t.Errorf("mapping %s berubah: mendapat %q, ingin %q", field, got, want)
		}
	}
}

// 5. Test Preview Adalah Read-Only (Database Record Count Tidak Berubah)
func TestImportPreviewIsReadOnly(t *testing.T) {
	_, baseURL, _, workSvc, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	ctx := context.Background()
	initialItems, _ := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	initialCount := len(initialItems)

	csvContent := "Judul Pekerjaan,Tanggal Diterima\n" +
		"Pekerjaan Pratinjau 1,2026-10-18\n" +
		"Pekerjaan Pratinjau 2,2026-10-19\n"

	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{"duplicate_mode": "skip"}, "file", "preview_only.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("Gagal preview: %v", err)
	}
	res.Body.Close()

	afterItems, _ := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	if len(afterItems) != initialCount {
		t.Fatalf("PELANGGARAN: Preview mengubah isi database! Initial=%d, After=%d", initialCount, len(afterItems))
	}
}

// 6. Test Atomic Batch Commit & Error Reporting & Compensatory Rollback
func TestImportAtomicBatchAndCompensatoryRollback(t *testing.T) {
	_, baseURL, _, workSvc, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	// File dengan 2 baris valid dan 1 baris error
	csvContent := "Judul Pekerjaan,Tanggal Diterima,Pemberi Tugas,Prioritas\n" +
		"Kompensasi Task 1,2026-10-18,Audit,normal\n" +
		"Kompensasi Task 2,2026-10-19,Legal,high\n" +
		",2026-10-20,Risk,normal\n" // Error baris 4

	mapping := map[string]string{
		"title":        "Judul Pekerjaan",
		"received_on":  "Tanggal Diterima",
		"requested_by": "Pemberi Tugas",
		"priority":     "Prioritas",
	}
	mappingJSON, _ := json.Marshal(mapping)

	commitFields := map[string]string{
		"mapping":        string(mappingJSON),
		"duplicate_mode": "skip",
	}

	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken, commitFields, "file", "batch_rollback.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("Gagal commit: %v", err)
	}
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("Commit status bukan 200/201: %d, body: %s", res.StatusCode, string(body))
	}

	var commitRes domain.CommitImportResult
	if err := json.NewDecoder(res.Body).Decode(&commitRes); err != nil {
		t.Fatalf("Gagal decode commit response: %v", err)
	}

	if commitRes.ImportedRows != 2 || commitRes.FailedRows != 1 {
		t.Fatalf("Hasil commit tidak sesuai: created=%d, failed=%d", commitRes.ImportedRows, commitRes.FailedRows)
	}

	batchID := commitRes.BatchID

	// 6a. Verifikasi Laporan Error (Error Report CSV)
	reqErr, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/import/batches/%d/error-report", baseURL, batchID), nil)
	resErr, err := client.Do(reqErr)
	if err != nil {
		t.Fatalf("Gagal ambil error report: %v", err)
	}
	defer resErr.Body.Close()

	if resErr.StatusCode != http.StatusOK {
		t.Fatalf("Status error report bukan 200: %d", resErr.StatusCode)
	}

	csvReader := csv.NewReader(resErr.Body)
	records, err := csvReader.ReadAll()
	if err != nil {
		t.Fatalf("Gagal baca CSV error report: %v", err)
	}

	// Minimal header + 1 baris error
	if len(records) < 2 {
		t.Errorf("Error report records kurang dari 2: %d", len(records))
	}

	// 6b. Verifikasi Tindakan Kompensasi (Compensatory Rollback)
	reqComp, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/import/batches/%d/compensate", baseURL, batchID), nil)
	reqComp.Header.Set("X-CSRF-Token", csrfToken)
	reqComp.Header.Set("Origin", baseURL)

	resComp, err := client.Do(reqComp)
	if err != nil {
		t.Fatalf("Gagal panggil compensate: %v", err)
	}
	defer resComp.Body.Close()

	if resComp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resComp.Body)
		t.Fatalf("Kompensasi gagal: %d, body: %s", resComp.StatusCode, string(body))
	}

	var compResult struct {
		Success          bool   `json:"success"`
		CompensatedCount int    `json:"compensated_count"`
		Status           string `json:"status"`
	}
	if err := json.NewDecoder(resComp.Body).Decode(&compResult); err != nil {
		t.Fatalf("Gagal decode compensate response: %v", err)
	}

	if compResult.CompensatedCount != 2 || compResult.Status != domain.ImportStatusRolledBack {
		t.Errorf("Kompensasi result tidak sesuai: count=%d, status=%s", compResult.CompensatedCount, compResult.Status)
	}

	// Verifikasi pekerjaan diarsipkan (bukan dihapus permanen!)
	ctx := context.Background()
	activeItems, _ := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: false})
	if len(activeItems) != 0 {
		t.Errorf("Pekerjaan aktif harusnya 0 setelah kompensasi, dapat: %d", len(activeItems))
	}

	allWithArchived, _ := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: true})
	if len(allWithArchived) != 2 {
		t.Errorf("Pekerjaan terarsip harusnya tetap 2, dapat: %d", len(allWithArchived))
	}
}

// 7. Test Penolakan Berkas Rusak / Tidak Valid / Oversized
func TestImportRejectCorruptedOrInvalidFiles(t *testing.T) {
	_, baseURL, _, _, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	// 7a. Berkas ZIP / XLSX Rusak
	corruptedBytes := []byte("PK\x03\x04ThisIsNotAValidZipOrXlsxFileCorruptedData")
	resCorrupt, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{"duplicate_mode": "skip"}, "file", "broken.xlsx", corruptedBytes)
	if err != nil {
		t.Fatalf("Gagal request corrupt file: %v", err)
	}
	defer resCorrupt.Body.Close()

	if resCorrupt.StatusCode == http.StatusOK {
		t.Errorf("Harusnya berkas rusak ditolak, tapi dapat status 200")
	}

	// 7b. Ekstensi tidak didukung (contoh: .exe atau .pdf)
	resExt, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{"duplicate_mode": "skip"}, "file", "malicious.exe", []byte("MZ..."))
	if err != nil {
		t.Fatalf("Gagal request invalid ext: %v", err)
	}
	defer resExt.Body.Close()

	if resExt.StatusCode == http.StatusOK {
		t.Errorf("Harusnya ekstensi non-CSV/XLSX ditolak")
	}

	// 7c. Berkas Kosong (0 bytes)
	resEmpty, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{"duplicate_mode": "skip"}, "file", "empty.csv", []byte{})
	if err != nil {
		t.Fatalf("Gagal request empty file: %v", err)
	}
	defer resEmpty.Body.Close()

	if resEmpty.StatusCode == http.StatusOK {
		t.Errorf("Harusnya berkas kosong ditolak")
	}
}

// 8. Test Formula Injection Defense: Clean DB Storage & Secure Export Sanitization
func TestImportFormulaInjectionSanitization(t *testing.T) {
	_, baseURL, _, workSvc, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	// Masukkan data dengan karakter formula dan bullet point sah
	csvContent := "Judul Pekerjaan,Tanggal Diterima,Deskripsi\n" +
		"- Tinjauan Kepatuhan Operasional,2026-10-18,+628123456789\n" +
		"=cmd|' /C calc'!A0,2026-10-19,@Divisi Audit Internal\n"

	mapping := map[string]string{
		"title":       "Judul Pekerjaan",
		"received_on": "Tanggal Diterima",
		"description": "Deskripsi",
	}
	mappingJSON, _ := json.Marshal(mapping)

	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken, map[string]string{"mapping": string(mappingJSON), "duplicate_mode": "skip"}, "file", "tasks.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("Gagal commit: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		t.Fatalf("Commit gagal: %d", res.StatusCode)
	}

	ctx := context.Background()
	items, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	if err != nil {
		t.Fatalf("Gagal list: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("Ekspektasi 2 items: %d", len(items))
	}

	// 1. Verifikasi bahwa nilai yang sah di database TIDAK dirusak / ditambahkan prefix quote secara keliru
	var foundCleanBullet, foundFormula bool
	for _, it := range items {
		if it.Title == "- Tinjauan Kepatuhan Operasional" {
			foundCleanBullet = true
			if it.Description != "+628123456789" {
				t.Errorf("Deskripsi telepon sah termodifikasi: %s", it.Description)
			}
		}
		if it.Title == "=cmd|' /C calc'!A0" {
			foundFormula = true
		}
	}
	if !foundCleanBullet {
		t.Errorf("Item teks sah diawali hyphen termodifikasi atau tidak ditemukan di database")
	}
	if !foundFormula {
		t.Errorf("Item formula tidak ditemukan di database")
	}

	// 2. Verifikasi bahwa saat di-export ke CSV, seluruh formula dan simbol pemicu formula disanitasi secara aman
	reqExport, _ := http.NewRequest(http.MethodGet, baseURL+"/api/export/work-items?format=csv", nil)
	resExport, err := client.Do(reqExport)
	if err != nil {
		t.Fatalf("Gagal export CSV: %v", err)
	}
	defer resExport.Body.Close()

	if resExport.StatusCode != http.StatusOK {
		t.Fatalf("Export CSV gagal: %d", resExport.StatusCode)
	}

	csvExportBytes, _ := io.ReadAll(resExport.Body)
	csvExportReader := csv.NewReader(bytes.NewReader(csvExportBytes))
	exportedRecords, err := csvExportReader.ReadAll()
	if err != nil {
		t.Fatalf("Gagal membaca hasil export CSV: %v", err)
	}

	for i := 1; i < len(exportedRecords); i++ {
		row := exportedRecords[i]
		title := row[1]
		if strings.HasPrefix(title, "=") || strings.HasPrefix(title, "-") || strings.HasPrefix(title, "+") || strings.HasPrefix(title, "@") {
			t.Errorf("Baris %d judul tidak disanitasi dalam export CSV: %s", i, title)
		}
		if !strings.HasPrefix(title, "'") {
			t.Errorf("Baris %d judul seharusnya diawali quote sanitasi formula: %s", i, title)
		}
	}
}

// 10. Test Compensatory Rollback Protection with Edited Items
func TestImportCompensationWithEditedItemsProtection(t *testing.T) {
	_, baseURL, _, workSvc, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	csvContent := "Judul,Tanggal Diterima,Pemberi Tugas\n" +
		"Pekerjaan Awal 1,2026-10-17,Direksi\n" +
		"Pekerjaan Awal 2,2026-10-18,Komite Kepatuhan\n"

	mapping := map[string]string{
		"title":        "Judul",
		"received_on":  "Tanggal Diterima",
		"requested_by": "Pemberi Tugas",
	}
	mappingJSON, _ := json.Marshal(mapping)

	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken, map[string]string{"mapping": string(mappingJSON), "duplicate_mode": "skip"}, "file", "initial.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("Gagal commit: %v", err)
	}
	defer res.Body.Close()

	var commitRes domain.CommitImportResult
	_ = json.NewDecoder(res.Body).Decode(&commitRes)
	if commitRes.ImportedRows != 2 {
		t.Fatalf("Ekspektasi 2 baris terimpor: %d", commitRes.ImportedRows)
	}

	ctx := context.Background()
	items, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	if err != nil || len(items) < 2 {
		t.Fatalf("Gagal list items: %v", err)
	}

	// Simulasikan pengguna mengedit salah satu pekerjaan hasil impor (Pekerjaan Awal 1)
	editTarget := items[0]
	_, err = workSvc.UpdateWorkItem(ctx, editTarget.ID, domain.UpdateWorkItemInput{
		Title:       editTarget.Title + " (Telah Ditindaklanjuti)",
		Description: "Catatan penyesuaian baru oleh analis kepatuhan",
		Category:    editTarget.Category,
		RequestedBy: editTarget.RequestedBy,
		ReceivedOn:  editTarget.ReceivedOn,
		Priority:    domain.PriorityHigh,
		Status:      domain.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("Gagal update work item: %v", err)
	}

	// 1. Percobaan pembatalan kompensasi tanpa force=true harus DITOLAK (HTTP 409 Conflict)
	reqComp1, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/import/batches/%d/compensate", baseURL, commitRes.BatchID), nil)
	reqComp1.Header.Set("X-CSRF-Token", csrfToken)
	reqComp1.Header.Set("Origin", baseURL)
	resComp1, err := client.Do(reqComp1)
	if err != nil {
		t.Fatalf("Gagal request compensate 1: %v", err)
	}
	defer resComp1.Body.Close()

	if resComp1.StatusCode != http.StatusConflict {
		t.Fatalf("Ekspektasi status 409 Conflict saat membatalkan batch dengan item diedit, didapat: %d", resComp1.StatusCode)
	}

	var conflictBody struct {
		HasEditedItems bool `json:"has_edited_items"`
		EditedCount    int  `json:"edited_count"`
	}
	_ = json.NewDecoder(resComp1.Body).Decode(&conflictBody)
	if !conflictBody.HasEditedItems || conflictBody.EditedCount != 1 {
		t.Errorf("Metadata konflik tidak sesuai: hasEdited=%v, count=%d", conflictBody.HasEditedItems, conflictBody.EditedCount)
	}

	// 2. Pembatalan kompensasi dengan force=true harus BERHASIL mengarsipkan pekerjaan
	reqComp2, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/import/batches/%d/compensate?force=true", baseURL, commitRes.BatchID), nil)
	reqComp2.Header.Set("X-CSRF-Token", csrfToken)
	reqComp2.Header.Set("Origin", baseURL)
	resComp2, err := client.Do(reqComp2)
	if err != nil {
		t.Fatalf("Gagal request compensate force: %v", err)
	}
	defer resComp2.Body.Close()

	if resComp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resComp2.Body)
		t.Fatalf("Ekspektasi status 200 OK saat force compensate, didapat: %d, body: %s", resComp2.StatusCode, string(body))
	}

	// Verifikasi semua pekerjaan aktif telah diarsipkan
	activeItems, _ := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: false})
	if len(activeItems) != 0 {
		t.Errorf("Pekerjaan aktif harusnya 0 setelah force compensate, didapat: %d", len(activeItems))
	}
}

// 11. Test XLSX Resource Limits (Zip Bomb / Oversized Uncompressed Protection)
func TestImportXLSXResourceLimits(t *testing.T) {
	// Buat payload zip yang memiliki uncompressed size 55 MB (> 50 MB batas aman)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatalf("Gagal membuat zip entry: %v", err)
	}

	chunk := make([]byte, 1024*1024) // 1 MB
	for i := 0; i < 55; i++ {
		_, _ = w.Write(chunk)
	}
	_ = zw.Close()

	// ParseImportFile harus menolak sebelum mengeksekusi ekstraksi memori
	_, err = service.ParseImportFile(buf.Bytes(), "oversized_bomb.xlsx", "xlsx", "")
	if err == nil {
		t.Fatalf("Harusnya ParseImportFile menolak berkas dengan deklarasi ukuran melebihi batas aman")
	}

	if !strings.Contains(err.Error(), "melebihi batas aman") {
		t.Errorf("Pesan error tidak memuat informasi batas aman: %v", err)
	}
}

// 9. Test Download Template Endpoints (CSV BOM & XLSX)
func TestImportTemplateEndpoints(t *testing.T) {
	_, baseURL, _, _, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, _ := getImportTestClientWithCSRF(t, baseURL)

	// 9a. Template CSV
	reqCSV, _ := http.NewRequest(http.MethodGet, baseURL+"/api/import/template/csv", nil)
	resCSV, err := client.Do(reqCSV)
	if err != nil {
		t.Fatalf("Gagal download template CSV: %v", err)
	}
	defer resCSV.Body.Close()

	if resCSV.StatusCode != http.StatusOK {
		t.Fatalf("Status template CSV bukan 200: %d", resCSV.StatusCode)
	}

	csvBytes, _ := io.ReadAll(resCSV.Body)
	if !bytes.HasPrefix(csvBytes, []byte{0xEF, 0xBB, 0xBF}) {
		t.Errorf("Template CSV harus memuat UTF-8 BOM prefix")
	}

	// 9b. Template XLSX
	reqXLSX, _ := http.NewRequest(http.MethodGet, baseURL+"/api/import/template/xlsx", nil)
	resXLSX, err := client.Do(reqXLSX)
	if err != nil {
		t.Fatalf("Gagal download template XLSX: %v", err)
	}
	defer resXLSX.Body.Close()

	if resXLSX.StatusCode != http.StatusOK {
		t.Fatalf("Status template XLSX bukan 200: %d", resXLSX.StatusCode)
	}

	xlsxData, _ := io.ReadAll(resXLSX.Body)
	f, err := excelize.OpenReader(bytes.NewReader(xlsxData))
	if err != nil {
		t.Fatalf("Gagal membaca hasil template XLSX: %v", err)
	}
	defer f.Close()

	rows, err := f.GetRows("Work Register Template")
	if err != nil {
		t.Fatalf("Gagal membaca sheet template XLSX: %v", err)
	}

	if len(rows) < 2 {
		t.Errorf("Template XLSX harus memuat minimal 1 baris header dan 1 baris contoh: %d", len(rows))
	}
}
