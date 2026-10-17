package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func setupTestServer(t *testing.T) (*server.Server, string, func()) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_http.db")

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
	activityRepo := repository.NewActivityLogRepository(db)
	svc := service.NewWorkItemService(db, workRepo, activityRepo)
	exportSvc := service.NewExportService(workRepo, activityRepo)
	backupDir := filepath.Join(tempDir, "backups")
	backupSvc := service.NewBackupService(svc, nil, nil, dbPath, backupDir)

	// Bind pada port bebas di 127.0.0.1
	srv, err := server.NewServer("127.0.0.1:0", svc, backupSvc, exportSvc)
	if err != nil {
		db.Close()
		t.Fatalf("Gagal membuat server test: %v", err)
	}

	go func() {
		_ = srv.Start()
	}()

	baseURL := fmt.Sprintf("http://%s", srv.Addr())

	cleanup := func() {
		_ = srv.Shutdown(context.Background())
		_ = db.Close()
	}

	return srv, baseURL, cleanup
}

func TestSecurityMiddlewareRejections(t *testing.T) {
	_, baseURL, cleanup := setupTestServer(t)
	defer cleanup()

	client := &http.Client{}

	// 1. Uji penolakan Host header yang tidak valid
	reqHost, _ := http.NewRequest(http.MethodGet, baseURL+"/api/csrf-token", nil)
	reqHost.Host = "evil.attacker.com"
	respHost, err := client.Do(reqHost)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer respHost.Body.Close()
	if respHost.StatusCode != http.StatusBadRequest {
		t.Errorf("Host asing seharusnya ditolak 400 Bad Request, didapat: %d", respHost.StatusCode)
	}

	// 2. Uji penolakan Origin asing pada request POST
	reqOrigin, _ := http.NewRequest(http.MethodPost, baseURL+"/api/work-items", strings.NewReader(`{}`))
	reqOrigin.Header.Set("Origin", "http://malicious-site.com")
	respOrigin, err := client.Do(reqOrigin)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer respOrigin.Body.Close()
	if respOrigin.StatusCode != http.StatusForbidden {
		t.Errorf("Origin asing seharusnya ditolak 403 Forbidden, didapat: %d", respOrigin.StatusCode)
	}

	// 3. Uji penolakan POST tanpa CSRF token
	reqNoCSRF, _ := http.NewRequest(http.MethodPost, baseURL+"/api/work-items", strings.NewReader(`{}`))
	respNoCSRF, err := client.Do(reqNoCSRF)
	if err != nil {
		t.Fatalf("Request gagal: %v", err)
	}
	defer respNoCSRF.Body.Close()
	if respNoCSRF.StatusCode != http.StatusForbidden {
		t.Errorf("POST tanpa CSRF token harus ditolak 403 Forbidden, didapat: %d", respNoCSRF.StatusCode)
	}
}

func TestHttpWorkItemAndActivityLifecycle(t *testing.T) {
	_, baseURL, cleanup := setupTestServer(t)
	defer cleanup()

	client := &http.Client{}

	// 1. Dapatkan CSRF Token & Cookie
	respToken, err := client.Get(baseURL + "/api/csrf-token")
	if err != nil {
		t.Fatalf("Gagal mengambil csrf token: %v", err)
	}
	defer respToken.Body.Close()

	var tokenBody map[string]string
	_ = json.NewDecoder(respToken.Body).Decode(&tokenBody)
	csrfToken := tokenBody["csrf_token"]
	if csrfToken == "" {
		t.Fatalf("Token CSRF kosong")
	}

	var csrfCookie *http.Cookie
	for _, c := range respToken.Cookies() {
		if c.Name == server.CSRFCookieName {
			csrfCookie = c
			break
		}
	}
	if csrfCookie == nil {
		t.Fatalf("Cookie CSRF tidak diset oleh server")
	}

	// Helper untuk membuat request yang terotentikasi CSRF
	doAuthorizedPost := func(method, urlStr string, body interface{}) (*http.Response, error) {
		b, _ := json.Marshal(body)
		req, err := http.NewRequest(method, urlStr, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(server.CSRFHeaderName, csrfToken)
		req.AddCookie(csrfCookie)
		return client.Do(req)
	}

	// 2. Buat Pekerjaan baru melalui HTTP POST
	createPayload := domain.CreateWorkItemInput{
		Title:       "Audit Investigasi Keuangan",
		Description: "Analisis transaksi mencurigakan",
		Category:    "AML/CFT",
		RequestedBy: "Unit Kepatuhan Pusat",
		ReceivedOn:  "2026-10-17",
		Priority:    domain.PriorityCritical,
	}

	respCreate, err := doAuthorizedPost(http.MethodPost, baseURL+"/api/work-items", createPayload)
	if err != nil {
		t.Fatalf("POST /api/work-items gagal: %v", err)
	}
	defer respCreate.Body.Close()

	if respCreate.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(respCreate.Body)
		t.Fatalf("Ekspektasi status 201 Created, didapat: %d, body: %s", respCreate.StatusCode, string(bodyBytes))
	}

	var createdItem domain.WorkItem
	_ = json.NewDecoder(respCreate.Body).Decode(&createdItem)
	if createdItem.ID == 0 || !strings.HasPrefix(createdItem.WorkCode, "CMP-2026-") {
		t.Fatalf("Pekerjaan tidak terbuat dengan benar: %+v", createdItem)
	}

	// 3. Baca Detail Pekerjaan melalui HTTP GET
	respGet, err := client.Get(fmt.Sprintf("%s/api/work-items/%d", baseURL, createdItem.ID))
	if err != nil {
		t.Fatalf("GET /api/work-items/{id} gagal: %v", err)
	}
	defer respGet.Body.Close()
	if respGet.StatusCode != http.StatusOK {
		t.Fatalf("Ekspektasi 200 OK, didapat: %d", respGet.StatusCode)
	}

	var fetchedItem domain.WorkItem
	_ = json.NewDecoder(respGet.Body).Decode(&fetchedItem)
	if fetchedItem.Title != createPayload.Title {
		t.Errorf("Judul tidak cocok: %s", fetchedItem.Title)
	}

	// 4. Perbarui Pekerjaan melalui HTTP PUT
	updatePayload := domain.UpdateWorkItemInput{
		Title:       "Audit Investigasi Keuangan [Selesai Review]",
		Description: "Analisis transaksi telah diverifikasi",
		Category:    "AML/CFT",
		RequestedBy: "Unit Kepatuhan Pusat",
		ReceivedOn:  "2026-10-17",
		Priority:    domain.PriorityHigh,
		Status:      domain.StatusInProgress,
	}

	respPut, err := doAuthorizedPost(http.MethodPut, fmt.Sprintf("%s/api/work-items/%d", baseURL, createdItem.ID), updatePayload)
	if err != nil {
		t.Fatalf("PUT /api/work-items/{id} gagal: %v", err)
	}
	defer respPut.Body.Close()
	if respPut.StatusCode != http.StatusOK {
		t.Fatalf("PUT ekspektasi 200 OK, didapat: %d", respPut.StatusCode)
	}

	// 5. Tambahkan Catatan Aktivitas Manual melalui HTTP POST
	actPayload := domain.AddActivityInput{
		Description: "Wawancara dengan staf operasional cabang telah selesai.",
	}
	respAct, err := doAuthorizedPost(http.MethodPost, fmt.Sprintf("%s/api/work-items/%d/activities", baseURL, createdItem.ID), actPayload)
	if err != nil {
		t.Fatalf("POST /api/work-items/{id}/activities gagal: %v", err)
	}
	defer respAct.Body.Close()
	if respAct.StatusCode != http.StatusCreated {
		t.Fatalf("Add activity ekspektasi 201 Created, didapat: %d", respAct.StatusCode)
	}

	// 6. Baca Seluruh Log Aktivitas melalui HTTP GET
	respLogs, err := client.Get(fmt.Sprintf("%s/api/work-items/%d/activities", baseURL, createdItem.ID))
	if err != nil {
		t.Fatalf("GET activities gagal: %v", err)
	}
	defer respLogs.Body.Close()
	if respLogs.StatusCode != http.StatusOK {
		t.Fatalf("GET activities ekspektasi 200 OK, didapat: %d", respLogs.StatusCode)
	}

	var logs []*domain.ActivityLog
	_ = json.NewDecoder(respLogs.Body).Decode(&logs)
	// Seharusnya ada: 1 log created, log edited/status_changed dari PUT, dan 1 manual activity
	if len(logs) < 3 {
		t.Fatalf("Jumlah log tidak sesuai ekspektasi: didapat %d", len(logs))
	}

	// 7. Arsipkan Pekerjaan melalui HTTP POST
	respArchive, err := doAuthorizedPost(http.MethodPost, fmt.Sprintf("%s/api/work-items/%d/archive", baseURL, createdItem.ID), nil)
	if err != nil {
		t.Fatalf("POST archive gagal: %v", err)
	}
	defer respArchive.Body.Close()
	if respArchive.StatusCode != http.StatusOK {
		t.Fatalf("Archive ekspektasi 200 OK, didapat: %d", respArchive.StatusCode)
	}

	// 8. Kembalikan Pekerjaan dari Arsip melalui HTTP POST
	respRestore, err := doAuthorizedPost(http.MethodPost, fmt.Sprintf("%s/api/work-items/%d/restore", baseURL, createdItem.ID), nil)
	if err != nil {
		t.Fatalf("POST restore gagal: %v", err)
	}
	defer respRestore.Body.Close()
	if respRestore.StatusCode != http.StatusOK {
		t.Fatalf("Restore ekspektasi 200 OK, didapat: %d", respRestore.StatusCode)
	}
}
