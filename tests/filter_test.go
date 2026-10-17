package tests

import (
	"context"
	"testing"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
)

func TestWorkItemFiltersAndSearch(t *testing.T) {
	svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Seed beberapa data uji
	items := []domain.CreateWorkItemInput{
		{
			Title:       "Audit Keamanan Data Nasabah",
			Description: "Pemeriksaan enkripsi database",
			Category:    "Data Privacy",
			RequestedBy: "BOD",
			ReceivedOn:  "2026-10-01",
			Priority:    domain.PriorityCritical,
		},
		{
			Title:       "Laporan Kepatuhan OJK Triwulan",
			Description: "Pengumpulan berkas regulasi",
			Category:    "Regulatory",
			RequestedBy: "Komite Audit",
			ReceivedOn:  "2026-10-05",
			Priority:    domain.PriorityHigh,
		},
		{
			Title:       "Sosialisasi Anti Penyuapan",
			Description: "Pelatihan internal karyawan",
			Category:    "Training",
			RequestedBy: "HRD",
			ReceivedOn:  "2026-10-10",
			Priority:    domain.PriorityLow,
		},
	}

	var createdIDs []int64
	for _, it := range items {
		created, err := svc.CreateWorkItem(ctx, it)
		if err != nil {
			t.Fatalf("Gagal membuat data filter: %v", err)
		}
		createdIDs = append(createdIDs, created.ID)
	}

	// Ubah status item kedua menjadi in_progress
	_, err := svc.UpdateWorkItem(ctx, createdIDs[1], domain.UpdateWorkItemInput{
		Title:       items[1].Title,
		Description: items[1].Description,
		Category:    items[1].Category,
		RequestedBy: items[1].RequestedBy,
		ReceivedOn:  items[1].ReceivedOn,
		Priority:    items[1].Priority,
		Status:      domain.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("Gagal update status: %v", err)
	}

	// 1. Filter berdasarkan status in_progress
	filteredStatus, err := svc.ListWorkItems(ctx, repository.WorkItemFilter{Status: domain.StatusInProgress})
	if err != nil {
		t.Fatalf("Filter status gagal: %v", err)
	}
	if len(filteredStatus) != 1 || filteredStatus[0].ID != createdIDs[1] {
		t.Errorf("Filter status salah: diharapkan 1 item ID %d, didapat %d", createdIDs[1], len(filteredStatus))
	}

	// 2. Filter berdasarkan prioritas critical
	filteredPriority, err := svc.ListWorkItems(ctx, repository.WorkItemFilter{Priority: domain.PriorityCritical})
	if err != nil {
		t.Fatalf("Filter prioritas gagal: %v", err)
	}
	if len(filteredPriority) != 1 || filteredPriority[0].ID != createdIDs[0] {
		t.Errorf("Filter prioritas salah: diharapkan 1 item ID %d, didapat %d", createdIDs[0], len(filteredPriority))
	}

	// 3. Pencarian teks (search keyword "OJK")
	searchedOJK, err := svc.ListWorkItems(ctx, repository.WorkItemFilter{Search: "OJK"})
	if err != nil {
		t.Fatalf("Search OJK gagal: %v", err)
	}
	if len(searchedOJK) != 1 || searchedOJK[0].Title != items[1].Title {
		t.Errorf("Hasil search OJK tidak cocok")
	}

	// 4. Pencarian teks (search keyword dalam description "enkripsi")
	searchedDesc, err := svc.ListWorkItems(ctx, repository.WorkItemFilter{Search: "enkripsi"})
	if err != nil {
		t.Fatalf("Search description gagal: %v", err)
	}
	if len(searchedDesc) != 1 || searchedDesc[0].ID != createdIDs[0] {
		t.Errorf("Hasil search description tidak cocok")
	}
}
