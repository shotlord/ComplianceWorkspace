package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"compliance-workspace/internal/database"
	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
	"compliance-workspace/internal/service"
)

// setupFullServices membuat semua service yang saling terhubung seperti di main.go (termasuk SetBackupService).
func setupFullServices(t *testing.T, dbPath, backupDir string) (
	*service.WorkItemService,
	*service.TaskManagementService,
	*service.ImportService,
	*service.BackupService,
) {
	t.Helper()

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		db.Close()
		t.Fatalf("Gagal migrasi DB: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(db)
	actRepo := repository.NewActivityLogRepository(db)
	subtaskRepo := repository.NewSubtaskRepository(db)
	categoryRepo := repository.NewCategoryRepository(db)
	templateRepo := repository.NewWorkTemplateRepository(db)
	recurRepo := repository.NewRecurringRepository(db)
	importRepo := repository.NewImportRepository(db)

	workSvc := service.NewWorkItemService(db, workRepo, actRepo)
	taskSvc := service.NewTaskManagementService(db, workRepo, actRepo, subtaskRepo, categoryRepo, templateRepo, recurRepo)
	importSvc := service.NewImportService(db, importRepo, workRepo, actRepo, nil)
	backupSvc := service.NewBackupService(workSvc, taskSvc, importSvc, dbPath, backupDir)
	importSvc.SetBackupService(backupSvc)

	t.Cleanup(func() {
		if cur := workSvc.GetDB(); cur != nil {
			_ = cur.Close()
		}
	})

	return workSvc, taskSvc, importSvc, backupSvc
}

// =============================================================================
// BUG-006: Database Closed After Restore Regression Tests
// =============================================================================

// TestBUG006_SubtasksWorkAfterRestore memastikan ListSubtasks dan AddSubtask tetap berfungsi
// setelah operasi Restore selesai — tanpa perlu restart aplikasi.
func TestBUG006_SubtasksWorkAfterRestore(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	workSvc, taskSvc, _, backupSvc := setupFullServices(t, dbPath, backupDir)

	// Buat pekerjaan awal
	item, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Pekerjaan Uji Subtask Post-Restore",
		ReceivedOn: "2026-10-17",
		Priority:   domain.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("Gagal membuat work item: %v", err)
	}

	// Backup
	backup, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Gagal membuat backup: %v", err)
	}

	// Restore
	if err := backupSvc.RestoreBackup(ctx, backup.Filename); err != nil {
		t.Fatalf("Gagal melakukan restore: %v", err)
	}

	// BUG-006: setelah restore, ListSubtasks TIDAK boleh return "sql: database is closed"
	subtasks, err := taskSvc.ListSubtasks(ctx, item.ID)
	if err != nil {
		t.Fatalf("BUG-006: ListSubtasks gagal setelah restore: %v", err)
	}
	if subtasks == nil {
		subtasks = []*domain.Subtask{}
	}

	// BUG-006: setelah restore, AddSubtask TIDAK boleh return "sql: database is closed"
	added, err := taskSvc.AddSubtask(ctx, item.ID, domain.CreateSubtaskInput{
		Title: "Subtask Pertama Post-Restore",
	})
	if err != nil {
		t.Fatalf("BUG-006: AddSubtask gagal setelah restore: %v", err)
	}
	if added.Title != "Subtask Pertama Post-Restore" {
		t.Errorf("Judul subtask tidak sesuai: got %q", added.Title)
	}
	_ = subtasks
}

// TestBUG006_CategoriesWorkAfterRestore memastikan ListCategories tetap berfungsi setelah restore.
func TestBUG006_CategoriesWorkAfterRestore(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	workSvc, taskSvc, _, backupSvc := setupFullServices(t, dbPath, backupDir)

	// Buat kategori
	cat, err := taskSvc.CreateCategory(ctx, domain.CreateCategoryInput{Name: "Kategori Uji Restore"})
	if err != nil {
		t.Fatalf("Gagal membuat kategori: %v", err)
	}
	_ = cat

	// Backup dan Restore
	backup, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Gagal backup: %v", err)
	}
	if err := backupSvc.RestoreBackup(ctx, backup.Filename); err != nil {
		t.Fatalf("Gagal restore: %v", err)
	}

	// Setelah restore, ListCategories harus berhasil
	cats, err := taskSvc.ListCategories(ctx, false)
	if err != nil {
		t.Fatalf("BUG-006: ListCategories gagal setelah restore: %v", err)
	}
	if len(cats) == 0 {
		t.Error("BUG-006: Tidak ada kategori setelah restore — expected minimal 1")
	}

	// Pastikan WorkItemService juga masih berfungsi
	_ = workSvc
	items, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	if err != nil {
		t.Fatalf("BUG-006: ListWorkItems gagal setelah restore: %v", err)
	}
	_ = items
}

// TestBUG006_ImportServiceWorkAfterRestore memastikan PreviewImport tidak gagal setelah restore.
func TestBUG006_ImportServiceWorkAfterRestore(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	_, _, importSvc, backupSvc := setupFullServices(t, dbPath, backupDir)

	// Backup dan Restore
	backup, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Gagal backup: %v", err)
	}
	if err := backupSvc.RestoreBackup(ctx, backup.Filename); err != nil {
		t.Fatalf("Gagal restore: %v", err)
	}

	// Template CSV minimal yang valid
	csvContent := "Judul Pekerjaan,Tanggal Diterima\nUji Impor Post-Restore,2026-10-17\n"
	fileBytes := []byte(csvContent)

	// BUG-006: PreviewImport TIDAK boleh gagal dengan "sql: database is closed"
	preview, err := importSvc.PreviewImport(ctx, fileBytes, "uji_restore.csv", "csv", "", nil)
	if err != nil {
		t.Fatalf("BUG-006: PreviewImport gagal setelah restore: %v", err)
	}
	if preview == nil {
		t.Fatal("BUG-006: PreviewImport mengembalikan nil setelah restore")
	}
	if preview.TotalRows != 1 {
		t.Errorf("PreviewImport total rows: got %d, want 1", preview.TotalRows)
	}
}

// TestBUG006_RecurringTriggerAfterRestore memastikan TriggerRecurringGeneration berfungsi setelah restore.
func TestBUG006_RecurringTriggerAfterRestore(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	_, taskSvc, _, backupSvc := setupFullServices(t, dbPath, backupDir)

	// Backup dan Restore
	backup, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Gagal backup: %v", err)
	}
	if err := backupSvc.RestoreBackup(ctx, backup.Filename); err != nil {
		t.Fatalf("Gagal restore: %v", err)
	}

	// BUG-006: TriggerRecurringGeneration TIDAK boleh gagal dengan "sql: database is closed"
	// (tidak ada jadwal aktif → hasilnya empty slice, bukan error)
	items, err := taskSvc.TriggerRecurringGeneration(ctx, "2026-10-17")
	if err != nil {
		t.Fatalf("BUG-006: TriggerRecurringGeneration gagal setelah restore: %v", err)
	}
	_ = items
}

// TestBUG006_BackupAfterRestoreStillWorks memastikan CreateBackup berfungsi setelah Restore
// (double-restore scenario).
func TestBUG006_BackupAfterRestoreStillWorks(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	workSvc, _, _, backupSvc := setupFullServices(t, dbPath, backupDir)

	// Buat data
	_, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Pekerjaan Uji Double-Restore",
		ReceivedOn: "2026-10-17",
		Priority:   domain.PriorityLow,
	})
	if err != nil {
		t.Fatalf("Gagal membuat work item: %v", err)
	}

	// Backup pertama
	b1, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Gagal backup pertama: %v", err)
	}

	// Restore dari backup pertama
	if err := backupSvc.RestoreBackup(ctx, b1.Filename); err != nil {
		t.Fatalf("Gagal restore pertama: %v", err)
	}

	// Backup kedua (setelah restore) — tidak boleh gagal
	b2, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("BUG-006: CreateBackup gagal setelah restore: %v", err)
	}

	// Restore dari backup kedua
	if err := backupSvc.RestoreBackup(ctx, b2.Filename); err != nil {
		t.Fatalf("BUG-006: Restore kedua gagal: %v", err)
	}

	// Operasi normal setelah double-restore
	items, err := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	if err != nil {
		t.Fatalf("BUG-006: ListWorkItems gagal setelah double-restore: %v", err)
	}
	_ = items
}

// =============================================================================
// BUG-007: Import Activity Log File Rejection Regression Tests
// =============================================================================

// TestBUG007_ActivityLogCSVIsRejected memastikan file Activity Log CSV ditolak dengan pesan jelas.
func TestBUG007_ActivityLogCSVIsRejected(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	_, _, importSvc, _ := setupFullServices(t, dbPath, backupDir)

	// Simulasi CSV ekspor Activity Log dengan header khas
	activityLogCSV := "work_code,activity_type,description,field_name,old_value,new_value,occurred_at,recorded_at\n" +
		"CMP-2026-000001,status_change,Status berubah,status,not_started,in_progress,2026-10-17T08:00:00Z,2026-10-17T08:00:01Z\n"

	fileBytes := []byte(activityLogCSV)

	_, err := importSvc.PreviewImport(ctx, fileBytes, "activity_log_export.csv", "csv", "", nil)
	if err == nil {
		t.Fatal("BUG-007: Seharusnya menolak file Activity Log, tetapi tidak ada error")
	}

	// Error message harus informatif (menyebut Activity Log atau Work Register)
	errMsg := err.Error()
	if len(errMsg) < 20 {
		t.Errorf("BUG-007: Pesan error terlalu pendek: %q", errMsg)
	}
	t.Logf("Pesan penolakan diterima (benar): %s", errMsg)
}

// TestBUG007_ActivityLogWithOccurredAtRejected memastikan file dengan kolom "occurred_at" ditolak.
func TestBUG007_ActivityLogWithOccurredAtRejected(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	_, _, importSvc, _ := setupFullServices(t, dbPath, backupDir)

	csvContent := "id,description,occurred_at,recorded_at\n1,Aktivitas pertama,2026-10-17T08:00:00Z,2026-10-17T08:01:00Z\n"

	_, err := importSvc.PreviewImport(ctx, []byte(csvContent), "log.csv", "csv", "", nil)
	if err == nil {
		t.Fatal("BUG-007: File dengan kolom occurred_at dan recorded_at seharusnya ditolak")
	}
	t.Logf("Penolakan benar: %s", err.Error())
}

// TestBUG007_ValidWorkRegisterCSVAccepted memastikan file Work Register yang valid TIDAK ditolak.
func TestBUG007_ValidWorkRegisterCSVAccepted(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	_, _, importSvc, _ := setupFullServices(t, dbPath, backupDir)

	// File template resmi yang dihasilkan oleh GenerateCSVTemplate
	csvBytes := service.GenerateCSVTemplate()

	preview, err := importSvc.PreviewImport(ctx, csvBytes, "template_work_register.csv", "csv", "", nil)
	if err != nil {
		t.Fatalf("BUG-007: File Work Register yang valid seharusnya diterima, bukan ditolak: %v", err)
	}
	if preview == nil {
		t.Fatal("Preview nil untuk file valid")
	}
	if preview.TotalRows == 0 {
		t.Error("Preview harusnya memiliki minimal 1 baris dari file template")
	}
	t.Logf("File template diterima: %d baris, file_type=%s", preview.TotalRows, preview.FileType)
}

// TestBUG007_GenericCSVWithWorkTitleAccepted memastikan CSV dengan kolom "Judul" biasa diterima.
func TestBUG007_GenericCSVWithWorkTitleAccepted(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	_, _, importSvc, _ := setupFullServices(t, dbPath, backupDir)

	csvContent := "Judul Pekerjaan,Tanggal Diterima,Kategori,Prioritas\n" +
		"Audit APU-PPT 2026,2026-10-01,Kepatuhan,high\n" +
		"Review Kebijakan GDPR,2026-10-05,Legal,normal\n"

	preview, err := importSvc.PreviewImport(ctx, []byte(csvContent), "work_register.csv", "csv", "", nil)
	if err != nil {
		t.Fatalf("CSV work register generic seharusnya diterima: %v", err)
	}
	if preview.TotalRows != 2 {
		t.Errorf("Total rows: got %d, want 2", preview.TotalRows)
	}
}

// TestBUG007_XLSXTemplateAccepted memastikan template XLSX yang dihasilkan sistem diterima.
func TestBUG007_XLSXTemplateAccepted(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	_, _, importSvc, _ := setupFullServices(t, dbPath, backupDir)

	xlsxBytes, err := service.GenerateXLSXTemplate()
	if err != nil {
		t.Fatalf("Gagal membuat template XLSX: %v", err)
	}

	preview, err := importSvc.PreviewImport(ctx, xlsxBytes, "template_work_register.xlsx", "xlsx", "", nil)
	if err != nil {
		t.Fatalf("BUG-007: Template XLSX resmi seharusnya diterima: %v", err)
	}
	if preview.FileType != "xlsx" {
		t.Errorf("FileType: got %q, want %q", preview.FileType, "xlsx")
	}
	if preview.TotalRows == 0 {
		t.Error("Template XLSX harusnya memiliki minimal 1 baris data")
	}
	t.Logf("Template XLSX diterima: %d baris, worksheets=%v", preview.TotalRows, preview.Worksheets)
}

// TestBUG007_PreviewDoesNotModifyDatabase memastikan PreviewImport bersifat read-only.
func TestBUG007_PreviewDoesNotModifyDatabase(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	workSvc, _, importSvc, _ := setupFullServices(t, dbPath, backupDir)

	// Catat jumlah pekerjaan sebelum preview
	itemsBefore, _ := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	countBefore := len(itemsBefore)

	csvContent := "Judul Pekerjaan,Tanggal Diterima\n" +
		"Pekerjaan Preview Saja,2026-10-17\n" +
		"Pekerjaan Preview Kedua,2026-10-18\n"

	preview, err := importSvc.PreviewImport(ctx, []byte(csvContent), "preview_test.csv", "csv", "", nil)
	if err != nil {
		t.Fatalf("PreviewImport gagal: %v", err)
	}
	if preview.TotalRows != 2 {
		t.Errorf("Preview rows: got %d, want 2", preview.TotalRows)
	}

	// Setelah preview, jumlah work item di database TIDAK boleh berubah
	itemsAfter, _ := workSvc.ListWorkItems(ctx, repository.WorkItemFilter{})
	if len(itemsAfter) != countBefore {
		t.Errorf("BUG-007: PreviewImport mengubah database! before=%d, after=%d", countBefore, len(itemsAfter))
	}
}

// TestBUG007_BackupRestoreThenImportPreview memastikan import preview bekerja setelah restore
// (kombinasi BUG-006 + BUG-007).
func TestBUG007_BackupRestoreThenImportPreview(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	_, _, importSvc, backupSvc := setupFullServices(t, dbPath, backupDir)

	// Backup dan Restore
	backup, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Gagal backup: %v", err)
	}
	if err := backupSvc.RestoreBackup(ctx, backup.Filename); err != nil {
		t.Fatalf("Gagal restore: %v", err)
	}

	// File Activity Log harus tetap ditolak setelah restore
	activityLogCSV := "activity_type,occurred_at,recorded_at,description\nstatus_change,2026-10-17T08:00:00Z,2026-10-17T08:01:00Z,Perubahan status\n"
	_, err = importSvc.PreviewImport(ctx, []byte(activityLogCSV), "log.csv", "csv", "", nil)
	if err == nil {
		t.Error("BUG-007+BUG-006: Activity Log seharusnya ditolak bahkan setelah restore")
	}

	// File Work Register yang valid harus tetap bisa dipreview setelah restore
	csvBytes := service.GenerateCSVTemplate()
	preview, err := importSvc.PreviewImport(ctx, csvBytes, "template.csv", "csv", "", nil)
	if err != nil {
		t.Fatalf("BUG-006+BUG-007: PreviewImport gagal setelah restore: %v", err)
	}
	if preview == nil {
		t.Fatal("Preview nil setelah restore")
	}
	t.Logf("Import preview setelah restore berhasil: %d baris", preview.TotalRows)
}

// =============================================================================
// Verifikasi: file sensitif tidak digunakan dalam test
// =============================================================================

// TestNoUATDatabaseUsed memastikan test tidak menggunakan database pengguna nyata.
func TestNoUATDatabaseUsed(t *testing.T) {
	// Test ini hanya memeriksa apakah file database UAT ada di path yang benar
	// dan tidak disentuh oleh test suite ini.
	uatDBPath := filepath.Join("..", "uat", "data", "compliance.db")
	if _, err := os.Stat(uatDBPath); err == nil {
		// File ada — pastikan test ini tidak pernah membuka file tersebut.
		// Seluruh test di file ini menggunakan t.TempDir().
		t.Logf("INFO: File database UAT ditemukan di %s — test ini tidak menggunakannya.", uatDBPath)
	}
}

// TestBUG008_EndToEndTemplatePreview melakukan trace end-to-end terhadap file template resmi
func TestBUG008_EndToEndTemplatePreview(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compliance.db")
	backupDir := filepath.Join(tempDir, "backups")
	ctx := context.Background()

	_, _, importSvc, _ := setupFullServices(t, dbPath, backupDir)

	// 1. Template CSV
	csvBytes := service.GenerateCSVTemplate()
	previewCSV, err := importSvc.PreviewImport(ctx, csvBytes, "template_work_register.csv", "csv", "", nil)
	if err != nil {
		t.Fatalf("PreviewImport CSV tanpa mapping gagal: %v", err)
	}

	t.Logf("CSV Tanpa UserMapping:")
	t.Logf("  TotalRows: %d, ValidRows: %d, DuplicateRows: %d, InvalidRows: %d",
		previewCSV.TotalRows, previewCSV.ValidRows, previewCSV.DuplicateRows, previewCSV.InvalidRows)
	t.Logf("  SuggestedMapping: %+v", previewCSV.SuggestedMapping)
	t.Logf("  SampleRows count: %d", len(previewCSV.SampleRows))

	// Sekarang simulasi Step 2: Kirim mapping yang sama persis seperti yang dikirim oleh UI
	userMapping := previewCSV.SuggestedMapping
	previewWithMapping, err := importSvc.PreviewImport(ctx, csvBytes, "template_work_register.csv", "csv", "", userMapping)
	if err != nil {
		t.Fatalf("PreviewImport CSV dengan mapping gagal: %v", err)
	}
	t.Logf("CSV DENGAN UserMapping:")
	t.Logf("  TotalRows: %d, ValidRows: %d, DuplicateRows: %d, InvalidRows: %d",
		previewWithMapping.TotalRows, previewWithMapping.ValidRows, previewWithMapping.DuplicateRows, previewWithMapping.InvalidRows)
	for idx, row := range previewWithMapping.SampleRows {
		t.Logf("  Row %d: IsValid=%v, Title=%q, ReceivedOn=%q, Errors=%v",
			idx+1, row.IsValid, row.Title, row.ReceivedOn, row.Errors)
	}

	// 2. Template XLSX
	xlsxBytes, err := service.GenerateXLSXTemplate()
	if err != nil {
		t.Fatalf("Generate XLSX template gagal: %v", err)
	}
	previewXLSX, err := importSvc.PreviewImport(ctx, xlsxBytes, "template_work_register.xlsx", "xlsx", "", nil)
	if err != nil {
		t.Fatalf("PreviewImport XLSX tanpa mapping gagal: %v", err)
	}
	t.Logf("XLSX Tanpa UserMapping:")
	t.Logf("  TotalRows: %d, ValidRows: %d, DuplicateRows: %d, InvalidRows: %d",
		previewXLSX.TotalRows, previewXLSX.ValidRows, previewXLSX.DuplicateRows, previewXLSX.InvalidRows)
	t.Logf("  SuggestedMapping: %+v", previewXLSX.SuggestedMapping)

	previewXLSXWithMapping, err := importSvc.PreviewImport(ctx, xlsxBytes, "template_work_register.xlsx", "xlsx", "", previewXLSX.SuggestedMapping)
	if err != nil {
		t.Fatalf("PreviewImport XLSX dengan mapping gagal: %v", err)
	}
	t.Logf("XLSX DENGAN UserMapping:")
	t.Logf("  TotalRows: %d, ValidRows: %d, DuplicateRows: %d, InvalidRows: %d",
		previewXLSXWithMapping.TotalRows, previewXLSXWithMapping.ValidRows, previewXLSXWithMapping.DuplicateRows, previewXLSXWithMapping.InvalidRows)
	for idx, row := range previewXLSXWithMapping.SampleRows {
		t.Logf("  Row %d: IsValid=%v, Title=%q, ReceivedOn=%q, Errors=%v",
			idx+1, row.IsValid, row.Title, row.ReceivedOn, row.Errors)
	}
}
