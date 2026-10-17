package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"compliance-workspace/internal/config"
)

func TestCheckWritableSuccess(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")

	err := config.CheckWritable(dataDir)
	if err != nil {
		t.Fatalf("CheckWritable seharusnya sukses pada folder temp: %v", err)
	}

	// Pastikan folder data berhasil dibuat
	info, err := os.Stat(dataDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("Folder data tidak dibuat secara benar")
	}
}

func TestCheckWritableFailWhenPathIsFile(t *testing.T) {
	tempDir := t.TempDir()
	fakeDir := filepath.Join(tempDir, "fake_dir_as_file")

	// Buat file biasa dengan nama tersebut
	if err := os.WriteFile(fakeDir, []byte("isi file"), 0644); err != nil {
		t.Fatalf("Gagal membuat dummy file: %v", err)
	}

	// Coba panggil CheckWritable pada subfolder dari file tersebut
	blockedSubdir := filepath.Join(fakeDir, "data")
	err := config.CheckWritable(blockedSubdir)
	if err == nil {
		t.Fatalf("CheckWritable seharusnya gagal saat parent path berupa file")
	}

	if !strings.Contains(err.Error(), "tidak memiliki izin tulis") && !strings.Contains(err.Error(), "tidak dapat dibuat") {
		t.Errorf("Pesan error tidak informatif: %v", err)
	}
}
