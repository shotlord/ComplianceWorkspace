package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"compliance-workspace/internal/database"
	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
	"compliance-workspace/internal/server"
	"compliance-workspace/internal/service"
)

func TestBackupAndRestoreLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}

	if err := database.Migrate(ctx, db); err != nil {
		db.Close()
		t.Fatalf("Gagal migrasi DB: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(db)
	actRepo := repository.NewActivityLogRepository(db)
	svc := service.NewWorkItemService(db, workRepo, actRepo)
	backupSvc := service.NewBackupService(svc, nil, nil, dbPath, backupDir)
	defer func() {
		if curDB := svc.GetDB(); curDB != nil {
			_ = curDB.Close()
		}
	}()

	// 1. Buat data awal (2 pekerjaan)
	item1, err := svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Audit Kebijakan Privasi Snapshot Awal",
		ReceivedOn: "2026-10-15",
		Priority:   domain.PriorityHigh,
	})
	if err != nil {
		t.Fatalf("Gagal create item1: %v", err)
	}

	item2, err := svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Laporan Kepatuhan OJK Snapshot Awal",
		ReceivedOn: "2026-10-16",
		Priority:   domain.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("Gagal create item2: %v", err)
	}

	// Tambahkan log aktivitas manual
	_, err = svc.AddActivity(ctx, item1.ID, domain.AddActivityInput{
		Description: "Catatan penting sebelum pencadangan",
	})
	if err != nil {
		t.Fatalf("Gagal add activity: %v", err)
	}

	// 2. Buat backup menggunakan VACUUM INTO
	backupItem, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("CreateBackup gagal: %v", err)
	}

	if backupItem.SizeBytes <= 0 {
		t.Errorf("Ukuran file backup harus > 0: %d", backupItem.SizeBytes)
	}
	if backupItem.Status != "valid" {
		t.Errorf("Status backup harus valid: %s", backupItem.Status)
	}

	// Pastikan file backup benar-benar ada di disk
	fullBackupPath := filepath.Join(backupDir, backupItem.Filename)
	info, err := os.Stat(fullBackupPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("File backup tidak ditemukan atau kosong di disk: %v", err)
	}

	// 3. Mutasi database aktif setelah backup dibuat:
	// Tambah item ke-3 dan ubah status item ke-1
	_, err = svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Item Tambahan Setelah Backup",
		ReceivedOn: "2026-10-17",
	})
	if err != nil {
		t.Fatalf("Gagal create item3: %v", err)
	}

	_, err = svc.UpdateWorkItem(ctx, item1.ID, domain.UpdateWorkItemInput{
		Title:      item1.Title,
		ReceivedOn: item1.ReceivedOn,
		Priority:   item1.Priority,
		Status:     domain.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("Gagal update item1: %v", err)
	}

	// Verifikasi ada 3 item saat ini
	currentItems, _ := svc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: true})
	if len(currentItems) != 3 {
		t.Fatalf("Seharusnya ada 3 item setelah mutasi, didapat: %d", len(currentItems))
	}

	// 4. Lakukan pemulihan (Restore) dari file backup
	if err := backupSvc.RestoreBackup(ctx, backupItem.Filename); err != nil {
		t.Fatalf("RestoreBackup gagal: %v", err)
	}

	// 5. Verifikasi bahwa database telah kembali persis ke kondisi saat backup dibuat:
	// - Hanya ada 2 item (item ke-3 hilang karena belum ada saat backup)
	// - Status item1 harus kembali ke not_started (bukan completed)
	restoredItems, err := svc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Gagal membaca item setelah restore: %v", err)
	}
	if len(restoredItems) != 2 {
		t.Errorf("Setelah restore seharusnya ada 2 item, didapat: %d", len(restoredItems))
	}

	item1Restored, err := svc.GetWorkItem(ctx, item1.ID)
	if err != nil {
		t.Fatalf("Gagal membaca item1 setelah restore: %v", err)
	}
	if item1Restored.Status != domain.StatusNotStarted {
		t.Errorf("Status item1 harus kembali ke not_started, didapat: %s", item1Restored.Status)
	}

	_ = item2 // validasi pasif
}

func TestRestoreRejectCorruptedOrMaliciousFiles(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	_ = os.MkdirAll(backupDir, 0755)
	ctx := context.Background()

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}

	if err := database.Migrate(ctx, db); err != nil {
		db.Close()
		t.Fatalf("Gagal migrasi: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(db)
	actRepo := repository.NewActivityLogRepository(db)
	svc := service.NewWorkItemService(db, workRepo, actRepo)
	backupSvc := service.NewBackupService(svc, nil, nil, dbPath, backupDir)
	defer func() {
		if curDB := svc.GetDB(); curDB != nil {
			_ = curDB.Close()
		}
	}()

	// Buat 1 item valid di DB aktif
	item, err := svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Data Aktif Sebelum Serangan File Korup",
		ReceivedOn: "2026-10-17",
	})
	if err != nil {
		t.Fatalf("Gagal create item: %v", err)
	}

	// 1. Uji Penolakan File Path Traversal
	maliciousFilenames := []string{
		"../../etc/passwd",
		"..\\..\\boot.ini",
		"sub/folder/backup.db",
		"backup.txt",
		"backup.exe",
	}
	for _, fn := range maliciousFilenames {
		err := backupSvc.RestoreBackup(ctx, fn)
		if err == nil {
			t.Errorf("Path traversal atau ekstensi non-db '%s' seharusnya ditolak", fn)
		}
	}

	// 2. Uji Penolakan File Korup (File teks biasa dengan ekstensi .db)
	corruptFile := filepath.Join(backupDir, "corrupted_fake.db")
	if err := os.WriteFile(corruptFile, []byte("bukan format sqlite yang valid!"), 0644); err != nil {
		t.Fatalf("Gagal membuat file korup uji: %v", err)
	}

	errCorrupt := backupSvc.RestoreBackup(ctx, "corrupted_fake.db")
	if errCorrupt == nil {
		t.Fatalf("File korup seharusnya ditolak oleh verifikasi integritas restore!")
	}

	// 3. Pastikan Database Aktif Masih Tetap Utuh dan Berfungsi
	activeItem, err := svc.GetWorkItem(ctx, item.ID)
	if err != nil || activeItem.Title != item.Title {
		t.Fatalf("Database aktif rusak setelah percobaan restore file korup: %v", err)
	}
}

func TestBackupAndRestoreHTTPEndpoints(t *testing.T) {
	_, baseURL, cleanup := setupTestServer(t)
	defer cleanup()

	client := &http.Client{}

	// Ambil CSRF token & cookie
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

	doReq := func(method, urlStr string, body interface{}) (*http.Response, string) {
		var bodyReader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			bodyReader = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, urlStr, bodyReader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(server.CSRFHeaderName, csrfToken)
		if csrfCookie != nil {
			req.AddCookie(csrfCookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request %s %s gagal: %v", method, urlStr, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}

	// 1. Buat pekerjaan dulu via API
	respCreateWork, _ := doReq(http.MethodPost, baseURL+"/api/work-items", domain.CreateWorkItemInput{
		Title:      "Pekerjaan Untuk Cadangan HTTP",
		ReceivedOn: "2026-10-17",
	})
	if respCreateWork.StatusCode != http.StatusCreated {
		t.Fatalf("Gagal membuat pekerjaan: %d", respCreateWork.StatusCode)
	}

	// 2. Buat backup via HTTP POST /api/backups
	respBackup, bodyBackup := doReq(http.MethodPost, baseURL+"/api/backups", nil)
	if respBackup.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/backups gagal, status: %d, body: %s", respBackup.StatusCode, bodyBackup)
	}

	var createdBackup service.BackupItem
	_ = json.Unmarshal([]byte(bodyBackup), &createdBackup)
	if createdBackup.Filename == "" || createdBackup.SizeBytes <= 0 {
		t.Fatalf("Metadata backup tidak valid: %+v", createdBackup)
	}

	// 3. Ambil daftar backup via HTTP GET /api/backups
	respList, bodyList := doReq(http.MethodGet, baseURL+"/api/backups", nil)
	if respList.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/backups gagal: %d, body: %s", respList.StatusCode, bodyList)
	}

	var backupList []*service.BackupItem
	_ = json.Unmarshal([]byte(bodyList), &backupList)
	if len(backupList) == 0 {
		t.Fatalf("Daftar backup kosong padahal baru saja dibuat")
	}

	// 4. Pulihkan backup via HTTP POST /api/backups/restore
	respRestore, bodyRestore := doReq(http.MethodPost, baseURL+"/api/backups/restore", map[string]string{
		"filename": createdBackup.Filename,
	})
	if respRestore.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/backups/restore gagal: %d, body: %s", respRestore.StatusCode, bodyRestore)
	}

	if !strings.Contains(bodyRestore, "berhasil dipulihkan") {
		t.Errorf("Pesan restore tidak sesuai: %s", bodyRestore)
	}
}
