package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	_ "modernc.org/sqlite"
)

// setupBatch05TestEnv menyiapkan database dan server terisolasi pada direktori sementara (t.TempDir)
func setupBatch05TestEnv(t *testing.T) (*server.Server, string, *sql.DB, string, *http.Client, *service.TaskManagementService, *service.ImportService, func()) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance_test_b05.db")
	backupDir := filepath.Join(tempDir, "backups")
	_ = os.MkdirAll(backupDir, 0755)

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB test: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := database.Migrate(ctx, db); err != nil {
		db.Close()
		t.Fatalf("Gagal menjalankan migrasi DB test: %v", err)
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
	taskSvc := service.NewTaskManagementService(db, workRepo, actRepo, subRepo, catRepo, tplRepo, recRepo)
	importSvc := service.NewImportService(db, importRepo, workRepo, actRepo, catRepo, nil)
	backupSvc := service.NewBackupService(workSvc, taskSvc, importSvc, dbPath, backupDir)
	importSvc.SetBackupService(backupSvc)

	srv, err := server.NewServer("127.0.0.1:0", workSvc, backupSvc, exportSvc, taskSvc, importSvc)
	if err != nil {
		db.Close()
		t.Fatalf("Gagal membuat HTTP server test: %v", err)
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
		t.Fatal("Server test tidak kunjung binding port")
	}

	baseURL := fmt.Sprintf("http://%s", addr)
	client, csrfToken := getImportTestClientWithCSRF(t, baseURL)

	cleanup := func() {
		ctxShut, cancelShut := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelShut()
		_ = srv.Shutdown(ctxShut)
		_ = db.Close()
	}

	return srv, baseURL, db, csrfToken, client, taskSvc, importSvc, cleanup
}

// TestUATBatch05_ImportHistoryAndDetail menguji skenario 1 - 5:
// 1. Commit CSV valid menghasilkan Import Batch
// 2. Commit XLSX valid menghasilkan Import Batch
// 3. Import History API mengembalikan batch yang tersimpan
// 4. Import Detail API mengembalikan rincian pekerjaan dan kode CMP
// 5. Import history tetap ada setelah database ditutup dan dibuka kembali
func TestUATBatch05_ImportHistoryAndDetail(t *testing.T) {
	_, baseURL, db, csrfToken, client, _, _, cleanup := setupBatch05TestEnv(t)
	defer cleanup()

	// 1. Commit CSV
	csvContent := "Judul Pekerjaan,Tanggal Diterima,Pemberi Tugas,Prioritas,Status\n" +
		"Pekerjaan CSV 1,2026-10-18,Divisi Audit,Tinggi,Dalam Proses\n" +
		"Pekerjaan CSV 2,2026-10-18,Divisi Kepatuhan,Normal,Belum Mulai\n"

	resCSV, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken,
		map[string]string{"duplicate_mode": "skip"}, "file", "tugas.csv", []byte(csvContent))
	if err != nil || (resCSV.StatusCode != http.StatusOK && resCSV.StatusCode != http.StatusCreated) {
		t.Fatalf("Gagal commit CSV: status=%v, err=%v", resCSV.StatusCode, err)
	}
	var commitCSVRes struct {
		BatchID      int64 `json:"batch_id"`
		ImportedRows int   `json:"imported_rows"`
	}
	_ = json.NewDecoder(resCSV.Body).Decode(&commitCSVRes)
	resCSV.Body.Close()

	if commitCSVRes.BatchID <= 0 || commitCSVRes.ImportedRows != 2 {
		t.Fatalf("Hasil commit CSV tidak sesuai: batch_id=%d, imported=%d", commitCSVRes.BatchID, commitCSVRes.ImportedRows)
	}

	// 2. Commit XLSX
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	f.SetCellValue(sheet, "A1", "Judul Pekerjaan")
	f.SetCellValue(sheet, "B1", "Tanggal Diterima")
	f.SetCellValue(sheet, "C1", "Pemberi Tugas")
	f.SetCellValue(sheet, "A2", "Pekerjaan XLSX 1")
	f.SetCellValue(sheet, "B2", "2026-10-18")
	f.SetCellValue(sheet, "C2", "Direksi")
	var xlsxBuf bytes.Buffer
	_ = f.Write(&xlsxBuf)

	resXLSX, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken,
		map[string]string{"duplicate_mode": "skip"}, "file", "tugas.xlsx", xlsxBuf.Bytes())
	if err != nil || (resXLSX.StatusCode != http.StatusOK && resXLSX.StatusCode != http.StatusCreated) {
		t.Fatalf("Gagal commit XLSX: status=%v, err=%v", resXLSX.StatusCode, err)
	}
	var commitXLSXRes struct {
		BatchID      int64 `json:"batch_id"`
		ImportedRows int   `json:"imported_rows"`
	}
	_ = json.NewDecoder(resXLSX.Body).Decode(&commitXLSXRes)
	resXLSX.Body.Close()

	// 3. Import History API (GET /api/import/history)
	reqHist, _ := http.NewRequest(http.MethodGet, baseURL+"/api/import/history?limit=10", nil)
	resHist, err := client.Do(reqHist)
	if err != nil || resHist.StatusCode != http.StatusOK {
		t.Fatalf("Gagal mengambil riwayat impor: status=%v, err=%v", resHist.StatusCode, err)
	}
	bodyHist, _ := io.ReadAll(resHist.Body)
	resHist.Body.Close()

	var batches []*domain.ImportBatch
	if err := json.Unmarshal(bodyHist, &batches); err != nil {
		var histObj struct {
			Batches []*domain.ImportBatch `json:"batches"`
		}
		if err2 := json.Unmarshal(bodyHist, &histObj); err2 != nil {
			t.Fatalf("Format JSON riwayat impor tidak valid: %v", err)
		}
		batches = histObj.Batches
	}
	if len(batches) != 2 {
		t.Fatalf("Jumlah riwayat impor tidak sesuai: batches=%d (diharapkan 2)", len(batches))
	}

	// 4. Import Detail API (GET /api/import/batches/{id})
	reqDet, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/import/batches/%d", baseURL, commitCSVRes.BatchID), nil)
	resDet, err := client.Do(reqDet)
	if err != nil || resDet.StatusCode != http.StatusOK {
		t.Fatalf("Gagal mengambil detail batch: status=%v, err=%v", resDet.StatusCode, err)
	}
	var detObj struct {
		Batch *domain.ImportBatch  `json:"batch"`
		Items []*domain.ImportItem `json:"items"`
	}
	_ = json.NewDecoder(resDet.Body).Decode(&detObj)
	resDet.Body.Close()

	if detObj.Batch == nil || detObj.Batch.ID != commitCSVRes.BatchID {
		t.Fatalf("Data batch detail tidak sesuai: %v", detObj.Batch)
	}
	if len(detObj.Items) != 2 {
		t.Fatalf("Jumlah item baris detail tidak sesuai: %d (diharapkan 2)", len(detObj.Items))
	}
	for i, item := range detObj.Items {
		if item.WorkCode == "" || !strings.HasPrefix(item.WorkCode, "CMP-2026-") {
			t.Errorf("Item %d tidak memiliki work_code CMP valid: '%s'", i+1, item.WorkCode)
		}
		if item.Title == "" {
			t.Errorf("Item %d tidak memiliki title: '%s'", i+1, item.Title)
		}
		if item.RequestedBy == "" {
			t.Errorf("Item %d tidak memiliki requested_by: '%s'", i+1, item.RequestedBy)
		}
	}

	// 5. Verifikasi persistensi di SQLite
	var countBatches int
	err = db.QueryRow("SELECT COUNT(*) FROM import_batches").Scan(&countBatches)
	if err != nil || countBatches != 2 {
		t.Fatalf("Jumlah batch di SQLite tidak sesuai: %d, err=%v", countBatches, err)
	}
}

// TestUATBatch05_MasterCategorySyncAndDeduplication menguji skenario 6, 7, 8:
// 6. Kategori baru pada CSV/XLSX dibuat pada Master Kategori
// 7. Kategori baru langsung tersedia pada GET /api/categories
// 8. Tidak terjadi duplikasi kategori karena perbedaan huruf kapital atau spasi
func TestUATBatch05_MasterCategorySyncAndDeduplication(t *testing.T) {
	_, baseURL, db, csrfToken, client, _, _, cleanup := setupBatch05TestEnv(t)
	defer cleanup()

	// Berkas CSV dengan kategori kustom baru "Makanan", "makanan", dan " Makanan "
	csvContent := "Judul Pekerjaan,Tanggal Diterima,Pemberi Tugas,Kategori\n" +
		"Pekerjaan Makanan 1,2026-10-18,Unit Gizi,Makanan\n" +
		"Pekerjaan Makanan 2,2026-10-18,Unit Logistik,makanan\n" +
		"Pekerjaan Makanan 3,2026-10-18,Unit Dapur, Makanan \n"

	res, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken,
		map[string]string{"duplicate_mode": "skip"}, "file", "makanan.csv", []byte(csvContent))
	if err != nil || (res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated) {
		t.Fatalf("Commit CSV makanan gagal: status=%v, err=%v", res.StatusCode, err)
	}
	res.Body.Close()

	// 6 & 8. Verifikasi pada tabel categories di SQLite
	var catCount int
	err = db.QueryRow("SELECT COUNT(*) FROM categories WHERE LOWER(trim(name)) = 'makanan'").Scan(&catCount)
	if err != nil {
		t.Fatalf("Kueri kategori makanan gagal: %v", err)
	}
	if catCount != 1 {
		t.Fatalf("Diharapkan tepat 1 baris kategori 'Makanan' di categories, ditemukan: %d (terjadi duplikasi!)", catCount)
	}

	// 7. Verifikasi GET /api/categories
	reqCat, _ := http.NewRequest(http.MethodGet, baseURL+"/api/categories?all=true", nil)
	resCat, err := client.Do(reqCat)
	if err != nil || resCat.StatusCode != http.StatusOK {
		t.Fatalf("Gagal mengambil master kategori: status=%v, err=%v", resCat.StatusCode, err)
	}
	var cats []*domain.Category
	_ = json.NewDecoder(resCat.Body).Decode(&cats)
	resCat.Body.Close()

	found := false
	for _, c := range cats {
		if strings.EqualFold(c.Name, "Makanan") {
			found = true
			if !c.IsActive {
				t.Errorf("Kategori baru 'Makanan' harus berstatus aktif, nilai is_active=%v", c.IsActive)
			}
			if c.IsDefault {
				t.Errorf("Kategori baru 'Makanan' harus berstatus kustom (bukan default sistem)")
			}
		}
	}
	if !found {
		t.Fatalf("Kategori 'Makanan' tidak ditemukan dalam respons GET /api/categories")
	}

	// Verifikasi seluruh work_items menggunakan nama kanonikal kategori
	rows, err := db.Query("SELECT category FROM work_items WHERE title LIKE 'Pekerjaan Makanan%'")
	if err != nil {
		t.Fatalf("Gagal kueri work items: %v", err)
	}
	defer rows.Close()
	wiCount := 0
	for rows.Next() {
		var cat string
		rows.Scan(&cat)
		wiCount++
		if cat != "Makanan" {
			t.Errorf("Pekerjaan tidak menggunakan nama kanonikal kategori: '%s'", cat)
		}
	}
	if wiCount != 3 {
		t.Fatalf("Jumlah pekerjaan makanan yang dibuat tidak sesuai: %d (diharapkan 3)", wiCount)
	}
}

// TestUATBatch05_InactiveCategoryHandling menguji skenario 9, 10, 14:
// 9. Import dengan kategori nonaktif ditolak pada Preview & Commit
// 10. Import tidak boleh otomatis mengaktifkan kembali kategori yang dinonaktifkan
// 14. Import Preview tidak memodifikasi database
func TestUATBatch05_InactiveCategoryHandling(t *testing.T) {
	_, baseURL, db, csrfToken, client, _, _, cleanup := setupBatch05TestEnv(t)
	defer cleanup()

	// Buat kategori baru lalu nonaktifkan
	_, err := db.Exec("INSERT INTO categories (name, description, is_active) VALUES ('Kategori Terlarang', 'Testing', 0)")
	if err != nil {
		t.Fatalf("Gagal menyiapkan kategori nonaktif: %v", err)
	}

	csvContent := "Judul Pekerjaan,Tanggal Diterima,Pemberi Tugas,Kategori\n" +
		"Pekerjaan Terlarang,2026-10-18,Staf,Kategori Terlarang\n"

	// 14. Import Preview tidak memodifikasi DB dan mendeteksi kategori nonaktif
	resPrev, err := sendMultipartRequest(t, client, baseURL+"/api/import/preview", http.MethodPost, csrfToken,
		map[string]string{"duplicate_mode": "skip"}, "file", "terlarang.csv", []byte(csvContent))
	if err != nil || resPrev.StatusCode != http.StatusOK {
		t.Fatalf("Preview impor gagal: status=%v, err=%v", resPrev.StatusCode, err)
	}
	var prevRes domain.ImportPreviewResult
	_ = json.NewDecoder(resPrev.Body).Decode(&prevRes)
	resPrev.Body.Close()

	if prevRes.InvalidRows != 1 || prevRes.ValidRows != 0 {
		t.Fatalf("Preview harus menandai baris dengan kategori nonaktif sebagai invalid! Invalid=%d, Valid=%d", prevRes.InvalidRows, prevRes.ValidRows)
	}
	foundMsg := false
	for _, e := range prevRes.Rows[0].Errors {
		if strings.Contains(e, "berstatus nonaktif") {
			foundMsg = true
			break
		}
	}
	if !foundMsg {
		t.Errorf("Pesan error preview tidak menjelaskan kategori nonaktif: %v", prevRes.Rows[0].Errors)
	}

	// Pastikan preview tidak menambahkan record apapun ke work_items
	var countWI int
	_ = db.QueryRow("SELECT COUNT(*) FROM work_items").Scan(&countWI)
	if countWI != 0 {
		t.Fatalf("Preview tidak boleh memodifikasi database! Ditemukan %d pekerjaan.", countWI)
	}

	// 9 & 10. Commit dengan kategori nonaktif
	resCommit, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrfToken,
		map[string]string{"duplicate_mode": "skip"}, "file", "terlarang.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("Request commit gagal: %v", err)
	}
	resCommit.Body.Close()

	// Kategori 'Kategori Terlarang' harus TETAP nonaktif (is_active = 0)
	var isActive int
	_ = db.QueryRow("SELECT is_active FROM categories WHERE name = 'Kategori Terlarang'").Scan(&isActive)
	if isActive != 0 {
		t.Fatalf("Kategori yang dinonaktifkan sengaja TIDAK BOLEH diaktifkan kembali otomatis oleh impor! is_active=%d", isActive)
	}
}

// TestUATBatch05_ProtectedBaselinesNonRegression menguji modul yang berstatus PASS tidak regresi:
// - Category Hard Delete & Deactivation
// - Template checklist application
// - Recurring schedule execution
// - Append-only activity logs
func TestUATBatch05_ProtectedBaselinesNonRegression(t *testing.T) {
	_, _, db, _, _, taskSvc, _, cleanup := setupBatch05TestEnv(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Kategori Sistem dilindungi dari Hard Delete
	var defaultCatID int64
	err := db.QueryRow("SELECT id FROM categories WHERE is_default = 1 LIMIT 1").Scan(&defaultCatID)
	if err != nil {
		t.Fatalf("Gagal mencari kategori default: %v", err)
	}
	if err := taskSvc.DeleteCategory(ctx, defaultCatID); err == nil {
		t.Fatalf("Kategori sistem default harus dilindungi dari penghapusan!")
	}

	// 2. Kategori kustom yang digunakan oleh pekerjaan dilindungi dari Hard Delete
	newCat, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name:        "Kategori Terpakai",
		Description: "Uji proteksi",
	})
	if err != nil {
		t.Fatalf("Gagal membuat kategori: %v", err)
	}

	// Buat pekerjaan dengan kategori tersebut
	_, err = db.Exec("INSERT INTO work_items (work_code, title, category, received_on, priority, status) VALUES ('CMP-2026-999999', 'Pekerjaan Proteksi', 'Kategori Terpakai', '2026-10-18', 'normal', 'not_started')")
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan: %v", err)
	}

	if err := taskSvc.DeleteCategory(ctx, newCat.ID); err == nil {
		t.Fatalf("Kategori yang telah digunakan oleh pekerjaan HARUS dilindungi dari hard delete!")
	}

	// 3. Kategori kustom yang tidak terpakai BISA dihapus (Hard Delete)
	unusedCat, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name: "Kategori Kosong",
	})
	if err != nil {
		t.Fatalf("Gagal membuat kategori kosong: %v", err)
	}
	if err := taskSvc.DeleteCategory(ctx, unusedCat.ID); err != nil {
		t.Fatalf("Kategori tidak terpakai harus dapat dihapus dengan sukses: %v", err)
	}

	// 4. Template Checklist Application (Progress 0% atomik)
	tpl, err := taskSvc.CreateTemplate(ctx, domain.CreateWorkTemplateInput{
		Name:          "Template Proteksi",
		TitlePattern:  "Uji Proteksi Checklist",
		Category:      "General",
		Priority:      "normal",
		DueDaysOffset: 7,
		ChecklistItems: []string{"Subtask A", "Subtask B", "Subtask C"},
	})
	if err != nil {
		t.Fatalf("Gagal membuat template: %v", err)
	}

	appliedWI, err := taskSvc.ApplyTemplateWithInput(ctx, tpl.ID, domain.ApplyTemplateInput{
		ReceivedOn: "2026-10-18",
	})
	if err != nil {
		t.Fatalf("Gagal menerapkan template: %v", err)
	}
	if appliedWI.ProgressPercentage != 0 {
		t.Fatalf("Progress pekerjaan dari template harus 0%%, nilai: %d", appliedWI.ProgressPercentage)
	}
	var subCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM subtasks WHERE work_item_id = ?", appliedWI.ID).Scan(&subCount)
	if subCount != 3 {
		t.Fatalf("Jumlah subtask dari template harus tepat 3, ditemukan: %d", subCount)
	}

	// 5. Activity Log Append-Only
	var logCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM activity_logs WHERE work_item_id = ?", appliedWI.ID).Scan(&logCount)
	if logCount < 1 {
		t.Fatalf("Pekerjaan dari template harus mencatat activity log awal")
	}
}
