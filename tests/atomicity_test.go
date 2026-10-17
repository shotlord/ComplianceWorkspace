package tests

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"compliance-workspace/internal/database"
	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
	"compliance-workspace/internal/service"
)

func setupTestDB(t *testing.T) (*service.WorkItemService, func()) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_compliance.db")

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

	cleanup := func() {
		db.Close()
	}

	return svc, cleanup
}

func TestAtomicCreateWorkItem(t *testing.T) {
	svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	in := domain.CreateWorkItemInput{
		Title:       "Uji Kepatuhan Vendor Pihak Ketiga",
		Description: "Audit kepatuhan ISO 27001",
		Category:    "Vendor Assessment",
		RequestedBy: "Direktur Kepatuhan",
		ReceivedOn:  "2026-10-15",
		Priority:    domain.PriorityHigh,
	}

	item, err := svc.CreateWorkItem(ctx, in)
	if err != nil {
		t.Fatalf("CreateWorkItem gagal: %v", err)
	}

	if item.ID == 0 {
		t.Errorf("ID item seharusnya > 0")
	}
	if !strings.HasPrefix(item.WorkCode, "CMP-2026-") {
		t.Errorf("Format work_code salah: %s", item.WorkCode)
	}
	if item.Status != domain.StatusNotStarted {
		t.Errorf("Status awal harus not_started, didapat: %s", item.Status)
	}

	// Verifikasi activity log otomatis dibuat
	logs, err := svc.ListActivities(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListActivities gagal: %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("Seharusnya ada 1 activity log created, didapat: %d", len(logs))
	}
	if logs[0].EntryType != domain.EntryTypeCreated {
		t.Errorf("Tipe log harus created, didapat: %s", logs[0].EntryType)
	}
}

func TestAtomicStatusChangeAndUpdate(t *testing.T) {
	svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	item, err := svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Review Kebijakan Privasi Data",
		ReceivedOn: "2026-10-16",
		Priority:   domain.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("CreateWorkItem gagal: %v", err)
	}

	// Update status ke in_progress dan ubah prioritas
	upIn := domain.UpdateWorkItemInput{
		Title:       "Review Kebijakan Privasi Data v2",
		Description: "Revisi pasal 4 dan pasal 7",
		Category:    "Policy",
		RequestedBy: "Divisi Legal",
		ReceivedOn:  "2026-10-16",
		Priority:    domain.PriorityHigh,
		Status:      domain.StatusInProgress,
	}

	updated, err := svc.UpdateWorkItem(ctx, item.ID, upIn)
	if err != nil {
		t.Fatalf("UpdateWorkItem gagal: %v", err)
	}

	if updated.Status != domain.StatusInProgress {
		t.Errorf("Status gagal diperbarui ke in_progress: %s", updated.Status)
	}
	if updated.Priority != domain.PriorityHigh {
		t.Errorf("Prioritas gagal diperbarui ke high: %s", updated.Priority)
	}

	// Update status ke completed
	upIn.Status = domain.StatusCompleted
	completed, err := svc.UpdateWorkItem(ctx, item.ID, upIn)
	if err != nil {
		t.Fatalf("UpdateWorkItem completed gagal: %v", err)
	}
	if completed.CompletedAt == nil {
		t.Errorf("CompletedAt harus terisi saat status selesai")
	}

	// Periksa log aktivitas
	logs, err := svc.ListActivities(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListActivities gagal: %v", err)
	}

	var statusChanges []string
	for _, l := range logs {
		if l.EntryType == domain.EntryTypeStatusChanged {
			statusChanges = append(statusChanges, fmt.Sprintf("%s->%s", *l.OldValue, *l.NewValue))
		}
	}

	if len(statusChanges) != 2 {
		t.Fatalf("Seharusnya ada 2 log status_changed, didapat %d: %v", len(statusChanges), statusChanges)
	}
	if statusChanges[0] != "not_started->in_progress" {
		t.Errorf("Status change 1 salah: %s", statusChanges[0])
	}
	if statusChanges[1] != "in_progress->completed" {
		t.Errorf("Status change 2 salah: %s", statusChanges[1])
	}
}

func TestAppendOnlyTriggers(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_append_only.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("Gagal migrasi: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(db)
	activityRepo := repository.NewActivityLogRepository(db)
	svc := service.NewWorkItemService(db, workRepo, activityRepo)

	item, err := svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Uji Trigger Append-Only",
		ReceivedOn: "2026-10-17",
	})
	if err != nil {
		t.Fatalf("Gagal membuat work item: %v", err)
	}

	logs, err := svc.ListActivities(ctx, item.ID)
	if err != nil || len(logs) == 0 {
		t.Fatalf("Log tidak ditemukan: %v", err)
	}
	logID := logs[0].ID

	// Uji 1: Coba UPDATE secara ilegal langsung melalui SQL mentah
	_, errUpdate := db.ExecContext(ctx, "UPDATE activity_logs SET description = 'Manipulasi data' WHERE id = ?", logID)
	if errUpdate == nil {
		t.Fatalf("SEHARUSNYA GAGAL: SQLite trigger tidak memblokir operasi UPDATE pada activity_logs")
	}
	if !strings.Contains(errUpdate.Error(), "Activity logs are append-only") {
		t.Errorf("Pesan error trigger tidak sesuai: %v", errUpdate)
	}

	// Uji 2: Coba DELETE secara ilegal langsung melalui SQL mentah
	_, errDelete := db.ExecContext(ctx, "DELETE FROM activity_logs WHERE id = ?", logID)
	if errDelete == nil {
		t.Fatalf("SEHARUSNYA GAGAL: SQLite trigger tidak memblokir operasi DELETE pada activity_logs")
	}
	if !strings.Contains(errDelete.Error(), "Activity logs are append-only") {
		t.Errorf("Pesan error trigger tidak sesuai: %v", errDelete)
	}

	// Pastikan data log awal tetap utuh
	logsAfter, err := svc.ListActivities(ctx, item.ID)
	if err != nil || len(logsAfter) != 1 {
		t.Fatalf("Data log berubah setelah percobaan manipulasi: %v", err)
	}
	if logsAfter[0].Description != logs[0].Description {
		t.Errorf("Deskripsi log berubah: %s", logsAfter[0].Description)
	}
}

func TestArchiveAndRestoreWorkItem(t *testing.T) {
	svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	item, err := svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Pekerjaan Untuk Diarsipkan",
		ReceivedOn: "2026-10-17",
	})
	if err != nil {
		t.Fatalf("CreateWorkItem gagal: %v", err)
	}

	// Arsipkan
	archived, err := svc.ArchiveWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("ArchiveWorkItem gagal: %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Errorf("ArchivedAt seharusnya terisi")
	}

	// Pastikan tidak muncul pada daftar reguler
	list, err := svc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: false})
	if err != nil {
		t.Fatalf("ListWorkItems gagal: %v", err)
	}
	for _, l := range list {
		if l.ID == item.ID {
			t.Errorf("Pekerjaan yang diarsipkan seharusnya tidak muncul pada daftar tanpa include_archived")
		}
	}

	// Pulihkan
	restored, err := svc.RestoreWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("RestoreWorkItem gagal: %v", err)
	}
	if restored.ArchivedAt != nil {
		t.Errorf("ArchivedAt seharusnya nil setelah dipulihkan")
	}

	// Periksa log aktivitas memuat 'archived' dan 'restored'
	logs, err := svc.ListActivities(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListActivities gagal: %v", err)
	}
	var entryTypes []string
	for _, l := range logs {
		entryTypes = append(entryTypes, l.EntryType)
	}
	if len(entryTypes) < 3 || entryTypes[1] != domain.EntryTypeArchived || entryTypes[2] != domain.EntryTypeRestored {
		t.Errorf("Riwayat arsip/restore tidak tercatat dengan benar: %v", entryTypes)
	}
}
