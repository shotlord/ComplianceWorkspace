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
	mu              sync.RWMutex
	operationGate   *sync.RWMutex
	workItemService *WorkItemService
	taskMgmtSvc     *TaskManagementService
	importSvc       *ImportService
	dbPath          string
	backupDir       string
	faultInjector   func(point, path string) error
}

func NewBackupService(workItemService *WorkItemService, taskMgmtSvc *TaskManagementService, importSvc *ImportService, dbPath, backupDir string) *BackupService {
	gate := &sync.RWMutex{}
	service := &BackupService{
		operationGate:   gate,
		workItemService: workItemService,
		taskMgmtSvc:     taskMgmtSvc,
		importSvc:       importSvc,
		dbPath:          dbPath,
		backupDir:       backupDir,
	}
	if taskMgmtSvc != nil {
		taskMgmtSvc.SetOperationGate(gate)
	}
	return service
}

// OperationGate mengoordinasikan request biasa dengan penggantian koneksi saat restore.
func (s *BackupService) OperationGate() *sync.RWMutex {
	return s.operationGate
}

func (s *BackupService) injectFault(point, path string) error {
	if s.faultInjector == nil {
		return nil
	}
	return s.faultInjector(point, path)
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
		return "", errors.New("nama berkas cadangan tidak boleh kosong")
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned != filepath.Base(cleaned) || strings.Contains(cleaned, "..") || strings.ContainsAny(cleaned, `/\:`) {
		return "", errors.New("nama berkas cadangan tidak valid atau mengandung karakter berbahaya")
	}
	lowerName := strings.ToLower(cleaned)
	if !strings.HasSuffix(lowerName, ".db") && !strings.HasSuffix(lowerName, ".sqlite") {
		return "", errors.New("ekstensi berkas cadangan harus berupa .db atau .sqlite")
	}
	return cleaned, nil
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func ensureRealDirectory(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("gagal menyelesaikan direktori: %w", err)
	}
	info, err := os.Lstat(absDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("direktori cadangan tidak valid atau merupakan symbolic link/reparse point")
	}
	resolved, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return "", fmt.Errorf("gagal memvalidasi direktori cadangan: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	// Gunakan path hasil resolusi untuk seluruh containment check. Windows dapat
	// menormalkan nama panjang menjadi 8.3 tanpa berarti direktori adalah reparse point.
	return resolved, nil
}

func secureExistingBackupPath(backupDir, filename string) (string, os.FileInfo, error) {
	cleaned, err := validateBackupFilename(filename)
	if err != nil {
		return "", nil, err
	}
	realDir, err := ensureRealDirectory(backupDir)
	if err != nil {
		return "", nil, fmt.Errorf("direktori cadangan tidak aman: %w", err)
	}
	target := filepath.Join(realDir, cleaned)
	info, err := os.Lstat(target)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("berkas cadangan harus berupa berkas biasa dan bukan symbolic link/reparse point")
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", nil, fmt.Errorf("gagal memvalidasi jalur cadangan: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", nil, err
	}
	rel, err := filepath.Rel(realDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", nil, errors.New("jalur cadangan berada di luar direktori cadangan")
	}
	if !samePath(target, resolved) {
		return "", nil, errors.New("symbolic link/reparse point pada berkas cadangan tidak diizinkan")
	}
	return target, info, nil
}

func secureExistingDatabasePath(dbPath string) (string, string, error) {
	dataDir, err := ensureRealDirectory(filepath.Dir(dbPath))
	if err != nil {
		return "", "", fmt.Errorf("direktori database tidak aman: %w", err)
	}
	absDB, err := filepath.Abs(dbPath)
	if err != nil {
		return "", "", err
	}
	info, err := os.Lstat(absDB)
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("database aktif harus berupa berkas biasa dan bukan symbolic link/reparse point")
	}
	resolved, err := filepath.EvalSymlinks(absDB)
	if err != nil {
		return "", "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", "", err
	}
	expected := filepath.Join(dataDir, filepath.Base(absDB))
	if !samePath(expected, resolved) {
		return "", "", errors.New("database aktif melalui symbolic link/reparse point tidak diizinkan")
	}
	return resolved, dataDir, nil
}

func randomSuffix() (string, error) {
	randBytes := make([]byte, 4)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("gagal membuat nama berkas acak: %w", err)
	}
	return hex.EncodeToString(randBytes), nil
}

// getBackupFileInfo mengekstrak versi skema dan status integritas berkas cadangan secara aman
func getBackupFileInfo(ctx context.Context, filePath string) (int, string) {
	maxVer, err := database.ValidateDatabaseFile(ctx, filePath)
	if err != nil {
		return 0, "corrupt"
	}
	return maxVer, "valid"
}

// ListBackups mengambil daftar seluruh file backup pada folder backups/
func (s *BackupService) ListBackups() ([]*BackupItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*BackupItem{}, nil
		}
		return nil, fmt.Errorf("gagal membaca folder cadangan: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var items []*BackupItem
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lowerName := strings.ToLower(name)
		if !strings.HasSuffix(lowerName, ".db") && !strings.HasSuffix(lowerName, ".sqlite") {
			continue
		}

		filePath, info, err := secureExistingBackupPath(s.backupDir, name)
		if err != nil {
			continue
		}
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
		return nil, fmt.Errorf("gagal membuat direktori cadangan: %w", err)
	}
	backupDir, err := ensureRealDirectory(s.backupDir)
	if err != nil {
		return nil, fmt.Errorf("direktori cadangan tidak aman: %w", err)
	}

	// Buat nama file unik: compliance_backup_YYYYMMDD_HHMMSS_<random8>.db
	randSuffix, err := randomSuffix()
	if err != nil {
		return nil, err
	}

	timestamp := time.Now().UTC().Format("20060102_150405")
	filename := fmt.Sprintf("compliance_backup_%s_%s.db", timestamp, randSuffix)
	targetPath := filepath.Join(backupDir, filename)

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
	if err := s.injectFault("after_backup_vacuum", targetPath); err != nil {
		_ = os.Remove(targetPath)
		return nil, fmt.Errorf("kegagalan setelah pembuatan cadangan: %w", err)
	}

	// Lakukan pemeriksaan integritas pada file backup yang baru dibuat
	maxVer, err := database.ValidateDatabaseFile(ctx, targetPath)
	if err != nil {
		_ = os.Remove(targetPath)
		return nil, fmt.Errorf("verifikasi integritas cadangan gagal: %w", err)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca informasi berkas cadangan: %w", err)
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
		lowerName := strings.ToLower(name)
		if !strings.HasSuffix(lowerName, ".db") && !strings.HasSuffix(lowerName, ".sqlite") {
			continue
		}
		filePath, _, err := secureExistingBackupPath(s.backupDir, name)
		if err != nil {
			continue
		}
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

	targetPath, _, err := secureExistingBackupPath(s.backupDir, cleaned)
	if err != nil {
		return fmt.Errorf("berkas cadangan '%s' tidak ditemukan atau tidak aman: %w", cleaned, err)
	}
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("gagal menyelesaikan jalur tujuan: %w", err)
	}
	absDB, _ := filepath.Abs(s.dbPath)
	if absTarget == absDB {
		return errors.New("penghapusan ditolak: tidak diperbolehkan menghapus berkas database aktif")
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
		targetPath, _, err := secureExistingBackupPath(s.backupDir, cleaned)
		if err != nil {
			return nil, fmt.Errorf("berkas cadangan '%s' tidak ditemukan atau tidak aman: %w", cleaned, err)
		}
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

	remainingEntries, _ := os.ReadDir(s.backupDir)
	for _, entry := range remainingEntries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".sqlite") {
			result.RemainingCount++
		}
	}

	return result, nil
}

func countTableSafe(ctx context.Context, db *sql.DB, tableName string, filterDeleted bool) int {
	var tableExists int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&tableExists)
	if err != nil || tableExists == 0 {
		return 0
	}
	if filterDeleted && tableName == "work_items" {
		var count int
		// Mencoba query dengan filter deleted_at IS NULL
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_items WHERE deleted_at IS NULL").Scan(&count); err == nil {
			return count
		}
		// Jika kolom deleted_at belum ada (skema lama pre-v6), lanjut hitung total
	}
	var count int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cleaned, err := validateBackupFilename(filename)
	if err != nil {
		return nil, err
	}

	sourcePath, info, err := secureExistingBackupPath(s.backupDir, cleaned)
	if err != nil {
		return nil, fmt.Errorf("berkas cadangan '%s' tidak ditemukan atau tidak aman: %w", cleaned, err)
	}

	result := &domain.RestoreInspectionResult{
		Filename:      cleaned,
		SizeBytes:     info.Size(),
		FormattedSize: formatBytes(info.Size()),
		CreatedAt:     info.ModTime().UTC(),
		Warnings:      make([]string, 0),
	}

	schemaVersion, validationErr := database.ValidateDatabaseFile(inspectCtx, sourcePath)
	result.SchemaVersion = schemaVersion
	if validationErr != nil {
		result.IntegrityStatus = "invalid"
		result.Warnings = append(result.Warnings, fmt.Sprintf("Validasi database cadangan gagal: %v", validationErr))
	} else {
		result.IntegrityStatus = "ok"
	}

	checkDB, err := database.OpenReadOnlyDB(sourcePath)
	if err != nil {
		result.IntegrityStatus = fmt.Sprintf("gagal membuka berkas: %v", err)
		result.IsCompatible = false
		result.Warnings = append(result.Warnings, "Berkas cadangan rusak atau tidak dapat dibaca.")
		return result, nil
	}
	defer checkDB.Close()

	// Ringkasan data cadangan
	result.DataSummary = getDatabaseSummary(inspectCtx, checkDB)

	// Ringkasan database aktif
	activeDB := s.workItemService.GetDB()
	if activeDB != nil {
		result.ActiveDatabaseSummary = getDatabaseSummary(inspectCtx, activeDB)
	}

	// Evaluasi kompatibilitas dan perbedaan
	schemaUnsupported := schemaVersion > database.MaxSupportedSchemaVersion
	schemaMigrationNeeded := validationErr == nil && schemaVersion < database.MaxSupportedSchemaVersion && schemaVersion > 0
	recordsLostWarning := (result.ActiveDatabaseSummary.WorkItemsCount > result.DataSummary.WorkItemsCount) ||
		(result.ActiveDatabaseSummary.ActivityLogsCount > result.DataSummary.ActivityLogsCount)

	result.Differences = domain.RestoreDifferences{
		RecordsLostWarning:    recordsLostWarning,
		SchemaMigrationNeeded: schemaMigrationNeeded,
		SchemaUnsupported:     schemaUnsupported,
	}

	result.IsCompatible = validationErr == nil && !schemaUnsupported && schemaVersion > 0

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
	s.operationGate.Lock()
	defer s.operationGate.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Validasi seluruh path sebelum membaca atau mengganti database aktif.
	cleaned, err := validateBackupFilename(filename)
	if err != nil {
		return err
	}
	sourcePath, sourceInfo, err := secureExistingBackupPath(s.backupDir, cleaned)
	if err != nil {
		return fmt.Errorf("berkas cadangan '%s' tidak ditemukan atau tidak aman: %w", cleaned, err)
	}
	activePath, dataDir, err := secureExistingDatabasePath(s.dbPath)
	if err != nil {
		return fmt.Errorf("database aktif tidak aman untuk dipulihkan: %w", err)
	}
	activeInfo, err := os.Stat(activePath)
	if err != nil {
		return fmt.Errorf("gagal membaca metadata database aktif: %w", err)
	}
	if os.SameFile(sourceInfo, activeInfo) {
		return errors.New("pemulihan ditolak: sumber cadangan menunjuk berkas database aktif")
	}

	// 2. Salin sumber ke staging pada filesystem yang sama. Sumber tidak pernah
	// dibuka writable dan database aktif belum disentuh pada tahap ini.
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("gagal membuka berkas cadangan: %w", err)
	}
	defer sourceFile.Close()
	openedInfo, err := sourceFile.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(sourceInfo, openedInfo) {
		return errors.New("berkas cadangan berubah atau bukan berkas biasa saat akan dibaca")
	}
	stagingFile, err := os.CreateTemp(dataDir, ".restore-staging-*.db")
	if err != nil {
		return fmt.Errorf("gagal membuat berkas sementara pemulihan: %w", err)
	}
	stagingPath := stagingFile.Name()
	stagingActive := false
	defer func() {
		_ = stagingFile.Close()
		if !stagingActive {
			_ = os.Remove(stagingPath)
		}
	}()
	if _, err := io.Copy(stagingFile, sourceFile); err != nil {
		return fmt.Errorf("gagal menyalin cadangan ke berkas sementara pemulihan: %w", err)
	}
	if err := stagingFile.Sync(); err != nil {
		return fmt.Errorf("gagal menyinkronkan berkas sementara pemulihan: %w", err)
	}
	if err := stagingFile.Close(); err != nil {
		return fmt.Errorf("gagal menutup berkas sementara pemulihan: %w", err)
	}
	if err := s.injectFault("after_stage_copy", stagingPath); err != nil {
		return fmt.Errorf("kegagalan setelah penyalinan berkas sementara pemulihan: %w", err)
	}

	// 3. Verifikasi staging, migrasikan salinan bila perlu, lalu verifikasi ulang.
	schemaVersion, err := database.ValidateDatabaseFile(ctx, stagingPath)
	if schemaVersion > database.MaxSupportedSchemaVersion {
		return fmt.Errorf("pemulihan ditolak: versi skema cadangan (v%d) lebih baru dari versi yang didukung aplikasi ini (v%d)", schemaVersion, database.MaxSupportedSchemaVersion)
	}
	if err != nil {
		return fmt.Errorf("integritas atau struktur berkas cadangan tidak valid: %w", err)
	}
	stagingDB, err := database.OpenDB(stagingPath)
	if err != nil {
		return fmt.Errorf("gagal membuka berkas sementara pemulihan: %w", err)
	}
	if err := database.Migrate(ctx, stagingDB); err != nil {
		_ = stagingDB.Close()
		return fmt.Errorf("migrasi berkas sementara pemulihan gagal: %w", err)
	}
	if err := database.CheckIntegrity(ctx, stagingDB); err != nil {
		_ = stagingDB.Close()
		return fmt.Errorf("pemeriksaan integritas setelah migrasi gagal: %w", err)
	}
	if err := database.CheckForeignKeys(ctx, stagingDB); err != nil {
		_ = stagingDB.Close()
		return fmt.Errorf("pemeriksaan relasi data setelah migrasi gagal: %w", err)
	}
	if err := stagingDB.Close(); err != nil {
		return fmt.Errorf("gagal menutup berkas sementara pemulihan: %w", err)
	}
	if _, err := database.ValidateDatabaseFile(ctx, stagingPath); err != nil {
		return fmt.Errorf("verifikasi akhir berkas sementara pemulihan gagal: %w", err)
	}

	// 4. Safety snapshot wajib berhasil dan lolos integrity/FK check.
	activeDB := s.workItemService.GetDB()
	if activeDB == nil {
		return errors.New("koneksi database aktif tidak tersedia")
	}
	backupDir, err := ensureRealDirectory(s.backupDir)
	if err != nil {
		return fmt.Errorf("direktori cadangan tidak aman: %w", err)
	}
	randSuffix, err := randomSuffix()
	if err != nil {
		return err
	}
	safetyName := fmt.Sprintf("pre_restore_safety_%s_%s.db", time.Now().UTC().Format("20060102_150405"), randSuffix)
	safetyPath := filepath.Join(backupDir, safetyName)
	if err := s.injectFault("before_safety_vacuum", safetyPath); err != nil {
		return fmt.Errorf("cadangan pengaman sebelum pemulihan gagal: %w", err)
	}
	escapedSafety := strings.ReplaceAll(safetyPath, "'", "''")
	if _, err := activeDB.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", escapedSafety)); err != nil {
		_ = os.Remove(safetyPath)
		return fmt.Errorf("cadangan pengaman sebelum pemulihan gagal: %w", err)
	}
	if err := s.injectFault("after_safety_vacuum", safetyPath); err != nil {
		_ = os.Remove(safetyPath)
		return fmt.Errorf("kegagalan setelah pembuatan cadangan pengaman: %w", err)
	}
	if _, err := database.ValidateDatabaseFile(ctx, safetyPath); err != nil {
		_ = os.Remove(safetyPath)
		return fmt.Errorf("verifikasi cadangan pengaman gagal: %w", err)
	}

	// 5. Tutup pool lama, tukar file melalui rename pada filesystem yang sama,
	// lalu buka dan verifikasi pool baru. File aktif lama tetap tersedia sebagai
	// rollback sampai seluruh verifikasi selesai.
	rollbackSuffix, err := randomSuffix()
	if err != nil {
		return err
	}
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-"+rollbackSuffix+".db")
	if err := activeDB.Close(); err != nil {
		return fmt.Errorf("gagal menutup koneksi database aktif: %w", err)
	}
	if err := s.injectFault("after_active_close", activePath); err != nil {
		reopenErr := s.reopenActiveDatabase(ctx, activePath)
		return errors.Join(fmt.Errorf("kegagalan setelah koneksi aktif ditutup: %w", err), reopenErr)
	}
	if err := os.Rename(activePath, rollbackPath); err != nil {
		reopenErr := s.reopenActiveDatabase(ctx, activePath)
		return errors.Join(fmt.Errorf("gagal memindahkan database aktif ke area pemulihan: %w", err), reopenErr)
	}
	if err := os.Rename(stagingPath, activePath); err != nil {
		rollbackErr := s.rollbackRestore(ctx, activePath, rollbackPath, false)
		return errors.Join(fmt.Errorf("gagal mengaktifkan database hasil pemulihan: %w", err), rollbackErr)
	}
	stagingActive = true

	if err := s.injectFault("after_activate", activePath); err != nil {
		rollbackErr := s.rollbackRestore(ctx, activePath, rollbackPath, true)
		return errors.Join(fmt.Errorf("kegagalan setelah aktivasi database hasil pemulihan: %w", err), rollbackErr)
	}
	newDB, err := database.OpenDB(activePath)
	if err != nil {
		rollbackErr := s.rollbackRestore(ctx, activePath, rollbackPath, true)
		return errors.Join(fmt.Errorf("gagal membuka database hasil pemulihan: %w", err), rollbackErr)
	}
	if _, err := database.ValidateDatabaseFile(ctx, activePath); err != nil {
		_ = newDB.Close()
		rollbackErr := s.rollbackRestore(ctx, activePath, rollbackPath, true)
		return errors.Join(fmt.Errorf("validasi database hasil pemulihan gagal: %w", err), rollbackErr)
	}

	s.updateServiceDBs(newDB)
	if err := os.Remove(rollbackPath); err != nil {
		return fmt.Errorf("pemulihan berhasil, tetapi pembersihan berkas sementara gagal: %w", err)
	}
	return nil
}

func (s *BackupService) reopenActiveDatabase(ctx context.Context, activePath string) error {
	db, err := database.OpenDB(activePath)
	if err != nil {
		return fmt.Errorf("gagal membuka ulang database aktif: %w", err)
	}
	if err := database.CheckIntegrity(ctx, db); err != nil {
		_ = db.Close()
		return fmt.Errorf("pemeriksaan integritas database aktif gagal saat dibuka ulang: %w", err)
	}
	if err := database.CheckForeignKeys(ctx, db); err != nil {
		_ = db.Close()
		return fmt.Errorf("pemeriksaan relasi data database aktif gagal saat dibuka ulang: %w", err)
	}
	s.updateServiceDBs(db)
	return nil
}

func (s *BackupService) rollbackRestore(ctx context.Context, activePath, rollbackPath string, removeActivated bool) error {
	var rollbackErrors []error
	if removeActivated {
		if err := os.Remove(activePath); err != nil && !os.IsNotExist(err) {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("gagal menghapus kandidat pemulihan: %w", err))
		}
	}
	if _, err := os.Stat(rollbackPath); err == nil {
		if err := os.Rename(rollbackPath, activePath); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("gagal mengembalikan berkas database asli: %w", err))
		}
	} else {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("berkas pemulihan database asli tidak tersedia: %w", err))
	}
	if len(rollbackErrors) == 0 {
		if err := s.reopenActiveDatabase(ctx, activePath); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	return errors.Join(rollbackErrors...)
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
