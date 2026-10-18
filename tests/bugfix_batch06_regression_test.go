package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
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

	_ "modernc.org/sqlite"
)

func setupBatch06TestEnv(t *testing.T) (*server.Server, string, *sql.DB, string, string, *http.Client, *service.WorkItemService, *service.TaskManagementService, *service.ImportService, *service.BackupService, func()) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance_test_b06.db")
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

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("Gagal membuat cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}

	cleanup := func() {
		shutdownCtx, sCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer sCancel()
		_ = srv.Shutdown(shutdownCtx)
		_ = db.Close()
	}

	return srv, addr, db, dbPath, backupDir, client, workSvc, taskSvc, importSvc, backupSvc, cleanup
}

func getCSRFTokenB06(t *testing.T, client *http.Client, baseURL string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/csrf-token", nil)
	if err != nil {
		t.Fatalf("Gagal membuat request csrf: %v", err)
	}
	req.Header.Set("Origin", baseURL)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Gagal mengambil csrf-token: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Gagal decode csrf-token: %v", err)
	}
	return body.CSRFToken
}

// TestBugfixBatch06_Scenarios_01_to_06_RecurringLifecycle menguji skenario 1 s/d 6
func TestBugfixBatch06_Scenarios_01_to_06_RecurringLifecycle(t *testing.T) {
	_, addr, db, dbPath, _, client, _, taskSvc, _, _, cleanup := setupBatch06TestEnv(t)
	defer cleanup()

	baseURL := fmt.Sprintf("http://%s", addr)
	csrf := getCSRFTokenB06(t, client, baseURL)

	// 1. Membuat jadwal baru dan menyimpannya
	createPayload := map[string]interface{}{
		"title":          "Pengawasan Kepatuhan Bulanan",
		"description":    "Review rutin bulanan laporan kepatuhan",
		"category":       "General",
		"priority":       "high",
		"frequency":      "monthly",
		"interval_value": 1,
		"start_date":     "2026-10-01",
	}
	bodyBytes, _ := json.Marshal(createPayload)
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/recurring-schedules", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Origin", baseURL)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Gagal POST recurring schedule: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("Status expected 201, got %d: %s", resp.StatusCode, string(b))
	}

	var createdSch domain.RecurringSchedule
	if err := json.NewDecoder(resp.Body).Decode(&createdSch); err != nil {
		t.Fatalf("Gagal decode created schedule: %v", err)
	}
	if createdSch.ID <= 0 || createdSch.Title != "Pengawasan Kepatuhan Bulanan" {
		t.Fatalf("Jadwal tersimpan tidak sesuai: %+v", createdSch)
	}

	// 2. Menutup dan membuka kembali database: jadwal masih ada
	var schCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM recurring_schedules WHERE id = ?", createdSch.ID).Scan(&schCount); err != nil || schCount != 1 {
		t.Fatalf("Verifikasi DB awal gagal: count=%d, err=%v", schCount, err)
	}

	// Buka koneksi baru secara independen ke file yang sama
	db2, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB kedua: %v", err)
	}
	defer db2.Close()
	var schCount2 int
	if err := db2.QueryRow("SELECT COUNT(*) FROM recurring_schedules WHERE id = ?", createdSch.ID).Scan(&schCount2); err != nil || schCount2 != 1 {
		t.Fatalf("Jadwal tidak persisten setelah pembukaan koneksi baru: count=%d, err=%v", schCount2, err)
	}

	// 3. Menjalankan generator: Work Item terbentuk tepat satu kali
	ctx := context.Background()
	result1, err := taskSvc.TriggerRecurringWithReport(ctx, "2026-10-18")
	if err != nil {
		t.Fatalf("Generator run 1 error: %v", err)
	}
	if len(result1.CreatedItems) != 1 {
		t.Fatalf("Harus terbentuk tepat 1 work item pada run 1, didapat: %d", len(result1.CreatedItems))
	}
	createdWI := result1.CreatedItems[0]

	// Idempotency: Jalankan generator kedua kali untuk tanggal yang sama
	result2, err := taskSvc.TriggerRecurringWithReport(ctx, "2026-10-18")
	if err != nil {
		t.Fatalf("Generator run 2 error: %v", err)
	}
	if len(result2.CreatedItems) != 0 {
		t.Fatalf("Generator run 2 harus idempotent (0 created), didapat: %d", len(result2.CreatedItems))
	}

	// 4. Work Register menampilkan provenance Recurring
	reqList, _ := http.NewRequest(http.MethodGet, baseURL+"/api/work-items", nil)
	respList, err := client.Do(reqList)
	if err != nil {
		t.Fatalf("Gagal list work items: %v", err)
	}
	defer respList.Body.Close()
	var items []*domain.WorkItem
	_ = json.NewDecoder(respList.Body).Decode(&items)
	if len(items) != 1 {
		t.Fatalf("Work register harus memiliki 1 item, didapat: %d", len(items))
	}
	if items[0].Provenance != domain.ProvenanceRecurring {
		t.Fatalf("Provenance harus '%s', didapat '%s'", domain.ProvenanceRecurring, items[0].Provenance)
	}

	// 5. Activity Log mencatat pembuatan otomatis
	var logDesc string
	err = db.QueryRow("SELECT description FROM activity_logs WHERE work_item_id = ? AND entry_type = 'created'", createdWI.ID).Scan(&logDesc)
	if err != nil {
		t.Fatalf("Gagal query activity log creation: %v", err)
	}
	if !strings.Contains(logDesc, "dibuat otomatis oleh jadwal berulang") {
		t.Fatalf("Activity log description tidak mencatat generator otomatis: %s", logDesc)
	}

	// 6. Dashboard menunjukkan jumlah jadwal aktif yang sesuai
	// Sesuai Section E & implementasi frontend: sumber data metrik aktif adalah GET /api/recurring-schedules
	reqSchList, _ := http.NewRequest(http.MethodGet, baseURL+"/api/recurring-schedules", nil)
	respSchList, err := client.Do(reqSchList)
	if err != nil {
		t.Fatalf("Gagal request recurring schedules: %v", err)
	}
	defer respSchList.Body.Close()
	var schList []*domain.RecurringSchedule
	_ = json.NewDecoder(respSchList.Body).Decode(&schList)
	activeCount := 0
	for _, s := range schList {
		if s.IsActive {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Fatalf("Dashboard metrics (active recurring schedules) harus 1, didapat: %d", activeCount)
	}
}

// TestBugfixBatch06_Scenarios_07_to_10_ImportSafetyAndScheduleDelete menguji skenario 7 s/d 10
func TestBugfixBatch06_Scenarios_07_to_10_ImportSafetyAndScheduleDelete(t *testing.T) {
	_, addr, db, _, _, client, _, taskSvc, _, _, cleanup := setupBatch06TestEnv(t)
	defer cleanup()

	baseURL := fmt.Sprintf("http://%s", addr)
	csrf := getCSRFTokenB06(t, client, baseURL)
	ctx := context.Background()

	// Buat 1 recurring schedule
	sch, err := taskSvc.CreateRecurringSchedule(ctx, domain.CreateRecurringScheduleInput{
		Title:         "Schedule Tetap",
		Category:      "General",
		Priority:      "normal",
		Frequency:     "monthly",
		IntervalValue: 1,
		StartDate:     "2026-10-01",
	})
	if err != nil {
		t.Fatalf("Gagal membuat schedule: %v", err)
	}

	// Jalankan generator sekali
	rep, err := taskSvc.TriggerRecurringWithReport(ctx, "2026-10-05")
	if err != nil || len(rep.CreatedItems) != 1 {
		t.Fatalf("Gagal trigger schedule: err=%v, rep=%+v", err, rep)
	}
	generatedItemID := rep.CreatedItems[0].ID

	// Catat pointer schedule sebelum impor
	schBefore, err := taskSvc.GetRecurringSchedule(ctx, sch.ID)
	if err != nil {
		t.Fatalf("Gagal membaca schedule: %v", err)
	}

	// 7 & 8. Import CSV tidak mengubah recurring_schedules dan tidak mengubah pointer next_run_date / last_generated_date
	csvContent := "Judul Pekerjaan,Tanggal Diterima,Pemberi Tugas,Prioritas,Status\n" +
		"Pekerjaan Impor 1,2026-10-15,Divisi Audit,Tinggi,Belum Mulai\n" +
		"Pekerjaan Impor 2,2026-10-16,Jadwal Rutin #1,Normal,Belum Mulai\n"

	respCommit, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrf,
		map[string]string{"duplicate_mode": "skip"}, "file", "test_import.csv", []byte(csvContent))
	if err != nil || (respCommit.StatusCode != http.StatusOK && respCommit.StatusCode != http.StatusCreated) {
		t.Fatalf("Gagal commit CSV: status=%v, err=%v", respCommit.StatusCode, err)
	}
	respCommit.Body.Close()

	// Verifikasi pointer recurring schedule tidak berubah
	schAfter, err := taskSvc.GetRecurringSchedule(ctx, sch.ID)
	if err != nil {
		t.Fatalf("Schedule hilang setelah impor: %v", err)
	}
	if schAfter.NextRunDate != schBefore.NextRunDate {
		t.Fatalf("next_run_date berubah akibat impor! Before=%s, After=%s", schBefore.NextRunDate, schAfter.NextRunDate)
	}
	if (schBefore.LastGeneratedDate == nil && schAfter.LastGeneratedDate != nil) ||
		(schBefore.LastGeneratedDate != nil && schAfter.LastGeneratedDate == nil) ||
		(schBefore.LastGeneratedDate != nil && *schBefore.LastGeneratedDate != *schAfter.LastGeneratedDate) {
		t.Fatalf("last_generated_date berubah akibat impor!")
	}

	// 9. Setelah impor, jadwal existing tetap ditampilkan
	allSchedules, err := taskSvc.ListRecurringSchedules(ctx)
	if err != nil || len(allSchedules) != 1 {
		t.Fatalf("Jadwal existing tidak ditampilkan dengan benar: len=%d, err=%v", len(allSchedules), err)
	}

	// 10. Menghapus jadwal secara sah tidak menghapus Work Item historis
	reqDel, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/recurring-schedules/%d", baseURL, sch.ID), nil)
	reqDel.Header.Set("X-CSRF-Token", csrf)
	reqDel.Header.Set("Origin", baseURL)
	respDel, err := client.Do(reqDel)
	if err != nil || respDel.StatusCode != http.StatusOK {
		t.Fatalf("Gagal menghapus schedule: %v", err)
	}
	respDel.Body.Close()

	// Pastikan Work Item historis hasil generate tetap ada di database
	var wiCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM work_items WHERE id = ?", generatedItemID).Scan(&wiCount); err != nil || wiCount != 1 {
		t.Fatalf("Work Item historis ikut terhapus saat jadwal dihapus: count=%d, err=%v", wiCount, err)
	}

	// Pastikan dashboard sekarang menunjukkan 0 jadwal aktif karena jadwal sah dihapus
	reqSchListAfter, _ := http.NewRequest(http.MethodGet, baseURL+"/api/recurring-schedules", nil)
	respSchListAfter, _ := client.Do(reqSchListAfter)
	var schListAfter []*domain.RecurringSchedule
	_ = json.NewDecoder(respSchListAfter.Body).Decode(&schListAfter)
	respSchListAfter.Body.Close()
	activeCountAfter := 0
	for _, s := range schListAfter {
		if s.IsActive {
			activeCountAfter++
		}
	}
	if activeCountAfter != 0 {
		t.Fatalf("Setelah jadwal dihapus, active_schedules harus 0, didapat: %d", activeCountAfter)
	}
}

// TestBugfixBatch06_Scenarios_11_to_15_CategoryProvenanceRegression menguji skenario 11 s/d 15
func TestBugfixBatch06_Scenarios_11_to_15_CategoryProvenanceRegression(t *testing.T) {
	_, addr, _, _, backupDir, client, workSvc, taskSvc, importSvc, backupSvc, cleanup := setupBatch06TestEnv(t)
	defer cleanup()

	baseURL := fmt.Sprintf("http://%s", addr)
	csrf := getCSRFTokenB06(t, client, baseURL)
	ctx := context.Background()

	// 11. Kategori nonaktif tetap menahan generator sesuai kebijakan
	cat, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name:  "Kategori Khusus",
		Color: "#ff0000",
	})
	if err != nil {
		t.Fatalf("Gagal buat kategori: %v", err)
	}

	sch, err := taskSvc.CreateRecurringSchedule(ctx, domain.CreateRecurringScheduleInput{
		Title:         "Tugas Kategori Khusus",
		Category:      cat.Name,
		Priority:      "normal",
		Frequency:     "daily",
		IntervalValue: 1,
		StartDate:     "2026-10-01",
	})
	if err != nil {
		t.Fatalf("Gagal buat schedule: %v", err)
	}

	// Nonaktifkan kategori
	falseVal := false
	_, err = taskSvc.UpdateCategory(ctx, cat.ID, domain.UpdateCategoryInput{
		Name:     cat.Name,
		IsActive: &falseVal,
	})
	if err != nil {
		t.Fatalf("Gagal deactive category: %v", err)
	}

	// Jalankan generator: harus ditahan / di-skip
	repBlocked, err := taskSvc.TriggerRecurringWithReport(ctx, "2026-10-02")
	if err != nil {
		t.Fatalf("Trigger error: %v", err)
	}
	if len(repBlocked.CreatedItems) != 0 {
		t.Fatalf("Generator harus menahan tugas saat kategori nonaktif, tapi menghasilkan %d item", len(repBlocked.CreatedItems))
	}
	if len(repBlocked.SkippedDetails) == 0 {
		t.Fatalf("Harus ada catatan skip detail saat kategori nonaktif")
	}

	// 12. Kategori diaktifkan kembali dan generator tetap idempotent
	trueVal := true
	_, err = taskSvc.UpdateCategory(ctx, cat.ID, domain.UpdateCategoryInput{
		Name:     cat.Name,
		IsActive: &trueVal,
	})
	if err != nil {
		t.Fatalf("Gagal re-activate category: %v", err)
	}

	repAllowed, err := taskSvc.TriggerRecurringWithReport(ctx, "2026-10-02")
	if err != nil {
		t.Fatalf("Trigger setelah re-aktif error: %v", err)
	}
	if len(repAllowed.CreatedItems) != 1 {
		t.Fatalf("Generator harus menghasilkan 1 item setelah kategori aktif kembali, didapat: %d", len(repAllowed.CreatedItems))
	}

	// Idempotent test
	repIdempotent, err := taskSvc.TriggerRecurringWithReport(ctx, "2026-10-02")
	if err != nil || len(repIdempotent.CreatedItems) != 0 {
		t.Fatalf("Generator tidak idempotent setelah re-aktif: len=%d, err=%v", len(repIdempotent.CreatedItems), err)
	}

	// 13. Provenance Manual, Template, Import, Recurring ditampilkan sesuai sumber asli
	// 13a: Manual
	itemManual, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Form Manual",
		Category:    "General",
		Priority:    "normal",
		ReceivedOn:  "2026-10-10",
		RequestedBy: "Direksi",
	})
	if err != nil || itemManual.Provenance != domain.ProvenanceManual {
		t.Fatalf("Manual provenance mismatch: %+v, err=%v", itemManual, err)
	}

	// 13b: Template
	tpl, err := taskSvc.CreateTemplate(ctx, domain.CreateWorkTemplateInput{
		Name:           "Template SOP",
		TitlePattern:   "Pekerjaan SOP Bulanan",
		Category:       "General",
		Priority:       "normal",
		ChecklistItems: []string{"Langkah 1", "Langkah 2"},
	})
	if err != nil {
		t.Fatalf("Gagal membuat template: %v", err)
	}
	itemTpl, err := taskSvc.ApplyTemplateWithInput(ctx, tpl.ID, domain.ApplyTemplateInput{
		ReceivedOn: "2026-10-12",
	})
	if err != nil || itemTpl.Provenance != domain.ProvenanceTemplate {
		t.Fatalf("Template provenance mismatch: %+v, err=%v", itemTpl, err)
	}

	// 13c: Import
	// Impor data CSV yang di barisnya memiliki requested_by = 'Jadwal Rutin #1' dan 'Template: FOLRE'
	csvRows := "Judul Pekerjaan,Tanggal Diterima,Pemberi Tugas,Prioritas,Status\n" +
		"Baris Impor 1,2026-10-15,Jadwal Rutin #1,Tinggi,Belum Mulai\n" +
		"Baris Impor 2,2026-10-16,Template: FOLRE,Normal,Belum Mulai\n"

	respCom, err := sendMultipartRequest(t, client, baseURL+"/api/import/commit", http.MethodPost, csrf,
		map[string]string{"duplicate_mode": "skip"}, "file", "test_prov.csv", []byte(csvRows))
	if err != nil || (respCom.StatusCode != http.StatusOK && respCom.StatusCode != http.StatusCreated) {
		t.Fatalf("Commit CSV gagal: status=%v, err=%v", respCom.StatusCode, err)
	}
	respCom.Body.Close()

	// Ambil semua item dari API
	reqList, _ := http.NewRequest(http.MethodGet, baseURL+"/api/work-items", nil)
	respList, _ := client.Do(reqList)
	var allItems []*domain.WorkItem
	_ = json.NewDecoder(respList.Body).Decode(&allItems)
	respList.Body.Close()

	foundImport1 := false
	foundImport2 := false
	for _, item := range allItems {
		if item.Title == "Baris Impor 1" {
			foundImport1 = true
			if item.Provenance != domain.ProvenanceImport {
				t.Fatalf("Baris Impor 1 meski requested_by='Jadwal Rutin #1' harus ber-provenance 'import', didapat: %s", item.Provenance)
			}
		}
		if item.Title == "Baris Impor 2" {
			foundImport2 = true
			if item.Provenance != domain.ProvenanceImport {
				t.Fatalf("Baris Impor 2 meski requested_by='Template: FOLRE' harus ber-provenance 'import', didapat: %s", item.Provenance)
			}
		}
	}
	if !foundImport1 || !foundImport2 {
		t.Fatalf("Item impor tidak ditemukan di list work items")
	}

	// 14. Riwayat impor tetap muncul dan tidak berubah
	batches, err := importSvc.ListBatches(ctx, 10, 0)
	if err != nil || len(batches) == 0 {
		t.Fatalf("Riwayat impor tidak muncul: len=%d, err=%v", len(batches), err)
	}

	// 15. Category Management, Template, dan Backup/Restore tetap PASS
	backupItem, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Backup gagal: %v", err)
	}
	if backupItem.Filename == "" {
		t.Fatalf("Nama backup kosong")
	}

	// Verifikasi keutuhan skema dan data backup
	backupFilePath := filepath.Join(backupDir, backupItem.Filename)
	checkDB, err := database.OpenDB(backupFilePath)
	if err != nil {
		t.Fatalf("Gagal membuka DB backup: %v", err)
	}
	defer checkDB.Close()
	if err := database.CheckIntegrity(ctx, checkDB); err != nil {
		t.Fatalf("Integritas backup tidak lolos: %v", err)
	}
	var backupSchCount int
	_ = checkDB.QueryRow("SELECT COUNT(*) FROM recurring_schedules WHERE id = ?", sch.ID).Scan(&backupSchCount)
	if backupSchCount != 1 {
		t.Fatalf("Jadwal berulang tidak masuk dalam snapshot backup: count=%d", backupSchCount)
	}
}

// TestMigration005_ProvenanceBackfill menguji migrasi 005 pada database v4 existing
func TestMigration005_ProvenanceBackfill(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "migration_v4_to_v5_test.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	// Buat skema hingga migrasi 004 secara manual
	_, err = db.ExecContext(ctx, `
	CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	INSERT INTO schema_migrations (version) VALUES (1), (2), (3), (4);

	CREATE TABLE work_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_code TEXT NOT NULL UNIQUE,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT 'General',
		requested_by TEXT NOT NULL DEFAULT '',
		received_on TEXT NOT NULL,
		due_on TEXT,
		priority TEXT NOT NULL DEFAULT 'normal',
		status TEXT NOT NULL DEFAULT 'not_started',
		progress_percentage INTEGER NOT NULL DEFAULT 0,
		completed_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		archived_at TEXT
	);

	CREATE TABLE activity_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_item_id INTEGER NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
		entry_type TEXT NOT NULL,
		description TEXT NOT NULL,
		occurred_at TEXT NOT NULL,
		recorded_at TEXT NOT NULL
	);

	CREATE TABLE import_batches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		batch_code TEXT NOT NULL UNIQUE,
		file_name TEXT NOT NULL,
		status TEXT NOT NULL,
		total_rows INTEGER NOT NULL DEFAULT 0,
		imported_rows INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	);

	CREATE TABLE import_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		batch_id INTEGER NOT NULL REFERENCES import_batches(id) ON DELETE CASCADE,
		row_number INTEGER NOT NULL,
		work_item_id INTEGER REFERENCES work_items(id) ON DELETE SET NULL,
		status TEXT NOT NULL
	);
	`)
	if err != nil {
		t.Fatalf("Gagal setup skema dummy v4: %v", err)
	}

	// Insert data representatif seperti di UAT:
	// WI 1: manual
	// WI 2: template
	// WI 3: recurring
	// WI 4: imported (requested_by = Template: FOLRE)
	// WI 5: imported (requested_by = Jadwal Rutin #1)
	// WI 6: unknown (tidak ada activity log atau import item)
	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `
	INSERT INTO work_items (id, work_code, title, requested_by, received_on, created_at, updated_at) VALUES
		(1, 'CMP-2026-000001', 'Manual Audit', 'Direksi', '2026-10-01', ?, ?),
		(2, 'CMP-2026-000002', 'Template Task', 'Template: SOP', '2026-10-02', ?, ?),
		(3, 'CMP-2026-000003', 'Recurring Task', 'Jadwal Rutin #1', '2026-10-03', ?, ?),
		(4, 'CMP-2026-000004', 'Imported Tpl', 'Template: FOLRE', '2026-10-04', ?, ?),
		(5, 'CMP-2026-000005', 'TEST 40', 'Jadwal Rutin #1', '2026-10-05', ?, ?),
		(6, 'CMP-2026-000006', 'Legacy Task', 'Komite', '2026-10-06', ?, ?);

	INSERT INTO activity_logs (work_item_id, entry_type, description, occurred_at, recorded_at) VALUES
		(1, 'created', 'Pekerjaan dibuat dengan kode CMP-2026-000001 (Manual Audit)', ?, ?),
		(2, 'created', 'Pekerjaan dibuat dari template ''Template SOP'' dengan kode CMP-2026-000002 (2 subtask)', ?, ?),
		(3, 'created', 'Pekerjaan dibuat otomatis oleh jadwal berulang ''Rutin Bulanan'' untuk periode 2026-10-03', ?, ?),
		(4, 'created', 'Pekerjaan dibuat melalui impor batch ''IMP-001'' (baris 4)', ?, ?),
		(5, 'created', 'Pekerjaan dibuat melalui impor batch ''IMP-001'' (baris 5)', ?, ?);

	INSERT INTO import_batches (id, batch_code, file_name, status, created_at) VALUES
		(1, 'IMP-001', 'test.csv', 'completed', ?);

	INSERT INTO import_items (batch_id, row_number, work_item_id, status) VALUES
		(1, 4, 4, 'inserted'),
		(1, 5, 5, 'inserted');
	`, nowStr, nowStr, nowStr, nowStr, nowStr, nowStr, nowStr, nowStr, nowStr, nowStr, nowStr, nowStr,
		nowStr, nowStr, nowStr, nowStr, nowStr, nowStr, nowStr, nowStr, nowStr, nowStr,
		nowStr)
	if err != nil {
		t.Fatalf("Gagal insert data dummy v4: %v", err)
	}

	// Jalankan Migrate (akan menjalankan 005_work_item_provenance.sql)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("Gagal menjalankan migrasi 005: %v", err)
	}

	// Periksa versi skema
	var maxVersion int
	_ = db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&maxVersion)
	if maxVersion < 5 {
		t.Fatalf("Versi skema harus minimal 5, didapat: %d", maxVersion)
	}

	// Verifikasi hasil backfill provenance
	type provCheck struct {
		id       int64
		expected string
	}
	checks := []provCheck{
		{1, "manual"},
		{2, "template"},
		{3, "recurring"},
		{4, "import"},
		{5, "import"},
		{6, "unknown"},
	}

	for _, c := range checks {
		var actual string
		err := db.QueryRow("SELECT provenance FROM work_items WHERE id = ?", c.id).Scan(&actual)
		if err != nil {
			t.Fatalf("Gagal query provenance item %d: %v", c.id, err)
		}
		if actual != c.expected {
			t.Fatalf("Item %d: expected provenance '%s', got '%s'", c.id, c.expected, actual)
		}
	}
}
