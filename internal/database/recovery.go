package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
		return "", errors.New("direktori data tidak valid atau merupakan symbolic link/reparse point")
	}
	resolved, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return "", fmt.Errorf("gagal memvalidasi direktori data: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func tableHasColumn(ctx context.Context, db *sql.DB, tableName, colName string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s);", tableName))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(name, colName) {
			return true, nil
		}
	}
	return false, nil
}

func validateDatabaseStructure(ctx context.Context, db *sql.DB, schemaVersion int) error {
	// 1. Verifikasi tabel inti yang wajib ada pada seluruh versi yang didukung (v1..v6)
	requiredTables := []string{"work_items", "activity_logs"}
	if schemaVersion >= 2 {
		requiredTables = append(requiredTables, "categories", "subtasks", "work_templates", "recurring_schedules", "import_batches", "import_items")
	}

	for _, tbl := range requiredTables {
		var exists int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&exists); err != nil || exists == 0 {
			return fmt.Errorf("tabel wajib '%s' tidak ditemukan untuk versi skema v%d", tbl, schemaVersion)
		}
	}

	// 2. Verifikasi kolom wajib pada tabel inti (skema v1+)
	type colCheck struct {
		table string
		col   string
	}

	v1Cols := []colCheck{
		{"work_items", "title"},
		{"work_items", "work_code"},
		{"work_items", "status"},
		{"work_items", "received_on"},
		{"activity_logs", "work_item_id"},
		{"activity_logs", "entry_type"},
		{"activity_logs", "description"},
		{"activity_logs", "occurred_at"},
	}
	for _, c := range v1Cols {
		hasCol, err := tableHasColumn(ctx, db, c.table, c.col)
		if err != nil || !hasCol {
			return fmt.Errorf("kolom wajib '%s' tidak ditemukan pada tabel '%s'", c.col, c.table)
		}
	}

	// 3. Verifikasi kolom wajib yang diperkenalkan pada skema v2+
	if schemaVersion >= 2 {
		v2Cols := []colCheck{
			{"work_items", "progress_percentage"},
			{"categories", "name"},
			{"subtasks", "work_item_id"},
			{"subtasks", "title"},
			{"subtasks", "is_completed"},
			{"work_templates", "name"},
			{"work_templates", "title_pattern"},
			{"recurring_schedules", "title"},
			{"recurring_schedules", "frequency"},
			{"recurring_schedules", "next_run_date"},
			{"recurring_schedules", "is_active"},
			{"import_batches", "batch_code"},
			{"import_batches", "status"},
			{"import_items", "batch_id"},
			{"import_items", "status"},
		}
		for _, c := range v2Cols {
			hasCol, err := tableHasColumn(ctx, db, c.table, c.col)
			if err != nil || !hasCol {
				return fmt.Errorf("kolom wajib '%s' tidak ditemukan pada tabel '%s' untuk skema v2+", c.col, c.table)
			}
		}
	}

	// 4. Verifikasi kolom evolusioner berdasarkan versi skema (v4..v6)
	if schemaVersion >= 4 {
		hasCol, err := tableHasColumn(ctx, db, "categories", "is_active")
		if err != nil || !hasCol {
			return errors.New("kolom wajib 'is_active' tidak ditemukan pada tabel categories untuk skema v4+")
		}
	}
	if schemaVersion >= 5 {
		hasCol, err := tableHasColumn(ctx, db, "work_items", "provenance")
		if err != nil || !hasCol {
			return errors.New("kolom wajib 'provenance' tidak ditemukan pada tabel work_items untuk skema v5+")
		}
	}
	if schemaVersion >= 6 {
		hasCol, err := tableHasColumn(ctx, db, "work_items", "deleted_at")
		if err != nil || !hasCol {
			return errors.New("kolom wajib 'deleted_at' tidak ditemukan pada tabel work_items untuk skema v6+")
		}
	}

	// 5. Verifikasi konsistensi riwayat migrasi (tidak ada gap versi 1..schemaVersion)
	for v := 1; v <= schemaVersion; v++ {
		var hasVersion int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", v).Scan(&hasVersion)
		if err != nil || hasVersion == 0 {
			return fmt.Errorf("riwayat migrasi tidak konsisten: versi migrasi %d tidak tercatat", v)
		}
	}

	var countVersions int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&countVersions); err != nil || countVersions != schemaVersion {
		return fmt.Errorf("riwayat migrasi tidak konsisten: jumlah tercatat (%d) tidak sesuai versi skema maksimum (v%d)", countVersions, schemaVersion)
	}

	return nil
}

// ValidateDatabaseFile memverifikasi integritas SQLite, foreign key, versi dan
// struktur skema aplikasi tanpa mengubah berkas sumber.
func ValidateDatabaseFile(ctx context.Context, filePath string) (int, error) {
	checkDB, err := OpenReadOnlyDB(filePath)
	if err != nil {
		return 0, fmt.Errorf("file tidak dapat dibuka sebagai database SQLite read-only: %w", err)
	}
	defer checkDB.Close()

	if err := CheckIntegrity(ctx, checkDB); err != nil {
		return 0, fmt.Errorf("gagal uji integritas SQLite: %w", err)
	}
	if err := CheckForeignKeys(ctx, checkDB); err != nil {
		return 0, fmt.Errorf("gagal uji foreign key SQLite: %w", err)
	}

	var hasMigrations int
	if err := checkDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&hasMigrations); err != nil || hasMigrations == 0 {
		return 0, errors.New("tabel schema_migrations tidak ditemukan")
	}

	var maxVer int
	if err := checkDB.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&maxVer); err != nil || maxVer <= 0 {
		return 0, errors.New("versi skema database tidak valid")
	}
	if maxVer > MaxSupportedSchemaVersion {
		return maxVer, fmt.Errorf("versi skema database (v%d) melebihi batas maksimum yang didukung aplikasi ini (v%d)", maxVer, MaxSupportedSchemaVersion)
	}

	if err := validateDatabaseStructure(ctx, checkDB, maxVer); err != nil {
		return maxVer, fmt.Errorf("struktur skema database (v%d) tidak valid: %w", maxVer, err)
	}

	return maxVer, nil
}

// RecoverInterruptedRestore memeriksa dan memulihkan basis data apabila proses restore sebelumnya
// terinterupsi (crash) di antara pemindahan database aktif ke rollback dan aktivasi staging.
//
// Aturan:
//  1. Validasi keamanan direktori data induk dilakukan secara inkondisional (QA-111-02).
//  2. Jika database aktif tersedia tetapi berukuran 0-byte, korup, atau skemanya tidak valid,
//     startup harus gagal secara aman (fail-closed) untuk mencegah inisialisasi ulang otomatis,
//     tanpa menimpa active database dengan rollback atau menghapus file cadangan (QA-111-01).
//  3. Jika database aktif tersedia dan valid (skema v1..v6), startup melanjutkan normal tanpa menimpa data.
//  4. Artefak pemulihan diidentifikasi terlebih dahulu; direktori, symlink, atau reparse point bernama artefak
//     recovery langsung menghasilkan error dan tidak pernah dianggap instalasi pertama yang bersih.
//  5. Jika database aktif tidak ada tetapi ditemukan tepat satu rollback valid, pulihkan database asli
//     setelah validasi keamanan path, uji integritas read-only, dan validasi struktur skema aplikasi (QA-111-03).
//  6. Jika rollback korup, ambigu (lebih dari satu), atau tidak aman, gagalkan startup agar SQLite
//     tidak membuat database kosong baru yang menimpa/menelantarkan data pengguna.
//  7. Berkas staging dan safety backup tidak dihapus secara otomatis demi pelestarian bukti/data.
func RecoverInterruptedRestore(dbPath string) error {
	absDB, err := filepath.Abs(dbPath)
	if err != nil {
		return fmt.Errorf("gagal menyelesaikan path database: %w", err)
	}

	dataDir := filepath.Dir(absDB)

	// QA-111-02: Pastikan direktori data induk divalidasi terlebih dahulu.
	realDataDir, err := ensureRealDirectory(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Direktori belum ada (instalasi pertama kali yang bersih).
			return nil
		}
		return fmt.Errorf("direktori data tidak aman: %w", err)
	}

	// Periksa database aktif dengan path yang telah dinormalkan terhadap realDataDir
	expectedActive := filepath.Join(realDataDir, filepath.Base(absDB))
	info, err := os.Lstat(expectedActive)
	if err == nil {
		// Database aktif sudah ada di disk.
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("database aktif bukan file reguler atau merupakan symbolic link/reparse point")
		}
		resolvedDB, err := filepath.EvalSymlinks(expectedActive)
		if err != nil {
			return fmt.Errorf("gagal mengevaluasi path database aktif: %w", err)
		}
		resolvedDB, err = filepath.Abs(resolvedDB)
		if err != nil {
			return err
		}
		if !strings.EqualFold(filepath.Clean(expectedActive), filepath.Clean(resolvedDB)) {
			return errors.New("database aktif melalui symbolic link/reparse point tidak diizinkan")
		}

		// QA-111-01 (Active Database Fail-Open Guard):
		// File reguler saja tidak cukup. Validasi bahwa active database tidak kosong, tidak korup,
		// dan memiliki struktur skema aplikasi yang valid.
		if info.Size() == 0 {
			return errors.New("database aktif berukuran 0 byte (kosong/korup); startup dihentikan untuk mencegah inisialisasi ulang yang menimpa data, silakan lakukan pemulihan manual")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if _, err := ValidateDatabaseFile(ctx, expectedActive); err != nil {
			return fmt.Errorf("database aktif korup atau memiliki struktur skema tidak valid: %w; startup dihentikan demi keselamatan data, silakan lakukan pemulihan manual", err)
		}

		// Periksa apakah ada artefak recovery di realDataDir yang tidak aman (direktori/symlink/junction)
		entries, err := os.ReadDir(realDataDir)
		if err != nil {
			return fmt.Errorf("gagal membaca direktori data: %w", err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if (strings.HasPrefix(name, ".restore-rollback-") || strings.HasPrefix(name, ".restore-staging-")) && strings.HasSuffix(name, ".db") {
				targetPath := filepath.Join(realDataDir, name)
				artInfo, statErr := os.Lstat(targetPath)
				if statErr != nil {
					return fmt.Errorf("gagal membaca metadata artefak pemulihan '%s': %w", name, statErr)
				}
				if artInfo.IsDir() {
					return fmt.Errorf("artefak pemulihan '%s' tidak aman karena berupa direktori; startup dihentikan demi keselamatan data", name)
				}
				if artInfo.Mode()&os.ModeSymlink != 0 || !artInfo.Mode().IsRegular() {
					return fmt.Errorf("artefak pemulihan '%s' bukan berkas reguler atau merupakan symbolic link/reparse point; startup dihentikan demi keselamatan data", name)
				}
			}
		}

		// Aturan: Jika database aktif tersedia dan valid, jangan otomatis menggantinya dengan rollback.
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("gagal memeriksa keberadaan database aktif: %w", err)
	}

	// Database aktif tidak ditemukan di disk.
	// Pindai realDataDir untuk artefak pemulihan (rollback dan staging).
	entries, err := os.ReadDir(realDataDir)
	if err != nil {
		return fmt.Errorf("gagal membaca direktori data: %w", err)
	}

	var rollbackFiles []string
	var stagingFiles []string

	for _, entry := range entries {
		name := entry.Name()
		isRollback := strings.HasPrefix(name, ".restore-rollback-") && strings.HasSuffix(name, ".db")
		isStaging := strings.HasPrefix(name, ".restore-staging-") && strings.HasSuffix(name, ".db")

		if !isRollback && !isStaging {
			continue
		}

		// QA-111-01: Identifikasi nama artefak recovery terlebih dahulu.
		// Jangan abaikan direktori sebelum identifikasi nama artefak.
		targetPath := filepath.Join(realDataDir, name)
		artInfo, statErr := os.Lstat(targetPath)
		if statErr != nil {
			return fmt.Errorf("gagal membaca metadata artefak pemulihan '%s': %w", name, statErr)
		}

		// Artefak recovery non-regular, direktori, symlink, atau reparse point wajib menghasilkan error!
		if artInfo.IsDir() {
			return fmt.Errorf("artefak pemulihan '%s' tidak aman karena berupa direktori; startup dihentikan demi keselamatan data", name)
		}
		if artInfo.Mode()&os.ModeSymlink != 0 || !artInfo.Mode().IsRegular() {
			return fmt.Errorf("artefak pemulihan '%s' bukan berkas reguler atau merupakan symbolic link/reparse point; startup dihentikan demi keselamatan data", name)
		}

		resolvedArt, evalErr := filepath.EvalSymlinks(targetPath)
		if evalErr != nil {
			return fmt.Errorf("gagal mengevaluasi path artefak pemulihan '%s': %w", name, evalErr)
		}
		resolvedArt, evalErr = filepath.Abs(resolvedArt)
		if evalErr != nil {
			return evalErr
		}
		rel, relErr := filepath.Rel(realDataDir, resolvedArt)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			return fmt.Errorf("artefak pemulihan '%s' berada di luar direktori data yang sah", name)
		}
		if !strings.EqualFold(filepath.Clean(targetPath), filepath.Clean(resolvedArt)) {
			return fmt.Errorf("artefak pemulihan '%s' merupakan symbolic link/reparse point", name)
		}

		if isRollback {
			rollbackFiles = append(rollbackFiles, name)
		} else if isStaging {
			stagingFiles = append(stagingFiles, name)
		}
	}

	// Kasus 1: Tidak ada file rollback
	if len(rollbackFiles) == 0 {
		if len(stagingFiles) > 0 {
			// Ada staging sisa tanpa database aktif dan tanpa rollback: kondisi tidak aman/terinterupsi
			return fmt.Errorf("terdeteksi %d file staging sisa pemulihan terinterupsi tanpa file database aktif maupun rollback; startup dihentikan demi keselamatan data", len(stagingFiles))
		}
		// Instalasi pertama yang bersih: tidak ada database aktif, tidak ada rollback, tidak ada staging
		return nil
	}

	// Kasus 2: Ditemukan lebih dari satu file rollback (ambigu)
	if len(rollbackFiles) > 1 {
		return fmt.Errorf("terdeteksi %d file rollback pemulihan terinterupsi di %s; kondisi ambigu demi keselamatan data startup dihentikan", len(rollbackFiles), realDataDir)
	}

	// Kasus 3: Ditemukan tepat satu file rollback
	rollbackName := rollbackFiles[0]
	rollbackPath := filepath.Join(realDataDir, rollbackName)

	rbInfo, err := os.Lstat(rollbackPath)
	if err != nil {
		return fmt.Errorf("gagal membaca metadata file rollback: %w", err)
	}
	if rbInfo.Size() == 0 {
		return fmt.Errorf("file rollback '%s' berukuran 0 byte (kosong/korup); startup dihentikan", rollbackName)
	}

	// B. Pemeriksaan Integritas & Skema (Read-Only)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := ValidateDatabaseFile(ctx, rollbackPath); err != nil {
		return fmt.Errorf("file rollback '%s' tidak valid: %w", rollbackName, err)
	}

	// C. Pulihkan File Rollback ke Database Aktif
	if err := os.Rename(rollbackPath, expectedActive); err != nil {
		return fmt.Errorf("gagal memulihkan file rollback ke database aktif: %w", err)
	}

	log.Printf("[PERINGATAN] Terdeteksi operasi Restore sebelumnya terinterupsi. Basis data asli berhasil dipulihkan dari rollback (%s) ke %s", rollbackName, filepath.Base(expectedActive))
	return nil
}
