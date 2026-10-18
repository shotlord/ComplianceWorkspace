package service

import (
	"context"
	"crypto/rand"
	"database/sql"
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
	"compliance-workspace/internal/domain"
)

const ErrRetentionLimit = "penghapusan ditolak: sistem mewajibkan setidaknya satu cadangan valid dipertahankan demi keselamatan data dan pemulihan darurat"

// BackupItem mewakili metadata file cadangan SQLite
type BackupItem struct {
	Filename      string    `json:"filename"`
	SizeBytes     int64     `json:"size_bytes"`
	FormattedSize string    `json:"formatted_size"`
	CreatedAt     time.Time `json:"created_at"`
	Status        string    `json:"status"` // "valid" atau "corrupt"
	SchemaVersion int       `json:"schema_version"`
}

// BackupService mengelola proses backup konsisten (VACUUM INTO) dan restore basis data
type BackupService struct {
	mu              sync.Mutex
	workItemService *WorkItemService
	taskMgmtSvc    *TaskManagementService
	importSvc       *ImportService
	dbPath          string
	backupDir       string
}

func NewBackupService(workItemService *WorkItemService, taskMgmtSvc *TaskManagementService, importSvc *ImportService, dbPath, backupDir string) *BackupService {
	return &BackupService{
		workItemService: workItemService,
		taskMgmtSvc:    taskMgmtSvc,
		importSvc:      importSvc,
		dbPath:         dbPath,
		backupDir:      backupDir,
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

func validateBackupFilename(filename string) (string, error) {
	trimmed := strings.TrimSpace(filename)
	if trimmed == "" {
		return "", errors.New("nama file cadangan tidak boleh kosong")
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned != filepath.Base(cleaned) || strings.Contains(cleaned, "..") || strings.ContainsAny(cleaned, `/\:`) {
		return "", errors.New("nama file cadangan tidak valid atau mengandung karakter berbahaya")
	}
	if !strings.HasSuffix(cleaned, ".db") && !strings.HasSuffix(cleaned, ".sqlite") {
		return "", errors.New("ekstensi file cadangan harus berupa .db atau .sqlite")
	}
	return cleaned, nil
}

// getBackupFileInfo mengekstrak versi skema dan status integritas berkas cadangan secara aman
func getBackupFileInfo(ctx context.Context, filePath string) (int, string) {
	db, err := database.OpenDB(filePath)
	if err != nil {
		return 0, "corrupt"
	}
	defer db.Close()

	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check;").Scan(&integrity); err != nil || integrity != "ok" {
		return 0, "corrupt"
	}

	var hasMigrations int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&hasMigrations)
	if hasMigrations == 0 {
		return 0, "corrupt"
	}

	var maxVer int
	_ = db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations;").Scan(&maxVer)
	return maxVer, "valid"
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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

		filePath := filepath.Join(s.backupDir, name)
		ver, status := getBackupFileInfo(ctx, filePath)

		items = append(items, &BackupItem{
			Filename:      name,
			SizeBytes:     info.Size(),
			FormattedSize: formatBytes(info.Size()),
			CreatedAt:     info.ModTime().UTC(),
			Status:        status,
			SchemaVersion: ver,
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
	var maxVer int
	var migrationCount int
	err = checkDB.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(MAX(version), 0) FROM schema_migrations;").Scan(&migrationCount, &maxVer)
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
		SchemaVersion: maxVer,
	}, nil
}

func (s *BackupService) countValidBackups() (int, []string, error) {
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil, nil
		}
		return 0, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var validFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".db") && !strings.HasSuffix(name, ".sqlite") {
			continue
		}
		filePath := filepath.Join(s.backupDir, name)
		_, status := getBackupFileInfo(ctx, filePath)
		if status == "valid" {
			validFiles = append(validFiles, name)
		}
	}
	return len(validFiles), validFiles, nil
}

// DeleteBackup menghapus satu berkas cadangan dengan batasan retensi dan keamanan
func (s *BackupService) DeleteBackup(ctx context.Context, filename string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleaned, err := validateBackupFilename(filename)
	if err != nil {
		return err
	}

	targetPath := filepath.Join(s.backupDir, cleaned)
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("gagal menyelesaikan path target: %w", err)
	}
	absDB, _ := filepath.Abs(s.dbPath)
	if absTarget == absDB {
		return errors.New("penghapusan ditolak: tidak diperbolehkan menghapus berkas database aktif")
	}

	if _, err := os.Stat(targetPath); err != nil {
		return fmt.Errorf("berkas cadangan '%s' tidak ditemukan: %w", cleaned, err)
	}

	// Aturan Retensi Minimal: jika berkas yang akan dihapus valid dan tersisa <= 1 cadangan valid, tolak
	validCount, validFiles, err := s.countValidBackups()
	if err != nil {
		return fmt.Errorf("gagal memeriksa cadangan tersisa: %w", err)
	}

	isTargetValid := false
	for _, vf := range validFiles {
		if vf == cleaned {
			isTargetValid = true
			break
		}
	}

	if isTargetValid && validCount <= 1 {
		return errors.New(ErrRetentionLimit)
	}

	if err := os.Remove(targetPath); err != nil {
		return fmt.Errorf("gagal menghapus berkas cadangan: %w", err)
	}

	return nil
}

// DeleteBackups menghapus sejumlah berkas cadangan secara batch dengan batasan retensi
func (s *BackupService) DeleteBackups(ctx context.Context, filenames []string) (*domain.BatchDeleteBackupResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(filenames) == 0 {
		return nil, errors.New("daftar berkas yang akan dihapus tidak boleh kosong")
	}

	// Validasi seluruh filename terlebih dahulu dan buang duplikat
	cleanedMap := make(map[string]string)
	for _, fn := range filenames {
		cleaned, err := validateBackupFilename(fn)
		if err != nil {
			return nil, err
		}
		targetPath := filepath.Join(s.backupDir, cleaned)
		absTarget, _ := filepath.Abs(targetPath)
		absDB, _ := filepath.Abs(s.dbPath)
		if absTarget == absDB {
			return nil, errors.New("penghapusan ditolak: tidak diperbolehkan menghapus berkas database aktif")
		}
		cleanedMap[cleaned] = targetPath
	}

	validCount, validFiles, err := s.countValidBackups()
	if err != nil {
		return nil, fmt.Errorf("gagal memeriksa cadangan tersisa: %w", err)
	}

	validToDeleteCount := 0
	for _, vf := range validFiles {
		if _, ok := cleanedMap[vf]; ok {
			validToDeleteCount++
		}
	}

	// Tolak seluruh operasi jika akan menyisakan 0 cadangan valid
	if validCount-validToDeleteCount < 1 {
		return nil, errors.New(ErrRetentionLimit)
	}

	result := &domain.BatchDeleteBackupResult{
		DeletedFiles: make([]string, 0),
		FailedFiles:  make([]string, 0),
	}

	for cleaned, path := range cleanedMap {
		if err := os.Remove(path); err != nil {
			result.FailedFiles = append(result.FailedFiles, cleaned)
		} else {
			result.DeletedFiles = append(result.DeletedFiles, cleaned)
			result.DeletedCount++
		}
	}

	remainingEntries, _ := s.ListBackups()
	result.RemainingCount = len(remainingEntries)

	return result, nil
}

func countTableSafe(ctx context.Context, db *sql.DB, tableName string, filterDeleted bool) int {
	var tableExists int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&tableExists)
	if tableExists == 0 {
		return 0
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	if filterDeleted && tableName == "work_items" {
		var colExists int
		_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('work_items') WHERE name='deleted_at'").Scan(&colExists)
		if colExists > 0 {
			query += " WHERE deleted_at IS NULL"
		}
	}
	var count int
	_ = db.QueryRowContext(ctx, query).Scan(&count)
	return count
}

func getDatabaseSummary(ctx context.Context, db *sql.DB) domain.DatabaseSummary {
	return domain.DatabaseSummary{
		WorkItemsCount:          countTableSafe(ctx, db, "work_items", true),
		ActivityLogsCount:       countTableSafe(ctx, db, "activity_logs", false),
		TemplatesCount:          countTableSafe(ctx, db, "work_templates", false),
		RecurringSchedulesCount: countTableSafe(ctx, db, "recurring_schedules", false),
		CategoriesCount:         countTableSafe(ctx, db, "categories", false),
		ImportBatchesCount:      countTableSafe(ctx, db, "import_batches", false),
	}
}

// InspectBackup memeriksa isi berkas cadangan dan membandingkannya dengan database aktif saat ini
func (s *BackupService) InspectBackup(ctx context.Context, filename string) (*domain.RestoreInspectionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleaned, err := validateBackupFilename(filename)
	if err != nil {
		return nil, err
	}

	sourcePath := filepath.Join(s.backupDir, cleaned)
	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("berkas cadangan '%s' tidak ditemukan: %w", cleaned, err)
	}

	result := &domain.RestoreInspectionResult{
		Filename:      cleaned,
		SizeBytes:     info.Size(),
		FormattedSize: formatBytes(info.Size()),
		CreatedAt:     info.ModTime().UTC(),
		Warnings:      make([]string, 0),
	}

	checkDB, err := database.OpenDB(sourcePath)
	if err != nil {
		result.IntegrityStatus = fmt.Sprintf("gagal membuka berkas: %v", err)
		result.IsCompatible = false
		result.Warnings = append(result.Warnings, "Berkas cadangan rusak atau tidak dapat dibaca.")
		return result, nil
	}
	defer checkDB.Close()

	var integrityResult string
	if err := checkDB.QueryRowContext(ctx, "PRAGMA integrity_check;").Scan(&integrityResult); err != nil || integrityResult != "ok" {
		result.IntegrityStatus = integrityResult
		result.IsCompatible = false
		result.Warnings = append(result.Warnings, fmt.Sprintf("Integritas berkas cadangan tidak valid: %s", integrityResult))
	} else {
		result.IntegrityStatus = "ok"
	}

	var schemaVersion int
	var hasMigrations int
	_ = checkDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&hasMigrations)
	if hasMigrations > 0 {
		_ = checkDB.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&schemaVersion)
	}
	result.SchemaVersion = schemaVersion

	// Ringkasan data cadangan
	result.DataSummary = getDatabaseSummary(ctx, checkDB)

	// Ringkasan database aktif
	activeDB := s.workItemService.GetDB()
	if activeDB != nil {
		result.ActiveDatabaseSummary = getDatabaseSummary(ctx, activeDB)
	}

	// Evaluasi kompatibilitas dan perbedaan
	schemaUnsupported := schemaVersion > database.MaxSupportedSchemaVersion
	schemaMigrationNeeded := schemaVersion < database.MaxSupportedSchemaVersion && schemaVersion > 0
	recordsLostWarning := (result.ActiveDatabaseSummary.WorkItemsCount > result.DataSummary.WorkItemsCount) ||
		(result.ActiveDatabaseSummary.ActivityLogsCount > result.DataSummary.ActivityLogsCount)

	result.Differences = domain.RestoreDifferences{
		RecordsLostWarning:    recordsLostWarning,
		SchemaMigrationNeeded: schemaMigrationNeeded,
		SchemaUnsupported:     schemaUnsupported,
	}

	result.IsCompatible = (result.IntegrityStatus == "ok") && !schemaUnsupported && (schemaVersion > 0)

	if schemaUnsupported {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Versi skema cadangan (v%d) lebih baru dari versi maksimum yang didukung aplikasi ini (v%d). Pemulihan tidak dapat dilakukan.", schemaVersion, database.MaxSupportedSchemaVersion))
	}
	if schemaMigrationNeeded {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Berkas cadangan dibuat dengan versi skema v%d. Sistem akan otomatis memigrasikan struktur database ke versi v%d saat pemulihan.", schemaVersion, database.MaxSupportedSchemaVersion))
	}
	if recordsLostWarning {
		result.Warnings = append(result.Warnings, fmt.Sprintf("PERINGATAN RISIKO DATA: Basis data aktif saat ini memiliki lebih banyak data (%d pekerjaan, %d log) dibandingkan berkas cadangan (%d pekerjaan, %d log). Memulihkan berkas ini akan menimpa data aktif saat ini dan menghilangkan perubahan terbaru.", result.ActiveDatabaseSummary.WorkItemsCount, result.ActiveDatabaseSummary.ActivityLogsCount, result.DataSummary.WorkItemsCount, result.DataSummary.ActivityLogsCount))
	}

	return result, nil
}

// RestoreBackup memulihkan database dari file backup yang valid secara aman
func (s *BackupService) RestoreBackup(ctx context.Context, filename string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Validasi Keamanan Path Filename (Cegah Directory Traversal)
	cleaned, err := validateBackupFilename(filename)
	if err != nil {
		return err
	}

	sourcePath := filepath.Join(s.backupDir, cleaned)
	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("file cadangan '%s' tidak ditemukan: %w", cleaned, err)
	}

	// 2. Validasi Integritas dan Versi Skema File Cadangan SEBELUM Menyentuh Database Aktif
	checkDB, err := database.OpenDB(sourcePath)
	if err != nil {
		return fmt.Errorf("file cadangan tidak dapat dibuka atau korup: %w", err)
	}

	var integrityResult string
	if err := checkDB.QueryRowContext(ctx, "PRAGMA integrity_check;").Scan(&integrityResult); err != nil || integrityResult != "ok" {
		_ = checkDB.Close()
		return fmt.Errorf("integritas file cadangan tidak valid: %s (%v)", integrityResult, err)
	}

	var schemaVersion int
	var hasMigrations int
	_ = checkDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&hasMigrations)
	if hasMigrations == 0 {
		_ = checkDB.Close()
		return errors.New("file cadangan tidak kompatibel: tabel schema_migrations tidak ditemukan")
	}
	_ = checkDB.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations;").Scan(&schemaVersion)
	_ = checkDB.Close()

	if schemaVersion > database.MaxSupportedSchemaVersion {
		return fmt.Errorf("pemulihan ditolak: versi skema cadangan (v%d) lebih baru dari versi yang didukung aplikasi ini (v%d)", schemaVersion, database.MaxSupportedSchemaVersion)
	}

	// 3. Buat Salinan Pengaman Database Aktif Sebelum Digantikan (Safety Snapshot)
	activeDB := s.workItemService.GetDB()
	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	randSuffix := hex.EncodeToString(randBytes)
	safetyName := fmt.Sprintf("pre_restore_safety_%s_%s.db", time.Now().UTC().Format("20060102_150405"), randSuffix)
	safetyPath := filepath.Join(s.backupDir, safetyName)

	if activeDB != nil {
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
		// Rollback jika copy gagal
		if _, statErr := os.Stat(safetyPath); statErr == nil {
			_ = copyFile(safetyPath, s.dbPath)
			rbDB, _ := database.OpenDB(s.dbPath)
			if rbDB != nil {
				s.updateServiceDBs(rbDB)
			}
		}
		return fmt.Errorf("gagal menggantikan file database aktif: %w", err)
	}

	// 6. Buka Kembali Koneksi Database Baru dan Jalankan Migrasi
	newDB, err := database.OpenDB(s.dbPath)
	if err != nil {
		// Rollback
		if _, statErr := os.Stat(safetyPath); statErr == nil {
			_ = copyFile(safetyPath, s.dbPath)
			rbDB, _ := database.OpenDB(s.dbPath)
			if rbDB != nil {
				s.updateServiceDBs(rbDB)
			}
		}
		return fmt.Errorf("gagal membuka kembali database setelah pemulihan: %w", err)
	}

	if err := database.Migrate(ctx, newDB); err != nil {
		// ROLLBACK OTOMATIS: Migrasi pasca-restore gagal!
		_ = newDB.Close()
		if _, statErr := os.Stat(safetyPath); statErr == nil {
			_ = copyFile(safetyPath, s.dbPath)
			rbDB, _ := database.OpenDB(s.dbPath)
			if rbDB != nil {
				s.updateServiceDBs(rbDB)
			}
		}
		return fmt.Errorf("migrasi skema pasca-pemulihan gagal, database aktif telah dikembalikan ke kondisi sebelum restore (rollback): %w", err)
	}

	s.updateServiceDBs(newDB)
	return nil
}

func (s *BackupService) updateServiceDBs(db *sql.DB) {
	s.workItemService.SetDB(db)
	if s.taskMgmtSvc != nil {
		s.taskMgmtSvc.SetDB(db)
	}
	if s.importSvc != nil {
		s.importSvc.SetDB(db)
	}
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
