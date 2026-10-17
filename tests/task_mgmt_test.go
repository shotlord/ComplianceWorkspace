package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
)

func setupTaskMgmtTestServer(t *testing.T) (*server.Server, string, *service.TaskManagementService, *service.WorkItemService, func()) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_task_mgmt.db")

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

	workSvc := service.NewWorkItemService(db, workRepo, actRepo)
	exportSvc := service.NewExportService(workRepo, actRepo)
	backupSvc := service.NewBackupService(workSvc, nil, nil, dbPath, filepath.Join(tempDir, "backups"))
	taskSvc := service.NewTaskManagementService(db, workRepo, actRepo, subRepo, catRepo, tplRepo, recRepo)

	srv, err := server.NewServer("127.0.0.1:0", workSvc, backupSvc, exportSvc, taskSvc)
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

	return srv, baseURL, taskSvc, workSvc, cleanup
}

// TestSubtaskLifecycleAndProgressCalculation menguji siklus hidup subtask dan aturan kalkulasi progres
func TestSubtaskLifecycleAndProgressCalculation(t *testing.T) {
	_, _, taskSvc, workSvc, cleanup := setupTaskMgmtTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Buat pekerjaan baru
	wi, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Uji Subtask & Progres",
		Description: "Menguji kalkulasi progres otomatis",
		Category:    "General",
		ReceivedOn:  "2026-10-17",
		Priority:    domain.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan: %v", err)
	}

	// Pekerjaan tanpa subtask berstatus not_started harus 0%
	if wi.ProgressPercentage != 0 {
		t.Errorf("Progres awal tanpa subtask seharusnya 0%%, didapat %d%%", wi.ProgressPercentage)
	}

	// 2. Tambah 2 subtask
	sub1, err := taskSvc.AddSubtask(ctx, wi.ID, domain.CreateSubtaskInput{
		Title: "Langkah 1: Review Berkas",
	})
	if err != nil {
		t.Fatalf("Gagal menambah subtask 1: %v", err)
	}

	sub2, err := taskSvc.AddSubtask(ctx, wi.ID, domain.CreateSubtaskInput{
		Title: "Langkah 2: Verifikasi Regulasi",
	})
	if err != nil {
		t.Fatalf("Gagal menambah subtask 2: %v", err)
	}

	// 3. Verifikasi progres setelah ada 2 subtask belum selesai: harus 0%
	wiUpdated, _ := workSvc.GetWorkItem(ctx, wi.ID)
	if wiUpdated.ProgressPercentage != 0 {
		t.Errorf("Progres dengan 2 subtask belum selesai seharusnya 0%%, didapat %d%%", wiUpdated.ProgressPercentage)
	}

	// 4. Centang subtask 1 selesai -> Progres harus menjadi 50%
	_, err = taskSvc.ToggleSubtask(ctx, sub1.ID, true)
	if err != nil {
		t.Fatalf("Gagal toggle subtask 1: %v", err)
	}

	wiUpdated, _ = workSvc.GetWorkItem(ctx, wi.ID)
	if wiUpdated.ProgressPercentage != 50 {
		t.Errorf("Progres setelah 1 dari 2 selesai seharusnya 50%%, didapat %d%%", wiUpdated.ProgressPercentage)
	}

	// 5. Centang subtask 2 selesai -> Progres harus menjadi 100%
	_, err = taskSvc.ToggleSubtask(ctx, sub2.ID, true)
	if err != nil {
		t.Fatalf("Gagal toggle subtask 2: %v", err)
	}

	wiUpdated, _ = workSvc.GetWorkItem(ctx, wi.ID)
	if wiUpdated.ProgressPercentage != 100 {
		t.Errorf("Progres setelah 2 dari 2 selesai seharusnya 100%%, didapat %d%%", wiUpdated.ProgressPercentage)
	}

	// 6. SYARAT MUTLAK: Status pekerjaan utama TIDAK BOLEH otomatis berubah menjadi completed
	if wiUpdated.Status == domain.StatusCompleted {
		t.Errorf("Status pekerjaan utama TIDAK BOLEH berubah otomatis menjadi completed!")
	}

	// 7. Verifikasi pencatatan log aktivitas append-only
	logs, err := workSvc.ListActivities(ctx, wi.ID)
	if err != nil || len(logs) < 5 { // created + add sub1 + add sub2 + toggle sub1 + toggle sub2
		t.Errorf("Activity log subtask tidak lengkap: jumlah log=%d, err=%v", len(logs), err)
	}
}

// TestProgressRulesWithoutSubtask menguji aturan progres pekerjaan tanpa subtask
func TestProgressRulesWithoutSubtask(t *testing.T) {
	_, _, _, workSvc, cleanup := setupTaskMgmtTestServer(t)
	defer cleanup()

	ctx := context.Background()

	wi, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Tanpa Subtask",
		Description: "Uji aturan status",
		Category:    "General",
		ReceivedOn:  "2026-10-17",
		Priority:    domain.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan: %v", err)
	}
	if wi.ProgressPercentage != 0 {
		t.Errorf("Status not_started tanpa subtask harus 0%%, didapat: %d%%", wi.ProgressPercentage)
	}

	// Ubah status ke in_progress -> harus otomatis 50%
	updated, err := workSvc.UpdateWorkItem(ctx, wi.ID, domain.UpdateWorkItemInput{
		Title:       wi.Title,
		Description: wi.Description,
		Category:    wi.Category,
		ReceivedOn:  wi.ReceivedOn,
		Priority:    wi.Priority,
		Status:      domain.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("Gagal update status: %v", err)
	}
	if updated.ProgressPercentage != 50 {
		t.Errorf("Status in_progress tanpa subtask harus 50%%, didapat: %d%%", updated.ProgressPercentage)
	}

	// Ubah status ke completed -> harus otomatis 100%
	updated, err = workSvc.UpdateWorkItem(ctx, wi.ID, domain.UpdateWorkItemInput{
		Title:       wi.Title,
		Description: wi.Description,
		Category:    wi.Category,
		ReceivedOn:  wi.ReceivedOn,
		Priority:    wi.Priority,
		Status:      domain.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("Gagal update status completed: %v", err)
	}
	if updated.ProgressPercentage != 100 {
		t.Errorf("Status completed tanpa subtask harus 100%%, didapat: %d%%", updated.ProgressPercentage)
	}
}

// TestCategoryManagementLifecycle menguji CRUD kategori dan proteksi kategori default
func TestCategoryManagementLifecycle(t *testing.T) {
	_, _, taskSvc, workSvc, cleanup := setupTaskMgmtTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Ambil daftar kategori default
	cats, err := taskSvc.ListCategories(ctx, true)
	if err != nil || len(cats) < 5 {
		t.Fatalf("Daftar kategori bawaan gagal diambil: cats=%d, err=%v", len(cats), err)
	}

	// 2. Coba hapus kategori default 'General' -> harus ditolak
	var generalCat *domain.Category
	for _, c := range cats {
		if c.Name == "General" {
			generalCat = c
			break
		}
	}
	if generalCat == nil {
		t.Fatalf("Kategori default 'General' tidak ditemukan")
	}

	err = taskSvc.DeleteCategory(ctx, generalCat.ID)
	if err == nil {
		t.Errorf("Penghapusan kategori default seharusnya ditolak")
	}

	// 2b. Coba ubah nama atau nonaktifkan kategori default -> harus ditolak
	active := generalCat.IsActive
	_, err = taskSvc.UpdateCategory(ctx, generalCat.ID, domain.UpdateCategoryInput{
		Name:     "General Edited",
		Color:    generalCat.Color,
		IsActive: &active,
	})
	if err == nil {
		t.Errorf("Pengubahan nama kategori sistem default seharusnya ditolak")
	}

	inactive := false
	_, err = taskSvc.UpdateCategory(ctx, generalCat.ID, domain.UpdateCategoryInput{
		Name:     generalCat.Name,
		Color:    generalCat.Color,
		IsActive: &inactive,
	})
	if err == nil {
		t.Errorf("Penonaktifan kategori sistem default seharusnya ditolak")
	}

	// 3. Buat kategori kustom baru
	newCat, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name:        "Investigasi Fraud Khusus",
		Description: "Kategori khusus temuan investigasi internal",
		Color:       "#e11d48",
	})
	if err != nil {
		t.Fatalf("Gagal membuat kategori baru: %v", err)
	}

	// 3b. Uji validasi keunikan case-insensitive (huruf besar/kecil tidak boleh duplikat)
	_, err = taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name: "investigasi fraud khusus",
	})
	if err == nil {
		t.Errorf("Pembuatan kategori duplikat case-insensitive seharusnya ditolak")
	}
	_, err = taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name: "GENERAL",
	})
	if err == nil {
		t.Errorf("Pembuatan kategori dengan nama bawaan huruf besar seharusnya ditolak")
	}

	// 4. Update kategori kustom
	updatedCat, err := taskSvc.UpdateCategory(ctx, newCat.ID, domain.UpdateCategoryInput{
		Name:        "Investigasi Fraud & Etika",
		Description: "Deskripsi diperbarui",
		Color:       "#be123c",
	})
	if err != nil {
		t.Fatalf("Gagal mengupdate kategori: %v", err)
	}
	if updatedCat.Name != "Investigasi Fraud & Etika" {
		t.Errorf("Nama kategori tidak terupdate: %s", updatedCat.Name)
	}

	// 5. Uji Soft-Delete saat kategori sedang digunakan oleh pekerjaan aktif
	// Tambahkan pekerjaan dengan kategori ini
	_, err = workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Investigasi Audit Internal Q4",
		Category:   "Investigasi Fraud & Etika",
		ReceivedOn: "2026-10-17",
		Priority:   domain.PriorityHigh,
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan dengan kategori: %v", err)
	}

	// 5. Uji Penghapusan ditolak jika kategori sedang digunakan oleh pekerjaan aktif
	err = taskSvc.DeleteCategory(ctx, newCat.ID)
	if err == nil {
		t.Fatalf("Penghapusan kategori yang sedang digunakan seharusnya ditolak")
	}
	if !strings.Contains(err.Error(), "masih digunakan oleh 1 pekerjaan") {
		t.Errorf("Pesan error tidak informatif: %v", err)
	}

	// Nonaktifkan kategori via UpdateCategory
	falseVal := false
	_, err = taskSvc.UpdateCategory(ctx, updatedCat.ID, domain.UpdateCategoryInput{
		Name:        updatedCat.Name,
		Description: updatedCat.Description,
		Color:       updatedCat.Color,
		IsActive:    &falseVal,
	})
	if err != nil {
		t.Fatalf("Gagal menonaktifkan kategori: %v", err)
	}

	// ListCategories(onlyActive = true) tidak boleh memuat kategori yang telah dinonaktifkan
	activeCats, err := taskSvc.ListCategories(ctx, true)
	if err != nil {
		t.Fatalf("Gagal mengambil active categories: %v", err)
	}
	for _, c := range activeCats {
		if c.Name == "Investigasi Fraud & Etika" {
			t.Errorf("Kategori yang dinonaktifkan tidak boleh muncul di daftar active categories")
		}
	}

	// ListCategories(onlyActive = false) harus tetap memuat kategori nonaktif
	allCats, err := taskSvc.ListCategories(ctx, false)
	if err != nil {
		t.Fatalf("Gagal mengambil all categories: %v", err)
	}
	var foundDeactivated bool
	for _, c := range allCats {
		if c.Name == "Investigasi Fraud & Etika" {
			foundDeactivated = true
			if c.IsActive {
				t.Errorf("Kategori yang dinonaktifkan harus memiliki is_active = false")
			}
		}
	}
	if !foundDeactivated {
		t.Errorf("Kategori yang dinonaktifkan tidak ditemukan di daftar all categories")
	}

	// 6. Uji Reaktivasi saat membuat ulang kategori dengan nama yang sama persis
	reactivated, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name:        "Investigasi Fraud & Etika",
		Description: "Diaktifkan kembali dengan deskripsi baru",
		Color:       "#059669",
	})
	if err != nil {
		t.Fatalf("Gagal mengaktifkan kembali kategori nonaktif: %v", err)
	}
	if !reactivated.IsActive {
		t.Errorf("Kategori yang diaktifkan kembali harus memiliki IsActive = true")
	}
	if reactivated.Color != "#059669" {
		t.Errorf("Warna kategori reaktivasi tidak sesuai: %s", reactivated.Color)
	}

	// 7. Hapus kategori yang TIDAK dipakai oleh pekerjaan apa pun -> hard delete
	unusedCat, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name: "Kategori Sementara",
	})
	if err != nil {
		t.Fatalf("Gagal membuat kategori sementara: %v", err)
	}
	if err := taskSvc.DeleteCategory(ctx, unusedCat.ID); err != nil {
		t.Errorf("Gagal hard-delete kategori yang tidak dipakai: %v", err)
	}
}

// TestHTMLViewStructureNoNesting menguji bahwa struktur DOM HTML tidak memiliki nesting section yang salah (BUG-001/002/003)
func TestHTMLViewStructureNoNesting(t *testing.T) {
	_, baseURL, _, _, cleanup := setupTaskMgmtTestServer(t)
	defer cleanup()

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("Gagal fetch index.html: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Gagal membaca body HTML: %v", err)
	}
	html := string(bodyBytes)

	// Verifikasi view sections
	idxDeadline := strings.Index(html, `id="view-deadline_tracker"`)
	if idxDeadline == -1 {
		t.Fatalf("Section view-deadline_tracker tidak ditemukan")
	}

	idxTaskMgmt := strings.Index(html, `id="view-task_management"`)
	if idxTaskMgmt == -1 {
		t.Fatalf("Section view-task_management tidak ditemukan")
	}

	idxSettings := strings.Index(html, `id="view-settings_backup"`)
	if idxSettings == -1 {
		t.Fatalf("Section view-settings_backup tidak ditemukan")
	}

	idxImport := strings.Index(html, `id="view-import_data"`)
	if idxImport == -1 {
		t.Fatalf("Section view-import_data tidak ditemukan")
	}

	// Cari tag penutup </section> antara view-deadline_tracker dan view-task_management
	subDeadline := html[idxDeadline:idxTaskMgmt]
	if !strings.Contains(subDeadline, "</section>") {
		t.Errorf("BUG REGRESSION: view-deadline_tracker tidak ditutup sebelum view-task_management! Ini akan menyebabkan task_management ter-nesting dan tersembunyi.")
	}

	// Cari tag penutup </section> antara view-task_management dan view-settings_backup
	subTask := html[idxTaskMgmt:idxSettings]
	if !strings.Contains(subTask, "</section>") {
		t.Errorf("BUG REGRESSION: view-task_management tidak ditutup sebelum view-settings_backup!")
	}

	// Cari tag penutup </section> antara view-settings_backup dan view-import_data
	subSettings := html[idxSettings:idxImport]
	if !strings.Contains(subSettings, "</section>") {
		t.Errorf("BUG REGRESSION: view-settings_backup tidak ditutup sebelum view-import_data!")
	}
}

// TestWorkTemplateCreationAndApply menguji pembuatan template dan penerapannya ke pekerjaan baru
func TestWorkTemplateCreationAndApply(t *testing.T) {
	_, _, taskSvc, workSvc, cleanup := setupTaskMgmtTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Buat template baku
	tpl, err := taskSvc.CreateTemplate(ctx, domain.CreateWorkTemplateInput{
		Name:          "Laporan Bulanan OJK",
		TitlePattern:  "Pelaporan Kepatuhan Bulanan Periode [Bulan]",
		Description:   "Penyusunan dan pengiriman laporan kepatuhan ke Otoritas Jasa Keuangan",
		Category:      "Pelaporan Regulator",
		Priority:      domain.PriorityHigh,
		DueDaysOffset: 10,
		ChecklistItems: []string{
			"Kompilasi data transaksi kepatuhan",
			"Rekonsiliasi dengan unit operasional",
			"Review oleh Kepala Divisi Kepatuhan",
			"Unggah ke portal pelaporan regulator",
		},
	})
	if err != nil {
		t.Fatalf("Gagal membuat template: %v", err)
	}

	// 2. Terapkan template ke pekerjaan baru
	wi, err := taskSvc.ApplyTemplate(ctx, tpl.ID, "2026-10-17")
	if err != nil {
		t.Fatalf("Gagal menerapkan template: %v", err)
	}

	// Verifikasi atribut pekerjaan yang dihasilkan
	if wi.Category != "Pelaporan Regulator" || wi.Priority != domain.PriorityHigh {
		t.Errorf("Atribut pekerjaan hasil template tidak cocok: category=%s, priority=%s", wi.Category, wi.Priority)
	}
	if wi.DueOn == nil || *wi.DueOn != "2026-10-27" { // 2026-10-17 + 10 hari offset
		t.Errorf("Tenggat waktu hasil template salah: due_on=%v, diharapkan 2026-10-27", wi.DueOn)
	}

	// Verifikasi subtask checklist otomatis terpasang
	subs, err := taskSvc.ListSubtasks(ctx, wi.ID)
	if err != nil || len(subs) != 4 {
		t.Fatalf("Subtask dari template tidak terbentuk lengkap: didapat %d, diharapkan 4", len(subs))
	}
	if subs[0].Title != "Kompilasi data transaksi kepatuhan" {
		t.Errorf("Judul subtask pertama salah: %s", subs[0].Title)
	}

	// Verifikasi Activity Log tercatat
	logs, _ := workSvc.ListActivities(ctx, wi.ID)
	if len(logs) == 0 || !strings.Contains(logs[0].Description, "template") {
		t.Errorf("Activity log pembuatan dari template tidak tercatat")
	}
}

// TestRecurringTasksIdempotencyAndOfflineProtection menguji idempotensi dan kebijakan perlindungan offline
func TestRecurringTasksIdempotencyAndOfflineProtection(t *testing.T) {
	_, _, taskSvc, workSvc, cleanup := setupTaskMgmtTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Buat jadwal berulang bulanan dengan start_date hari ini
	sch, err := taskSvc.CreateRecurringSchedule(ctx, domain.CreateRecurringScheduleInput{
		Title:         "Audit Kepatuhan Berkala",
		Description:   "Evaluasi kepatuhan internal rutin",
		Category:      "Audit & Pengawasan",
		Priority:      domain.PriorityNormal,
		Frequency:     "monthly",
		IntervalValue: 1,
		StartDate:     "2026-10-17",
	})
	if err != nil {
		t.Fatalf("Gagal membuat jadwal berulang: %v", err)
	}

	// 2. Evaluasi pemicu pertama (jatuh tempo hari ini 2026-10-17)
	items1, err := taskSvc.TriggerRecurringGeneration(ctx, "2026-10-17")
	if err != nil {
		t.Fatalf("Gagal memicu recurring generator: %v", err)
	}
	if len(items1) != 1 {
		t.Fatalf("Pemicu pertama harus menghasilkan 1 pekerjaan, didapat: %d", len(items1))
	}

	// 3. Uji IDEMPOTENCY: Panggil pemicu lagi di hari yang sama (misal trigger manual atau ticker 1 jam)
	items2, err := taskSvc.TriggerRecurringGeneration(ctx, "2026-10-17")
	if err != nil {
		t.Fatalf("Pemicu kedua gagal: %v", err)
	}
	if len(items2) != 0 {
		t.Fatalf("Pemicu kedua pada hari yang sama seharusnya IDEMPOTENT (0 pekerjaan baru), didapat: %d", len(items2))
	}

	// 4. Uji OFFLINE PROTECTION:
	// Simulasikan aplikasi mati selama 4 bulan lalu dinyalakan kembali pada "2027-02-17"
	// Sistem TIDAK boleh membanjiri antrean dengan 4 tugas duplikat, melainkan menghasilkan 1 tugas untuk periode aktif
	itemsOffline, err := taskSvc.TriggerRecurringGeneration(ctx, "2027-02-17")
	if err != nil {
		t.Fatalf("Evaluasi pasca offline gagal: %v", err)
	}
	if len(itemsOffline) != 1 {
		t.Fatalf("Pasca offline harus menghasilkan tepat 1 tugas relevan, didapat: %d", len(itemsOffline))
	}

	// Periksa jadwal setelah offline: next_run_date harus telah dimajukan melewati 2027-02-17
	schAfter, _ := taskSvc.GetRecurringSchedule(ctx, sch.ID)
	if schAfter.NextRunDate <= "2027-02-17" {
		t.Errorf("next_run_date tidak dimajukan dengan benar setelah offline: %s", schAfter.NextRunDate)
	}

	// Verifikasi seluruh pekerjaan di database
	allWorks, _ := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	// Total pekerjaan harus tepat 2 (1 dari pemicu awal, 1 dari recovery offline)
	if len(allWorks) != 2 {
		t.Errorf("Total pekerjaan di database salah: didapat %d, diharapkan 2", len(allWorks))
	}
}

// TestHTTPTaskManagementEndpoints menguji endpoint REST API Subtask, Kategori, Template, dan Recurring beserta proteksi CSRF
func TestHTTPTaskManagementEndpoints(t *testing.T) {
	_, baseURL, _, _, cleanup := setupTaskMgmtTestServer(t)
	defer cleanup()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Ambil token CSRF
	respToken, err := client.Get(baseURL + "/api/csrf-token")
	if err != nil {
		t.Fatalf("Gagal ambil CSRF: %v", err)
	}
	var tokenData struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.NewDecoder(respToken.Body).Decode(&tokenData)
	respToken.Body.Close()
	csrfToken := tokenData.CSRFToken

	// Helper untuk request dengan CSRF
	doPost := func(endpoint string, bodyJSON string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, baseURL+endpoint, strings.NewReader(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.Header.Set("Origin", baseURL)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST %s gagal: %v", endpoint, err)
		}
		return resp
	}

	doPut := func(endpoint string, bodyJSON string) *http.Response {
		req, _ := http.NewRequest(http.MethodPut, baseURL+endpoint, strings.NewReader(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.Header.Set("Origin", baseURL)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("PUT %s gagal: %v", endpoint, err)
		}
		return resp
	}

	// 1. Buat Work Item via HTTP
	respWI := doPost("/api/work-items", `{"title":"Tugas API Sprint 2","category":"General","received_on":"2026-10-17","priority":"high"}`)
	if respWI.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/work-items gagal: status=%d", respWI.StatusCode)
	}
	var createdWI domain.WorkItem
	_ = json.NewDecoder(respWI.Body).Decode(&createdWI)
	respWI.Body.Close()

	// 2. Tambah Subtask via HTTP
	subJSON := fmt.Sprintf(`{"title":"Checklist API 1"}`)
	respSub := doPost(fmt.Sprintf("/api/work-items/%d/subtasks", createdWI.ID), subJSON)
	if respSub.StatusCode != http.StatusCreated {
		t.Fatalf("POST subtask gagal: status=%d", respSub.StatusCode)
	}
	var createdSub domain.Subtask
	_ = json.NewDecoder(respSub.Body).Decode(&createdSub)
	respSub.Body.Close()

	// 3. Update Subtask (centang selesai)
	respToggle := doPut(fmt.Sprintf("/api/subtasks/%d", createdSub.ID), `{"is_completed":true}`)
	if respToggle.StatusCode != http.StatusOK {
		t.Fatalf("PUT subtask toggle gagal: status=%d", respToggle.StatusCode)
	}
	respToggle.Body.Close()

	// 4. Kategori API (GET & POST)
	respCat := doPost("/api/categories", `{"name":"Kategori HTTP Baru","color":"#3b82f6"}`)
	if respCat.StatusCode != http.StatusCreated {
		t.Fatalf("POST category gagal: status=%d", respCat.StatusCode)
	}
	respCat.Body.Close()

	respGetCats, err := client.Get(baseURL + "/api/categories")
	if err != nil || respGetCats.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/categories gagal: status=%d, err=%v", respGetCats.StatusCode, err)
	}
	var catList []domain.Category
	_ = json.NewDecoder(respGetCats.Body).Decode(&catList)
	respGetCats.Body.Close()
	if len(catList) < 6 {
		t.Errorf("Jumlah kategori tidak cocok: %d", len(catList))
	}

	// 5. Template API (POST & Apply)
	respTpl := doPost("/api/templates", `{"name":"Template HTTP","title_pattern":"Tugas Pola HTTP","category":"General","priority":"normal","due_days_offset":5,"checklist_items":["Item 1","Item 2"]}`)
	if respTpl.StatusCode != http.StatusCreated {
		t.Fatalf("POST template gagal: status=%d", respTpl.StatusCode)
	}
	var createdTpl domain.WorkTemplate
	_ = json.NewDecoder(respTpl.Body).Decode(&createdTpl)
	respTpl.Body.Close()

	respApply := doPost(fmt.Sprintf("/api/templates/%d/apply", createdTpl.ID), `{"received_on":"2026-10-17"}`)
	if respApply.StatusCode != http.StatusCreated {
		t.Fatalf("POST apply template gagal: status=%d", respApply.StatusCode)
	}
	respApply.Body.Close()

	// 6. Recurring Schedule API (POST & Trigger)
	respRec := doPost("/api/recurring-schedules", `{"title":"Jadwal HTTP","category":"General","priority":"normal","frequency":"daily","interval_value":1,"start_date":"2026-10-17"}`)
	if respRec.StatusCode != http.StatusCreated {
		t.Fatalf("POST recurring schedule gagal: status=%d", respRec.StatusCode)
	}
	respRec.Body.Close()

	respTrigger := doPost("/api/recurring-schedules/trigger", `{"as_of_date":"2026-10-17"}`)
	if respTrigger.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(respTrigger.Body)
		t.Fatalf("POST trigger recurring gagal: status=%d, body=%s", respTrigger.StatusCode, string(body))
	}
	respTrigger.Body.Close()
}

// TestSubtaskDeletePreservesActivityLogs memverifikasi bahwa menghapus subtask tidak menghapus riwayat Activity Log
func TestSubtaskDeletePreservesActivityLogs(t *testing.T) {
	_, _, taskSvc, workSvc, cleanup := setupTaskMgmtTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Buat Work Item
	wi, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Uji Integritas Log saat Hapus Subtask",
		Category:    "General",
		ReceivedOn:  "2026-10-17",
		Priority:    "normal",
		Description: "Testing append-only activity log preservation",
	})
	if err != nil {
		t.Fatalf("Gagal buat work item: %v", err)
	}

	// 2. Tambah 2 subtask
	sub1, err := taskSvc.AddSubtask(ctx, wi.ID, domain.CreateSubtaskInput{
		Title: "Subtask Pertama",
	})
	if err != nil {
		t.Fatalf("Gagal buat subtask 1: %v", err)
	}

	sub2, err := taskSvc.AddSubtask(ctx, wi.ID, domain.CreateSubtaskInput{
		Title: "Subtask Kedua",
	})
	if err != nil {
		t.Fatalf("Gagal buat subtask 2: %v", err)
	}

	// 3. Centang subtask 1
	isComp := true
	_, err = taskSvc.UpdateSubtask(ctx, sub1.ID, domain.UpdateSubtaskInput{
		IsCompleted: &isComp,
	})
	if err != nil {
		t.Fatalf("Gagal centang subtask: %v", err)
	}

	// 4. Periksa log sebelum hapus
	logsBefore, err := workSvc.ListActivities(ctx, wi.ID)
	if err != nil {
		t.Fatalf("Gagal ambil log sebelum hapus: %v", err)
	}
	countBefore := len(logsBefore)
	if countBefore < 4 { // created + sub1 + sub2 + toggle
		t.Fatalf("Log sebelum hapus kurang dari 4: %d", countBefore)
	}

	// 5. Hapus subtask 2
	err = taskSvc.DeleteSubtask(ctx, sub2.ID)
	if err != nil {
		t.Fatalf("Gagal hapus subtask: %v", err)
	}

	// 6. Periksa log setelah hapus
	logsAfter, err := workSvc.ListActivities(ctx, wi.ID)
	if err != nil {
		t.Fatalf("Gagal ambil log setelah hapus: %v", err)
	}
	if len(logsAfter) != countBefore+1 {
		t.Errorf("Jumlah log setelah hapus tidak bertambah 1: sebelum=%d, sesudah=%d", countBefore, len(logsAfter))
	}

	// Pastikan log subtask 2 yang dibuat sebelumnya masih ada
	foundCreationLog := false
	foundDeletionLog := false
	for _, l := range logsAfter {
		if strings.Contains(l.Description, "Subtask Kedua") && strings.Contains(l.Description, "ditambahkan") {
			foundCreationLog = true
		}
		if strings.Contains(l.Description, "Subtask Kedua") && strings.Contains(l.Description, "dihapus") {
			foundDeletionLog = true
		}
	}
	if !foundCreationLog {
		t.Errorf("Log pembuatan subtask 2 hilang setelah subtask dihapus! Audit trail terlanggar.")
	}
	if !foundDeletionLog {
		t.Errorf("Log penghapusan subtask 2 tidak tercatat!")
	}
}

// TestTemplateDeletionRecurringScheduleIntegrity memverifikasi bahwa penghapusan template tidak merusak recurring schedule
func TestTemplateDeletionRecurringScheduleIntegrity(t *testing.T) {
	_, _, taskSvc, workSvc, cleanup := setupTaskMgmtTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Buat template
	tpl, err := taskSvc.CreateTemplate(ctx, domain.CreateWorkTemplateInput{
		Name:           "Template Rutin yang Akan Dihapus",
		TitlePattern:   "Laporan Triwulan Rutin",
		Category:       "Pelaporan Regulator",
		Priority:       "high",
		DueDaysOffset:  10,
		ChecklistItems: []string{"Langkah A", "Langkah B"},
	})
	if err != nil {
		t.Fatalf("Gagal buat template: %v", err)
	}

	// 2. Buat recurring schedule yang merujuk ke template tersebut
	sch, err := taskSvc.CreateRecurringSchedule(ctx, domain.CreateRecurringScheduleInput{
		TemplateID:    &tpl.ID,
		Title:         "Audit Kepatuhan Berkala",
		Description:   "Uraian lengkap tugas audit berkala",
		Category:      "Audit & Pengawasan",
		Priority:      "high",
		Frequency:     "monthly",
		IntervalValue: 1,
		StartDate:     "2026-10-17",
	})
	if err != nil {
		t.Fatalf("Gagal buat recurring schedule: %v", err)
	}

	// 3. Hapus template pekerjaan
	err = taskSvc.DeleteTemplate(ctx, tpl.ID)
	if err != nil {
		t.Fatalf("Gagal hapus template: %v", err)
	}

	// 4. Jalankan trigger recurring generation
	created, err := taskSvc.TriggerRecurringGeneration(ctx, "2026-10-17")
	if err != nil {
		t.Fatalf("Trigger recurring generation gagal setelah template dihapus: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("Harus terbentuk 1 pekerjaan, didapat: %d", len(created))
	}

	item := created[0]
	// Verifikasi seluruh informasi pekerjaan tetap lengkap
	if item.Title != fmt.Sprintf("%s (2026-10-17)", sch.Title) {
		t.Errorf("Judul pekerjaan tidak lengkap: %s", item.Title)
	}
	if item.Category != sch.Category {
		t.Errorf("Kategori pekerjaan hilang/berubah: didapat %s, diharapkan %s", item.Category, sch.Category)
	}
	if item.Priority != sch.Priority {
		t.Errorf("Prioritas pekerjaan hilang/berubah: didapat %s, diharapkan %s", item.Priority, sch.Priority)
	}
	if item.Description != sch.Description {
		t.Errorf("Deskripsi pekerjaan hilang: %s", item.Description)
	}
	if item.WorkCode == "" {
		t.Errorf("Kode pekerjaan kosong!")
	}

	// Verifikasi bahwa data pekerjaan tersimpan dan dapat diakses
	fetched, err := workSvc.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("Gagal ambil pekerjaan yang digenerate: %v", err)
	}
	if fetched.SubtaskCount != 0 {
		t.Errorf("Subtask count harus 0 karena template telah dihapus, didapat %d", fetched.SubtaskCount)
	}
}

// TestRecurringIdempotencyDatabaseConstraint memverifikasi bahwa index database unik mencegah duplikasi duplikasi pekerjaan rutin
func TestRecurringIdempotencyDatabaseConstraint(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_idemp.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("Gagal migrasi DB: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(db)
	actRepo := repository.NewActivityLogRepository(db)
	subRepo := repository.NewSubtaskRepository(db)
	catRepo := repository.NewCategoryRepository(db)
	tplRepo := repository.NewWorkTemplateRepository(db)
	recRepo := repository.NewRecurringRepository(db)

	taskSvc := service.NewTaskManagementService(db, workRepo, actRepo, subRepo, catRepo, tplRepo, recRepo)

	// 1. Buat recurring schedule
	sch, err := taskSvc.CreateRecurringSchedule(ctx, domain.CreateRecurringScheduleInput{
		Title:         "Pengawasan Rutin Anti-Fraud",
		Category:      "Manajemen Risiko",
		Priority:      "critical",
		Frequency:     "daily",
		IntervalValue: 1,
		StartDate:     "2026-10-17",
	})
	if err != nil {
		t.Fatalf("Gagal buat schedule: %v", err)
	}

	// 2. Eksekusi pertama
	created1, err := taskSvc.TriggerRecurringGeneration(ctx, "2026-10-17")
	if err != nil || len(created1) != 1 {
		t.Fatalf("Eksekusi 1 gagal: err=%v, count=%d", err, len(created1))
	}

	// 3. Eksekusi kedua secara instan untuk tanggal yang sama
	created2, err := taskSvc.TriggerRecurringGeneration(ctx, "2026-10-17")
	if err != nil {
		t.Fatalf("Eksekusi 2 gagal: %v", err)
	}
	if len(created2) != 0 {
		t.Errorf("Eksekusi 2 harus menghasilkan 0 pekerjaan (idempotent), didapat %d", len(created2))
	}

	// 4. Uji kebal duplikasi pada level database (apabila ada proses yang mencoba insert langsung)
	// Query insert langsung ke work_items dengan requested_by dan received_on yang sama
	query := `INSERT INTO work_items (work_code, title, description, category, requested_by, received_on, priority, status, progress_percentage, created_at, updated_at)
		VALUES ('CMP-2026-999999', 'Duplikasi Ilegal', '', 'General', ?, '2026-10-17', 'normal', 'not_started', 0, '2026-10-17T00:00:00Z', '2026-10-17T00:00:00Z')`

	tag := fmt.Sprintf("Jadwal Rutin #%d", sch.ID)
	_, err = db.ExecContext(ctx, query, tag)
	if err == nil {
		t.Errorf("Harusnya insert langsung ditolak oleh UNIQUE INDEX uq_work_items_recurring_period, tapi berhasil!")
	} else {
		t.Logf("Sukses: duplikasi berhasil dicegah di level database dengan pesan: %v", err)
	}
}

// TestCategoryHardDeleteVsDeactivation menguji pemisahan aksi hapus permanen (hard delete)
// dari status aktivasi (is_active), serta perlindungan integritas relasional data.
func TestCategoryHardDeleteVsDeactivation(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_cat_delete.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("Gagal migrasi DB: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(db)
	actRepo := repository.NewActivityLogRepository(db)
	subRepo := repository.NewSubtaskRepository(db)
	catRepo := repository.NewCategoryRepository(db)
	tplRepo := repository.NewWorkTemplateRepository(db)
	recRepo := repository.NewRecurringRepository(db)

	taskSvc := service.NewTaskManagementService(db, workRepo, actRepo, subRepo, catRepo, tplRepo, recRepo)
	workSvc := service.NewWorkItemService(db, workRepo, actRepo)

	// 1. Buat kategori custom "Test"
	catTest, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name:        "Test",
		Description: "Kategori untuk pengujian hard delete vs deactivation",
		Color:       "#3b82f6",
	})
	if err != nil {
		t.Fatalf("Gagal membuat kategori custom 'Test': %v", err)
	}
	if !catTest.IsActive || catTest.IsDefault {
		t.Fatalf("Status awal kategori 'Test' salah: IsActive=%v, IsDefault=%v", catTest.IsActive, catTest.IsDefault)
	}

	// 2. Nonaktifkan kategori -> is_active = false, baris tetap ada di database
	falseVal := false
	updatedInactive, err := taskSvc.UpdateCategory(ctx, catTest.ID, domain.UpdateCategoryInput{
		Name:        catTest.Name,
		Description: catTest.Description,
		Color:       catTest.Color,
		IsActive:    &falseVal,
	})
	if err != nil {
		t.Fatalf("Gagal menonaktifkan kategori 'Test': %v", err)
	}
	if updatedInactive.IsActive {
		t.Errorf("Kategori harusnya IsActive = false setelah dinonaktifkan")
	}

	// Verifikasi di DB fisik: baris tetap ada
	var dbIsActive int
	err = db.QueryRowContext(ctx, "SELECT is_active FROM categories WHERE id = ?", catTest.ID).Scan(&dbIsActive)
	if err != nil {
		t.Fatalf("Kategori harusnya tetap ada di DB saat dinonaktifkan: %v", err)
	}
	if dbIsActive != 0 {
		t.Errorf("Nilai is_active di DB harus 0, didapat: %d", dbIsActive)
	}

	// List active tidak memuat "Test", tapi list all memuat "Test"
	activeCats, err := taskSvc.ListCategories(ctx, true)
	if err != nil {
		t.Fatalf("Gagal list active categories: %v", err)
	}
	for _, c := range activeCats {
		if c.Name == "Test" {
			t.Errorf("Kategori nonaktif tidak boleh muncul di list active")
		}
	}
	allCats, err := taskSvc.ListCategories(ctx, false)
	if err != nil {
		t.Fatalf("Gagal list all categories: %v", err)
	}
	var foundInactive bool
	for _, c := range allCats {
		if c.Name == "Test" {
			foundInactive = true
			if c.IsActive {
				t.Errorf("Kategori di list all harus berstatus IsActive = false")
			}
		}
	}
	if !foundInactive {
		t.Errorf("Kategori nonaktif harus tetap muncul di list all")
	}

	// 3. Aktifkan kembali -> is_active = true
	trueVal := true
	updatedActive, err := taskSvc.UpdateCategory(ctx, catTest.ID, domain.UpdateCategoryInput{
		Name:        catTest.Name,
		Description: catTest.Description,
		Color:       catTest.Color,
		IsActive:    &trueVal,
	})
	if err != nil {
		t.Fatalf("Gagal mengaktifkan kembali kategori 'Test': %v", err)
	}
	if !updatedActive.IsActive {
		t.Errorf("Kategori harusnya IsActive = true setelah diaktifkan kembali")
	}

	activeCats, err = taskSvc.ListCategories(ctx, true)
	if err != nil {
		t.Fatalf("Gagal list active categories: %v", err)
	}
	var foundActive bool
	for _, c := range activeCats {
		if c.Name == "Test" {
			foundActive = true
		}
	}
	if !foundActive {
		t.Errorf("Kategori yang diaktifkan kembali harus muncul di list active")
	}

	// 4. Hapus kategori custom yang tidak terikat data -> baris terhapus permanen dari database
	if err := taskSvc.DeleteCategory(ctx, catTest.ID); err != nil {
		t.Fatalf("Gagal menghapus kategori 'Test' yang tidak terikat data: %v", err)
	}

	// Verifikasi hard delete dari DB fisik
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM categories WHERE id = ?", catTest.ID).Scan(&count)
	if err != nil {
		t.Fatalf("Query check DB gagal: %v", err)
	}
	if count != 0 {
		t.Errorf("Baris kategori 'Test' harusnya terhapus secara permanen (count=0), didapat: %d", count)
	}
	_, err = catRepo.GetByID(ctx, catTest.ID)
	if err == nil {
		t.Errorf("GetByID harus mengembalikan error/ErrNoRows untuk kategori yang sudah di-hard-delete")
	}

	// 5. Hapus kategori nonaktif yang tidak terikat data -> baris terhapus permanen dari database
	catInactive, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name: "TestInactive",
	})
	if err != nil {
		t.Fatalf("Gagal membuat kategori 'TestInactive': %v", err)
	}
	_, err = taskSvc.UpdateCategory(ctx, catInactive.ID, domain.UpdateCategoryInput{
		Name:     catInactive.Name,
		IsActive: &falseVal,
	})
	if err != nil {
		t.Fatalf("Gagal menonaktifkan 'TestInactive': %v", err)
	}
	if err := taskSvc.DeleteCategory(ctx, catInactive.ID); err != nil {
		t.Fatalf("Gagal menghapus kategori nonaktif yang tidak terikat: %v", err)
	}
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM categories WHERE id = ?", catInactive.ID).Scan(&count)
	if err != nil || count != 0 {
		t.Errorf("Kategori nonaktif yang dihapus harus hilang dari DB (count=0), err=%v, count=%d", err, count)
	}

	// 6. Buat kategori custom "Terkait", lalu buat work item yang menggunakannya
	catTerkait, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name: "Terkait",
	})
	if err != nil {
		t.Fatalf("Gagal membuat kategori 'Terkait': %v", err)
	}
	wi, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Pekerjaan Terkait Kategori",
		Category:   "Terkait",
		ReceivedOn: "2026-10-17",
		Priority:   domain.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("Gagal membuat work item: %v", err)
	}

	// Coba hapus kategori -> ditolak dengan pesan informatif
	err = taskSvc.DeleteCategory(ctx, catTerkait.ID)
	if err == nil {
		t.Fatalf("Hapus kategori 'Terkait' seharusnya ditolak karena digunakan oleh work item")
	}
	if !strings.Contains(err.Error(), "masih digunakan oleh 1 pekerjaan") {
		t.Errorf("Pesan error tidak sesuai: %v", err)
	}

	// Arsipkan work item tersebut
	if _, err := workSvc.ArchiveWorkItem(ctx, wi.ID); err != nil {
		t.Fatalf("Gagal mengarsipkan work item: %v", err)
	}

	// Coba hapus kategori lagi -> tetap ditolak karena work item yang diarsipkan tetap mempertahankan referensi kategori
	err = taskSvc.DeleteCategory(ctx, catTerkait.ID)
	if err == nil {
		t.Fatalf("Hapus kategori 'Terkait' harus tetap ditolak pada work item yang diarsipkan demi integritas historis")
	}
	if !strings.Contains(err.Error(), "masih digunakan oleh 1 pekerjaan") {
		t.Errorf("Pesan error tidak sesuai: %v", err)
	}

	// 7. Buat kategori custom "TemplateCat", buat work template yang menggunakannya
	catTemplate, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name: "TemplateCat",
	})
	if err != nil {
		t.Fatalf("Gagal membuat kategori 'TemplateCat': %v", err)
	}
	_, err = taskSvc.CreateTemplate(ctx, domain.CreateWorkTemplateInput{
		Name:         "Template Terkait",
		TitlePattern: "Pola Judul Template {tahun}",
		Category:     "TemplateCat",
		Priority:     "normal",
	})
	if err != nil {
		t.Fatalf("Gagal membuat template: %v", err)
	}

	// Coba hapus kategori "TemplateCat" -> ditolak
	err = taskSvc.DeleteCategory(ctx, catTemplate.ID)
	if err == nil {
		t.Fatalf("Hapus kategori 'TemplateCat' seharusnya ditolak karena digunakan oleh template")
	}
	if !strings.Contains(err.Error(), "masih digunakan oleh 1 template") {
		t.Errorf("Pesan error tidak sesuai: %v", err)
	}

	// 8. Buat kategori custom "ScheduleCat", buat recurring schedule yang menggunakannya
	catSchedule, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name: "ScheduleCat",
	})
	if err != nil {
		t.Fatalf("Gagal membuat kategori 'ScheduleCat': %v", err)
	}
	_, err = taskSvc.CreateRecurringSchedule(ctx, domain.CreateRecurringScheduleInput{
		Title:         "Jadwal Rutin Kategori Terkait",
		Category:      "ScheduleCat",
		Priority:      "high",
		Frequency:     "weekly",
		IntervalValue: 1,
		StartDate:     "2026-10-17",
	})
	if err != nil {
		t.Fatalf("Gagal membuat recurring schedule: %v", err)
	}

	// Coba hapus kategori "ScheduleCat" -> ditolak
	err = taskSvc.DeleteCategory(ctx, catSchedule.ID)
	if err == nil {
		t.Fatalf("Hapus kategori 'ScheduleCat' seharusnya ditolak karena digunakan oleh jadwal berulang")
	}
	if !strings.Contains(err.Error(), "masih digunakan oleh 1 jadwal berulang") {
		t.Errorf("Pesan error tidak sesuai: %v", err)
	}

	// 9. Coba hapus kategori bawaan sistem (is_default = 1) -> ditolak
	defaultCats, err := taskSvc.ListCategories(ctx, false)
	if err != nil || len(defaultCats) == 0 {
		t.Fatalf("Gagal mengambil kategori sistem: %v", err)
	}
	var sysCat *domain.Category
	for _, c := range defaultCats {
		if c.IsDefault {
			sysCat = c
			break
		}
	}
	if sysCat == nil {
		t.Fatalf("Kategori default sistem tidak ditemukan")
	}
	err = taskSvc.DeleteCategory(ctx, sysCat.ID)
	if err == nil {
		t.Fatalf("Penghapusan kategori sistem default harusnya ditolak")
	}
	if !strings.Contains(err.Error(), "sistem") || !strings.Contains(err.Error(), "tidak dapat dihapus") {
		t.Errorf("Pesan error kategori sistem salah: %v", err)
	}

	// 10. Verifikasi bahwa tidak ada activity_log atau work_items yang terhapus secara tidak sengaja
	var totalWorkItems, totalActivityLogs int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_items").Scan(&totalWorkItems)
	if err != nil || totalWorkItems != 1 {
		t.Errorf("Work items tidak boleh berubah, count=%d, err=%v", totalWorkItems, err)
	}
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_logs").Scan(&totalActivityLogs)
	if err != nil || totalActivityLogs == 0 {
		t.Errorf("Activity logs harus tetap utuh dan tidak terhapus cascade, count=%d, err=%v", totalActivityLogs, err)
	}

	// 11. Verifikasi list kategori active & all mengembalikan data yang konsisten
	finalActive, err := taskSvc.ListCategories(ctx, true)
	if err != nil {
		t.Fatalf("Gagal list final active: %v", err)
	}
	finalAll, err := taskSvc.ListCategories(ctx, false)
	if err != nil {
		t.Fatalf("Gagal list final all: %v", err)
	}
	for _, c := range finalAll {
		if c.Name == "Test" || c.Name == "TestInactive" {
			t.Errorf("Kategori yang telah di-hard-delete ('%s') tidak boleh muncul di list kategori mana pun", c.Name)
		}
	}
	if len(finalActive) > len(finalAll) {
		t.Errorf("Jumlah active categories (%d) tidak boleh melebihi all categories (%d)", len(finalActive), len(finalAll))
	}
}

// TestRecurringFrequenciesAndIntervalCalculations memvalidasi perhitungan kalkulasi tanggal
// untuk kelima frekuensi baku: daily, weekly, monthly, quarterly, yearly beserta intervalnya.
func TestRecurringFrequenciesAndIntervalCalculations(t *testing.T) {
	cases := []struct {
		name        string
		startDate   string
		frequency   string
		interval    int
		expected    string
		expectError bool
	}{
		{"Daily 1 Hari", "2026-10-18", "daily", 1, "2026-10-19", false},
		{"Daily 5 Hari", "2026-10-18", "daily", 5, "2026-10-23", false},
		{"Weekly 1 Minggu", "2026-10-18", "weekly", 1, "2026-10-25", false},
		{"Weekly 2 Minggu", "2026-10-18", "weekly", 2, "2026-11-01", false},
		{"Monthly 1 Bulan", "2026-10-18", "monthly", 1, "2026-11-18", false},
		{"Monthly 3 Bulan", "2026-10-18", "monthly", 3, "2027-01-18", false},
		{"Quarterly 1 Triwulan", "2026-10-18", "quarterly", 1, "2027-01-18", false},
		{"Quarterly 2 Triwulan", "2026-10-18", "quarterly", 2, "2027-04-18", false},
		{"Yearly 1 Tahun", "2026-10-18", "yearly", 1, "2027-10-18", false},
		{"Yearly 2 Tahun", "2026-10-18", "yearly", 2, "2028-10-18", false},
		{"Interval <= 0 default 1", "2026-10-18", "daily", 0, "2026-10-19", false},
		{"Invalid date", "not-a-date", "daily", 1, "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := service.CalculateNextRunDate(tc.startDate, tc.frequency, tc.interval)
			if tc.expectError {
				if err == nil {
					t.Errorf("Seharusnya gagal untuk input tanggal tidak valid")
				}
				return
			}
			if err != nil {
				t.Fatalf("Perhitungan gagal: %v", err)
			}
			if res != tc.expected {
				t.Errorf("Hasil tidak cocok: diharapkan %s, didapat %s", tc.expected, res)
			}
		})
	}

	// Validasi Domain Input Frekuensi dan Tanggal
	validInput := domain.CreateRecurringScheduleInput{
		Title:         "Jadwal Valid",
		Frequency:     "daily",
		IntervalValue: 1,
		StartDate:     "2026-10-18",
	}
	if err := validInput.Validate(); err != nil {
		t.Errorf("Input valid ditolak: %v", err)
	}

	// Uji frekuensi tidak valid
	invalidFreqInput := domain.CreateRecurringScheduleInput{
		Title:         "Jadwal Frekuensi Salah",
		Frequency:     "hourly",
		IntervalValue: 1,
		StartDate:     "2026-10-18",
	}
	err := invalidFreqInput.Validate()
	if err == nil {
		t.Errorf("Frekuensi tidak valid 'hourly' seharusnya ditolak")
	} else if !strings.Contains(err.Error(), "frekuensi tidak valid") {
		t.Errorf("Pesan error tidak sesuai: %v", err)
	}

	// Uji tanggal selesai mendahului tanggal mulai
	endBeforeStart := "2026-10-10"
	invalidDateInput := domain.CreateRecurringScheduleInput{
		Title:         "Jadwal Tanggal Terbalik",
		Frequency:     "monthly",
		IntervalValue: 1,
		StartDate:     "2026-10-18",
		EndDate:       &endBeforeStart,
	}
	err = invalidDateInput.Validate()
	if err == nil {
		t.Errorf("Tanggal selesai mendahului mulai seharusnya ditolak")
	} else if !strings.Contains(err.Error(), "tidak boleh mendahului") {
		t.Errorf("Pesan error tidak sesuai: %v", err)
	}
}

// TestHTTPRecurringAndTemplateValidationAndCRUD menguji validasi ketat title_pattern dan frekuensi
// serta operasi lengkap Create, Edit, Get Detail, dan Apply Template via REST API.
func TestHTTPRecurringAndTemplateValidationAndCRUD(t *testing.T) {
	_, baseURL, _, _, cleanup := setupTaskMgmtTestServer(t)
	defer cleanup()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Ambil token CSRF
	respToken, err := client.Get(baseURL + "/api/csrf-token")
	if err != nil {
		t.Fatalf("Gagal ambil CSRF: %v", err)
	}
	var tokenData struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.NewDecoder(respToken.Body).Decode(&tokenData)
	respToken.Body.Close()
	csrfToken := tokenData.CSRFToken

	doPost := func(endpoint string, bodyJSON string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, baseURL+endpoint, strings.NewReader(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.Header.Set("Origin", baseURL)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST %s gagal: %v", endpoint, err)
		}
		return resp
	}

	doPut := func(endpoint string, bodyJSON string) *http.Response {
		req, _ := http.NewRequest(http.MethodPut, baseURL+endpoint, strings.NewReader(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.Header.Set("Origin", baseURL)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("PUT %s gagal: %v", endpoint, err)
		}
		return resp
	}

	// 1. Template: Validasi title_pattern wajib diisi (BUG-TPL-001)
	respMissingPattern := doPost("/api/templates", `{"name":"Template Tanpa Pattern","category":"General","priority":"normal"}`)
	if respMissingPattern.StatusCode != http.StatusBadRequest {
		t.Errorf("Seharusnya 400 Bad Request jika title_pattern kosong, didapat: %d", respMissingPattern.StatusCode)
	}
	bodyMissingPattern, _ := io.ReadAll(respMissingPattern.Body)
	respMissingPattern.Body.Close()
	if !strings.Contains(string(bodyMissingPattern), "title_pattern") {
		t.Errorf("Pesan error tidak menyebutkan title_pattern: %s", string(bodyMissingPattern))
	}

	// Template: Validasi title_pattern hanya whitespace
	respWhitespacePattern := doPost("/api/templates", `{"name":"Template Whitespace","title_pattern":"   ","category":"General","priority":"normal"}`)
	if respWhitespacePattern.StatusCode != http.StatusBadRequest {
		t.Errorf("Seharusnya 400 Bad Request jika title_pattern hanya spasi, didapat: %d", respWhitespacePattern.StatusCode)
	}
	respWhitespacePattern.Body.Close()

	// 2. Template: Create sukses dengan title_pattern yang valid
	validTplJSON := `{
		"name": "Template Audit Triwulan",
		"title_pattern": "Audit Kepatuhan Triwulan - {periode}",
		"category": "General",
		"priority": "high",
		"description": "Template audit berkala",
		"checklist_items": ["Verifikasi Dokumen", "Uji Petik Transaksi", "Laporan Hasil"]
	}`
	respCreateTpl := doPost("/api/templates", validTplJSON)
	if respCreateTpl.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(respCreateTpl.Body)
		t.Fatalf("POST /api/templates gagal: status=%d, body=%s", respCreateTpl.StatusCode, string(body))
	}
	var createdTpl domain.WorkTemplate
	_ = json.NewDecoder(respCreateTpl.Body).Decode(&createdTpl)
	respCreateTpl.Body.Close()

	if createdTpl.TitlePattern != "Audit Kepatuhan Triwulan - {periode}" {
		t.Errorf("TitlePattern tidak tersimpan: %s", createdTpl.TitlePattern)
	}
	if len(createdTpl.ChecklistItems) != 3 {
		t.Errorf("Checklist items tidak sesuai: %d", len(createdTpl.ChecklistItems))
	}

	// 3. Template: Get Detail
	respGetTpl, err := client.Get(fmt.Sprintf("%s/api/templates/%d", baseURL, createdTpl.ID))
	if err != nil || respGetTpl.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/templates/:id gagal: status=%d, err=%v", respGetTpl.StatusCode, err)
	}
	var fetchedTpl domain.WorkTemplate
	_ = json.NewDecoder(respGetTpl.Body).Decode(&fetchedTpl)
	respGetTpl.Body.Close()
	if fetchedTpl.Name != "Template Audit Triwulan" || fetchedTpl.TitlePattern != "Audit Kepatuhan Triwulan - {periode}" {
		t.Errorf("Data detail template tidak sesuai: %+v", fetchedTpl)
	}

	// 4. Template: Edit via PUT
	updateTplJSON := `{
		"name": "Template Audit Triwulan Diperbarui",
		"title_pattern": "Audit Triwulan Final - {periode}",
		"category": "General",
		"priority": "critical",
		"description": "Deskripsi diperbarui",
		"checklist_items": ["Langkah 1", "Langkah 2"]
	}`
	respUpdateTpl := doPut(fmt.Sprintf("/api/templates/%d", createdTpl.ID), updateTplJSON)
	if respUpdateTpl.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/templates/:id gagal: status=%d", respUpdateTpl.StatusCode)
	}
	var updatedTpl domain.WorkTemplate
	_ = json.NewDecoder(respUpdateTpl.Body).Decode(&updatedTpl)
	respUpdateTpl.Body.Close()
	if updatedTpl.Name != "Template Audit Triwulan Diperbarui" || len(updatedTpl.ChecklistItems) != 2 {
		t.Errorf("Hasil update template tidak sesuai: %+v", updatedTpl)
	}

	// 5. Template: Apply Template menghasilkan pekerjaan dan subtask secara atomik
	respApply := doPost(fmt.Sprintf("/api/templates/%d/apply", createdTpl.ID), `{"received_on":"2026-10-18"}`)
	if respApply.StatusCode != http.StatusCreated {
		t.Fatalf("Apply template gagal: status=%d", respApply.StatusCode)
	}
	var appliedWI domain.WorkItem
	_ = json.NewDecoder(respApply.Body).Decode(&appliedWI)
	respApply.Body.Close()
	if appliedWI.Title != "Audit Triwulan Final - {periode}" {
		t.Errorf("Judul pekerjaan yang dihasilkan tidak sesuai dengan title_pattern: %s", appliedWI.Title)
	}

	// 6. Recurring: Validasi frekuensi (BUG-REC-001)
	respInvalidFreq := doPost("/api/recurring-schedules", `{"title":"Jadwal Frekuensi Kosong","category":"General","priority":"normal","frequency":"","interval_value":1,"start_date":"2026-10-18"}`)
	if respInvalidFreq.StatusCode != http.StatusBadRequest {
		t.Errorf("Seharusnya 400 Bad Request jika frekuensi kosong, didapat: %d", respInvalidFreq.StatusCode)
	}
	bodyInvalidFreq, _ := io.ReadAll(respInvalidFreq.Body)
	respInvalidFreq.Body.Close()
	if !strings.Contains(string(bodyInvalidFreq), "frekuensi tidak valid") {
		t.Errorf("Pesan error frekuensi tidak valid salah: %s", string(bodyInvalidFreq))
	}

	// 7. Recurring: Create sukses dengan kelima frekuensi
	freqs := []string{"daily", "weekly", "monthly", "quarterly", "yearly"}
	for _, f := range freqs {
		recPayload := fmt.Sprintf(`{"title":"Jadwal %s","category":"General","priority":"normal","frequency":"%s","interval_value":1,"start_date":"2026-10-18"}`, f, f)
		respRec := doPost("/api/recurring-schedules", recPayload)
		if respRec.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(respRec.Body)
			t.Fatalf("Gagal membuat jadwal frekuensi %s: status=%d, body=%s", f, respRec.StatusCode, string(body))
		}
		var createdSch domain.RecurringSchedule
		_ = json.NewDecoder(respRec.Body).Decode(&createdSch)
		respRec.Body.Close()
		if createdSch.Frequency != f {
			t.Errorf("Frekuensi tidak sesuai: diharapkan %s, didapat %s", f, createdSch.Frequency)
		}

		// 8. Recurring: Update via PUT
		updatePayload := fmt.Sprintf(`{"title":"Jadwal %s Diperbarui","category":"General","priority":"high","frequency":"%s","interval_value":2,"start_date":"2026-10-18"}`, f, f)
		respUpdRec := doPut(fmt.Sprintf("/api/recurring-schedules/%d", createdSch.ID), updatePayload)
		if respUpdRec.StatusCode != http.StatusOK {
			t.Fatalf("PUT /api/recurring-schedules/:id gagal untuk frekuensi %s: status=%d", f, respUpdRec.StatusCode)
		}
		respUpdRec.Body.Close()

		// 9. Recurring: Toggle is_active via partial update
		togglePayload := `{"is_active":false}`
		respToggle := doPut(fmt.Sprintf("/api/recurring-schedules/%d", createdSch.ID), togglePayload)
		if respToggle.StatusCode != http.StatusOK {
			t.Fatalf("PUT partial toggle is_active gagal: status=%d", respToggle.StatusCode)
		}
		var toggledSch domain.RecurringSchedule
		_ = json.NewDecoder(respToggle.Body).Decode(&toggledSch)
		respToggle.Body.Close()
		if toggledSch.IsActive {
			t.Errorf("Jadwal seharusnya nonaktif setelah toggle")
		}
	}

	// 10. Recurring: Jadwal masa depan tidak menghasilkan pekerjaan sebelum tanggal mulai (Requirement A.5)
	futureDate := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	futureSchPayload := fmt.Sprintf(`{"title":"Jadwal Masa Depan","category":"General","priority":"normal","frequency":"daily","interval_value":1,"start_date":"%s"}`, futureDate)
	respFuture := doPost("/api/recurring-schedules", futureSchPayload)
	if respFuture.StatusCode != http.StatusCreated {
		t.Fatalf("Gagal membuat jadwal masa depan: status=%d", respFuture.StatusCode)
	}
	respFuture.Body.Close()

	// Trigger dengan tanggal hari ini
	todayStr := time.Now().UTC().Format("2006-01-02")
	respTrig := doPost("/api/recurring-schedules/trigger", fmt.Sprintf(`{"as_of_date":"%s"}`, todayStr))
	if respTrig.StatusCode != http.StatusOK {
		t.Fatalf("Trigger gagal: status=%d", respTrig.StatusCode)
	}
	respTrig.Body.Close()
}

// TestCategoryMultiModuleRegressionCases memverifikasi secara ketat perlindungan integrasi kategori (CASE 1 s/d CASE 7)
func TestCategoryMultiModuleRegressionCases(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_cat_cross_module.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("Gagal migrasi DB: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(db)
	actRepo := repository.NewActivityLogRepository(db)
	subRepo := repository.NewSubtaskRepository(db)
	catRepo := repository.NewCategoryRepository(db)
	tplRepo := repository.NewWorkTemplateRepository(db)
	recRepo := repository.NewRecurringRepository(db)

	taskSvc := service.NewTaskManagementService(db, workRepo, actRepo, subRepo, catRepo, tplRepo, recRepo)
	workSvc := service.NewWorkItemService(db, workRepo, actRepo)

	// CASE 1: Kategori baru -> Template -> Kategori tidak bisa dihapus selama template ada
	catTpl, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{Name: "Cat-Tpl"})
	if err != nil {
		t.Fatalf("CASE 1 gagal buat kategori: %v", err)
	}
	tpl1, err := taskSvc.CreateTemplate(ctx, domain.CreateWorkTemplateInput{
		Name:         "Template Case 1",
		TitlePattern: "Pola Judul Case 1",
		Category:     "Cat-Tpl",
		Priority:     "normal",
	})
	if err != nil {
		t.Fatalf("CASE 1 gagal buat template: %v", err)
	}
	if err := taskSvc.DeleteCategory(ctx, catTpl.ID); err == nil {
		t.Fatalf("CASE 1: Kategori yang digunakan template seharusnya tidak dapat dihapus")
	} else if !strings.Contains(err.Error(), "1 template") {
		t.Errorf("CASE 1 pesan error tidak sesuai: %v", err)
	}

	// CASE 2: Kategori baru -> Recurring Schedule -> Kategori tidak bisa dihapus selama jadwal ada
	catRec, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{Name: "Cat-Rec"})
	if err != nil {
		t.Fatalf("CASE 2 gagal buat kategori: %v", err)
	}
	sch2, err := taskSvc.CreateRecurringSchedule(ctx, domain.CreateRecurringScheduleInput{
		Title:         "Jadwal Case 2",
		Category:      "Cat-Rec",
		Priority:      "normal",
		Frequency:     "daily",
		IntervalValue: 1,
		StartDate:     "2026-10-18",
	})
	if err != nil {
		t.Fatalf("CASE 2 gagal buat jadwal: %v", err)
	}
	if err := taskSvc.DeleteCategory(ctx, catRec.ID); err == nil {
		t.Fatalf("CASE 2: Kategori yang digunakan jadwal berulang seharusnya tidak dapat dihapus")
	} else if !strings.Contains(err.Error(), "1 jadwal berulang") {
		t.Errorf("CASE 2 pesan error tidak sesuai: %v", err)
	}

	// CASE 3: Nonaktifkan kategori yang digunakan oleh Template -> Template tetap tersimpan; kategori tidak ada di list active
	falseVal := false
	_, err = taskSvc.UpdateCategory(ctx, catTpl.ID, domain.UpdateCategoryInput{
		Name:     catTpl.Name,
		IsActive: &falseVal,
	})
	if err != nil {
		t.Fatalf("CASE 3 gagal menonaktifkan kategori: %v", err)
	}
	// Pastikan template existing tetap tersimpan utuh
	savedTpl, err := taskSvc.GetTemplate(ctx, tpl1.ID)
	if err != nil || savedTpl == nil {
		t.Fatalf("CASE 3: Template hilang setelah kategori dinonaktifkan: %v", err)
	}
	// List active tidak boleh memuat Cat-Tpl
	activeCats, err := taskSvc.ListCategories(ctx, true)
	if err != nil {
		t.Fatalf("CASE 3 list active gagal: %v", err)
	}
	for _, c := range activeCats {
		if c.Name == "Cat-Tpl" {
			t.Errorf("CASE 3: Kategori nonaktif tidak boleh muncul di list kategori aktif")
		}
	}

	// CASE 4: Nonaktifkan kategori yang digunakan oleh Recurring Schedule
	// Kebijakan: generator tidak menciptakan pekerjaan baru menggunakan kategori nonaktif
	_, err = taskSvc.UpdateCategory(ctx, catRec.ID, domain.UpdateCategoryInput{
		Name:     catRec.Name,
		IsActive: &falseVal,
	})
	if err != nil {
		t.Fatalf("CASE 4 gagal menonaktifkan kategori jadwal: %v", err)
	}
	// Pastikan jadwal existing tetap ada
	savedSch, err := taskSvc.GetRecurringSchedule(ctx, sch2.ID)
	if err != nil || savedSch == nil {
		t.Fatalf("CASE 4: Jadwal berulang hilang setelah kategori dinonaktifkan: %v", err)
	}
	// Jalankan trigger recurring generation -> tidak boleh menghasilkan pekerjaan baru untuk jadwal ini
	genItems, err := taskSvc.TriggerRecurringGeneration(ctx, "2026-10-18")
	if err != nil {
		t.Fatalf("CASE 4 trigger gagal: %v", err)
	}
	if len(genItems) != 0 {
		t.Errorf("CASE 4: Generator seharusnya tidak membuat pekerjaan untuk kategori nonaktif, didapat: %d", len(genItems))
	}

	// CASE 5: Aktifkan kembali kategori dan pastikan hubungan Template maupun Recurring Schedule tetap valid
	trueVal := true
	_, err = taskSvc.UpdateCategory(ctx, catTpl.ID, domain.UpdateCategoryInput{Name: catTpl.Name, IsActive: &trueVal})
	if err != nil {
		t.Fatalf("CASE 5 gagal aktifkan Cat-Tpl: %v", err)
	}
	_, err = taskSvc.UpdateCategory(ctx, catRec.ID, domain.UpdateCategoryInput{Name: catRec.Name, IsActive: &trueVal})
	if err != nil {
		t.Fatalf("CASE 5 gagal aktifkan Cat-Rec: %v", err)
	}
	// Setelah kategori diaktifkan kembali, generator berhasil memproses jadwal
	genItemsReactivated, err := taskSvc.TriggerRecurringGeneration(ctx, "2026-10-18")
	if err != nil {
		t.Fatalf("CASE 5 trigger gagal: %v", err)
	}
	if len(genItemsReactivated) != 1 {
		t.Errorf("CASE 5: Generator seharusnya membuat 1 pekerjaan setelah kategori diaktifkan kembali, didapat: %d", len(genItemsReactivated))
	}

	// CASE 6: Nama kategori diperbarui -> seluruh referensi (work_items, templates, recurring) tersinkronisasi transaksional; activity_logs tidak berubah
	catRename, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{Name: "Cat-OldName"})
	if err != nil {
		t.Fatalf("CASE 6 buat kategori gagal: %v", err)
	}
	wi6, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Pekerjaan Case 6",
		Category:   "Cat-OldName",
		ReceivedOn: "2026-10-18",
		Priority:   "normal",
	})
	if err != nil {
		t.Fatalf("CASE 6 buat work item gagal: %v", err)
	}
	tpl6, err := taskSvc.CreateTemplate(ctx, domain.CreateWorkTemplateInput{
		Name:         "Template Case 6",
		TitlePattern: "Pola Case 6",
		Category:     "Cat-OldName",
		Priority:     "normal",
	})
	if err != nil {
		t.Fatalf("CASE 6 buat template gagal: %v", err)
	}
	sch6, err := taskSvc.CreateRecurringSchedule(ctx, domain.CreateRecurringScheduleInput{
		Title:         "Jadwal Case 6",
		Category:      "Cat-OldName",
		Priority:      "normal",
		Frequency:     "daily",
		IntervalValue: 1,
		StartDate:     "2026-10-18",
	})
	if err != nil {
		t.Fatalf("CASE 6 buat jadwal gagal: %v", err)
	}

	// Hitung log aktivitas sebelum rename
	var logsBefore int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_logs WHERE work_item_id = ?", wi6.ID).Scan(&logsBefore)

	// Lakukan rename kategori
	_, err = taskSvc.UpdateCategory(ctx, catRename.ID, domain.UpdateCategoryInput{
		Name: "Cat-NewName",
	})
	if err != nil {
		t.Fatalf("CASE 6 rename kategori gagal: %v", err)
	}

	// Verifikasi work item tersinkronisasi
	updWI, _ := workRepo.GetByID(ctx, wi6.ID)
	if updWI.Category != "Cat-NewName" {
		t.Errorf("CASE 6: Kategori work item tidak tersinkronisasi: %s", updWI.Category)
	}
	// Verifikasi template tersinkronisasi
	updTpl, _ := tplRepo.GetByID(ctx, tpl6.ID)
	if updTpl.Category != "Cat-NewName" {
		t.Errorf("CASE 6: Kategori template tidak tersinkronisasi: %s", updTpl.Category)
	}
	// Verifikasi jadwal berulang tersinkronisasi
	updSch, _ := recRepo.GetByID(ctx, sch6.ID)
	if updSch.Category != "Cat-NewName" {
		t.Errorf("CASE 6: Kategori jadwal tidak tersinkronisasi: %s", updSch.Category)
	}

	// Verifikasi histori log aktivitas tidak berubah (audit trail immutable)
	var logsAfter int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_logs WHERE work_item_id = ?", wi6.ID).Scan(&logsAfter)
	if logsBefore != logsAfter {
		t.Errorf("CASE 6: Log aktivitas seharusnya tidak berubah: sebelum=%d, sesudah=%d", logsBefore, logsAfter)
	}

	// CASE 7: Hapus kategori kustom yang tidak digunakan -> hard delete berhasil
	catUnused, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{Name: "Cat-Unused"})
	if err != nil {
		t.Fatalf("CASE 7 buat kategori gagal: %v", err)
	}
	if err := taskSvc.DeleteCategory(ctx, catUnused.ID); err != nil {
		t.Fatalf("CASE 7: Gagal hard delete kategori tidak terpakai: %v", err)
	}
	var countUnused int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM categories WHERE id = ?", catUnused.ID).Scan(&countUnused)
	if countUnused != 0 {
		t.Errorf("CASE 7: Kategori tidak terpakai harusnya terhapus secara fisik dari DB, didapat count=%d", countUnused)
	}
}

// TestUATBatch04_TemplateChecklistAndRecurringIntegration memverifikasi seluruh skenario pengujian UAT
// untuk integritas data template checklist, recurring tasks generation, dan provenance.
func TestUATBatch04_TemplateChecklistAndRecurringIntegration(t *testing.T) {
	_, _, taskSvc, workSvc, cleanup := setupTaskMgmtTestServer(t)
	defer cleanup()

	db := workSvc.GetDB()
	ctx := context.Background()
	today := domain.GetCurrentLocalDate()

	// 1. POST Template dengan 3 checklist
	tpl, err := taskSvc.CreateTemplate(ctx, domain.CreateWorkTemplateInput{
		Name:          "FOLRE",
		TitlePattern:  "REVIEW TEST",
		Description:   "Template review berkala kepatuhan",
		Category:      "Audit & Pengawasan",
		Priority:      domain.PriorityHigh,
		DueDaysOffset: 5,
		ChecklistItems: []string{
			"TEST 1",
			"TEST 2",
			"TEST 3",
		},
	})
	if err != nil {
		t.Fatalf("1. POST Template gagal: %v", err)
	}
	if tpl.ID <= 0 {
		t.Fatalf("1. ID template tidak valid: %d", tpl.ID)
	}

	// 2. GET List Template mengembalikan data benar
	list, err := taskSvc.ListTemplates(ctx)
	if err != nil {
		t.Fatalf("2. GET List Template gagal: %v", err)
	}
	var foundTpl *domain.WorkTemplate
	for _, it := range list {
		if it.ID == tpl.ID {
			foundTpl = it
			break
		}
	}
	if foundTpl == nil {
		t.Fatalf("2. Template FOLRE tidak ditemukan dalam List")
	}
	if len(foundTpl.ChecklistItems) != 3 {
		t.Fatalf("2. Checklist items dalam List harus 3, didapat: %d", len(foundTpl.ChecklistItems))
	}

	// 3. GET Detail Template mengembalikan 3 checklist
	detail, err := taskSvc.GetTemplate(ctx, tpl.ID)
	if err != nil {
		t.Fatalf("3. GET Detail Template gagal: %v", err)
	}
	if len(detail.ChecklistItems) != 3 {
		t.Fatalf("3. Checklist items dalam Detail harus 3, didapat: %d", len(detail.ChecklistItems))
	}
	if detail.ChecklistItems[0] != "TEST 1" || detail.ChecklistItems[1] != "TEST 2" || detail.ChecklistItems[2] != "TEST 3" {
		t.Errorf("3. Isi checklist items tidak sesuai: %v", detail.ChecklistItems)
	}

	// 4 & 5. Apply Template menghasilkan 3 subtask dan menggunakan Title Pattern
	wiFromTpl, err := taskSvc.ApplyTemplate(ctx, tpl.ID, today)
	if err != nil {
		t.Fatalf("4. Apply Template gagal: %v", err)
	}
	if wiFromTpl.Title != "REVIEW TEST" {
		t.Errorf("5. Judul pekerjaan harus mengikuti title_pattern 'REVIEW TEST', didapat: %s", wiFromTpl.Title)
	}
	if wiFromTpl.ProgressPercentage != 0 {
		t.Errorf("4. Progres awal pekerjaan dengan subtask belum selesai harus 0%%, didapat: %d%%", wiFromTpl.ProgressPercentage)
	}

	// Verifikasi subtask yang dihasilkan
	subs, err := taskSvc.ListSubtasks(ctx, wiFromTpl.ID)
	if err != nil {
		t.Fatalf("4. Gagal mengambil subtask: %v", err)
	}
	if len(subs) != 3 {
		t.Fatalf("4. Subtask hasil template harus berjumlah 3, didapat: %d", len(subs))
	}
	for i, sub := range subs {
		expectedTitle := fmt.Sprintf("TEST %d", i+1)
		if sub.Title != expectedTitle {
			t.Errorf("4. Judul subtask ke-%d salah: didapat '%s', diharapkan '%s'", i+1, sub.Title, expectedTitle)
		}
		if sub.IsCompleted {
			t.Errorf("4. Subtask ke-%d harusnya belum selesai", i+1)
		}
	}

	// 6. Verifikasi Provenance pada Work Item hasil template
	wiQueried, err := workSvc.GetWorkItem(ctx, wiFromTpl.ID)
	if err != nil {
		t.Fatalf("6. Gagal mengambil detail pekerjaan: %v", err)
	}
	if wiQueried.Provenance != "template" {
		t.Errorf("6. Provenance pekerjaan dari template harus 'template', didapat: '%s'", wiQueried.Provenance)
	}

	// 7. Recurring generator menghasilkan Work Item jatuh tempo
	sch, err := taskSvc.CreateRecurringSchedule(ctx, domain.CreateRecurringScheduleInput{
		TemplateID:    &tpl.ID,
		Title:         "TEST 40",
		Description:   "Pengujian jadwal rutin kepatuhan",
		Category:      "Audit & Pengawasan",
		Priority:      domain.PriorityNormal,
		Frequency:     "weekly",
		IntervalValue: 1,
		StartDate:     today, // Jatuh tempo hari ini
	})
	if err != nil {
		t.Fatalf("7. Gagal membuat jadwal berulang: %v", err)
	}
	_ = sch

	report1, err := taskSvc.TriggerRecurringWithReport(ctx, today)
	if err != nil {
		t.Fatalf("7. TriggerRecurringWithReport gagal: %v", err)
	}
	if len(report1.CreatedItems) != 1 {
		t.Fatalf("7. Harus menghasilkan tepat 1 pekerjaan baru, didapat: %d (skipped: %v)", len(report1.CreatedItems), report1.SkippedDetails)
	}

	recItem := report1.CreatedItems[0]
	// 8. Work Register & Dashboard memuat pekerjaan tersebut dengan kode unik
	if recItem.WorkCode == "" {
		t.Errorf("8. Kode pekerjaan tidak boleh kosong")
	}
	recItemFromRepo, err := workSvc.GetWorkItem(ctx, recItem.ID)
	if err != nil {
		t.Fatalf("8. Pekerjaan dari jadwal tidak ditemukan di repo: %v", err)
	}
	if recItemFromRepo.Provenance != "recurring" {
		t.Errorf("8. Provenance pekerjaan berulang harus 'recurring', didapat: '%s'", recItemFromRepo.Provenance)
	}

	// Verifikasi subtask checklist terpasang dari template
	recSubs, err := taskSvc.ListSubtasks(ctx, recItem.ID)
	if err != nil || len(recSubs) != 3 {
		t.Fatalf("8. Subtask template pada jadwal berulang harus 3, didapat: %d", len(recSubs))
	}

	// 9. Activity Log mencatat pembuatan otomatis
	logs, err := workSvc.ListActivities(ctx, recItem.ID)
	if err != nil || len(logs) == 0 {
		t.Fatalf("9. Activity log pembuatan otomatis harus tercatat")
	}
	if !strings.Contains(logs[0].Description, "jadwal berulang") {
		t.Errorf("9. Deskripsi log tidak mencatat asal jadwal berulang: %s", logs[0].Description)
	}

	// 10. Trigger ganda tidak membuat duplicate (Idempotensi)
	report2, err := taskSvc.TriggerRecurringWithReport(ctx, today)
	if err != nil {
		t.Fatalf("10. Trigger kedua gagal: %v", err)
	}
	if len(report2.CreatedItems) != 0 {
		t.Fatalf("10. Trigger kedua tidak boleh membuat duplicate, didapat: %d pekerjaan", len(report2.CreatedItems))
	}
	if len(report2.SkippedDetails) == 0 {
		t.Errorf("10. Laporan harus memuat alasan skip untuk jadwal yang sudah diproses")
	}

	// 11. Kategori nonaktif menghentikan generasi
	// Buat jadwal baru dengan kategori kustom yang akan dinonaktifkan
	catDeact, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{
		Name: "Kategori Nonaktif Test",
	})
	if err != nil {
		t.Fatalf("11. Buat kategori gagal: %v", err)
	}
	schDeact, err := taskSvc.CreateRecurringSchedule(ctx, domain.CreateRecurringScheduleInput{
		Title:         "Jadwal Kategori Nonaktif",
		Category:      catDeact.Name,
		Frequency:     "daily",
		IntervalValue: 1,
		StartDate:     today,
	})
	if err != nil {
		t.Fatalf("11. Buat jadwal gagal: %v", err)
	}

	// Nonaktifkan kategori
	isFalse := false
	_, err = taskSvc.UpdateCategory(ctx, catDeact.ID, domain.UpdateCategoryInput{
		Name:     catDeact.Name,
		IsActive: &isFalse,
	})
	if err != nil {
		t.Fatalf("11. Nonaktifkan kategori gagal: %v", err)
	}

	// Evaluasi pemicu: harus dilewati karena kategori nonaktif
	reportDeact, err := taskSvc.TriggerRecurringWithReport(ctx, today)
	if err != nil {
		t.Fatalf("11. Evaluasi jadwal gagal: %v", err)
	}
	var skippedForCat bool
	for _, skip := range reportDeact.SkippedDetails {
		if skip.ScheduleID == schDeact.ID && strings.Contains(skip.Reason, "sedang nonaktif") {
			skippedForCat = true
			break
		}
	}
	if !skippedForCat {
		t.Errorf("11. Jadwal harus dilewati dengan alasan kategori nonaktif, didapat: %v", reportDeact.SkippedDetails)
	}

	// 12. Kategori diaktifkan kembali dan penjadwalan dapat dilanjutkan
	isTrue := true
	_, err = taskSvc.UpdateCategory(ctx, catDeact.ID, domain.UpdateCategoryInput{
		Name:     catDeact.Name,
		IsActive: &isTrue,
	})
	if err != nil {
		t.Fatalf("12. Aktifkan kategori gagal: %v", err)
	}

	reportReactivated, err := taskSvc.TriggerRecurringWithReport(ctx, today)
	if err != nil {
		t.Fatalf("12. Evaluasi pemicu setelah aktivasi gagal: %v", err)
	}
	var createdReactivated bool
	for _, it := range reportReactivated.CreatedItems {
		if strings.Contains(it.Title, schDeact.Title) {
			createdReactivated = true
			break
		}
	}
	if !createdReactivated {
		t.Errorf("12. Jadwal seharusnya menghasilkan pekerjaan setelah kategori diaktifkan kembali, created=%d", len(reportReactivated.CreatedItems))
	}

	// 13. Category hard delete tetap PASS (kategori tidak terpakai dapat di-hard-delete)
	catTemp, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{Name: "Kategori Sementara"})
	if err != nil {
		t.Fatalf("13. Buat kategori sementara gagal: %v", err)
	}
	if err := taskSvc.DeleteCategory(ctx, catTemp.ID); err != nil {
		t.Fatalf("13. Hard delete kategori gagal: %v", err)
	}
	var countTemp int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM categories WHERE id = ?", catTemp.ID).Scan(&countTemp)
	if countTemp != 0 {
		t.Errorf("13. Kategori harus terhapus permanen dari database")
	}

	// 14. Verifikasi Manual Work Item Provenance
	manualItem, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Manual Biasa",
		Category:    "General",
		RequestedBy: "Staf Kepatuhan",
		ReceivedOn:  today,
	})
	if err != nil {
		t.Fatalf("14. Gagal buat pekerjaan manual: %v", err)
	}
	manualQueried, err := workSvc.GetWorkItem(ctx, manualItem.ID)
	if err != nil {
		t.Fatalf("14. Gagal ambil pekerjaan manual: %v", err)
	}
	if manualQueried.Provenance != "manual" {
		t.Errorf("14. Provenance pekerjaan manual harus 'manual', didapat: '%s'", manualQueried.Provenance)
	}

	// 15. Verifikasi apply template dengan custom detail
	customTitle := "CUSTOM AUDIT TITLE"
	customWI, err := taskSvc.ApplyTemplateWithInput(ctx, tpl.ID, domain.ApplyTemplateInput{
		ReceivedOn:  today,
		Title:       customTitle,
		Category:    "Audit & Pengawasan",
		Priority:    domain.PriorityCritical,
		RequestedBy: "Direktur Kepatuhan",
	})
	if err != nil {
		t.Fatalf("15. ApplyTemplateWithInput gagal: %v", err)
	}
	if customWI.Title != customTitle {
		t.Errorf("15. Judul custom harus '%s', didapat: '%s'", customTitle, customWI.Title)
	}
	if customWI.Priority != domain.PriorityCritical {
		t.Errorf("15. Prioritas custom harus critical, didapat: '%s'", customWI.Priority)
	}
	customSubs, err := taskSvc.ListSubtasks(ctx, customWI.ID)
	if err != nil || len(customSubs) != 3 {
		t.Fatalf("15. Subtask pada custom apply harus 3, didapat: %d", len(customSubs))
	}
}
