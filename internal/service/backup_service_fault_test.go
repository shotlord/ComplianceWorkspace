package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"compliance-workspace/internal/database"
	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
)

func setupBackupFaultTest(t *testing.T) (*WorkItemService, *BackupService, string, func()) {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")
	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	workRepo := repository.NewWorkItemRepository(db)
	activityRepo := repository.NewActivityLogRepository(db)
	workSvc := NewWorkItemService(db, workRepo, activityRepo)
	backupSvc := NewBackupService(workSvc, nil, nil, dbPath, backupDir)
	cleanup := func() {
		if current := workSvc.GetDB(); current != nil {
			_ = current.Close()
		}
	}
	return workSvc, backupSvc, backupDir, cleanup
}

func createFaultTestItem(t *testing.T, svc *WorkItemService, title string) {
	t.Helper()
	if _, err := svc.CreateWorkItem(context.Background(), domain.CreateWorkItemInput{
		Title:      title,
		ReceivedOn: "2026-10-19",
	}); err != nil {
		t.Fatalf("create item gagal: %v", err)
	}
}

func countFaultTestItems(t *testing.T, svc *WorkItemService) int {
	t.Helper()
	items, err := svc.ListWorkItems(context.Background(), repository.WorkItemFilter{IncludeArchived: true})
	if err != nil {
		t.Fatalf("list item gagal: %v", err)
	}
	return len(items)
}

func TestCreateBackupRejectsCorruptedSnapshotFault(t *testing.T) {
	workSvc, backupSvc, backupDir, cleanup := setupBackupFaultTest(t)
	defer cleanup()
	createFaultTestItem(t, workSvc, "Data untuk uji integritas backup")

	backupSvc.faultInjector = func(point, path string) error {
		if point == "after_backup_vacuum" {
			return os.WriteFile(path, []byte("not-a-sqlite-database"), 0600)
		}
		return nil
	}
	if _, err := backupSvc.CreateBackup(context.Background()); err == nil {
		t.Fatal("backup korup hasil fault injection harus ditolak")
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "compliance_backup_") {
			t.Fatalf("backup korup tidak dibersihkan: %s", entry.Name())
		}
	}
	if countFaultTestItems(t, workSvc) != 1 {
		t.Fatal("database aktif berubah setelah backup korup ditolak")
	}
}

func TestRestoreAbortsWhenSafetySnapshotFails(t *testing.T) {
	workSvc, backupSvc, backupDir, cleanup := setupBackupFaultTest(t)
	defer cleanup()
	createFaultTestItem(t, workSvc, "Data sebelum backup")
	backup, err := backupSvc.CreateBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	createFaultTestItem(t, workSvc, "Data aktif setelah backup")

	backupSvc.faultInjector = func(point, _ string) error {
		if point == "before_safety_vacuum" {
			return errors.New("simulated VACUUM INTO failure")
		}
		return nil
	}
	if err := backupSvc.RestoreBackup(context.Background(), backup.Filename); err == nil {
		t.Fatal("restore harus gagal bila safety snapshot gagal")
	}
	if countFaultTestItems(t, workSvc) != 2 {
		t.Fatal("data aktif hilang setelah safety snapshot gagal")
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "pre_restore_safety_") {
			t.Fatalf("safety snapshot parsial tidak boleh tertinggal: %s", entry.Name())
		}
	}
}

func TestRestoreRollsBackAfterActivationFault(t *testing.T) {
	workSvc, backupSvc, _, cleanup := setupBackupFaultTest(t)
	defer cleanup()
	createFaultTestItem(t, workSvc, "Snapshot memiliki satu item")
	backup, err := backupSvc.CreateBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	createFaultTestItem(t, workSvc, "Item aktif yang wajib dipertahankan saat fault")

	backupSvc.faultInjector = func(point, _ string) error {
		if point == "after_activate" {
			return errors.New("simulated activation failure")
		}
		return nil
	}
	if err := backupSvc.RestoreBackup(context.Background(), backup.Filename); err == nil {
		t.Fatal("restore harus melaporkan fault setelah aktivasi")
	}
	if countFaultTestItems(t, workSvc) != 2 {
		t.Fatal("rollback tidak mengembalikan database aktif asli")
	}
	if err := database.CheckIntegrity(context.Background(), workSvc.GetDB()); err != nil {
		t.Fatalf("database hasil rollback gagal integrity check: %v", err)
	}
	if err := database.CheckForeignKeys(context.Background(), workSvc.GetDB()); err != nil {
		t.Fatalf("database hasil rollback gagal foreign key check: %v", err)
	}
}

func TestRestoreRejectsBackupSymlink(t *testing.T) {
	workSvc, backupSvc, backupDir, cleanup := setupBackupFaultTest(t)
	defer cleanup()
	createFaultTestItem(t, workSvc, "Data symlink test")
	backup, err := backupSvc.CreateBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	linkName := "linked_backup.db"
	if err := os.Symlink(filepath.Join(backupDir, backup.Filename), filepath.Join(backupDir, linkName)); err != nil {
		t.Skipf("symlink tidak tersedia pada platform/policy ini: %v", err)
	}
	if err := backupSvc.RestoreBackup(context.Background(), linkName); err == nil {
		t.Fatal("restore melalui symbolic link harus ditolak")
	}
}

func TestRestoreOperationGateBlocksConcurrentDatabaseWork(t *testing.T) {
	workSvc, backupSvc, _, cleanup := setupBackupFaultTest(t)
	defer cleanup()
	createFaultTestItem(t, workSvc, "Data gate concurrency")
	backup, err := backupSvc.CreateBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	backupSvc.faultInjector = func(point, _ string) error {
		if point == "after_stage_copy" {
			close(entered)
			<-release
			return errors.New("stop after gate assertion")
		}
		return nil
	}
	restoreDone := make(chan error, 1)
	go func() {
		restoreDone <- backupSvc.RestoreBackup(context.Background(), backup.Filename)
	}()
	<-entered

	readerAcquired := make(chan struct{})
	go func() {
		backupSvc.OperationGate().RLock()
		close(readerAcquired)
		backupSvc.OperationGate().RUnlock()
	}()
	select {
	case <-readerAcquired:
		t.Fatal("operasi database lain tidak boleh masuk selama restore memegang exclusive gate")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err := <-restoreDone; err == nil {
		t.Fatal("fault injection harus menghentikan restore")
	}
	select {
	case <-readerAcquired:
	case <-time.After(2 * time.Second):
		t.Fatal("operation gate tidak dilepas setelah restore gagal")
	}
}

func createStructurallyInvalidBackup(t *testing.T, backupDir, filename string) {
	t.Helper()
	path := filepath.Join(backupDir, filename)
	db, err := database.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	_, execErr := db.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		);
		INSERT INTO schema_migrations (version) VALUES (1), (2), (3), (4), (5), (6);
	`)
	closeErr := db.Close()
	if execErr != nil {
		t.Fatal(execErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestRestoreRejectsStructurallyInvalidBackup(t *testing.T) {
	workSvc, backupSvc, backupDir, cleanup := setupBackupFaultTest(t)
	defer cleanup()
	createFaultTestItem(t, workSvc, "Data aktif yang tidak boleh berubah")

	const filename = "structurally_invalid_v6.db"
	createStructurallyInvalidBackup(t, backupDir, filename)

	inspection, err := backupSvc.InspectBackup(context.Background(), filename)
	if err != nil {
		t.Fatalf("InspectBackup gagal: %v", err)
	}
	if inspection.IsCompatible {
		t.Fatal("backup tanpa tabel aplikasi wajib tidak boleh dinyatakan kompatibel")
	}

	if err := backupSvc.RestoreBackup(context.Background(), filename); err == nil {
		t.Fatal("restore harus menolak backup yang structurally invalid")
	}
	if count := countFaultTestItems(t, workSvc); count != 1 {
		t.Fatalf("database aktif berubah setelah restore invalid ditolak: jumlah item=%d", count)
	}
	if err := database.CheckIntegrity(context.Background(), workSvc.GetDB()); err != nil {
		t.Fatalf("database aktif gagal integrity check setelah penolakan restore: %v", err)
	}
}

func TestInspectBackupRejectsMigrationHistoryGap(t *testing.T) {
	workSvc, backupSvc, backupDir, cleanup := setupBackupFaultTest(t)
	defer cleanup()
	createFaultTestItem(t, workSvc, "Data untuk backup dengan gap migrasi")

	backup, err := backupSvc.CreateBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(backupDir, backup.Filename)
	backupDB, err := database.OpenDB(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	_, deleteErr := backupDB.Exec("DELETE FROM schema_migrations WHERE version = 3")
	closeErr := backupDB.Close()
	if deleteErr != nil {
		t.Fatal(deleteErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}

	inspection, err := backupSvc.InspectBackup(context.Background(), backup.Filename)
	if err != nil {
		t.Fatalf("InspectBackup gagal: %v", err)
	}
	if inspection.IsCompatible {
		t.Fatal("backup dengan migration history gap tidak boleh dinyatakan kompatibel")
	}
	if err := backupSvc.RestoreBackup(context.Background(), backup.Filename); err == nil {
		t.Fatal("restore harus menolak backup dengan migration history gap")
	}
	if count := countFaultTestItems(t, workSvc); count != 1 {
		t.Fatalf("database aktif berubah setelah backup bergap ditolak: jumlah item=%d", count)
	}
}

func TestRestoreSupportedLegacyV1BackupMigrates(t *testing.T) {
	workSvc, backupSvc, backupDir, cleanup := setupBackupFaultTest(t)
	defer cleanup()
	createFaultTestItem(t, workSvc, "Data aktif sebelum restore legacy")

	migrationSQL, err := os.ReadFile(filepath.Join("..", "database", "migrations", "001_initial_schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	const filename = "supported_legacy_v1.db"
	legacyPath := filepath.Join(backupDir, filename)
	legacyDB, err := database.OpenDB(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacyDB.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		);
	`); err != nil {
		_ = legacyDB.Close()
		t.Fatal(err)
	}
	if _, err := legacyDB.Exec(string(migrationSQL)); err != nil {
		_ = legacyDB.Close()
		t.Fatal(err)
	}
	if _, err := legacyDB.Exec(`
		INSERT INTO schema_migrations (version) VALUES (1);
		INSERT INTO work_items (work_code, title, received_on)
		VALUES ('CMP-2026-000001', 'Data Legacy V1', '2026-10-20');
	`); err != nil {
		_ = legacyDB.Close()
		t.Fatal(err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatal(err)
	}

	if err := backupSvc.RestoreBackup(context.Background(), filename); err != nil {
		t.Fatalf("restore backup v1 yang valid harus berhasil: %v", err)
	}
	items, err := workSvc.ListWorkItems(context.Background(), repository.WorkItemFilter{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Data Legacy V1" {
		t.Fatalf("data legacy tidak utuh setelah restore: %+v", items)
	}
	var maxVersion int
	if err := workSvc.GetDB().QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&maxVersion); err != nil {
		t.Fatal(err)
	}
	if maxVersion != database.MaxSupportedSchemaVersion {
		t.Fatalf("backup legacy tidak dimigrasikan ke v%d: mendapat v%d", database.MaxSupportedSchemaVersion, maxVersion)
	}
}
