package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"compliance-workspace/internal/server"
)

func securityTestCSRF(t *testing.T, client *http.Client, baseURL string) (string, *http.Cookie) {
	t.Helper()
	resp, err := client.Get(baseURL + "/api/csrf-token")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload struct {
		Token string `json:"csrf_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	var csrfCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == server.CSRFCookieName {
			csrfCookie = cookie
			break
		}
	}
	if len(payload.Token) != 64 || csrfCookie == nil {
		t.Fatal("token/cookie CSRF tidak valid")
	}
	return payload.Token, csrfCookie
}

func TestSecurityHardeningExactHostOriginAndCSRF(t *testing.T) {
	_, baseURL, cleanup := setupTestServer(t)
	defer cleanup()
	client := &http.Client{}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	port := parsed.Port()

	for _, badHost := range []string{"127.0.0.1:1", "localhost.evil:" + port, "127.0.0.1." + port + ".evil"} {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/csrf-token", nil)
		req.Host = badHost
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request Host %q gagal: %v", badHost, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Host %q harus ditolak 400, mendapat %d", badHost, resp.StatusCode)
		}
	}

	token, cookie := securityTestCSRF(t, client, baseURL)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie CSRF harus HttpOnly dan SameSite=Strict: %+v", cookie)
	}

	for _, badOrigin := range []string{"http://localhost.evil:" + port, "http://127.0.0.1:1", "https://" + parsed.Host} {
		req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/work-items", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", badOrigin)
		req.Header.Set(server.CSRFHeaderName, token)
		req.AddCookie(cookie)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request Origin %q gagal: %v", badOrigin, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Origin %q harus ditolak 403, mendapat %d", badOrigin, resp.StatusCode)
		}
	}

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/work-items", strings.NewReader(`{"title":"Valid","received_on":"2026-10-19"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", baseURL)
	req.Header.Set(server.CSRFHeaderName, token)
	req.AddCookie(cookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Origin lokal exact harus diterima, mendapat %d: %s", resp.StatusCode, body)
	}
}

func TestSecurityHardeningCSPAndRequestLimits(t *testing.T) {
	_, baseURL, cleanup := setupTestServer(t)
	defer cleanup()
	client := &http.Client{}

	rootResp, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	csp := rootResp.Header.Get("Content-Security-Policy")
	_ = rootResp.Body.Close()
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("CSP masih mengizinkan inline script: %s", csp)
	}
	for _, directive := range []string{"script-src 'self'", "object-src 'none'", "base-uri 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP tidak memuat %q: %s", directive, csp)
		}
	}

	token, cookie := securityTestCSRF(t, client, baseURL)
	largeBody := `{"title":"` + strings.Repeat("A", (1<<20)+1) + `","received_on":"2026-10-19"}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/work-items", strings.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", baseURL)
	req.Header.Set(server.CSRFHeaderName, token)
	req.AddCookie(cookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("JSON lebih dari 1 MiB harus ditolak 413, mendapat %d", resp.StatusCode)
	}

	for _, malformed := range []string{
		`{"title":"A","received_on":"2026-10-19"}{"title":"B"}`,
		`{"title":"A","received_on":"2026-10-19","unknown_field":true}`,
	} {
		req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/work-items", strings.NewReader(malformed))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", baseURL)
		req.Header.Set(server.CSRFHeaderName, token)
		req.AddCookie(cookie)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("JSON malformed/unknown harus ditolak 400, mendapat %d", resp.StatusCode)
		}
	}
}

func TestSecurityHardeningMultipartLimitsAndCleanup(t *testing.T) {
	_, baseURL, _, _, cleanup := setupImportTestServer(t)
	defer cleanup()
	client := &http.Client{}
	token, cookie := securityTestCSRF(t, client, baseURL)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "oversized.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, strings.NewReader(strings.Repeat("A", 10*1024*1024+1))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/import/preview", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Content-Length", strconv.Itoa(body.Len()))
	req.Header.Set("Origin", baseURL)
	req.Header.Set(server.CSRFHeaderName, token)
	req.AddCookie(cookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("file impor >10 MiB harus ditolak 413, mendapat %d: %s", resp.StatusCode, responseBody)
	}
}
