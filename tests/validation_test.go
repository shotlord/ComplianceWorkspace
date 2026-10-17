package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/server"
)

func TestInputValidationAndErrorHandling(t *testing.T) {
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
			if s, ok := body.(string); ok {
				bodyReader = bytes.NewReader([]byte(s))
			} else {
				b, _ := json.Marshal(body)
				bodyReader = bytes.NewReader(b)
			}
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

	// 1. Uji Judul Kosong
	resp, body := doReq(http.MethodPost, baseURL+"/api/work-items", map[string]interface{}{
		"title":       "   ",
		"received_on": "2026-10-17",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Judul kosong seharusnya 400 Bad Request, didapat: %d, body: %s", resp.StatusCode, body)
	}

	// 2. Uji Format Tanggal Diterima Tidak Valid (bukan YYYY-MM-DD)
	resp, body = doReq(http.MethodPost, baseURL+"/api/work-items", map[string]interface{}{
		"title":       "Judul Valid",
		"received_on": "17-10-2026",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Format tanggal salah seharusnya 400 Bad Request, didapat: %d, body: %s", resp.StatusCode, body)
	}

	// 3. Uji Tanggal Tidak Masuk Akal (misal 30 Februari 2026)
	resp, body = doReq(http.MethodPost, baseURL+"/api/work-items", map[string]interface{}{
		"title":       "Judul Valid",
		"received_on": "2026-02-30",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Tanggal kalender salah seharusnya 400 Bad Request, didapat: %d, body: %s", resp.StatusCode, body)
	}

	// 4. Uji Prioritas Tidak Valid
	resp, body = doReq(http.MethodPost, baseURL+"/api/work-items", map[string]interface{}{
		"title":       "Judul Valid",
		"received_on": "2026-10-17",
		"priority":    "super_critical",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Prioritas salah seharusnya 400 Bad Request, didapat: %d, body: %s", resp.StatusCode, body)
	}

	// 5. Uji JSON Rusak / Malformed
	resp, body = doReq(http.MethodPost, baseURL+"/api/work-items", "{ malformed json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Malformed JSON seharusnya 400 Bad Request, didapat: %d, body: %s", resp.StatusCode, body)
	}

	// 6. Uji ID Tidak Ditemukan (404)
	resp, body = doReq(http.MethodGet, baseURL+"/api/work-items/999999", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("ID 999999 seharusnya 404 Not Found, didapat: %d, body: %s", resp.StatusCode, body)
	}

	// 7. Uji ID Bukan Numerik (400)
	resp, body = doReq(http.MethodGet, baseURL+"/api/work-items/invalid-id-abc", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("ID non-numerik seharusnya 400 Bad Request, didapat: %d, body: %s", resp.StatusCode, body)
	}

	// 8. Uji Catatan Aktivitas Kosong
	// Buat item valid dulu
	respValid, _ := doReq(http.MethodPost, baseURL+"/api/work-items", domain.CreateWorkItemInput{
		Title:      "Item Valid Untuk Uji Log",
		ReceivedOn: "2026-10-17",
	})
	var validItem domain.WorkItem
	_ = json.Unmarshal([]byte(body), &validItem)
	if respValid.StatusCode == http.StatusCreated {
		_ = json.NewDecoder(respValid.Body).Decode(&validItem)
	}

	resp, body = doReq(http.MethodPost, fmt.Sprintf("%s/api/work-items/1/activities", baseURL), map[string]interface{}{
		"description": "   ",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Deskripsi aktivitas kosong seharusnya 400 Bad Request, didapat: %d, body: %s", resp.StatusCode, body)
	}
}

func TestDomainModelValidationDirect(t *testing.T) {
	// 1. Validasi Status Enum
	validStatuses := []string{
		domain.StatusNotStarted,
		domain.StatusInProgress,
		domain.StatusWaiting,
		domain.StatusCompleted,
		domain.StatusCancelled,
	}
	for _, s := range validStatuses {
		if !domain.IsValidStatus(s) {
			t.Errorf("Status %s seharusnya valid", s)
		}
	}
	if domain.IsValidStatus("unknown_status") {
		t.Errorf("unknown_status seharusnya tidak valid")
	}

	// 2. Validasi Prioritas Enum
	validPriorities := []string{
		domain.PriorityLow,
		domain.PriorityNormal,
		domain.PriorityHigh,
		domain.PriorityCritical,
	}
	for _, p := range validPriorities {
		if !domain.IsValidPriority(p) {
			t.Errorf("Prioritas %s seharusnya valid", p)
		}
	}
	if domain.IsValidPriority("urgent") {
		t.Errorf("urgent seharusnya tidak valid")
	}

	// 3. Validasi Tanggal
	if err := domain.ValidateDateYYYYMMDD("2026-12-31"); err != nil {
		t.Errorf("2026-12-31 seharusnya valid: %v", err)
	}
	if err := domain.ValidateDateYYYYMMDD("2026-02-29"); err == nil {
		t.Errorf("2026-02-29 bukan tahun kabisat, seharusnya tidak valid")
	}
}
