package tests

import (
	"context"
	"os"
	"strings"
	"testing"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
)

func TestXSSAndSQLInjectionSafety(t *testing.T) {
	svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Uji Penyimpanan Karakter Berbahaya XSS & SQL Injection
	xssPayload := "<script>alert('XSS-ATTACK');</script><img src='x' onerror='alert(1)'>"
	sqlPayload := "'; DROP TABLE work_items; --"

	item, err := svc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Uji Payload XSS: " + xssPayload,
		Description: "Uji SQL Injection: " + sqlPayload,
		Category:    "<b>Security Test</b>",
		RequestedBy: "<iframe src='javascript:alert(1)'>",
		ReceivedOn:  "2026-10-17",
		Priority:    domain.PriorityHigh,
	})
	if err != nil {
		t.Fatalf("Penyimpanan payload XSS/SQL gagal: %v", err)
	}

	// Pastikan data tersimpan secara verbatim dan aman
	fetched, err := svc.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("Gagal membaca item payload: %v", err)
	}
	if !strings.Contains(fetched.Title, xssPayload) {
		t.Errorf("Payload XSS tidak tersimpan secara tepat")
	}
	if !strings.Contains(fetched.Description, sqlPayload) {
		t.Errorf("Payload SQL Injection tidak tersimpan secara tepat")
	}

	// Pastikan tabel database masih utuh (tidak terhapus oleh SQL Injection)
	listAll, err := svc.ListWorkItems(ctx, repository.WorkItemFilter{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Tabel database rusak atau terhapus: %v", err)
	}
	if len(listAll) == 0 {
		t.Fatalf("Data tidak ditemukan setelah SQL injection attempt")
	}
}

func TestFrontendCodeNoUnsafeInnerHTML(t *testing.T) {
	// Analisis kode sumber JavaScript (app.js) untuk memastikan tidak ada penggabungan string innerHTML secara berbahaya
	appJSBytes, err := os.ReadFile("../web/static/js/app.js")
	if err != nil {
		// Coba path relatif langsung
		appJSBytes, err = os.ReadFile("web/static/js/app.js")
	}
	if err != nil {
		t.Skip("File app.js tidak ditemukan dari root testing saat ini")
		return
	}

	jsContent := string(appJSBytes)

	// Pastikan tidak ada pola berbahaya: innerHTML = ... + item.title atau innerHTML = ... + item.description
	dangerousPatterns := []string{
		"innerHTML = item.title",
		"innerHTML = item.description",
		"innerHTML += item.",
		"innerHTML = `<tr><td>${item.title}",
		"innerHTML = `<div class=\"empty-state\" style=\"color: var(--danger);\">${err.message}",
		"innerHTML = `<tr><td colspan=\"10\" class=\"empty-state\" style=\"color: var(--danger);\">Gagal memuat riwayat: ${err.message}",
		"metaEl.innerHTML = `",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(jsContent, pattern) {
			t.Errorf("Ditemukan potensi kerentanan XSS pada app.js: '%s'", pattern)
		}
	}
}
