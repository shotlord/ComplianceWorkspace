package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

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

// Migrate menjalankan migrasi berbasis versi terstruktur
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

	// Baca seluruh file migrasi dari embed.FS
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("gagal membaca folder migrasi: %w", err)
	}

	type migrationFile struct {
		version  int
		fileName string
	}
	var migrations []migrationFile

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
		migrations = append(migrations, migrationFile{version: v, fileName: entry.Name()})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	for _, m := range migrations {
		var exists bool
		err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", m.version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("gagal mengecek status migrasi versi %d: %w", m.version, err)
		}
		if exists {
			continue
		}

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

	return nil
}
