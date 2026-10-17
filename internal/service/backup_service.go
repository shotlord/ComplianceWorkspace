package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"compliance-workspace/internal/database"
)

// BackupItem mewakili metadata file cadangan SQLite
type BackupItem struct {
	Filename      string    `json:"filename"`
	SizeBytes     int64     `json:"size_bytes"`
	FormattedSize string    `json:"formatted_size"`
	CreatedAt     time.Time `json:"created_at"`
	Status        string    `json:"status"` // "valid" atau "corrupt"
}

// BackupService mengelola proses backup konsisten (VACUUM INTO) dan restore basis data
type BackupService struct {
	mu              sync.Mutex
	workItemService *WorkItemService
	dbPath          string
	backupDir       string
}

func NewBackupService(workItemService *WorkItemService, dbPath, backupDir string) *BackupService {
	return &BackupService{
		workItemService: workItemService,
		dbPath:          dbPath,
		backupDir:       backupDir,
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ListBackups mengambil daftar seluruh file backup pada folder backups/
func (s *BackupService) ListBackups() ([]*BackupItem, error) {
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*BackupItem{}, nil
		}
		return nil, fmt.Errorf("gagal membaca folder backup: %w", err)
	}

	var items []*BackupItem
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".db") && !strings.HasSuffix(name, ".sqlite") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		items = append(items, &BackupItem{
			Filename:      name,
			SizeBytes:     info.Size(),
			FormattedSize: formatBytes(info.Size()),
			CreatedAt:     info.ModTime().UTC(),
			Status:        "valid",
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	return items, nil
}

// CreateBackup membuat backup konsisten SQLite menggunakan VACUUM INTO dan memverifikasi integritasnya
func (s *BackupService) CreateBackup(ctx context.Context) (*BackupItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.backupDir, 0755); err != nil {
		return nil, fmt.Errorf("gagal membuat direktori backup: %w", err)
	}

	// Buat nama file unik: compliance_backup_YYYYMMDD_HHMMSS_<random8>.db
	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	randSuffix := hex.EncodeToString(randBytes)

	timestamp := time.Now().UTC().Format("20060102_150405")
	filename := fmt.Sprintf("compliance_backup_%s_%s.db", timestamp, randSuffix)
	targetPath := filepath.Join(s.backupDir, filename)

	// Pastikan database aktif siap
	activeDB := s.workItemService.GetDB()
	if activeDB == nil {
		return nil, errors.New("koneksi database aktif tidak tersedia")
	}

	// Gunakan VACUUM INTO untuk membuat snapshot konsisten SQLite tanpa menyalin file mentah
	escapedPath := strings.ReplaceAll(targetPath, "'", "''")
	vacuumQuery := fmt.Sprintf("VACUUM INTO '%s'", escapedPath)

	if _, err := activeDB.ExecContext(ctx, vacuumQuery); err != nil {
		_ = os.Remove(targetPath)
		return nil, fmt.Errorf("eksekusi VACUUM INTO gagal: %w", err)
	}

	// Lakukan pemeriksaan integritas pada file backup yang baru dibuat
	checkDB, err := database.OpenDB(targetPath)
	if err != nil {
		_ = os.Remove(targetPath)
		return nil, fmt.Errorf("gagal membuka file cadangan untuk verifikasi: %w", err)
	}

	var integrityResult string
	err = checkDB.QueryRowContext(ctx, "PRAGMA integrity_check;").Scan(&integrityResult)
	if err != nil || integrityResult != "ok" {
		_ = checkDB.Close()
		_ = os.Remove(targetPath)
		return nil, fmt.Errorf("pemeriksaan integritas cadangan gagal: %v (%s)", err, integrityResult)
	}

	// Pastikan tabel metadata migrasi ada dan valid
	var migrationCount int
	err = checkDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations;").Scan(&migrationCount)
	_ = checkDB.Close()
	if err != nil || migrationCount == 0 {
		_ = os.Remove(targetPath)
		return nil, fmt.Errorf("verifikasi tabel migrasi pada cadangan gagal: %w", err)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca info file cadangan: %w", err)
	}

	return &BackupItem{
		Filename:      filename,
		SizeBytes:     info.Size(),
		FormattedSize: formatBytes(info.Size()),
		CreatedAt:     info.ModTime().UTC(),
		Status:        "valid",
	}, nil
}

// RestoreBackup memulihkan database dari file backup yang valid secara aman
func (s *BackupService) RestoreBackup(ctx context.Context, filename string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Validasi Keamanan Path Filename (Cegah Directory Traversal)
	cleaned := filepath.Clean(filename)
	if cleaned != filepath.Base(cleaned) || strings.Contains(cleaned, "..") || strings.ContainsAny(cleaned, `/\`) {
		return errors.New("nama file cadangan tidak valid atau mengandung karakter berbahaya")
	}
	if !strings.HasSuffix(cleaned, ".db") && !strings.HasSuffix(cleaned, ".sqlite") {
		return errors.New("ekstensi file cadangan harus berupa .db atau .sqlite")
	}

	sourcePath := filepath.Join(s.backupDir, cleaned)
	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("file cadangan '%s' tidak ditemukan: %w", cleaned, err)
	}

	// 2. Validasi Integritas File Cadangan SEBELUM Menyentuh Database Aktif
	checkDB, err := database.OpenDB(sourcePath)
	if err != nil {
		return fmt.Errorf("file cadangan tidak dapat dibuka atau korup: %w", err)
	}

	var integrityResult string
	if err := checkDB.QueryRowContext(ctx, "PRAGMA integrity_check;").Scan(&integrityResult); err != nil || integrityResult != "ok" {
		_ = checkDB.Close()
		return fmt.Errorf("integritas file cadangan tidak valid: %s (%v)", integrityResult, err)
	}

	var migrationCount int
	err = checkDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations;").Scan(&migrationCount)
	_ = checkDB.Close()
	if err != nil || migrationCount == 0 {
		return errors.New("file cadangan tidak kompatibel: tabel schema_migrations tidak ditemukan")
	}

	// 3. Buat Salinan Pengaman Database Aktif Sebelum Digantikan (Safety Snapshot)
	activeDB := s.workItemService.GetDB()
	if activeDB != nil {
		safetyName := fmt.Sprintf("pre_restore_safety_%s.db", time.Now().UTC().Format("20060102_150405"))
		safetyPath := filepath.Join(s.backupDir, safetyName)
		escapedSafety := strings.ReplaceAll(safetyPath, "'", "''")
		_, _ = activeDB.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", escapedSafety))
	}

	// 4. Tutup Koneksi Database Aktif Agar File Bebas dari Lock Windows
	if activeDB != nil {
		if err := activeDB.Close(); err != nil {
			return fmt.Errorf("gagal menutup koneksi database aktif: %w", err)
		}
	}

	// 5. Salin Isi File Cadangan Menggantikan database aktif (data/compliance.db)
	if err := copyFile(sourcePath, s.dbPath); err != nil {
		return fmt.Errorf("gagal menggantikan file database aktif: %w", err)
	}

	// 6. Buka Kembali Koneksi Database Baru dan Sinkronkan ke Service/Repo
	newDB, err := database.OpenDB(s.dbPath)
	if err != nil {
		return fmt.Errorf("gagal membuka kembali database setelah pemulihan: %w", err)
	}

	if err := database.Migrate(ctx, newDB); err != nil {
		return fmt.Errorf("gagal menyinkronkan migrasi pada database yang dipulihkan: %w", err)
	}

	s.workItemService.SetDB(newDB)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
