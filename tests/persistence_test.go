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

func TestDataPersistenceAcrossRestartAndDirectoryMove(t *testing.T) {
	tempBase := t.TempDir()
	workspace1 := filepath.Join(tempBase, "workspace1")
	dataDir1 := filepath.Join(workspace1, "data")
	if err := os.MkdirAll(dataDir1, 0755); err != nil {
		t.Fatalf("Gagal membuat workspace1: %v", err)
	}

	dbPath1 := filepath.Join(dataDir1, "compliance.db")
	ctx := context.Background()

	// 1. Sesi 1: Buka DB, migrasi, dan masukkan data
	db1, err := database.OpenDB(dbPath1)
	if err != nil {
		t.Fatalf("Sesi 1: Gagal membuka DB: %v", err)
	}

	if err := database.Migrate(ctx, db1); err != nil {
		db1.Close()
		t.Fatalf("Sesi 1: Gagal migrasi: %v", err)
	}

	workRepo1 := repository.NewWorkItemRepository(db1)
	actRepo1 := repository.NewActivityLogRepository(db1)
	svc1 := service.NewWorkItemService(db1, workRepo1, actRepo1)

	// Buat 3 pekerjaan
	item1, err := svc1.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Audit Kebijakan Perlindungan Konsumen",
		ReceivedOn: "2026-10-10",
		Priority:   domain.PriorityHigh,
	})
	if err != nil {
		db1.Close()
		t.Fatalf("Sesi 1: Gagal create item 1: %v", err)
	}

	item2, err := svc1.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Penyesuaian Klausula Kontrak Vendor",
		ReceivedOn: "2026-10-11",
		Priority:   domain.PriorityNormal,
	})
	if err != nil {
		db1.Close()
		t.Fatalf("Sesi 1: Gagal create item 2: %v", err)
	}

	// Update status item1 ke completed
	_, err = svc1.UpdateWorkItem(ctx, item1.ID, domain.UpdateWorkItemInput{
		Title:      item1.Title,
		ReceivedOn: item1.ReceivedOn,
		Priority:   item1.Priority,
		Status:     domain.StatusCompleted,
	})
	if err != nil {
		db1.Close()
		t.Fatalf("Sesi 1: Gagal update item 1: %v", err)
	}

	// Tambah catatan manual pada item2
	_, err = svc1.AddActivity(ctx, item2.ID, domain.AddActivityInput{
		Description: "Melakukan koordinasi dengan divisi pengadaan.",
	})
	if err != nil {
		db1.Close()
		t.Fatalf("Sesi 1: Gagal add activity: %v", err)
	}

	// Tutup sesi 1 (simulasi aplikasi dimatikan)
	if err := db1.Close(); err != nil {
		t.Fatalf("Gagal menutup DB sesi 1: %v", err)
	}

	// 2. Sesi 2: Buka kembali database dari path yang sama (simulasi aplikasi dinyalakan ulang)
	db2, err := database.OpenDB(dbPath1)
	if err != nil {
		t.Fatalf("Sesi 2: Gagal membuka DB setelah restart: %v", err)
	}

	workRepo2 := repository.NewWorkItemRepository(db2)
	actRepo2 := repository.NewActivityLogRepository(db2)
	svc2 := service.NewWorkItemService(db2, workRepo2, actRepo2)

	// Verifikasi data item1 dan item2 tetap utuh
	fetched1, err := svc2.GetWorkItem(ctx, item1.ID)
	if err != nil {
		db2.Close()
		t.Fatalf("Sesi 2: Gagal membaca item1: %v", err)
	}
	if fetched1.Status != domain.StatusCompleted || fetched1.CompletedAt == nil {
		t.Errorf("Sesi 2: Status item1 tidak persisten: %+v", fetched1)
	}

	logs2, err := svc2.ListActivities(ctx, item2.ID)
	if err != nil || len(logs2) != 2 {
		t.Errorf("Sesi 2: Log aktivitas item2 tidak persisten, jumlah log: %d", len(logs2))
	}

	// Buat item ke-3 di sesi 2: pastikan urutan kode CMP-2026-000003
	item3, err := svc2.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Review Tata Kelola Risiko AI",
		ReceivedOn: "2026-10-12",
	})
	if err != nil {
		db2.Close()
		t.Fatalf("Sesi 2: Gagal membuat item 3: %v", err)
	}
	if item3.WorkCode != "CMP-2026-000003" {
		t.Errorf("Sesi 2: Nomor urut kode melompat atau salah: %s (ekspektasi CMP-2026-000003)", item3.WorkCode)
	}

	// Tutup sesi 2
	if err := db2.Close(); err != nil {
		t.Fatalf("Gagal menutup DB sesi 2: %v", err)
	}

	// 3. Sesi 3: Simulasi pemindahan folder aplikasi ke direktori baru
	workspace2 := filepath.Join(tempBase, "workspace_moved")
	if err := os.Rename(workspace1, workspace2); err != nil {
		t.Fatalf("Gagal memindahkan folder workspace: %v", err)
	}

	dbPathMoved := filepath.Join(workspace2, "data", "compliance.db")
	db3, err := database.OpenDB(dbPathMoved)
	if err != nil {
		t.Fatalf("Sesi 3: Gagal membuka DB di lokasi baru: %v", err)
	}
	defer db3.Close()

	workRepo3 := repository.NewWorkItemRepository(db3)
	actRepo3 := repository.NewActivityLogRepository(db3)
	svc3 := service.NewWorkItemService(db3, workRepo3, actRepo3)

	// Pastikan seluruh 3 pekerjaan terbaca di lokasi baru
	allMoved, err := svc3.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Sesi 3: Gagal membaca data di folder baru: %v", err)
	}
	if len(allMoved) != 3 {
		t.Errorf("Sesi 3: Jumlah item tidak cocok setelah dipindahkan: %d", len(allMoved))
	}

	// Verifikasi penambahan data baru di lokasi baru berhasil
	item4, err := svc3.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Uji Penambahan Setelah Folder Dipindahkan",
		ReceivedOn: "2026-10-13",
	})
	if err != nil {
		t.Fatalf("Sesi 3: Gagal menambah pekerjaan di lokasi baru: %v", err)
	}
	if item4.WorkCode != "CMP-2026-000004" {
		t.Errorf("Sesi 3: Urutan kode di lokasi baru salah: %s", item4.WorkCode)
	}
}
