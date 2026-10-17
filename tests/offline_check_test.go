package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"compliance-workspace/internal/server"
)

func TestOfflineNoExternalCDNOrAnalytics(t *testing.T) {
	// Direktori frontend
	webDir := "../web/static"
	if _, err := os.Stat(webDir); err != nil {
		webDir = "web/static"
	}

	filesToCheck := []string{
		filepath.Join(webDir, "index.html"),
		filepath.Join(webDir, "css", "app.css"),
		filepath.Join(webDir, "js", "api.js"),
		filepath.Join(webDir, "js", "app.js"),
	}

	// Pola CDN dan layanan pihak ketiga yang dilarang
	forbiddenPatterns := []*regexp.Regexp{
		regexp.MustCompile(`https?://cdn`),
		regexp.MustCompile(`https?://cdnjs`),
		regexp.MustCompile(`https?://unpkg`),
		regexp.MustCompile(`https?://jsdelivr`),
		regexp.MustCompile(`fonts\.googleapis\.com`),
		regexp.MustCompile(`fonts\.gstatic\.com`),
		regexp.MustCompile(`google-analytics\.com`),
		regexp.MustCompile(`googletagmanager\.com`),
		regexp.MustCompile(`mixpanel\.com`),
		regexp.MustCompile(`fontawesome`),
	}

	for _, file := range filesToCheck {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("Gagal membaca file %s: %v", file, err)
		}

		for _, pattern := range forbiddenPatterns {
			if pattern.Match(content) {
				t.Errorf("File %s mengandung referensi eksternal/CDN terlarang: %s", file, pattern.String())
			}
		}
	}
}

func TestRejectNonLocalhostBinding(t *testing.T) {
	// Pastikan server menolak binding ke 0.0.0.0
	_, err := server.NewServer("0.0.0.0:8080", nil, nil, nil)
	if err == nil {
		t.Errorf("Server seharusnya menolak binding ke 0.0.0.0")
	}
	if !strings.Contains(err.Error(), "hanya diizinkan mengikat ke 127.0.0.1") {
		t.Errorf("Pesan error tidak sesuai: %v", err)
	}

	// Pastikan server menolak binding ke IP selain 127.0.0.1
	_, err2 := server.NewServer("192.168.1.100:8080", nil, nil, nil)
	if err2 == nil {
		t.Errorf("Server seharusnya menolak binding ke IP non-localhost")
	}
}
