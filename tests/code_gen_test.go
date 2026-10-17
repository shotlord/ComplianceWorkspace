package tests

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"compliance-workspace/internal/domain"
)

func TestWorkCodeFormatAndMonotonicSequence(t *testing.T) {
	svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	pattern := regexp.MustCompile(`^CMP-2026-\d{6}$`)

	for i := 1; i <= 5; i++ {
		item, err := svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
			Title:      fmt.Sprintf("Pekerjaan Kepatuhan #%d", i),
			ReceivedOn: "2026-05-10",
		})
		if err != nil {
			t.Fatalf("Gagal membuat pekerjaan #%d: %v", i, err)
		}

		if !pattern.MatchString(item.WorkCode) {
			t.Errorf("Format kode salah: %s (ekspektasi CMP-2026-XXXXXX)", item.WorkCode)
		}

		expectedCode := fmt.Sprintf("CMP-2026-%06d", i)
		if item.WorkCode != expectedCode {
			t.Errorf("Kode tidak berurutan: didapat %s, ekspektasi %s", item.WorkCode, expectedCode)
		}
	}
}

func TestWorkCodeYearPartitioning(t *testing.T) {
	svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Pekerjaan tahun 2025
	item2025, err := svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Review Tahun Lalu",
		ReceivedOn: "2025-12-01",
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan 2025: %v", err)
	}
	if item2025.WorkCode != "CMP-2025-000001" {
		t.Errorf("Ekspektasi CMP-2025-000001, didapat: %s", item2025.WorkCode)
	}

	// Pekerjaan tahun 2026
	item2026, err := svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:      "Review Tahun Berjalan",
		ReceivedOn: "2026-01-15",
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan 2026: %v", err)
	}
	if item2026.WorkCode != "CMP-2026-000001" {
		t.Errorf("Ekspektasi CMP-2026-000001, didapat: %s", item2026.WorkCode)
	}
}
