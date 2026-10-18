package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"compliance-workspace/internal/database"
	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
	"compliance-workspace/internal/server"
	"compliance-workspace/internal/service"
)

// setupBatch08Environment menyiapkan seluruh service dan repository untuk pengujian regresi Batch 08
func setupBatch08Environment(t *testing.T) (
	*sql.DB,
	string,
	string,
	*service.WorkItemService,
	*service.TaskManagementService,
	*service.BackupService,
	*service.ImportService,
	*server.Server,
	string,
	func(),
) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	// Pastikan direktori backup dibuat terlebih dahulu
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("Gagal membuat direktori backup: %v", err)
	}

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
	importRepo := repository.NewImportRepository(db)

	workSvc := service.NewWorkItemService(db, workRepo, actRepo)
	taskMgmtSvc := service.NewTaskManagementService(db, workRepo, actRepo, subtaskRepo, catRepo, tplRepo, recRepo)
	backupSvc := service.NewBackupService(workSvc, taskMgmtSvc, nil, dbPath, backupDir)
	importSvc := service.NewImportService(db, importRepo, workRepo, actRepo, catRepo, backupSvc)

	srv, err := server.NewServer("127.0.0.1:0", workSvc, backupSvc, nil, taskMgmtSvc, importSvc)
	if err != nil {
		_ = db.Close()
		t.Fatalf("Gagal membuat server: %v", err)
	}

	go func() {
		_ = srv.Start()
	}()
	time.Sleep(50 * time.Millisecond)

	baseURL := fmt.Sprintf("http://%s", srv.Addr())

	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		if curDB := workSvc.GetDB(); curDB != nil {
			_ = curDB.Close()
		}
	}

	return db, dbPath, backupDir, workSvc, taskMgmtSvc, backupSvc, importSvc, srv, baseURL, cleanup
}

func getAuthorizedCSRFClient(t *testing.T, baseURL string) (*http.Client, string, *http.Cookie) {
	client := &http.Client{Timeout: 5 * time.Second}
	respToken, err := client.Get(baseURL + "/api/csrf-token")
	if err != nil {
		t.Fatalf("Gagal mengambil token CSRF: %v", err)
	}
	defer respToken.Body.Close()

	var tokenBody map[string]string
	_ = json.NewDecoder(respToken.Body).Decode(&tokenBody)
	csrfToken := tokenBody["csrf_token"]

	var csrfCookie *http.Cookie
	for _, c := range respToken.Cookies() {
		if c.Name == server.CSRFCookieName {
			csrfCookie = c
			break
		}
	}

	return client, csrfToken, csrfCookie
}

// 1. Permintaan delete-preview pada pekerjaan aktif (belum diarsipkan) menghasilkan HTTP 400
func TestBatch08_TestCase01_DeletePreviewActiveItem_Fails400(t *testing.T) {
	_, _, _, workSvc, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	item, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Masih Aktif",
		Category:    "Audit",
		RequestedBy: "Direksi",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan: %v", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/work-items/%d/delete-preview", baseURL, item.ID), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Diharapkan status 400 untuk pekerjaan aktif, tetapi mendapat %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("diarsipkan terlebih dahulu")) {
		t.Fatalf("Pesan error tidak memuat keharusan arsip: %s", string(body))
	}
}

// 2. Permintaan delete-preview pada pekerjaan berstatus diarsipkan menghasilkan HTTP 200
func TestBatch08_TestCase02_DeletePreviewArchivedItem_Success200(t *testing.T) {
	_, _, _, workSvc, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	item, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Diarsipkan",
		Category:    "Kepatuhan",
		RequestedBy: "Legal",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan: %v", err)
	}
	if _, err := workSvc.ArchiveWorkItem(ctx, item.ID); err != nil {
		t.Fatalf("Gagal mengarsipkan pekerjaan: %v", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/work-items/%d/delete-preview", baseURL, item.ID), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Diharapkan status 200, tetapi mendapat %d: %s", resp.StatusCode, string(body))
	}
	var preview domain.WorkItemDeletePreview
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		t.Fatalf("Gagal decode respons: %v", err)
	}
	if preview.ID != item.ID || preview.WorkCode != item.WorkCode {
		t.Fatalf("Data preview tidak cocok: ID=%d, Code=%s", preview.ID, preview.WorkCode)
	}
}

// 3. Permintaan delete-preview pada pekerjaan dengan subtask dan activity log menghitung jumlah secara akurat
func TestBatch08_TestCase03_DeletePreviewCountsAccurate(t *testing.T) {
	_, _, _, workSvc, taskMgmtSvc, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	item, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Dengan Relasi",
		Category:    "Operasional",
		RequestedBy: "Risk Team",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityHigh,
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan: %v", err)
	}

	// Tambahkan 3 subtask
	for i := 1; i <= 3; i++ {
		_, err := taskMgmtSvc.AddSubtask(ctx, item.ID, domain.CreateSubtaskInput{
			Title: fmt.Sprintf("Subtask %d", i),
		})
		if err != nil {
			t.Fatalf("Gagal membuat subtask: %v", err)
		}
	}

	// Tambahkan 2 aktivitas manual
	for i := 1; i <= 2; i++ {
		_, err := workSvc.AddActivity(ctx, item.ID, domain.AddActivityInput{
			Description: fmt.Sprintf("Aktivitas manual %d", i),
		})
		if err != nil {
			t.Fatalf("Gagal menambah aktivitas: %v", err)
		}
	}

	if _, err := workSvc.ArchiveWorkItem(ctx, item.ID); err != nil {
		t.Fatalf("Gagal mengarsipkan: %v", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/work-items/%d/delete-preview", baseURL, item.ID), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	var preview domain.WorkItemDeletePreview
	_ = json.NewDecoder(resp.Body).Decode(&preview)
	if preview.SubtaskCount != 3 {
		t.Fatalf("Diharapkan SubtaskCount = 3, tetapi mendapat %d", preview.SubtaskCount)
	}
	if preview.ActivityLogCount < 4 {
		t.Fatalf("Diharapkan minimal 4 activity logs, tetapi mendapat %d", preview.ActivityLogCount)
	}
}

// 4. Permintaan delete-preview pada pekerjaan tanpa subtask menghitung subtask_count = 0
func TestBatch08_TestCase04_DeletePreviewZeroSubtasks(t *testing.T) {
	_, _, _, workSvc, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	item, _ := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Tanpa Subtask",
		Category:    "Umum",
		RequestedBy: "Admin",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityLow,
	})
	_, _ = workSvc.ArchiveWorkItem(ctx, item.ID)

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/work-items/%d/delete-preview", baseURL, item.ID), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	var preview domain.WorkItemDeletePreview
	_ = json.NewDecoder(resp.Body).Decode(&preview)
	if preview.SubtaskCount != 0 {
		t.Fatalf("Diharapkan SubtaskCount = 0, tetapi mendapat %d", preview.SubtaskCount)
	}
}

// 5. Permintaan delete-preview pada pekerjaan ID tidak valid menghasilkan HTTP 400
func TestBatch08_TestCase05_DeletePreviewInvalidID_Fails400(t *testing.T) {
	_, _, _, _, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	invalidIDs := []string{"abc", "0", "-5", "!@#"}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, idStr := range invalidIDs {
		req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/work-items/%s/delete-preview", baseURL, idStr), nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request gagal: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("Untuk ID '%s', diharapkan HTTP 400 tetapi mendapat %d", idStr, resp.StatusCode)
		}
	}
}

// 6. Permintaan delete-preview pada pekerjaan yang tidak ada menghasilkan HTTP 404
func TestBatch08_TestCase06_DeletePreviewNonExistentItem_Fails404(t *testing.T) {
	_, _, _, _, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/work-items/99999/delete-preview", baseURL), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Diharapkan HTTP 404 untuk ID non-existent, tetapi mendapat %d", resp.StatusCode)
	}
}

// 7. Permintaan delete-preview pada pekerjaan yang sudah berstatus tombstone menghasilkan HTTP 404
func TestBatch08_TestCase07_DeletePreviewTombstone_Fails404(t *testing.T) {
	_, _, _, workSvc, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	item, _ := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Akan Dihapus",
		Category:    "Audit",
		RequestedBy: "Admin",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	_, _ = workSvc.ArchiveWorkItem(ctx, item.ID)
	_, _ = workSvc.PermanentDeleteWorkItem(ctx, item.ID, item.WorkCode)

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/work-items/%d/delete-preview", baseURL, item.ID), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Diharapkan HTTP 404 untuk pekerjaan tombstone, tetapi mendapat %d", resp.StatusCode)
	}
}

// 8. Permintaan delete-preview pada record yang dibuat dengan format timestamp fleksibel
func TestBatch08_TestCase08_DeletePreviewFlexibleTimestamp_Success200(t *testing.T) {
	db, _, _, workSvc, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	item, _ := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Format Lama",
		Category:    "Kepatuhan",
		RequestedBy: "Sistem",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})

	// Simulasikan timestamp SQLite standar tanpa ISO-8601 offset ("YYYY-MM-DD HH:MM:SS")
	_, err := db.ExecContext(ctx, `UPDATE work_items SET archived_at = '2026-10-18 08:00:00' WHERE id = ?`, item.ID)
	if err != nil {
		t.Fatalf("Gagal set archived_at lama: %v", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/work-items/%d/delete-preview", baseURL, item.ID), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Diharapkan HTTP 200 untuk timestamp lama, tetapi mendapat %d: %s", resp.StatusCode, string(body))
	}
}

// 9. Permintaan permanent-delete dengan confirm_code yang salah menghasilkan HTTP 400 dan data tidak berubah
func TestBatch08_TestCase09_PermanentDeleteWrongCode_Fails400(t *testing.T) {
	_, _, _, workSvc, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	item, _ := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Uji Konfirmasi Salah",
		Category:    "Audit",
		RequestedBy: "Admin",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	_, _ = workSvc.ArchiveWorkItem(ctx, item.ID)

	client, token, cookie := getAuthorizedCSRFClient(t, baseURL)
	payload := []byte(`{"confirm_code":"CMP-SALAH-KODE"}`)
	req, _ := http.NewRequestWithContext(ctx, "DELETE", fmt.Sprintf("%s/api/work-items/%d", baseURL, item.ID), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Diharapkan HTTP 400 untuk confirm_code salah, tetapi mendapat %d: %s", resp.StatusCode, string(body))
	}

	// Verifikasi data masih ada
	recheck, err := workSvc.GetWorkItem(ctx, item.ID)
	if err != nil || recheck == nil || recheck.DeletedAt != nil {
		t.Fatalf("Pekerjaan tidak boleh berubah atau terhapus: %v", err)
	}
}

// 10. Permintaan permanent-delete dengan confirm_code benar berhasil menghapus record (tombstone)
func TestBatch08_TestCase10_PermanentDeleteCorrectCode_Success200(t *testing.T) {
	_, _, _, workSvc, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	item, _ := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Uji Konfirmasi Benar",
		Category:    "Audit",
		RequestedBy: "Admin",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	_, _ = workSvc.ArchiveWorkItem(ctx, item.ID)

	client, token, cookie := getAuthorizedCSRFClient(t, baseURL)
	payload := []byte(fmt.Sprintf(`{"confirm_code":"%s"}`, item.WorkCode))
	req, _ := http.NewRequestWithContext(ctx, "DELETE", fmt.Sprintf("%s/api/work-items/%d", baseURL, item.ID), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Diharapkan HTTP 200, tetapi mendapat %d: %s", resp.StatusCode, string(body))
	}
}

// 11. Permintaan delete-preview setelah penghapusan permanen menghasilkan HTTP 404
func TestBatch08_TestCase11_DeletePreviewAfterPermanentDelete_Fails404(t *testing.T) {
	_, _, _, workSvc, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	item, _ := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Diuji Setelah Dihapus",
		Category:    "Audit",
		RequestedBy: "Admin",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	_, _ = workSvc.ArchiveWorkItem(ctx, item.ID)
	_, _ = workSvc.PermanentDeleteWorkItem(ctx, item.ID, item.WorkCode)

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/work-items/%d/delete-preview", baseURL, item.ID), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Diharapkan HTTP 404, tetapi mendapat %d", resp.StatusCode)
	}
}

// 12. Permintaan inspeksi backup pada file valid menghasilkan HTTP 200 dengan payload lengkap
func TestBatch08_TestCase12_InspectBackupValidFile_Success200(t *testing.T) {
	_, _, _, workSvc, _, backupSvc, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	_, _ = workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Backup",
		Category:    "Audit",
		RequestedBy: "Admin",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	backup, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Gagal membuat backup: %v", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/backup/inspect?filename=%s", baseURL, backup.Filename), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Diharapkan HTTP 200, tetapi mendapat %d: %s", resp.StatusCode, string(body))
	}
	var res domain.RestoreInspectionResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("Gagal decode JSON: %v", err)
	}
	if !res.IsCompatible || res.IntegrityStatus != "ok" {
		t.Fatalf("Hasil inspeksi tidak valid: compatible=%v, integrity=%s", res.IsCompatible, res.IntegrityStatus)
	}
}

// 13. Permintaan inspeksi backup pada file skema sama (v6) menghasilkan schema_migration_needed = false dan schema_unsupported = false
func TestBatch08_TestCase13_InspectBackupSchemaCompatibility(t *testing.T) {
	_, _, _, _, _, backupSvc, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	backup, _ := backupSvc.CreateBackup(ctx)

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/backup/inspect?filename=%s", baseURL, backup.Filename), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	var res domain.RestoreInspectionResult
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if res.Differences.SchemaMigrationNeeded {
		t.Fatalf("Diharapkan SchemaMigrationNeeded = false untuk v6")
	}
	if res.Differences.SchemaUnsupported {
		t.Fatalf("Diharapkan SchemaUnsupported = false untuk v6")
	}
}

// 14. Permintaan inspeksi backup pada file dengan record lebih sedikit menghasilkan records_lost_warning = true
func TestBatch08_TestCase14_InspectBackupRecordsLostWarning(t *testing.T) {
	_, _, _, workSvc, _, backupSvc, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	// Backup pertama dibuat saat database kosong
	b1, _ := backupSvc.CreateBackup(ctx)

	// Tambah 3 pekerjaan baru setelah backup
	for i := 1; i <= 3; i++ {
		_, _ = workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
			Title:       fmt.Sprintf("Item Tambahan %d", i),
			Category:    "Audit",
			RequestedBy: "Admin",
			ReceivedOn:  "2026-10-18",
			Priority:    domain.PriorityNormal,
		})
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/backup/inspect?filename=%s", baseURL, b1.Filename), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	var res domain.RestoreInspectionResult
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if !res.Differences.RecordsLostWarning {
		t.Fatalf("Diharapkan RecordsLostWarning = true saat data aktif lebih banyak dari cadangan")
	}
	if len(res.Warnings) == 0 {
		t.Fatalf("Diharapkan ada pesan peringatan risiko data")
	}
}

// 15. Permintaan inspeksi backup pada file cadangan yang tidak ada menghasilkan HTTP 404
func TestBatch08_TestCase15_InspectBackupNonExistent_Fails404(t *testing.T) {
	_, _, _, _, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/backup/inspect?filename=compliance_backup_20260101_000000_nonexistent.db", baseURL), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Diharapkan HTTP 404 untuk file tidak ada, tetapi mendapat %d", resp.StatusCode)
	}
}

// 16. Permintaan inspeksi backup dengan filename tidak valid ditolak dengan HTTP 400
func TestBatch08_TestCase16_InspectBackupInvalidFilename_Fails400(t *testing.T) {
	_, _, _, _, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	traversals := []string{"../test.db", "sub/test.db", "something;bad.db", ""}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, fn := range traversals {
		req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/backup/inspect?filename=%s", baseURL, fn), nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request gagal: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("Untuk filename '%s', diharapkan HTTP 400 tetapi mendapat %d", fn, resp.StatusCode)
		}
	}
}

// 17. Permintaan inspeksi backup pada file rusak menghasilkan is_compatible = false dan integrity status yang menjelaskan
func TestBatch08_TestCase17_InspectBackupCorruptedFile_Incompatible(t *testing.T) {
	_, _, backupDir, _, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	corruptName := "compliance_backup_20261018_120000_corrupt12.db"
	corruptPath := filepath.Join(backupDir, corruptName)
	if err := os.WriteFile(corruptPath, []byte("Ini adalah file teks rusak dan bukan database sqlite"), 0644); err != nil {
		t.Fatalf("Gagal menulis file rusak: %v", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/backup/inspect?filename=%s", baseURL, corruptName), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Diharapkan HTTP 200 dengan payload status rusak, mendapat %d: %s", resp.StatusCode, string(body))
	}
	var res domain.RestoreInspectionResult
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if res.IsCompatible {
		t.Fatalf("Diharapkan IsCompatible = false untuk file rusak")
	}
	if res.IntegrityStatus == "ok" {
		t.Fatalf("IntegrityStatus tidak boleh 'ok' untuk file rusak")
	}
}

// 18. Permintaan inspeksi backup tidak memodifikasi isi atau metadata file cadangan (file tetap read-only)
func TestBatch08_TestCase18_InspectBackupPreservesFileIntegrity(t *testing.T) {
	_, _, backupDir, workSvc, _, backupSvc, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	_, _ = workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Uji Hash",
		Category:    "Audit",
		RequestedBy: "Admin",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	backup, _ := backupSvc.CreateBackup(ctx)

	targetFile := filepath.Join(backupDir, backup.Filename)
	originalBytes, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("Gagal membaca file backup: %v", err)
	}
	hashBefore := sha256.Sum256(originalBytes)

	// Lakukan 3 kali inspeksi berturut-turut
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/backup/inspect?filename=%s", baseURL, backup.Filename), nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request gagal: %v", err)
		}
		resp.Body.Close()
	}

	bytesAfter, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("Gagal membaca file backup setelah inspeksi: %v", err)
	}
	hashAfter := sha256.Sum256(bytesAfter)

	if hex.EncodeToString(hashBefore[:]) != hex.EncodeToString(hashAfter[:]) {
		t.Fatalf("File backup termodifikasi saat inspeksi! Hash sebelum: %s, sesudah: %s",
			hex.EncodeToString(hashBefore[:]), hex.EncodeToString(hashAfter[:]))
	}
}

// 19. Permintaan restore pada file cadangan valid berhasil memulihkan database dan membuat auto safety backup
func TestBatch08_TestCase19_RestoreValidBackupCreatesSafetyBackup(t *testing.T) {
	_, _, _, workSvc, _, backupSvc, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	item1, _ := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Versi Cadangan",
		Category:    "Audit",
		RequestedBy: "Admin",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	backup, _ := backupSvc.CreateBackup(ctx)

	// Tambah item 2 setelah backup
	_, _ = workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Dibuat Setelah Cadangan",
		Category:    "Kepatuhan",
		RequestedBy: "Staff",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityLow,
	})

	client, token, cookie := getAuthorizedCSRFClient(t, baseURL)
	payload := []byte(fmt.Sprintf(`{"filename":"%s"}`, backup.Filename))
	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/backups/restore", baseURL), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request restore gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Diharapkan HTTP 200 pada restore, tetapi mendapat %d: %s", resp.StatusCode, string(body))
	}

	// Verifikasi database aktif kembali memiliki item1 dan item2 hilang
	items, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	if err != nil {
		t.Fatalf("Gagal list pekerjaan: %v", err)
	}
	if len(items) != 1 || items[0].ID != item1.ID {
		t.Fatalf("Data tidak terpulihkan dengan benar: %d item ditemukan", len(items))
	}

	// Verifikasi safety backup otomatis dibuat
	backups, err := backupSvc.ListBackups()
	if err != nil {
		t.Fatalf("Gagal list backup: %v", err)
	}
	foundSafety := false
	for _, b := range backups {
		if bytes.Contains([]byte(b.Filename), []byte("pre_restore")) || len(backups) >= 2 {
			foundSafety = true
			break
		}
	}
	if !foundSafety {
		t.Fatalf("Safety backup sebelum pemulihan tidak ditemukan")
	}
}

// 20. Permintaan restore pada file dengan skema lebih tinggi dari v6 ditolak
func TestBatch08_TestCase20_RestoreHigherSchemaRejected(t *testing.T) {
	_, _, backupDir, _, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	futureDBName := "compliance_backup_20261018_999999_futurev99.db"
	futureDBPath := filepath.Join(backupDir, futureDBName)

	fDB, err := database.OpenDB(futureDBPath)
	if err != nil {
		t.Fatalf("Gagal membuat db dummy masa depan: %v", err)
	}
	_, _ = fDB.ExecContext(ctx, "CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT);")
	_, _ = fDB.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (99, '2026-10-18T00:00:00Z');")
	_ = fDB.Close()

	client, token, cookie := getAuthorizedCSRFClient(t, baseURL)
	payload := []byte(fmt.Sprintf(`{"filename":"%s"}`, futureDBName))
	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/backups/restore", baseURL), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request restore gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatalf("Restore dengan skema tidak didukung harus ditolak!")
	}
}

// 21. Konkurensi: Permintaan delete-preview bersamaan dengan listWorkItems tidak menyebabkan starvation atau timeout
func TestBatch08_TestCase21_ConcurrencyNoStarvationOrTimeout(t *testing.T) {
	_, _, _, workSvc, _, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	item, _ := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Uji Konkurensi",
		Category:    "Kepatuhan",
		RequestedBy: "Admin",
		ReceivedOn:  "2026-10-18",
		Priority:    domain.PriorityNormal,
	})
	_, _ = workSvc.ArchiveWorkItem(ctx, item.ID)

	var wg sync.WaitGroup
	errCh := make(chan error, 30)

	for i := 0; i < 15; i++ {
		wg.Add(2)

		// Goroutine 1: delete-preview
		go func() {
			defer wg.Done()
			reqCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(reqCtx, "GET", fmt.Sprintf("%s/api/work-items/%d/delete-preview", baseURL, item.ID), nil)
			client := &http.Client{Timeout: 4 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				errCh <- fmt.Errorf("delete-preview timeout/error: %w", err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("delete-preview status non-200: %d", resp.StatusCode)
			}
		}()

		// Goroutine 2: listWorkItems
		go func() {
			defer wg.Done()
			reqCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(reqCtx, "GET", fmt.Sprintf("%s/api/work-items?include_archived=true", baseURL), nil)
			client := &http.Client{Timeout: 4 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				errCh <- fmt.Errorf("listWorkItems timeout/error: %w", err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("listWorkItems status non-200: %d", resp.StatusCode)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("Terjadi kegagalan konkurensi: %v", err)
	}
}

// 22. Non-regresi: Pembuatan, pembaruan, dan filter kategori tetap berfungsi normal
func TestBatch08_TestCase22_CategoryNonRegression(t *testing.T) {
	_, _, _, _, taskMgmtSvc, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	cat, err := taskMgmtSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name:        "Kategori Baru Batch08",
		Description: "Uji non-regresi kategori",
	})
	if err != nil {
		t.Fatalf("Gagal membuat kategori: %v", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/categories", baseURL), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Diharapkan HTTP 200, mendapat %d", resp.StatusCode)
	}
	var cats []*domain.Category
	_ = json.NewDecoder(resp.Body).Decode(&cats)
	found := false
	for _, c := range cats {
		if c.ID == cat.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Kategori baru tidak ditemukan pada API")
	}
}

// 23. Non-regresi: Pembuatan template pekerjaan dan penerapan template ke pekerjaan tetap berfungsi normal
func TestBatch08_TestCase23_TemplateNonRegression(t *testing.T) {
	_, _, _, workSvc, taskMgmtSvc, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	tpl, err := taskMgmtSvc.CreateTemplate(ctx, domain.CreateWorkTemplateInput{
		Name:          "Template SOP Rutin",
		TitlePattern:  "SOP Rutin {date}",
		Category:      "Operasional",
		Priority:      domain.PriorityNormal,
		DueDaysOffset: 3,
		ChecklistItems: []string{
			"Langkah A",
			"Langkah B",
		},
	})
	if err != nil {
		t.Fatalf("Gagal membuat template: %v", err)
	}

	// Terapkan template via API
	client, token, cookie := getAuthorizedCSRFClient(t, baseURL)
	payload := []byte(`{"received_on":"2026-10-18"}`)
	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/templates/%d/apply", baseURL, tpl.ID), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request apply template gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Diharapkan HTTP 201 apply template, mendapat %d", resp.StatusCode)
	}

	// Verifikasi subtask dari template ada pada pekerjaan baru
	var appliedItem domain.WorkItem
	_ = json.NewDecoder(resp.Body).Decode(&appliedItem)
	subs, err := taskMgmtSvc.ListSubtasks(ctx, appliedItem.ID)
	if err != nil {
		t.Fatalf("Gagal list subtask: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("Diharapkan 2 subtask terpasang, tetapi ada %d", len(subs))
	}

	_ = workSvc
}

// 24. Non-regresi: Pembuatan jadwal berulang dan eksekusi generator pekerjaan tetap berfungsi normal
func TestBatch08_TestCase24_RecurringScheduleNonRegression(t *testing.T) {
	_, _, _, workSvc, taskMgmtSvc, _, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	sched, err := taskMgmtSvc.CreateRecurringSchedule(ctx, domain.CreateRecurringScheduleInput{
		Title:         "Audit Triwulanan Rutin",
		Category:      "Audit",
		Frequency:     "daily",
		IntervalValue: 1,
		StartDate:     "2026-10-18",
		Priority:      domain.PriorityHigh,
	})
	if err != nil {
		t.Fatalf("Gagal membuat jadwal berulang: %v", err)
	}

	// Trigger generator via API
	client, token, cookie := getAuthorizedCSRFClient(t, baseURL)
	payload := []byte(`{"as_of_date":"2026-10-18"}`)
	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/recurring-schedules/trigger", baseURL), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request trigger recurring gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Diharapkan HTTP 200 trigger recurring, mendapat %d", resp.StatusCode)
	}

	// Verifikasi pekerjaan baru ter-generate dengan provenance yang tepat
	items, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	if err != nil {
		t.Fatalf("Gagal list pekerjaan: %v", err)
	}
	found := false
	for _, it := range items {
		if it.Provenance == domain.ProvenanceRecurring && it.RequestedBy == fmt.Sprintf("Jadwal Rutin #%d", sched.ID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Pekerjaan dari generator jadwal berulang tidak ditemukan")
	}
}

// 25. Non-regresi: Riwayat impor data tetap dapat dibaca dan batch kompensasi tetap berfungsi normal
func TestBatch08_TestCase25_ImportHistoryAndCompensateNonRegression(t *testing.T) {
	_, _, _, _, _, _, importSvc, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()
	ctx := context.Background()

	// Commit batch impor sederhana
	csvContent := "Judul,Deskripsi,Kategori,Pemberi Tugas,Tanggal Diterima,Tenggat Waktu,Prioritas,Status\nPekerjaan Impor 1,,Kepatuhan,Direksi,2026-10-18,,Tinggi,Belum Dimulai\n"
	mapping := map[string]string{
		"title":        "Judul",
		"description":  "Deskripsi",
		"category":     "Kategori",
		"requested_by": "Pemberi Tugas",
		"received_on":  "Tanggal Diterima",
		"due_on":       "Tenggat Waktu",
		"priority":     "Prioritas",
		"status":       "Status",
	}
	result, err := importSvc.CommitImport(ctx, []byte(csvContent), domain.CommitImportRequest{
		FileName:      "test_import.csv",
		FileType:      "csv",
		ColumnMapping: mapping,
		DuplicateMode: "create",
	})
	if err != nil {
		t.Fatalf("Gagal commit impor: %v", err)
	}

	// Baca riwayat via API
	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/import/history", baseURL), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request history gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Diharapkan HTTP 200 pada riwayat impor, mendapat %d", resp.StatusCode)
	}

	// Kompensasi batch impor via API
	clientAuth, token, cookie := getAuthorizedCSRFClient(t, baseURL)
	reqComp, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/import/batches/%d/compensate", baseURL, result.BatchID), nil)
	reqComp.Header.Set("X-CSRF-Token", token)
	if cookie != nil {
		reqComp.AddCookie(cookie)
	}
	respComp, err := clientAuth.Do(reqComp)
	if err != nil {
		t.Fatalf("Request kompensasi gagal: %v", err)
	}
	defer respComp.Body.Close()

	if respComp.StatusCode != http.StatusOK {
		t.Fatalf("Diharapkan HTTP 200 pada kompensasi impor, mendapat %d", respComp.StatusCode)
	}
}
