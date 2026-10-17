package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"compliance-workspace/internal/database"
	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
	"compliance-workspace/internal/service"
)

// TestFreshDatabaseMigration menguji inisialisasi database baru langsung ke skema v1.1.0
func TestFreshDatabaseMigration(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "fresh_v1_1.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrasi database baru gagal: %v", err)
	}

	// 1. Verifikasi tabel schema_migrations mencatat versi 1, 2, 3, dan 4
	var v1Exists, v2Exists, v3Exists, v4Exists bool
	_ = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = 1);").Scan(&v1Exists)
	_ = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = 2);").Scan(&v2Exists)
	_ = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = 3);").Scan(&v3Exists)
	_ = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = 4);").Scan(&v4Exists)
	if !v1Exists || !v2Exists || !v3Exists || !v4Exists {
		t.Fatalf("Versi migrasi tidak lengkap: v1=%v, v2=%v, v3=%v, v4=%v", v1Exists, v2Exists, v3Exists, v4Exists)
	}

	// 1b. Verifikasi kolom is_active pada categories
	var isActCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pragma_table_info('categories')
		WHERE name = 'is_active';
	`).Scan(&isActCount)
	if err != nil || isActCount != 1 {
		t.Fatalf("Kolom is_active tidak ditemukan pada categories: err=%v, count=%d", err, isActCount)
	}

	// 2. Verifikasi kolom progress_percentage pada work_items
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pragma_table_info('work_items')
		WHERE name = 'progress_percentage';
	`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("Kolom progress_percentage tidak ditemukan pada work_items: err=%v, count=%d", err, count)
	}

	// 3. Verifikasi kategori bawaan di-seed dengan benar
	var catCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM categories;").Scan(&catCount)
	if err != nil || catCount < 5 {
		t.Fatalf("Tabel categories tidak memiliki seed yang sesuai, jumlah: %d, err=%v", catCount, err)
	}

	// 4. Verifikasi keberadaan seluruh tabel baru v1.1.0
	expectedTables := []string{
		"categories",
		"subtasks",
		"work_templates",
		"recurring_schedules",
		"import_batches",
		"import_items",
	}

	for _, tbl := range expectedTables {
		var exists bool
		query := "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?);"
		if err := db.QueryRowContext(ctx, query, tbl).Scan(&exists); err != nil || !exists {
			t.Errorf("Tabel baru v1.1.0 '%s' tidak ditemukan di database: %v", tbl, err)
		}
	}

	// 5. Verifikasi integritas database dan foreign key
	if err := database.CheckIntegrity(ctx, db); err != nil {
		t.Fatalf("Integritas database baru tidak valid: %v", err)
	}
	if err := database.CheckForeignKeys(ctx, db); err != nil {
		t.Fatalf("Foreign key database baru tidak valid: %v", err)
	}
}

// TestIncrementalMigrationFromV1 menguji migrasi bertahap dari database v1.0.0 ke v1.1.0
func TestIncrementalMigrationFromV1(t *testing.T) {
	tempDir := t.TempDir()
	appDataDir := filepath.Join(tempDir, "data")
	backupDir := filepath.Join(tempDir, "backups")
	_ = os.MkdirAll(appDataDir, 0755)
	_ = os.MkdirAll(backupDir, 0755)

	dbPath := filepath.Join(appDataDir, "compliance.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Simulasikan database v1.0.0: buat schema_migrations versi 1 dan terapkan 001_initial_schema.sql
	v1DDL := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	INSERT INTO schema_migrations (version) VALUES (1);

	CREATE TABLE IF NOT EXISTS work_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_code TEXT UNIQUE,
		title TEXT NOT NULL CHECK (length(trim(title)) > 0),
		description TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT 'General',
		requested_by TEXT,
		received_on TEXT NOT NULL,
		due_on TEXT,
		priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high','critical')),
		status TEXT NOT NULL DEFAULT 'not_started' CHECK (status IN ('not_started','in_progress','waiting','completed','cancelled')),
		completed_at TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		archived_at TEXT
	);

	CREATE TABLE IF NOT EXISTS activity_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_item_id INTEGER NOT NULL,
		entry_type TEXT NOT NULL CHECK (entry_type IN ('created','activity','edited','status_changed','archived','restored')),
		description TEXT NOT NULL CHECK (length(trim(description)) > 0),
		field_name TEXT,
		old_value TEXT,
		new_value TEXT,
		occurred_at TEXT NOT NULL,
		recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		FOREIGN KEY (work_item_id) REFERENCES work_items(id) ON DELETE RESTRICT
	);

	CREATE TRIGGER IF NOT EXISTS activity_logs_no_update
	BEFORE UPDATE ON activity_logs
	BEGIN
		SELECT RAISE(ABORT, 'Activity logs are append-only');
	END;

	CREATE TRIGGER IF NOT EXISTS activity_logs_no_delete
	BEFORE DELETE ON activity_logs
	BEGIN
		SELECT RAISE(ABORT, 'Activity logs are append-only');
	END;
	`
	if _, err := db.ExecContext(ctx, v1DDL); err != nil {
		t.Fatalf("Gagal menyiapkan DDL v1.0.0: %v", err)
	}

	// 2. Masukkan data dummy v1.0.0:
	// - Item 1: kategori 'General'
	// - Item 2: kategori kustom 'Investigasi Anti-Fraud'
	insertWorkQuery := `
	INSERT INTO work_items (work_code, title, description, category, requested_by, received_on, due_on, priority, status)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	res1, err := db.ExecContext(ctx, insertWorkQuery, "CMP-2026-000001", "Pekerjaan Baseline 1", "Deskripsi 1", "General", "Direksi", "2026-10-10", "2026-10-20", "high", "not_started")
	if err != nil {
		t.Fatalf("Gagal insert item 1: %v", err)
	}
	id1, _ := res1.LastInsertId()

	res2, err := db.ExecContext(ctx, insertWorkQuery, "CMP-2026-000002", "Pekerjaan Baseline 2", "Deskripsi 2", "Investigasi Anti-Fraud", "Komite Audit", "2026-10-12", nil, "critical", "in_progress")
	if err != nil {
		t.Fatalf("Gagal insert item 2: %v", err)
	}
	id2, _ := res2.LastInsertId()

	// Masukkan log aktivitas
	insertLogQuery := `
	INSERT INTO activity_logs (work_item_id, entry_type, description, occurred_at)
	VALUES (?, 'created', 'Pekerjaan dibuat', '2026-10-10T08:00:00Z');
	`
	if _, err := db.ExecContext(ctx, insertLogQuery, id1); err != nil {
		t.Fatalf("Gagal insert log 1: %v", err)
	}
	if _, err := db.ExecContext(ctx, insertLogQuery, id2); err != nil {
		t.Fatalf("Gagal insert log 2: %v", err)
	}

	// 3. Jalankan database.Migrate (ini harus mendeteksi v1.0.0 -> membuat backup snapshot -> menerapkan v1.1.0)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("Gagal menjalankan migrasi incremental v1.1.0: %v", err)
	}

	// 4. Verifikasi bahwa file snapshot cadangan pra-migrasi berhasil dibuat
	backupEntries, err := os.ReadDir(backupDir)
	if err != nil || len(backupEntries) == 0 {
		t.Fatalf("Snapshot pra-migrasi tidak ditemukan di direktori backup: entries=%v, err=%v", backupEntries, err)
	}

	var foundPreMigrationBackup bool
	for _, entry := range backupEntries {
		if strings.HasPrefix(entry.Name(), "pre_migration_v1_") && strings.HasSuffix(entry.Name(), ".db") {
			foundPreMigrationBackup = true
			// Verifikasi isi file snapshot cadangan
			snapPath := filepath.Join(backupDir, entry.Name())
			snapDB, err := database.OpenDB(snapPath)
			if err != nil {
				t.Fatalf("Snapshot backup rusak, gagal dibuka: %v", err)
			}
			defer snapDB.Close()
			if err := database.CheckIntegrity(ctx, snapDB); err != nil {
				t.Fatalf("Integritas snapshot backup pra-migrasi tidak valid: %v", err)
			}
			var snapCount int
			_ = snapDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_items;").Scan(&snapCount)
			if snapCount != 2 {
				t.Errorf("Jumlah work_items di snapshot backup pra-migrasi tidak cocok: didapat %d, diharapkan 2", snapCount)
			}
		}
	}
	if !foundPreMigrationBackup {
		t.Errorf("Snapshot pra-migrasi dengan prefiks 'pre_migration_v1_' tidak ditemukan")
	}

	// 5. Verifikasi bahwa data v1.0.0 tetap utuh di database aktif yang telah dimigrasi
	var activeCount int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_items;").Scan(&activeCount)
	if activeCount != 2 {
		t.Fatalf("Jumlah data work_items aktif berubah setelah migrasi: didapat %d, diharapkan 2", activeCount)
	}

	var p1, p2 int
	err1 := db.QueryRowContext(ctx, "SELECT progress_percentage FROM work_items WHERE id = ?;", id1).Scan(&p1)
	err2 := db.QueryRowContext(ctx, "SELECT progress_percentage FROM work_items WHERE id = ?;", id2).Scan(&p2)
	if err1 != nil || err2 != nil || p1 != 0 || p2 != 0 {
		t.Errorf("Nilai default progress_percentage tidak sesuai: p1=%d (err=%v), p2=%d (err=%v)", p1, err1, p2, err2)
	}

	// 6. Verifikasi kategori kustom v1.0.0 ('Investigasi Anti-Fraud') otomatis termigrasi ke tabel categories
	var catMigrated bool
	err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM categories WHERE name = 'Investigasi Anti-Fraud');").Scan(&catMigrated)
	if err != nil || !catMigrated {
		t.Errorf("Kategori kustom 'Investigasi Anti-Fraud' tidak termigrasi ke tabel categories: err=%v", err)
	}

	// 7. Verifikasi relasi foreign key subtask cascade
	_, err = db.ExecContext(ctx, "INSERT INTO subtasks (work_item_id, title) VALUES (?, 'Subtask Uji 1');", id1)
	if err != nil {
		t.Fatalf("Gagal menambahkan subtask pada work item v1: %v", err)
	}

	var subCount int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM subtasks WHERE work_item_id = ?;", id1).Scan(&subCount)
	if subCount != 1 {
		t.Errorf("Subtask tidak tercatat dengan benar: count=%d", subCount)
	}

	// 8. Verifikasi integritas akhir database aktif dan foreign key
	if err := database.CheckIntegrity(ctx, db); err != nil {
		t.Fatalf("Integritas database setelah migrasi incremental tidak valid: %v", err)
	}
	if err := database.CheckForeignKeys(ctx, db); err != nil {
		t.Fatalf("Foreign key database setelah migrasi incremental tidak valid: %v", err)
	}

	// 9. Verifikasi bahwa relasi ON DELETE RESTRICT pada activity_logs melindungi riwayat audit
	// Percobaan menghapus work_item yang memiliki activity_log harus gagal dan ditolak oleh SQLite
	_, err = db.ExecContext(ctx, "DELETE FROM work_items WHERE id = ?;", id1)
	if err == nil {
		t.Errorf("Penghapusan work_item yang memiliki activity_log harus DITOLAK demi integritas audit!")
	}
}

// TestSchemaVersionGuardRejection menguji penolakan jika database berasal dari versi masa depan (di atas MaxSupportedSchemaVersion)
func TestSchemaVersionGuardRejection(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "future_schema.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Masukkan versi di luar batas yang didukung (misal versi 999)
	initDDL := `
	CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	INSERT INTO schema_migrations (version) VALUES (999);
	`
	if _, err := db.ExecContext(ctx, initDDL); err != nil {
		t.Fatalf("Gagal inisialisasi versi future: %v", err)
	}

	// database.Migrate harus menolak dengan galat ketidakcocokan versi
	err = database.Migrate(ctx, db)
	if err == nil {
		t.Fatalf("database.Migrate seharusnya menolak database dengan versi di atas batas maksimum")
	}

	if !strings.Contains(err.Error(), "ketidakcocokan versi skema") {
		t.Errorf("Pesan galat tidak mengandung 'ketidakcocokan versi skema', didapat: %v", err)
	}
}

// TestV1RepositoryCompatibility menguji kompatibilitas service dan repository v1.0.0 pada database yang telah dimigrasi ke v1.1.0
func TestV1RepositoryCompatibility(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "compat_v1.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrasi gagal: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(db)
	actRepo := repository.NewActivityLogRepository(db)
	svc := service.NewWorkItemService(db, workRepo, actRepo)

	// Uji pembuatan pekerjaan menggunakan service v1.0.0
	in := domain.CreateWorkItemInput{
		Title:       "Uji Kompatibilitas Operasi v1",
		Description: "Memastikan query repositori v1 tetap berjalan mulus",
		Category:    "General",
		RequestedBy: "Unit Penguji",
		ReceivedOn:  "2026-10-17",
		Priority:    domain.PriorityNormal,
	}

	created, err := svc.CreateWorkItem(ctx, in)
	if err != nil {
		t.Fatalf("CreateWorkItem v1 gagal pada skema v1.1.0: %v", err)
	}

	if created.WorkCode == "" {
		t.Errorf("Kode pekerjaan kosong")
	}

	// Uji pengambilan pekerjaan
	fetched, err := svc.GetWorkItem(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetWorkItem gagal: %v", err)
	}
	if fetched.Title != in.Title {
		t.Errorf("Judul tidak cocok: %s != %s", fetched.Title, in.Title)
	}

	// Uji pembaruan data
	updateIn := domain.UpdateWorkItemInput{
		Title:       "Uji Kompatibilitas Operasi v1 (Diperbarui)",
		Description: "Deskripsi baru",
		Category:    "General",
		RequestedBy: "Unit Penguji",
		ReceivedOn:  "2026-10-17",
		Priority:    domain.PriorityHigh,
		Status:      domain.StatusInProgress,
	}
	updated, err := svc.UpdateWorkItem(ctx, created.ID, updateIn)
	if err != nil {
		t.Fatalf("UpdateWorkItem gagal: %v", err)
	}
	if updated.Priority != domain.PriorityHigh {
		t.Errorf("Prioritas tidak terupdate: %s", updated.Priority)
	}

	// Uji activity logs
	logs, err := svc.ListActivities(ctx, created.ID)
	if err != nil || len(logs) < 2 {
		t.Fatalf("Log aktivitas tidak tercatat dengan benar: logs=%d, err=%v", len(logs), err)
	}
}

// TestIncrementalMigrationFromV2ToV3 menguji migrasi database yang telah berada di versi skema 2 ke versi 3
func TestIncrementalMigrationFromV2ToV3(t *testing.T) {
	tempDir := t.TempDir()
	appDataDir := filepath.Join(tempDir, "data")
	backupDir := filepath.Join(tempDir, "backups")
	_ = os.MkdirAll(appDataDir, 0755)
	_ = os.MkdirAll(backupDir, 0755)

	dbPath := filepath.Join(appDataDir, "compliance.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Simulasikan database skema versi 2 (Sprint 1/2)
	v2InitDDL := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	INSERT INTO schema_migrations (version) VALUES (1), (2);

	CREATE TABLE IF NOT EXISTS work_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_code TEXT UNIQUE,
		title TEXT NOT NULL CHECK (length(trim(title)) > 0),
		description TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT 'General',
		requested_by TEXT,
		received_on TEXT NOT NULL,
		due_on TEXT,
		priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high','critical')),
		status TEXT NOT NULL DEFAULT 'not_started' CHECK (status IN ('not_started','in_progress','waiting','completed','cancelled')),
		completed_at TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		archived_at TEXT,
		progress_percentage INTEGER NOT NULL DEFAULT 0 CHECK (progress_percentage >= 0 AND progress_percentage <= 100)
	);

	CREATE TABLE IF NOT EXISTS activity_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_item_id INTEGER NOT NULL,
		entry_type TEXT NOT NULL CHECK (entry_type IN ('created','activity','edited','status_changed','archived','restored')),
		description TEXT NOT NULL CHECK (length(trim(description)) > 0),
		field_name TEXT,
		old_value TEXT,
		new_value TEXT,
		occurred_at TEXT NOT NULL,
		recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		FOREIGN KEY (work_item_id) REFERENCES work_items(id) ON DELETE RESTRICT
	);

	CREATE TABLE IF NOT EXISTS categories (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL CHECK (length(trim(name)) > 0),
		description TEXT NOT NULL DEFAULT '',
		color TEXT NOT NULL DEFAULT '#4f46e5',
		is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);

	CREATE TABLE IF NOT EXISTS subtasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_item_id INTEGER NOT NULL,
		title TEXT NOT NULL CHECK (length(trim(title)) > 0),
		is_completed INTEGER NOT NULL DEFAULT 0 CHECK (is_completed IN (0, 1)),
		due_on TEXT,
		sort_order INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		FOREIGN KEY (work_item_id) REFERENCES work_items(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS work_templates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL CHECK (length(trim(name)) > 0),
		title_pattern TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT 'General',
		priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high','critical')),
		due_days_offset INTEGER NOT NULL DEFAULT 7,
		checklist_items_json TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);

	CREATE TABLE IF NOT EXISTS recurring_schedules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		template_id INTEGER REFERENCES work_templates(id) ON DELETE SET NULL,
		title TEXT NOT NULL CHECK (length(trim(title)) > 0),
		description TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT 'General',
		priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high','critical')),
		frequency TEXT NOT NULL CHECK (frequency IN ('daily', 'weekly', 'monthly', 'quarterly', 'yearly')),
		interval_value INTEGER NOT NULL DEFAULT 1 CHECK (interval_value > 0),
		day_of_month INTEGER,
		day_of_week INTEGER,
		start_date TEXT NOT NULL,
		end_date TEXT,
		last_generated_date TEXT,
		next_run_date TEXT NOT NULL,
		is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);

	CREATE TABLE IF NOT EXISTS import_batches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		batch_code TEXT UNIQUE NOT NULL,
		file_name TEXT NOT NULL,
		file_type TEXT NOT NULL CHECK (file_type IN ('csv', 'xlsx')),
		file_size INTEGER NOT NULL,
		total_rows INTEGER NOT NULL DEFAULT 0,
		imported_rows INTEGER NOT NULL DEFAULT 0,
		skipped_rows INTEGER NOT NULL DEFAULT 0,
		failed_rows INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL CHECK (status IN ('preview', 'completed', 'failed', 'rolled_back')),
		error_summary TEXT,
		column_mapping_json TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		completed_at TEXT
	);

	CREATE TABLE IF NOT EXISTS import_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		batch_id INTEGER NOT NULL,
		row_number INTEGER NOT NULL,
		work_item_id INTEGER,
		status TEXT NOT NULL CHECK (status IN ('created', 'updated', 'skipped', 'failed')),
		action_type TEXT NOT NULL DEFAULT 'insert',
		error_message TEXT,
		raw_data_json TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		FOREIGN KEY (batch_id) REFERENCES import_batches(id) ON DELETE CASCADE,
		FOREIGN KEY (work_item_id) REFERENCES work_items(id) ON DELETE SET NULL
	);
	`
	if _, err := db.ExecContext(ctx, v2InitDDL); err != nil {
		t.Fatalf("Gagal menyiapkan DDL v2: %v", err)
	}

	// Masukkan data sebelum migrasi ke v3
	insertQuery := `
	INSERT INTO work_items (work_code, title, category, requested_by, received_on, priority, status)
	VALUES ('CMP-2026-000001', 'Tugas Rutin 1', 'General', 'Jadwal Rutin #1', '2026-10-17', 'normal', 'not_started');
	`
	if _, err := db.ExecContext(ctx, insertQuery); err != nil {
		t.Fatalf("Gagal insert data dummy v2: %v", err)
	}

	// 2. Jalankan database.Migrate (harus menerapkan 003_v1_1_refinements.sql)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("Gagal menjalankan migrasi incremental v2 ke v3: %v", err)
	}

	// 3. Verifikasi versi 3 tercatat di schema_migrations
	var v3Exists bool
	err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = 3);").Scan(&v3Exists)
	if err != nil || !v3Exists {
		t.Fatalf("Versi 3 tidak tercatat di schema_migrations: err=%v, v3Exists=%v", err, v3Exists)
	}

	// 4. Verifikasi indeks unik uq_work_items_recurring_period aktif
	var idxCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='uq_work_items_recurring_period';").Scan(&idxCount)
	if err != nil || idxCount != 1 {
		t.Fatalf("Indeks uq_work_items_recurring_period tidak ditemukan: count=%d, err=%v", idxCount, err)
	}

	// 5. Verifikasi bahwa duplikasi jadwal rutin yang sama ditolak oleh indeks unik
	_, err = db.ExecContext(ctx, `
	INSERT INTO work_items (work_code, title, category, requested_by, received_on, priority, status)
	VALUES ('CMP-2026-000002', 'Tugas Rutin Duplikat', 'General', 'Jadwal Rutin #1', '2026-10-17', 'normal', 'not_started');
	`)
	if err == nil {
		t.Errorf("Indeks unik seharusnya menolak duplikasi Jadwal Rutin #1 pada received_on yang sama!")
	}

	// 6. Verifikasi integritas dan foreign key
	if err := database.CheckIntegrity(ctx, db); err != nil {
		t.Fatalf("Integritas database setelah migrasi v2->v3 tidak valid: %v", err)
	}
	if err := database.CheckForeignKeys(ctx, db); err != nil {
		t.Fatalf("Foreign key database setelah migrasi v2->v3 tidak valid: %v", err)
	}
}

// TestIncrementalMigrationFromV3ToV4 menguji migrasi dari skema v3 ke v4 (penambahan kolom is_active pada categories)
func TestIncrementalMigrationFromV3ToV4(t *testing.T) {
	tempDir := t.TempDir()
	appDataDir := filepath.Join(tempDir, "data")
	backupDir := filepath.Join(tempDir, "backups")
	_ = os.MkdirAll(appDataDir, 0755)
	_ = os.MkdirAll(backupDir, 0755)

	dbPath := filepath.Join(appDataDir, "compliance.db")

	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Setup schema v3 lengkap
	v3InitDDL := `
	CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	INSERT INTO schema_migrations (version) VALUES (1), (2), (3);

	CREATE TABLE IF NOT EXISTS work_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_code TEXT UNIQUE NOT NULL,
		title TEXT NOT NULL,
		description TEXT,
		category TEXT NOT NULL DEFAULT 'General',
		requested_by TEXT,
		received_on TEXT NOT NULL,
		due_on TEXT,
		priority TEXT NOT NULL DEFAULT 'normal',
		status TEXT NOT NULL DEFAULT 'not_started',
		progress_percentage INTEGER NOT NULL DEFAULT 0,
		completed_at TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		archived_at TEXT
	);

	CREATE TABLE IF NOT EXISTS categories (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		description TEXT,
		color TEXT DEFAULT '#0284c7',
		is_default INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);

	INSERT INTO categories (name, description, color, is_default) VALUES
	('General', 'Kategori umum kepatuhan', '#64748b', 1),
	('Perpajakan & Keuangan', 'Kepatuhan pajak, pelaporan SPT, dan regulasi finansial', '#0284c7', 0);
	`
	if _, err := db.ExecContext(ctx, v3InitDDL); err != nil {
		t.Fatalf("Gagal inisialisasi DDL v3: %v", err)
	}

	// 2. Jalankan database.Migrate (ini harus menerapkan 004_category_is_active.sql)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("Gagal migrasi incremental v3 ke v4: %v", err)
	}

	// 3. Verifikasi schema_migrations mencatat versi 4
	var v4Exists bool
	err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = 4);").Scan(&v4Exists)
	if err != nil || !v4Exists {
		t.Fatalf("Versi 4 tidak tercatat di schema_migrations: err=%v, v4Exists=%v", err, v4Exists)
	}

	// 4. Verifikasi kolom is_active default 1 untuk data yang sudah ada
	var isActGen, isActPajak int
	err1 := db.QueryRowContext(ctx, "SELECT is_active FROM categories WHERE name = 'General';").Scan(&isActGen)
	err2 := db.QueryRowContext(ctx, "SELECT is_active FROM categories WHERE name = 'Perpajakan & Keuangan';").Scan(&isActPajak)
	if err1 != nil || err2 != nil || isActGen != 1 || isActPajak != 1 {
		t.Fatalf("Default is_active tidak 1 pada data migrasi: gen=%d (err=%v), pajak=%d (err=%v)", isActGen, err1, isActPajak, err2)
	}

	// 5. Verifikasi bahwa nilai is_active dapat diupdate ke 0 (soft delete)
	_, err = db.ExecContext(ctx, "UPDATE categories SET is_active = 0 WHERE name = 'Perpajakan & Keuangan';")
	if err != nil {
		t.Fatalf("Gagal mengubah is_active ke 0: %v", err)
	}

	// 6. Verifikasi integritas dan foreign key
	if err := database.CheckIntegrity(ctx, db); err != nil {
		t.Fatalf("Integritas database setelah migrasi v3->v4 tidak valid: %v", err)
	}
	if err := database.CheckForeignKeys(ctx, db); err != nil {
		t.Fatalf("Foreign key database setelah migrasi v3->v4 tidak valid: %v", err)
	}
}
