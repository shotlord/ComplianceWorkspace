package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
)

// setupCleanupTestEnvironment menyiapkan database dan layanan isolasi di direktori temporer
func setupCleanupTestEnvironment(t *testing.T) (
	*sql.DB,
	string,
	string,
	*service.WorkItemService,
	*service.TaskManagementService,
	*service.BackupService,
	func(),
) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka database: %v", err)
	}

	if err := database.Migrate(ctx, db); err != nil {
		_ = db.Close()
		t.Fatalf("Gagal migrasi database: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(db)
	actRepo := repository.NewActivityLogRepository(db)
	subtaskRepo := repository.NewSubtaskRepository(db)
	catRepo := repository.NewCategoryRepository(db)
	tplRepo := repository.NewWorkTemplateRepository(db)
	recRepo := repository.NewRecurringRepository(db)

	workSvc := service.NewWorkItemService(db, workRepo, actRepo)
	taskMgmtSvc := service.NewTaskManagementService(db, workRepo, actRepo, subtaskRepo, catRepo, tplRepo, recRepo)
	backupSvc := service.NewBackupService(workSvc, taskMgmtSvc, nil, dbPath, backupDir)

	cleanup := func() {
		if curDB := workSvc.GetDB(); curDB != nil {
			_ = curDB.Close()
		}
	}

	return db, dbPath, backupDir, workSvc, taskMgmtSvc, backupSvc, cleanup
}

func TestWorkItemSafePermanentDelete(t *testing.T) {
	db, _, _, workSvc, taskMgmtSvc, _, cleanup := setupCleanupTestEnvironment(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Buat pekerjaan baru
	item, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Audit Investigasi",
		Description: "Uji penghapusan permanen",
		Category:    "Investigasi",
		RequestedBy: "Divisi Audit",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityHigh,
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan: %v", err)
	}
	originalCode := item.WorkCode

	// Tambahkan subtask
	subtask, err := taskMgmtSvc.AddSubtask(ctx, item.ID, domain.CreateSubtaskInput{
		Title: "Kumpulkan bukti transaksi",
	})
	if err != nil {
		t.Fatalf("Gagal membuat subtask: %v", err)
	}

	// Tambahkan aktivitas
	_, err = workSvc.AddActivity(ctx, item.ID, domain.AddActivityInput{
		Description: "Melakukan verifikasi bukti dokumen",
	})
	if err != nil {
		t.Fatalf("Gagal menambah aktivitas: %v", err)
	}

	// 2. Coba hapus saat status belum diarsipkan (harus ditolak)
	_, err = workSvc.PermanentDeleteWorkItem(ctx, item.ID, item.WorkCode)
	if err == nil || !strings.Contains(err.Error(), "harus diarsipkan terlebih dahulu") {
		t.Fatalf("Penghapusan pekerjaan aktif seharusnya ditolak, got err: %v", err)
	}

	// Coba ambil preview saat belum diarsipkan (harus ditolak)
	_, err = workSvc.GetDeletePreview(ctx, item.ID)
	if err == nil || !strings.Contains(err.Error(), "harus diarsipkan terlebih dahulu") {
		t.Fatalf("Preview penghapusan pekerjaan aktif seharusnya ditolak, got err: %v", err)
	}

	// 3. Arsipkan pekerjaan
	archivedItem, err := workSvc.ArchiveWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("Gagal mengarsipkan pekerjaan: %v", err)
	}
	if archivedItem.ArchivedAt == nil {
		t.Fatalf("Pekerjaan belum berstatus diarsipkan")
	}

	// 4. Periksa Pre-delete Inspection
	preview, err := workSvc.GetDeletePreview(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetDeletePreview gagal: %v", err)
	}
	if preview.WorkCode != originalCode {
		t.Errorf("Expected preview work_code %s, got %s", originalCode, preview.WorkCode)
	}
	if preview.SubtaskCount != 1 {
		t.Errorf("Expected preview subtask_count 1, got %d", preview.SubtaskCount)
	}
	if preview.ActivityLogCount < 2 { // create, log, archive
		t.Errorf("Expected preview activity_log_count >= 2, got %d", preview.ActivityLogCount)
	}
	if preview.WarningMessage == "" {
		t.Errorf("WarningMessage tidak boleh kosong")
	}

	// 5. Coba hapus dengan kode konfirmasi yang salah
	_, err = workSvc.PermanentDeleteWorkItem(ctx, item.ID, "CMP-9999-999999")
	if err == nil || !strings.Contains(err.Error(), "kode konfirmasi tidak cocok") {
		t.Fatalf("Penghapusan dengan kode konfirmasi salah harus ditolak, got: %v", err)
	}

	// 6. Eksekusi Hapus Permanen dengan kode konfirmasi benar
	deletedItem, err := workSvc.PermanentDeleteWorkItem(ctx, item.ID, originalCode)
	if err != nil {
		t.Fatalf("PermanentDeleteWorkItem gagal: %v", err)
	}
	if deletedItem.DeletedAt == nil {
		t.Fatalf("DeletedAt harus terisi setelah penghapusan permanen")
	}

	// 7. Verifikasi tombstone pada database langsung
	var deletedAtStr sql.NullString
	err = db.QueryRow("SELECT deleted_at FROM work_items WHERE id = ?", item.ID).Scan(&deletedAtStr)
	if err != nil {
		t.Fatalf("Gagal query deleted_at dari work_items: %v", err)
	}
	if !deletedAtStr.Valid || deletedAtStr.String == "" {
		t.Fatalf("Kolom deleted_at di tabel work_items harus terisi (tombstone)")
	}

	// 8. Verifikasi pekerjaan tidak muncul di ListWorkItems aktif maupun arsip
	activeList, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: false})
	if err != nil {
		t.Fatalf("ListWorkItems active gagal: %v", err)
	}
	for _, it := range activeList {
		if it.ID == item.ID {
			t.Fatalf("Pekerjaan yang dihapus permanen masih muncul di daftar aktif")
		}
	}

	archivedList, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: true})
	if err != nil {
		t.Fatalf("ListWorkItems archived gagal: %v", err)
	}
	for _, it := range archivedList {
		if it.ID == item.ID {
			t.Fatalf("Pekerjaan yang dihapus permanen masih muncul di daftar arsip")
		}
	}

	// 9. Verifikasi GetWorkItem mengembalikan not found / error
	_, err = workSvc.GetWorkItem(ctx, item.ID)
	if err == nil {
		t.Fatalf("GetWorkItem untuk pekerjaan yang terhapus permanen seharusnya error")
	}

	// 10. Verifikasi subtask terhapus
	var subtaskCount int
	err = db.QueryRow("SELECT COUNT(*) FROM subtasks WHERE id = ?", subtask.ID).Scan(&subtaskCount)
	if err != nil {
		t.Fatalf("Gagal query subtasks: %v", err)
	}
	if subtaskCount != 0 {
		t.Fatalf("Subtask seharusnya dihapus bersama pekerjaan, count: %d", subtaskCount)
	}

	// 11. Verifikasi audit trail dipertahankan
	var actCount int
	err = db.QueryRow("SELECT COUNT(*) FROM activity_logs WHERE work_item_id = ?", item.ID).Scan(&actCount)
	if err != nil {
		t.Fatalf("Gagal query activity_logs: %v", err)
	}
	if actCount < 3 {
		t.Fatalf("Activity logs harus tetap dipertahankan untuk audit trail, count: %d", actCount)
	}

	// Periksa log aktivitas terakhir bertipe activity
	var lastLogDesc string
	err = db.QueryRow("SELECT description FROM activity_logs WHERE work_item_id = ? ORDER BY id DESC LIMIT 1", item.ID).Scan(&lastLogDesc)
	if err != nil {
		t.Fatalf("Gagal query log terakhir: %v", err)
	}
	if !strings.Contains(lastLogDesc, "dihapus secara permanen") {
		t.Errorf("Deskripsi log terakhir harus mencatat penghapusan permanen, got: %s", lastLogDesc)
	}

	// 12. Verifikasi nomor kode CMP TIDAK digunakan kembali (non-reuse)
	nextItem, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Baru Setelah Hapus",
		Category:    "Umum",
		RequestedBy: "Manajemen",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan baru: %v", err)
	}
	if nextItem.WorkCode == originalCode {
		t.Fatalf("Kode CMP yang sudah digunakan tidak boleh digunakan kembali! Original: %s, Next: %s", originalCode, nextItem.WorkCode)
	}
}

func TestBackupLifecycleManagement(t *testing.T) {
	_, dbPath, backupDir, workSvc, _, backupSvc, cleanup := setupCleanupTestEnvironment(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Buat 3 cadangan awal
	b1, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Gagal create backup 1: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	b2, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Gagal create backup 2: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	b3, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Gagal create backup 3: %v", err)
	}

	// 2. Periksa ListBackups menampilkan schema_version dan status valid
	list, err := backupSvc.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups gagal: %v", err)
	}
	if len(list) < 3 {
		t.Fatalf("Expected >= 3 backups, got %d", len(list))
	}
	for _, b := range list {
		if b.SchemaVersion != 6 {
			t.Errorf("Expected SchemaVersion 6, got %d for file %s", b.SchemaVersion, b.Filename)
		}
		if b.Status != "valid" {
			t.Errorf("Expected status valid, got %s for file %s", b.Status, b.Filename)
		}
	}

	// 3. Uji Keamanan: Path Traversal ditolak
	traversalFiles := []string{
		"../data/compliance.db",
		"..\\data\\compliance.db",
		"sub/../../compliance.db",
		"file.txt",
	}
	for _, tf := range traversalFiles {
		err := backupSvc.DeleteBackup(ctx, tf)
		if err == nil {
			t.Errorf("Penghapusan file dengan traversal '%s' harus ditolak", tf)
		}
	}

	// Coba hapus file database aktif (harus ditolak)
	err = backupSvc.DeleteBackup(ctx, filepath.Base(dbPath))
	if err == nil {
		t.Errorf("Penghapusan database aktif harus ditolak")
	}

	// 4. Hapus satu berkas cadangan (b1) -> sukses, tersisa 2
	if err := backupSvc.DeleteBackup(ctx, b1.Filename); err != nil {
		t.Fatalf("DeleteBackup b1 gagal: %v", err)
	}

	// 5. Hapus berkas kedua (b2) -> sukses, tersisa 1
	if err := backupSvc.DeleteBackup(ctx, b2.Filename); err != nil {
		t.Fatalf("DeleteBackup b2 gagal: %v", err)
	}

	// 6. Aturan Retensi Minimal: Hapus berkas terakhir (b3) HARUS DITOLAK
	err = backupSvc.DeleteBackup(ctx, b3.Filename)
	if err == nil || !strings.Contains(err.Error(), "setidaknya satu cadangan valid") {
		t.Fatalf("Penghapusan berkas cadangan terakhir harus ditolak oleh aturan retensi, got: %v", err)
	}

	// 7. Uji Batch Delete
	// Tambah 3 backup lagi (sehingga ada 4 total)
	nb1, _ := backupSvc.CreateBackup(ctx)
	time.Sleep(50 * time.Millisecond)
	nb2, _ := backupSvc.CreateBackup(ctx)
	time.Sleep(50 * time.Millisecond)
	nb3, _ := backupSvc.CreateBackup(ctx)

	// Batch delete yang menyisakan 0 valid backup -> harus ditolak
	all4 := []string{b3.Filename, nb1.Filename, nb2.Filename, nb3.Filename}
	_, err = backupSvc.DeleteBackups(ctx, all4)
	if err == nil || !strings.Contains(err.Error(), "setidaknya satu cadangan valid") {
		t.Fatalf("Batch delete yang menghapus seluruh cadangan harus ditolak, got: %v", err)
	}

	// Batch delete 2 file (menyisakan 2) -> harus sukses
	res, err := backupSvc.DeleteBackups(ctx, []string{nb1.Filename, nb2.Filename})
	if err != nil {
		t.Fatalf("Batch delete 2 file gagal: %v", err)
	}
	if res.DeletedCount != 2 {
		t.Errorf("Expected DeletedCount 2, got %d", res.DeletedCount)
	}
	if res.RemainingCount != 2 {
		t.Errorf("Expected RemainingCount 2, got %d", res.RemainingCount)
	}

	_ = workSvc
	_ = backupDir
}

func TestRestoreSafetyGuardAndRollback(t *testing.T) {
	_, dbPath, backupDir, workSvc, _, backupSvc, cleanup := setupCleanupTestEnvironment(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Buat data dan buat backup
	_, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Data Sebelum Backup",
		Category:    "Umum",
		RequestedBy: "User A",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("Gagal membuat work item: %v", err)
	}

	backup1, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("CreateBackup gagal: %v", err)
	}

	// 2. Tambah data baru di database aktif
	_, err = workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Data Baru Pasca Backup",
		Category:    "Umum",
		RequestedBy: "User B",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityHigh,
	})
	if err != nil {
		t.Fatalf("Gagal membuat work item kedua: %v", err)
	}

	// 3. Inspeksi backup1: periksa records_lost_warning
	inspection, err := backupSvc.InspectBackup(ctx, backup1.Filename)
	if err != nil {
		t.Fatalf("InspectBackup gagal: %v", err)
	}
	if !inspection.IsCompatible {
		t.Fatalf("Backup1 seharusnya kompatibel")
	}
	if inspection.SchemaVersion != 6 {
		t.Errorf("Expected SchemaVersion 6, got %d", inspection.SchemaVersion)
	}
	if !inspection.Differences.RecordsLostWarning {
		t.Errorf("Expected RecordsLostWarning true karena database aktif memiliki 2 item vs 1 di cadangan")
	}
	if len(inspection.Warnings) == 0 {
		t.Errorf("Expected warnings mengenai potensi kehilangan data")
	}

	// 4. Uji Penolakan Cadangan dengan Skema Lebih Tinggi dari MaxSupported (v7)
	futureBackupName := "compliance_backup_v7_future.db"
	futurePath := filepath.Join(backupDir, futureBackupName)
	// Salin backup1 ke futurePath, lalu ubah schema_migrations version menjadi 7
	data, err := os.ReadFile(filepath.Join(backupDir, backup1.Filename))
	if err != nil {
		t.Fatalf("Gagal membaca backup1: %v", err)
	}
	if err := os.WriteFile(futurePath, data, 0644); err != nil {
		t.Fatalf("Gagal menulis future backup: %v", err)
	}
	fDB, err := database.OpenDB(futurePath)
	if err != nil {
		t.Fatalf("Gagal membuka future db: %v", err)
	}
	_, _ = fDB.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (7, strftime('%Y-%m-%dT%H:%M:%fZ','now'));")
	_ = fDB.Close()

	// Inspeksi future backup
	futureInsp, err := backupSvc.InspectBackup(ctx, futureBackupName)
	if err != nil {
		t.Fatalf("Inspect future backup gagal: %v", err)
	}
	if futureInsp.IsCompatible {
		t.Errorf("Cadangan versi skema 7 seharusnya tidak kompatibel")
	}
	if !futureInsp.Differences.SchemaUnsupported {
		t.Errorf("SchemaUnsupported harus bernilai true untuk v7")
	}

	// Coba pulihkan cadangan v7 (harus ditolak)
	err = backupSvc.RestoreBackup(ctx, futureBackupName)
	if err == nil || !strings.Contains(err.Error(), "lebih baru dari versi yang didukung") {
		t.Fatalf("Pemulihan cadangan v7 seharusnya ditolak, got: %v", err)
	}

	// 5. Uji Penolakan Cadangan Korup
	corruptBackupName := "compliance_backup_corrupted.db"
	corruptPath := filepath.Join(backupDir, corruptBackupName)
	if err := os.WriteFile(corruptPath, []byte("NOT A SQLITE FILE DUMMY CORRUPT DATA"), 0644); err != nil {
		t.Fatalf("Gagal menulis corrupt backup: %v", err)
	}

	corruptInsp, err := backupSvc.InspectBackup(ctx, corruptBackupName)
	if err != nil {
		t.Fatalf("Inspect corrupt backup gagal: %v", err)
	}
	if corruptInsp.IsCompatible {
		t.Errorf("Cadangan korup seharusnya tidak kompatibel")
	}

	err = backupSvc.RestoreBackup(ctx, corruptBackupName)
	if err == nil {
		t.Fatalf("Pemulihan cadangan korup seharusnya gagal")
	}

	// 6. Uji Pemulihan Berhasil (Restore backup1)
	if err := backupSvc.RestoreBackup(ctx, backup1.Filename); err != nil {
		t.Fatalf("Restore backup1 gagal: %v", err)
	}

	// Verifikasi database telah kembali ke snapshot backup1 (hanya 1 item)
	restoredItems, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	if err != nil {
		t.Fatalf("ListWorkItems setelah restore gagal: %v", err)
	}
	if len(restoredItems) != 1 {
		t.Errorf("Expected 1 work item setelah restore backup1, got %d", len(restoredItems))
	}
	if restoredItems[0].Title != "Data Sebelum Backup" {
		t.Errorf("Data yang dipulihkan tidak sesuai: %s", restoredItems[0].Title)
	}

	_ = dbPath
}

func TestHTTPAPIWorkDeleteAndBackupLifecycle(t *testing.T) {
	_, dbPath, backupDir, workSvc, taskMgmtSvc, backupSvc, cleanup := setupCleanupTestEnvironment(t)
	defer cleanup()
	ctx := context.Background()

	// Siapkan test server
	srv, err := server.NewServer("127.0.0.1:0", workSvc, backupSvc, nil, taskMgmtSvc, nil)
	if err != nil {
		t.Fatalf("Gagal membuat server: %v", err)
	}
	defer srv.Shutdown(ctx)
	go func() {
		_ = srv.Start()
	}()
	time.Sleep(50 * time.Millisecond)

	// Buat item dan arsipkan
	item, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan HTTP Delete Test",
		Category:    "Kepatuhan",
		RequestedBy: "Auditor",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityLow,
	})
	if err != nil {
		t.Fatalf("Gagal membuat work item: %v", err)
	}
	_, _ = workSvc.ArchiveWorkItem(ctx, item.ID)

	// Buat 2 backup
	b1, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("CreateBackup 1 gagal: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	b2, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("CreateBackup 2 gagal: %v", err)
	}

	baseURL := fmt.Sprintf("http://%s", srv.Addr())

	// 1. GET /api/work-items/{id}/delete-preview
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/work-items/%d/delete-preview", baseURL, item.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request delete-preview gagal: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for delete-preview, got %d", resp.StatusCode)
	}
	var preview domain.WorkItemDeletePreview
	_ = json.NewDecoder(resp.Body).Decode(&preview)
	resp.Body.Close()
	if preview.WorkCode != item.WorkCode {
		t.Errorf("Preview work code mismatch: %s vs %s", preview.WorkCode, item.WorkCode)
	}

	// 2. DELETE /api/work-items/{id} dengan CSRF
	// Ambil token CSRF
	csrfReq, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/csrf-token", baseURL), nil)
	csrfResp, err := http.DefaultClient.Do(csrfReq)
	if err != nil {
		t.Fatalf("Gagal mengambil token CSRF: %v", err)
	}
	var csrfData struct {
		Token string `json:"csrf_token"`
	}
	_ = json.NewDecoder(csrfResp.Body).Decode(&csrfData)
	var csrfCookie *http.Cookie
	for _, c := range csrfResp.Cookies() {
		if c.Name == server.CSRFCookieName {
			csrfCookie = c
			break
		}
	}
	csrfResp.Body.Close()

	delPayload, _ := json.Marshal(map[string]string{"confirm_code": item.WorkCode})
	delReq, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/work-items/%d", baseURL, item.ID), bytes.NewReader(delPayload))
	delReq.Header.Set("Content-Type", "application/json")
	delReq.Header.Set("X-CSRF-Token", csrfData.Token)
	if csrfCookie != nil {
		delReq.AddCookie(csrfCookie)
	}
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("HTTP DELETE work item gagal: %v", err)
	}
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for DELETE work item, got %d", delResp.StatusCode)
	}
	delResp.Body.Close()

	// 3. GET /api/backup/inspect?filename=...
	inspectReq, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/backup/inspect?filename=%s", baseURL, b1.Filename), nil)
	inspectResp, err := http.DefaultClient.Do(inspectReq)
	if err != nil {
		t.Fatalf("HTTP inspect request gagal: %v", err)
	}
	if inspectResp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for inspect, got %d", inspectResp.StatusCode)
	}
	var inspResult domain.RestoreInspectionResult
	_ = json.NewDecoder(inspectResp.Body).Decode(&inspResult)
	inspectResp.Body.Close()
	if inspResult.SchemaVersion != 6 {
		t.Errorf("Expected inspect SchemaVersion 6, got %d", inspResult.SchemaVersion)
	}

	// 4. POST /api/backup/delete-batch (coba hapus kedua backup -> ditolak oleh aturan retensi)
	batchPayload, _ := json.Marshal(map[string][]string{"filenames": {b1.Filename, b2.Filename}})
	batchReq, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/backup/delete-batch", baseURL), bytes.NewReader(batchPayload))
	batchReq.Header.Set("Content-Type", "application/json")
	batchReq.Header.Set("X-CSRF-Token", csrfData.Token)
	if csrfCookie != nil {
		batchReq.AddCookie(csrfCookie)
	}
	batchResp, err := http.DefaultClient.Do(batchReq)
	if err != nil {
		t.Fatalf("HTTP batch delete request gagal: %v", err)
	}
	if batchResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Expected 400 Bad Request for batch delete violating retention, got %d", batchResp.StatusCode)
	}
	batchResp.Body.Close()

	// 5. DELETE /api/backup/{filename} (hapus b1 -> sukses, b2 tersisa)
	delB1Req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/backup/%s", baseURL, b1.Filename), nil)
	delB1Req.Header.Set("X-CSRF-Token", csrfData.Token)
	if csrfCookie != nil {
		delB1Req.AddCookie(csrfCookie)
	}
	delB1Resp, err := http.DefaultClient.Do(delB1Req)
	if err != nil {
		t.Fatalf("HTTP DELETE backup gagal: %v", err)
	}
	if delB1Resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for DELETE backup, got %d", delB1Resp.StatusCode)
	}
	delB1Resp.Body.Close()

	_ = dbPath
	_ = backupDir
}
