package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// MaxSupportedSchemaVersion adalah versi skema database maksimum yang didukung oleh aplikasi ini (v1.1.0)
const MaxSupportedSchemaVersion = 5

//go:embed migrations/*.sql
var migrationFS embed.FS

// OpenDB membuka koneksi SQLite pada path yang ditentukan dan mengatur PRAGMA sesuai AGENTS.md.
func OpenDB(dbPath string) (*sql.DB, error) {
	// DSN dengan busy_timeout dan foreign_keys
	dsn := fmt.Sprintf("%s?_pragma=foreign_keys(ON)&_pragma=journal_mode(DELETE)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("gagal membuka database SQLite: %w", err)
	}

	// Membatasi koneksi untuk keamanan penulisan file tunggal SQLite
	db.SetMaxOpenConns(1)

	// Pastikan koneksi hidup dan terapkan PRAGMA secara eksplisit
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("gagal menghubungkan ke database SQLite: %w", err)
	}

	pragmas := []string{
		"PRAGMA foreign_keys = ON;",
		"PRAGMA journal_mode = DELETE;",
		"PRAGMA busy_timeout = 5000;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("gagal menetapkan %s: %w", p, err)
		}
	}

	return db, nil
}

// CheckIntegrity menjalankan PRAGMA integrity_check pada database
func CheckIntegrity(ctx context.Context, db *sql.DB) error {
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check;").Scan(&result); err != nil {
		return fmt.Errorf("gagal mengeksekusi pemeriksaan integritas SQLite: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("integritas database tidak valid: %s", result)
	}
	return nil
}

// CheckForeignKeys menjalankan PRAGMA foreign_key_check pada database
func CheckForeignKeys(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check;")
	if err != nil {
		return fmt.Errorf("gagal mengeksekusi foreign_key_check SQLite: %w", err)
	}
	defer rows.Close()

	var violations []string
	for rows.Next() {
		var table, parent string
		var rowid, fkid int64
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		violations = append(violations, fmt.Sprintf("tabel %s baris %d referensi ke %s melanggar foreign key #%d", table, rowid, parent, fkid))
	}
	if len(violations) > 0 {
		return fmt.Errorf("pelanggaran integritas foreign key: %s", strings.Join(violations, "; "))
	}
	return nil
}

// GetDatabaseFilePath mengambil path absolut file database aktif dari PRAGMA database_list
func GetDatabaseFilePath(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA database_list;")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return "", err
		}
		if name == "main" {
			return file, nil
		}
	}
	return "", nil
}

// createPreMigrationSnapshot membuat backup snapshot SQLite sebelum migrasi dijalankan
func createPreMigrationSnapshot(ctx context.Context, db *sql.DB, dbFilePath string, fromVersion int) (string, error) {
	cleanPath := filepath.Clean(dbFilePath)
	parentDir := filepath.Dir(cleanPath)

	// Tentukan lokasi backup (folder backups/ jika ada atau sejajar dengan data)
	backupDir := filepath.Join(filepath.Dir(parentDir), "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		backupDir = parentDir
	}

	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	randSuffix := hex.EncodeToString(randBytes)

	timestamp := time.Now().UTC().Format("20060102_150405")
	backupFileName := fmt.Sprintf("pre_migration_v%d_%s_%s.db", fromVersion, timestamp, randSuffix)
	targetBackupPath := filepath.Join(backupDir, backupFileName)

	escapedPath := strings.ReplaceAll(targetBackupPath, "'", "''")
	vacuumQuery := fmt.Sprintf("VACUUM INTO '%s'", escapedPath)

	if _, err := db.ExecContext(ctx, vacuumQuery); err != nil {
		_ = os.Remove(targetBackupPath)
		return "", fmt.Errorf("eksekusi VACUUM INTO sebelum migrasi gagal: %w", err)
	}

	// Verifikasi integritas file backup yang baru dibuat
	checkDB, err := OpenDB(targetBackupPath)
	if err != nil {
		_ = os.Remove(targetBackupPath)
		return "", fmt.Errorf("gagal membuka snapshot migrasi untuk verifikasi: %w", err)
	}
	defer checkDB.Close()

	if err := CheckIntegrity(ctx, checkDB); err != nil {
		_ = os.Remove(targetBackupPath)
		return "", fmt.Errorf("file snapshot migrasi tidak lolos verifikasi integritas: %w", err)
	}

	return targetBackupPath, nil
}

// Migrate menjalankan migrasi berbasis versi terstruktur dengan verifikasi integritas dan backup pra-migrasi
func Migrate(ctx context.Context, db *sql.DB) error {
	// Buat tabel schema_migrations jika belum ada
	createMigrationTable := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);`
	if _, err := db.ExecContext(ctx, createMigrationTable); err != nil {
		return fmt.Errorf("gagal membuat tabel schema_migrations: %w", err)
	}

	// Periksa versi skema maksimum saat ini di database
	var currentMaxVersion sql.NullInt64
	err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&currentMaxVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("gagal memeriksa versi skema database saat ini: %w", err)
	}

	// Guard: Jika versi database lebih tinggi dari yang didukung aplikasi, tolak demi keselamatan data
	if currentMaxVersion.Valid && currentMaxVersion.Int64 > MaxSupportedSchemaVersion {
		return fmt.Errorf("ketidakcocokan versi skema: versi basis data (%d) lebih baru daripada versi maksimum yang didukung aplikasi ini (%d). Mohon perbarui aplikasi Anda", currentMaxVersion.Int64, MaxSupportedSchemaVersion)
	}

	// Baca seluruh file migrasi dari embed.FS
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("gagal membaca folder migrasi: %w", err)
	}

	type migrationFile struct {
		version  int
		fileName string
	}
	var allMigrations []migrationFile

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		allMigrations = append(allMigrations, migrationFile{version: v, fileName: entry.Name()})
	}

	sort.Slice(allMigrations, func(i, j int) bool {
		return allMigrations[i].version < allMigrations[j].version
	})

	var pendingMigrations []migrationFile
	for _, m := range allMigrations {
		var exists bool
		err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", m.version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("gagal mengecek status migrasi versi %d: %w", m.version, err)
		}
		if !exists {
			pendingMigrations = append(pendingMigrations, m)
		}
	}

	if len(pendingMigrations) == 0 {
		return nil
	}

	// Jika ini adalah upgrade database existing (versi > 0) dan ada migrasi tertunda:
	if currentMaxVersion.Valid && currentMaxVersion.Int64 > 0 {
		// 1. Verifikasi integritas dan foreign key awal sebelum migrasi
		if err := CheckIntegrity(ctx, db); err != nil {
			return fmt.Errorf("verifikasi integritas database sebelum migrasi gagal: %w", err)
		}
		if err := CheckForeignKeys(ctx, db); err != nil {
			return fmt.Errorf("verifikasi foreign key database sebelum migrasi gagal: %w", err)
		}

		// 2. Buat snapshot cadangan otomatis (pre-migration backup) jika ada path file fisik
		dbFile, err := GetDatabaseFilePath(ctx, db)
		if err == nil && dbFile != "" {
			if _, err := createPreMigrationSnapshot(ctx, db, dbFile, int(currentMaxVersion.Int64)); err != nil {
				return fmt.Errorf("gagal membuat snapshot pra-migrasi: %w", err)
			}
		}
	}

	// Jalankan setiap migrasi tertunda dalam transaksi terisolasi
	for _, m := range pendingMigrations {
		content, err := migrationFS.ReadFile("migrations/" + m.fileName)
		if err != nil {
			return fmt.Errorf("gagal membaca file migrasi %s: %w", m.fileName, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("gagal memulai transaksi migrasi %s: %w", m.fileName, err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("gagal mengeksekusi migrasi %s: %w", m.fileName, err)
		}

		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", m.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("gagal mencatat versi migrasi %d: %w", m.version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("gagal commit migrasi %s: %w", m.fileName, err)
		}
	}

	// Verifikasi integritas dan foreign key akhir pasca-migrasi
	if err := CheckIntegrity(ctx, db); err != nil {
		return fmt.Errorf("verifikasi integritas database setelah migrasi gagal: %w", err)
	}
	if err := CheckForeignKeys(ctx, db); err != nil {
		return fmt.Errorf("verifikasi foreign key database setelah migrasi gagal: %w", err)
	}

	return nil
}
