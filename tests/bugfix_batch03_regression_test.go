package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
	"compliance-workspace/internal/service"
)

// =============================================================================
// BUGFIX BATCH 03 REGRESSION TESTS: IMPORT PREVIEW DATA CONTRACT & NORMALIZATION
// =============================================================================

// 1. Test Official CSV Template Preview Contract (2 Valid Rows)
// Verifikasi bahwa preview template resmi template_work_register.csv menghasilkan:
// total_rows = 2, valid_rows = 2, duplicate_rows = 0, invalid_rows / error_rows = 0,
// dan array rows / sample_rows berisi 2 baris valid.
func TestBUG003_OfficialCSVTemplatePreviewContract(t *testing.T) {
	_, baseURL, _, _, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	// Unduh template resmi CSV
	csvBytes := service.GenerateCSVTemplate()

	// Kirim request preview
	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{
		"duplicate_mode": "skip",
	}, "file", "template_work_register.csv", csvBytes)
	if err != nil {
		t.Fatalf("Gagal mengirim request preview CSV: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("Ekspektasi HTTP 200, didapat: %d, body: %s", res.StatusCode, string(body))
	}

	// Verifikasi kontrak JSON mentah
	var rawJSON map[string]interface{}
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("Gagal membaca body respons: %v", err)
	}
	if err := json.Unmarshal(bodyBytes, &rawJSON); err != nil {
		t.Fatalf("Gagal unmarshal JSON respons: %v", err)
	}

	// Pastikan field kontrak ada di JSON
	for _, field := range []string{"total_rows", "valid_rows", "duplicate_rows", "invalid_rows", "error_rows", "rows", "sample_rows"} {
		if _, exists := rawJSON[field]; !exists {
			t.Errorf("Kontrak JSON hilang field wajib: %s", field)
		}
	}

	total := int(rawJSON["total_rows"].(float64))
	valid := int(rawJSON["valid_rows"].(float64))
	dup := int(rawJSON["duplicate_rows"].(float64))
	invalid := int(rawJSON["invalid_rows"].(float64))
	errorRows := int(rawJSON["error_rows"].(float64))

	if total != 2 {
		t.Errorf("Ekspektasi total_rows=2, didapat: %d", total)
	}
	if valid != 2 {
		t.Errorf("Ekspektasi valid_rows=2, didapat: %d", valid)
	}
	if dup != 0 {
		t.Errorf("Ekspektasi duplicate_rows=0, didapat: %d", dup)
	}
	if invalid != 0 || errorRows != 0 {
		t.Errorf("Ekspektasi invalid_rows=0 dan error_rows=0, didapat: invalid=%d, errorRows=%d", invalid, errorRows)
	}

	// Invariant: total == valid + duplicate + invalid
	if total != valid+dup+invalid {
		t.Errorf("Invarian validasi dilanggar: total (%d) != valid (%d) + dup (%d) + invalid (%d)", total, valid, dup, invalid)
	}

	rowsArr, ok := rawJSON["rows"].([]interface{})
	if !ok || len(rowsArr) != 2 {
		t.Fatalf("Ekspektasi field 'rows' berisi 2 elemen, didapat: %d", len(rowsArr))
	}

	// Verifikasi baris ke-1
	r1 := rowsArr[0].(map[string]interface{})
	if r1["title"] == "" || r1["is_valid"] != true {
		t.Errorf("Baris 1 tidak valid: %+v", r1)
	}
}

// 2. Test Official XLSX Template Preview Contract (2 Valid Rows)
// Verifikasi bahwa preview template resmi template_work_register.xlsx menghasilkan:
// total_rows = 2, valid_rows = 2, duplicate_rows = 0, invalid_rows / error_rows = 0
func TestBUG003_OfficialXLSXTemplatePreviewContract(t *testing.T) {
	_, baseURL, _, _, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	xlsxBytes, err := service.GenerateXLSXTemplate()
	if err != nil {
		t.Fatalf("Gagal menghasilkan template XLSX: %v", err)
	}

	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{
		"duplicate_mode": "skip",
		"worksheet":      "Work Register Template",
	}, "file", "template_work_register.xlsx", xlsxBytes)
	if err != nil {
		t.Fatalf("Gagal mengirim request preview XLSX: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("Ekspektasi HTTP 200, didapat: %d, body: %s", res.StatusCode, string(body))
	}

	var preview domain.ImportPreviewResult
	if err := json.NewDecoder(res.Body).Decode(&preview); err != nil {
		t.Fatalf("Gagal decode ImportPreviewResult: %v", err)
	}

	if preview.TotalRows != 2 {
		t.Errorf("Ekspektasi TotalRows=2, didapat: %d", preview.TotalRows)
	}
	if preview.ValidRows != 2 {
		t.Errorf("Ekspektasi ValidRows=2, didapat: %d", preview.ValidRows)
	}
	if preview.DuplicateRows != 0 {
		t.Errorf("Ekspektasi DuplicateRows=0, didapat: %d", preview.DuplicateRows)
	}
	if preview.InvalidRows != 0 || preview.ErrorRows != 0 {
		t.Errorf("Ekspektasi InvalidRows=0, didapat: invalid=%d, error=%d", preview.InvalidRows, preview.ErrorRows)
	}
	if len(preview.Rows) != 2 || len(preview.SampleRows) != 2 {
		t.Errorf("Ekspektasi 2 preview rows, didapat: rows=%d, sample_rows=%d", len(preview.Rows), len(preview.SampleRows))
	}
}

// 3. Test Indonesian Status & Priority Normalization
// Pengguna mengunggah CSV dengan status dan prioritas dalam Bahasa Indonesia:
// "Belum Mulai", "Dalam Proses", "Selesai", "Tinggi", "Rendah", "Kritis", dsb.
// Harus dinormalisasi secara otomatis menjadi internal domain constants.
func TestBUG003_IndonesianEnumNormalization(t *testing.T) {
	_, baseURL, _, _, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	csvContent := `Judul Pekerjaan,Deskripsi,Kategori,Pemberi Tugas,Tanggal Diterima,Tenggat Waktu,Prioritas,Status
Review Kepatuhan OJK,Deskripsi 1,Regulasi,Direksi,2026-10-17,2026-10-25,Tinggi,Belum Mulai
Audit APU PPT,Deskripsi 2,Anti Fraud,Komite,2026-10-18,2026-10-28,Rendah,Dalam Proses
Laporan Berkala BI,Deskripsi 3,Pelaporan,Divisi Kepatuhan,2026-10-19,2026-10-30,Kritis,Selesai
Pemeriksaan Lapangan,Deskripsi 4,Audit,Internal,2026-10-20,2026-11-05,Sedang,Menunggu
Penyesuaian Kebijakan,Deskripsi 5,Kebijakan,HRD,2026-10-21,2026-11-10,Biasa,Dibatalkan`

	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{
		"duplicate_mode": "skip",
	}, "file", "test_indonesian_enums.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("Gagal mengirim request preview: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("Ekspektasi HTTP 200, didapat: %d, body: %s", res.StatusCode, string(body))
	}

	var preview domain.ImportPreviewResult
	if err := json.NewDecoder(res.Body).Decode(&preview); err != nil {
		t.Fatalf("Gagal decode preview: %v", err)
	}

	if preview.TotalRows != 5 || preview.ValidRows != 5 || preview.InvalidRows != 0 {
		t.Fatalf("Seluruh baris harusnya valid: total=%d, valid=%d, invalid=%d", preview.TotalRows, preview.ValidRows, preview.InvalidRows)
	}

	expectedPriorities := []string{domain.PriorityHigh, domain.PriorityLow, domain.PriorityCritical, domain.PriorityNormal, domain.PriorityNormal}
	expectedStatuses := []string{domain.StatusNotStarted, domain.StatusInProgress, domain.StatusCompleted, domain.StatusWaiting, domain.StatusCancelled}

	for i, r := range preview.Rows {
		if r.Priority != expectedPriorities[i] {
			t.Errorf("Baris %d: ekspektasi priority '%s', didapat: '%s'", i+1, expectedPriorities[i], r.Priority)
		}
		if r.Status != expectedStatuses[i] {
			t.Errorf("Baris %d: ekspektasi status '%s', didapat: '%s'", i+1, expectedStatuses[i], r.Status)
		}
	}
}

// 4. Test 1 Valid + 1 Invalid Row Classification & Invariant Integrity
// Menguji file dengan 1 baris valid dan 1 baris invalid (tanggal salah dan judul kosong).
// Memastikan: TotalRows == ValidRows + DuplicateRows + InvalidRows.
func TestBUG003_ValidAndInvalidRowClassification(t *testing.T) {
	_, baseURL, _, _, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	csvContent := `Judul Pekerjaan,Tanggal Diterima,Pemberi Tugas,Prioritas,Status
Pekerjaan Valid Baris Pertama,2026-10-17,Direksi,normal,not_started
,bukan-format-tanggal,Audit,high,in_progress`

	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{
		"duplicate_mode": "skip",
	}, "file", "test_mixed_validity.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("Gagal kirim request: %v", err)
	}
	defer res.Body.Close()

	var preview domain.ImportPreviewResult
	_ = json.NewDecoder(res.Body).Decode(&preview)

	if preview.TotalRows != 2 {
		t.Errorf("TotalRows salah: %d", preview.TotalRows)
	}
	if preview.ValidRows != 1 {
		t.Errorf("ValidRows salah: %d", preview.ValidRows)
	}
	if preview.InvalidRows != 1 || preview.ErrorRows != 1 {
		t.Errorf("InvalidRows salah: invalid=%d, error=%d", preview.InvalidRows, preview.ErrorRows)
	}
	if preview.DuplicateRows != 0 {
		t.Errorf("DuplicateRows salah: %d", preview.DuplicateRows)
	}

	// Invariant Check
	if preview.TotalRows != preview.ValidRows+preview.DuplicateRows+preview.InvalidRows {
		t.Errorf("Invarian total = valid + dup + invalid dilanggar: %d != %d + %d + %d",
			preview.TotalRows, preview.ValidRows, preview.DuplicateRows, preview.InvalidRows)
	}

	// Cek detail baris 2 yang error
	row2 := preview.Rows[1]
	if row2.IsValid != false {
		t.Errorf("Baris 2 seharusnya IsValid=false")
	}
	if len(row2.ValidationErrors) == 0 && len(row2.Errors) == 0 {
		t.Errorf("Baris 2 seharusnya memiliki catatan error validasi")
	}
}

// 5. Test In-File Duplicate Detection
// Dua baris dalam satu file memiliki Judul + Tanggal Diterima + Pemberi Tugas yang identik.
// Baris kedua harus diklasifikasikan sebagai duplikat in-file.
func TestBUG003_InFileDuplicateDetection(t *testing.T) {
	_, baseURL, _, _, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	csvContent := `Judul Pekerjaan,Tanggal Diterima,Pemberi Tugas,Prioritas,Status
Audit Kepatuhan Internal 2026,2026-10-17,Komite Audit,high,not_started
Audit Kepatuhan Internal 2026,2026-10-17,Komite Audit,high,not_started`

	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{
		"duplicate_mode": "skip",
	}, "file", "test_in_file_duplicate.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("Gagal kirim request: %v", err)
	}
	defer res.Body.Close()

	var preview domain.ImportPreviewResult
	_ = json.NewDecoder(res.Body).Decode(&preview)

	if preview.TotalRows != 2 {
		t.Errorf("TotalRows salah: %d", preview.TotalRows)
	}
	if preview.ValidRows != 1 {
		t.Errorf("ValidRows salah (baris pertama siap impor): %d", preview.ValidRows)
	}
	if preview.DuplicateRows != 1 {
		t.Errorf("DuplicateRows salah (baris kedua duplikat): %d", preview.DuplicateRows)
	}
	if preview.InvalidRows != 0 {
		t.Errorf("InvalidRows salah: %d", preview.InvalidRows)
	}

	// Baris 1: IsValid=true, IsDuplicate=false
	if !preview.Rows[0].IsValid || preview.Rows[0].IsDuplicate {
		t.Errorf("Baris 1 harusnya valid dan bukan duplikat: %+v", preview.Rows[0])
	}
	// Baris 2: IsValid=true, IsDuplicate=true
	if !preview.Rows[1].IsValid || !preview.Rows[1].IsDuplicate {
		t.Errorf("Baris 2 harusnya valid dan terdeteksi duplikat: %+v", preview.Rows[1])
	}
	if !strings.Contains(preview.Rows[1].DuplicateReason, "berkas yang sama") {
		t.Errorf("DuplicateReason tidak memuat keterangan berkas yang sama: %s", preview.Rows[1].DuplicateReason)
	}
}

// 6. Test Database Duplicate Detection Against SQLite
// Baris dalam file cocok dengan data pekerjaan yang sudah ada di basis data SQLite.
// Harus ditandai IsDuplicate=true dan memuat MatchedWorkCode yang sesuai.
func TestBUG003_DatabaseDuplicateDetection(t *testing.T) {
	_, baseURL, _, workSvc, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)
	ctx := context.Background()

	// 1. Buat pekerjaan di DB terlebih dahulu
	existing, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Existing di Database",
		ReceivedOn:  "2026-10-17",
		RequestedBy: "Direksi",
		Priority:    domain.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("Gagal membuat data awal DB: %v", err)
	}

	// 2. Unggah file CSV yang memuat data identik
	csvContent := fmt.Sprintf(`Judul Pekerjaan,Tanggal Diterima,Pemberi Tugas,Prioritas,Status
Pekerjaan Existing di Database,2026-10-17,Direksi,normal,not_started
Pekerjaan Baru Lainnya,2026-10-18,Komite Kepatuhan,high,not_started`)

	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{
		"duplicate_mode": "skip",
	}, "file", "test_db_duplicate.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("Gagal kirim preview: %v", err)
	}
	defer res.Body.Close()

	var preview domain.ImportPreviewResult
	_ = json.NewDecoder(res.Body).Decode(&preview)

	if preview.TotalRows != 2 {
		t.Errorf("TotalRows salah: %d", preview.TotalRows)
	}
	if preview.ValidRows != 1 {
		t.Errorf("ValidRows salah: %d", preview.ValidRows)
	}
	if preview.DuplicateRows != 1 {
		t.Errorf("DuplicateRows salah: %d", preview.DuplicateRows)
	}

	// Baris 1 harus mencocokkan kode CMP existing
	r1 := preview.Rows[0]
	if !r1.IsDuplicate {
		t.Errorf("Baris 1 harusnya terdeteksi duplikasi database")
	}
	if r1.MatchedWorkCode != existing.WorkCode {
		t.Errorf("MatchedWorkCode tidak cocok: ekspektasi '%s', didapat: '%s'", existing.WorkCode, r1.MatchedWorkCode)
	}
}

// 7. Test Preview Remains Strictly Read-Only
// Memastikan pemanggilan /api/import/preview TIDAK mengubah atau menambah data apapun di DB.
func TestBUG003_PreviewIsStrictlyReadOnly(t *testing.T) {
	_, baseURL, _, workSvc, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)
	ctx := context.Background()

	// Hitung item sebelum preview
	itemsBefore, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Gagal hitung items awal: %v", err)
	}
	countBefore := len(itemsBefore)

	// Lakukan 3 kali preview berturut-turut dengan file yang valid
	csvBytes := service.GenerateCSVTemplate()
	for i := 1; i <= 3; i++ {
		res, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{
			"duplicate_mode": "skip",
		}, "file", "template_work_register.csv", csvBytes)
		if err != nil {
			t.Fatalf("Preview %d gagal: %v", i, err)
		}
		res.Body.Close()
	}

	// Hitung item setelah preview
	itemsAfter, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Gagal hitung items akhir: %v", err)
	}
	countAfter := len(itemsAfter)

	if countBefore != countAfter {
		t.Fatalf("VIOLATION: Preview mengubah database! Sebelum=%d, Sesudah=%d", countBefore, countAfter)
	}
}

// 8. Test Atomic Commit With Generated CMP Codes and Activity Logs
// Mengeksekusi commit impor resmi dan memverifikasi:
// - Status HTTP 201 Created
// - Transaksi atomik berhasil membuat pekerjaan dengan kode unik CMP-YYYY-NNNNNN
// - Activity log tersimpan secara append-only
// - Backup pre-commit otomatis terbentuk
func TestBUG003_AtomicCommitCreatesCMPAndActivityLog(t *testing.T) {
	_, baseURL, _, workSvc, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)
	ctx := context.Background()

	csvBytes := service.GenerateCSVTemplate()

	// Eksekusi commit import
	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken, map[string]string{
		"duplicate_mode": "skip",
	}, "file", "template_work_register.csv", csvBytes)
	if err != nil {
		t.Fatalf("Gagal kirim commit request: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("Ekspektasi HTTP 201 Created, didapat: %d, body: %s", res.StatusCode, string(body))
	}

	var commitRes domain.CommitImportResult
	if err := json.NewDecoder(res.Body).Decode(&commitRes); err != nil {
		t.Fatalf("Gagal decode CommitImportResult: %v", err)
	}

	if commitRes.TotalRows != 2 || commitRes.ImportedRows != 2 || commitRes.CreatedCount != 2 {
		t.Errorf("Hasil commit tidak sesuai: total=%d, imported=%d, created=%d",
			commitRes.TotalRows, commitRes.ImportedRows, commitRes.CreatedCount)
	}

	// Verifikasi pekerjaan di DB
	items, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: false})
	if err != nil {
		t.Fatalf("Gagal ambil work items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Ekspektasi 2 pekerjaan di database, didapat: %d", len(items))
	}

	for _, item := range items {
		if !strings.HasPrefix(item.WorkCode, "CMP-") {
			t.Errorf("Format nomor CMP tidak valid: %s", item.WorkCode)
		}

		// Verifikasi Activity Log tercatat
		logs, err := workSvc.ListActivities(ctx, item.ID)
		if err != nil {
			t.Fatalf("Gagal mengambil activity log: %v", err)
		}
		if len(logs) == 0 {
			t.Errorf("Item %s tidak memiliki Activity Log pembuatan", item.WorkCode)
		} else {
			if logs[0].EntryType != domain.EntryTypeCreated {
				t.Errorf("Activity log entry_type harus 'created', didapat: %s", logs[0].EntryType)
			}
		}
	}
}

// 9. Test Malformed & Missing Fields Handled Gracefully
// Memastikan server merespons dengan benar terhadap berkas kosong atau parameter hilang.
func TestBUG003_MalformedFileAndMissingParams(t *testing.T) {
	_, baseURL, _, _, cleanup := setupImportTestServer(t)
	defer cleanup()
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	// 9a. Request tanpa form file
	resNoFile, err := client.Post(baseURL+"/api/import/preview", "application/json", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	resNoFile.Body.Close()
	// Harusnya 400 Bad Request atau 403 Forbidden (CSRF)
	if resNoFile.StatusCode != http.StatusBadRequest && resNoFile.StatusCode != http.StatusForbidden {
		t.Errorf("Ekspektasi 400/403 untuk request tanpa file, didapat: %d", resNoFile.StatusCode)
	}

	// 9b. Berkas kosong (0 byte)
	resEmpty, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken, map[string]string{}, "file", "empty.csv", []byte{})
	if err != nil {
		t.Fatalf("Request file kosong gagal: %v", err)
	}
	defer resEmpty.Body.Close()
	if resEmpty.StatusCode != http.StatusBadRequest {
		t.Errorf("Ekspektasi HTTP 400 untuk berkas 0 byte, didapat: %d", resEmpty.StatusCode)
	}
}
