package tests

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"compliance-workspace/internal/database"
	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
	"compliance-workspace/internal/service"
)

func createDummyDatabaseWithItem(t *testing.T, dbPath, title string) {
	t.Helper()
	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka dummy DB %s: %v", dbPath, err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrasi dummy DB gagal: %v", err)
	}

	workRepo := repository.NewWorkItemRepository(db)
	activityRepo := repository.NewActivityLogRepository(db)
	svc := service.NewWorkItemService(db, workRepo, activityRepo)
	in := domain.CreateWorkItemInput{
		Title:       title,
		Category:    "Internal Audit",
		RequestedBy: "Head of Compliance",
		ReceivedOn:  "2026-10-19",
		Priority:    domain.PriorityHigh,
	}
	if _, err := svc.CreateWorkItem(ctx, in); err != nil {
		t.Fatalf("Gagal membuat item di dummy DB: %v", err)
	}
}

func countDummyItems(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB untuk hitung item: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM work_items").Scan(&count); err != nil {
		t.Fatalf("Gagal query work_items: %v", err)
	}
	return count
}

func getFirstItemTitle(t *testing.T, dbPath string) string {
	t.Helper()
	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB untuk ambil title: %v", err)
	}
	defer db.Close()

	var title string
	if err := db.QueryRow("SELECT title FROM work_items LIMIT 1").Scan(&title); err != nil {
		t.Fatalf("Gagal query title work_items: %v", err)
	}
	return title
}

// TestCrashRecoveryInterruptedRestoreRestoresOriginalDB menguji skenario utama di mana Restore crash
// setelah database aktif di-rename ke rollback. Startup recovery harus memulihkan database asli
// dan TIDAK membuat database kosong baru.
func TestCrashRecoveryInterruptedRestoreRestoresOriginalDB(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	// 1. Buat database asli dengan data pengguna
	originalTitle := "Data Pekerjaan Penting Pengguna Sebelum Crash"
	createDummyDatabaseWithItem(t, dbPath, originalTitle)

	// 2. Simulasikan crash di antara 2 rename:
	// compliance.db dipindahkan ke .restore-rollback-abcdef01.db
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-abcdef01.db")
	if err := os.Rename(dbPath, rollbackPath); err != nil {
		t.Fatalf("Gagal simulasi rename ke rollback: %v", err)
	}

	// Buat berkas staging yang tertinggal
	stagingPath := filepath.Join(dataDir, ".restore-staging-123456.db")
	if err := os.WriteFile(stagingPath, []byte("staging-data"), 0600); err != nil {
		t.Fatal(err)
	}

	// Pastikan compliance.db benar-benar tidak ada di disk saat ini
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("compliance.db harus tidak ada sebelum recovery dijalankan")
	}

	// 3. Jalankan startup recovery sebelum OpenDB
	if err := database.RecoverInterruptedRestore(dbPath); err != nil {
		t.Fatalf("RecoverInterruptedRestore gagal: %v", err)
	}

	// 4. Verifikasi compliance.db berhasil dipulihkan
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("compliance.db tidak ditemukan setelah recovery: %v", err)
	}

	// 5. Verifikasi isi data asli tetap utuh
	if count := countDummyItems(t, dbPath); count != 1 {
		t.Fatalf("Jumlah data setelah recovery harus 1, mendapat %d", count)
	}
	if title := getFirstItemTitle(t, dbPath); title != originalTitle {
		t.Fatalf("Judul data setelah recovery salah. Harap: %q, dapat: %q", originalTitle, title)
	}

	// 6. Verifikasi berkas staging dan safety backup tidak dihapus secara otomatis (Requirement 8)
	if _, err := os.Stat(stagingPath); os.IsNotExist(err) {
		t.Fatal("berkas staging tidak boleh dihapus otomatis saat recovery")
	}
}

// TestCrashRecoveryActiveDBExistsDoesNotOverwrite menguji bahwa jika database aktif sudah ada di disk,
// startup recovery tidak boleh menggantinya dengan berkas rollback yang ada.
func TestCrashRecoveryActiveDBExistsDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	activeTitle := "Data Database Aktif Terkini"
	createDummyDatabaseWithItem(t, dbPath, activeTitle)

	// Buat rollback lama
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-old12345.db")
	createDummyDatabaseWithItem(t, rollbackPath, "Data Lama di Rollback")

	// Jalankan recovery
	if err := database.RecoverInterruptedRestore(dbPath); err != nil {
		t.Fatalf("RecoverInterruptedRestore harus sukses saat active DB ada: %v", err)
	}

	// Database aktif tidak boleh tertimpa
	if title := getFirstItemTitle(t, dbPath); title != activeTitle {
		t.Fatalf("Database aktif tertimpa oleh rollback! Dapat: %q", title)
	}

	// Berkas rollback tidak boleh dihapus otomatis
	if _, err := os.Stat(rollbackPath); os.IsNotExist(err) {
		t.Fatal("berkas rollback tidak boleh dihapus otomatis")
	}
}

// TestCrashRecoveryCorruptedRollbackAbortsStartup menguji bahwa jika berkas rollback korup,
// startup harus digagalkan dan database kosong TIDAK boleh dibuat.
func TestCrashRecoveryCorruptedRollbackAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	// Tulis rollback korup
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-corrupt1.db")
	if err := os.WriteFile(rollbackPath, []byte("ini bukan database sqlite yang sah"), 0600); err != nil {
		t.Fatal(err)
	}

	// Jalankan recovery
	err := database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus gagal ketika berkas rollback korup")
	}
	if !strings.Contains(err.Error(), "korup") && !strings.Contains(err.Error(), "integritas") {
		t.Logf("Pesan error: %v", err)
	}

	// Pastikan compliance.db TIDAK dibuat
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("compliance.db kosong TIDAK boleh dibuat jika recovery rollback gagal")
	}
}

// TestCrashRecoveryMultipleRollbacksAbortsStartup menguji bahwa jika ada lebih dari 1 file rollback,
// kondisi dianggap ambigu demi keselamatan data dan startup harus dihentikan.
func TestCrashRecoveryMultipleRollbacksAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	rb1 := filepath.Join(dataDir, ".restore-rollback-11111111.db")
	rb2 := filepath.Join(dataDir, ".restore-rollback-22222222.db")
	createDummyDatabaseWithItem(t, rb1, "Item Rollback 1")
	createDummyDatabaseWithItem(t, rb2, "Item Rollback 2")

	err := database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus gagal ketika ditemukan lebih dari satu rollback (ambigu)")
	}
	if !strings.Contains(err.Error(), "ambigu") {
		t.Errorf("Error harus menyebutkan kondisi ambigu, mendapat: %v", err)
	}

	// Pastikan compliance.db TIDAK dibuat
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("compliance.db kosong TIDAK boleh dibuat jika kondisi rollback ambigu")
	}
}

// TestCrashRecoveryCleanFreshInstallProceeds menguji bahwa instalasi pertama yang bersih
// (tidak ada database aktif, tidak ada rollback, tidak ada staging) diizinkan berjalan normal.
func TestCrashRecoveryCleanFreshInstallProceeds(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	if err := database.RecoverInterruptedRestore(dbPath); err != nil {
		t.Fatalf("RecoverInterruptedRestore gagal pada fresh install: %v", err)
	}

	// compliance.db belum dibuat oleh recovery (akan dibuat oleh OpenDB)
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("compliance.db seharusnya belum ada sebelum OpenDB dipanggil")
	}

	// Pastikan OpenDB dan Migrate berjalan sukses
	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB gagal pada fresh install: %v", err)
	}
	defer db.Close()

	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate gagal pada fresh install: %v", err)
	}
}

// TestCrashRecoveryStagingOnlyWithoutRollbackAbortsStartup menguji bahwa jika ditemukan berkas staging
// sisa restore tetapi tidak ada database aktif maupun rollback, startup digagalkan demi keselamatan data.
func TestCrashRecoveryStagingOnlyWithoutRollbackAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	stagingPath := filepath.Join(dataDir, ".restore-staging-orphan.db")
	if err := os.WriteFile(stagingPath, []byte("orphan-staging"), 0600); err != nil {
		t.Fatal(err)
	}

	err := database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus gagal bila ada staging tanpa active DB dan tanpa rollback")
	}

	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("compliance.db kosong TIDAK boleh dibuat jika ada indikasi staging terinterupsi")
	}
}

// TestCrashRecoveryRollbackHigherSchemaAbortsStartup menguji bahwa rollback dengan skema > MaxSupportedSchemaVersion
// ditolak dan tidak boleh dipulihkan.
func TestCrashRecoveryRollbackHigherSchemaAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-schema99.db")

	// Buat DB dengan versi skema 99
	db, err := database.OpenDB(rollbackPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT);
		INSERT INTO schema_migrations (version) VALUES (99);
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus menolak rollback dengan skema lebih tinggi dari batas aplikasi")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("v%d", database.MaxSupportedSchemaVersion)) {
		t.Errorf("Pesan error harus memuat batas versi skema, dapat: %v", err)
	}

	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("compliance.db kosong TIDAK boleh dibuat jika versi skema rollback melebihi batas")
	}
}

// TestCrashRecoveryDirectoryNamedRollbackArtifactAbortsStartup menguji temuan QA-111-01:
// Jika terdapat direktori bernama artefak rollback (.restore-rollback-*.db), sistem tidak boleh
// mengabaikannya atau fail-open ke fresh install. Startup harus dihentikan dengan error.
func TestCrashRecoveryDirectoryNamedRollbackArtifactAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	fakeRollbackDir := filepath.Join(dataDir, ".restore-rollback-fake1234.db")
	if err := os.MkdirAll(fakeRollbackDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus menolak artefak rollback yang berupa direktori")
	}
	if !strings.Contains(err.Error(), "direktori") && !strings.Contains(err.Error(), "tidak aman") {
		t.Logf("Pesan error: %v", err)
	}

	// Pastikan tidak ada database kosong yang dibuat (tidak fail-open)
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("compliance.db kosong TIDAK boleh dibuat ketika artefak rollback berupa direktori")
	}
}

// TestCrashRecoveryDirectoryNamedStagingArtifactAbortsStartup menguji temuan QA-111-01:
// Jika terdapat direktori bernama artefak staging (.restore-staging-*.db), sistem tidak boleh
// mengabaikannya atau fail-open ke fresh install. Startup harus dihentikan dengan error.
func TestCrashRecoveryDirectoryNamedStagingArtifactAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	fakeStagingDir := filepath.Join(dataDir, ".restore-staging-fake1234.db")
	if err := os.MkdirAll(fakeStagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus menolak artefak staging yang berupa direktori")
	}
	if !strings.Contains(err.Error(), "direktori") && !strings.Contains(err.Error(), "tidak aman") {
		t.Logf("Pesan error: %v", err)
	}

	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("compliance.db kosong TIDAK boleh dibuat ketika artefak staging berupa direktori")
	}
}

// TestCrashRecoveryIncompleteSchemaMissingCoreTablesAbortsStartup menguji temuan QA-111-03:
// Database yang lulus integritas SQLite tetapi tidak memiliki tabel inti aplikasi (work_items/activity_logs)
// harus ditolak demi integritas skema.
func TestCrashRecoveryIncompleteSchemaMissingCoreTablesAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-notables.db")

	// Buat DB dengan schema_migrations saja tanpa tabel work_items
	db, err := database.OpenDB(rollbackPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT);
		INSERT INTO schema_migrations (version) VALUES (6);
		CREATE TABLE some_other_table (id INTEGER PRIMARY KEY);
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus menolak rollback dengan struktur tabel inti yang tidak lengkap")
	}
	if !strings.Contains(err.Error(), "tabel wajib") {
		t.Logf("Pesan error: %v", err)
	}

	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("compliance.db kosong TIDAK boleh dibuat jika struktur tabel rollback tidak lengkap")
	}
}

// TestCrashRecoveryIncompleteSchemaMissingColumnsAbortsStartup menguji temuan QA-111-03:
// Database dengan work_items yang tidak memiliki kolom penting (title atau work_code) harus ditolak.
func TestCrashRecoveryIncompleteSchemaMissingColumnsAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-nocols.db")

	db, err := database.OpenDB(rollbackPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT);
		INSERT INTO schema_migrations (version) VALUES (1);
		CREATE TABLE work_items (id INTEGER PRIMARY KEY, note TEXT);
		CREATE TABLE activity_logs (id INTEGER PRIMARY KEY, work_item_id INTEGER);
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus menolak rollback dengan kolom work_items tidak lengkap")
	}
	if !strings.Contains(err.Error(), "kolom") {
		t.Logf("Pesan error: %v", err)
	}

	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("compliance.db kosong TIDAK boleh dibuat jika kolom work_items tidak lengkap")
	}
}

// TestCrashRecoverySupportedOlderSchemaV1RestoredAndMigrated menguji temuan QA-111-03:
// Rollback dengan skema lama yang sah (misal v1) HARUS berhasil dipulihkan, lalu dimigrasikan
// dengan aman ke versi skema terkini tanpa kehilangan data.
func TestCrashRecoverySupportedOlderSchemaV1RestoredAndMigrated(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-v1schema.db")

	// Buat DB dengan skema v1 murni
	db, err := database.OpenDB(rollbackPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT);
		INSERT INTO schema_migrations (version) VALUES (1);
		CREATE TABLE work_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT 'General',
			requested_by TEXT NOT NULL DEFAULT '',
			received_on TEXT NOT NULL,
			due_on TEXT,
			priority TEXT NOT NULL DEFAULT 'normal',
			status TEXT NOT NULL DEFAULT 'not_started',
			completed_at TEXT,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			archived_at TEXT,
			work_code TEXT NOT NULL UNIQUE
		);
		CREATE TABLE activity_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			work_item_id INTEGER NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
			entry_type TEXT NOT NULL DEFAULT 'manual_entry',
			description TEXT NOT NULL,
			field_name TEXT,
			old_value TEXT,
			new_value TEXT,
			occurred_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		);
		INSERT INTO work_items (id, title, category, requested_by, received_on, work_code)
		VALUES (1, 'Pekerjaan Kepatuhan Skema V1 Asli', 'General', 'Legal', '2026-10-15', 'CMP-2026-000001');
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Jalankan recovery: harus berhasil memulihkan rollback v1
	err = database.RecoverInterruptedRestore(dbPath)
	if err != nil {
		t.Fatalf("RecoverInterruptedRestore harus mendukung pemulihan skema lama yang sah (v1): %v", err)
	}

	// Buka compliance.db yang dipulihkan dan jalankan migrasi incremental hingga v6
	migDB, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB hasil pemulihan: %v", err)
	}
	defer migDB.Close()

	if err := database.Migrate(context.Background(), migDB); err != nil {
		t.Fatalf("Migrasi post-recovery dari v1 ke v6 gagal: %v", err)
	}

	// Verifikasi data pengguna dari skema v1 tetap utuh
	var title string
	if err := migDB.QueryRow("SELECT title FROM work_items WHERE id = 1").Scan(&title); err != nil {
		t.Fatalf("Gagal query data v1: %v", err)
	}
	if title != "Pekerjaan Kepatuhan Skema V1 Asli" {
		t.Errorf("Judul data v1 salah setelah migrasi: %s", title)
	}
}

// TestCrashRecoveryParentDirValidationActiveDBExists menguji temuan QA-111-02:
// Validasi parent directory harus tetap berjalan meskipun database aktif ada.
func TestCrashRecoveryParentDirValidationActiveDBExists(t *testing.T) {
	root := t.TempDir()
	fakeDirFile := filepath.Join(root, "file_as_dir")
	if err := os.WriteFile(fakeDirFile, []byte("bukan direktori"), 0600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(fakeDirFile, "compliance.db")

	err := database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus gagal bila parent data dir tidak valid")
	}
}

// TestCrashRecoverySymlinkArtifact menguji bahwa symlink bernama artefak pemulihan ditolak.
func TestCrashRecoverySymlinkArtifact(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	realFile := filepath.Join(root, "real_outside.db")
	if err := os.WriteFile(realFile, []byte("dummy outside"), 0600); err != nil {
		t.Fatal(err)
	}

	symlinkPath := filepath.Join(dataDir, ".restore-rollback-symlink.db")
	if err := os.Symlink(realFile, symlinkPath); err != nil {
		t.Skipf("symlink tidak didukung atau memerlukan hak istimewa pada environment ini: %v", err)
	}

	err := database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus menolak artefak pemulihan berupa symlink")
	}
	if !strings.Contains(err.Error(), "symbolic link") && !strings.Contains(err.Error(), "tidak aman") {
		t.Logf("Pesan error: %v", err)
	}
}

// TestCrashRecoveryActiveDBZeroByteAbortsStartup menguji temuan QA-111-01:
// Jika file database aktif berukuran 0-byte (kosong/terpotong akibat crash atau corrupt),
// startup harus digagalkan dengan error yang jelas untuk mencegah inisialisasi ulang
// database kosong oleh Migrate, dan rollback TIDAK boleh menimpa active DB secara otomatis.
func TestCrashRecoveryActiveDBZeroByteAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	// 1. Buat active database berukuran 0-byte
	if err := os.WriteFile(dbPath, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	// 2. Buat rollback valid yang mungkin ada dari upaya restore
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-valid1.db")
	createDummyDatabaseWithItem(t, rollbackPath, "Data Rollback Valid")

	// 3. Jalankan startup recovery
	err := database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus gagal ketika active database berukuran 0 byte")
	}
	if !strings.Contains(err.Error(), "0 byte") {
		t.Errorf("Pesan error harus secara spesifik menyebutkan kondisi 0 byte, dapat: %v", err)
	}

	// 4. Verifikasi active database TIDAK ditimpa oleh rollback
	info, statErr := os.Stat(dbPath)
	if statErr != nil {
		t.Fatalf("Stat active DB gagal: %v", statErr)
	}
	if info.Size() != 0 {
		t.Fatalf("Active DB 0 byte tidak boleh ditimpa secara diam-diam, ukuran sekarang: %d", info.Size())
	}

	// 5. Verifikasi berkas rollback tetap ada dan utuh untuk investigasi manual
	if _, err := os.Stat(rollbackPath); err != nil {
		t.Fatalf("Berkas rollback tidak boleh hilang atau dihapus: %v", err)
	}
}

// TestCrashRecoveryActiveDBCorruptAbortsStartup menguji temuan QA-111-01:
// Jika file database aktif korup (bukan SQLite valid), startup harus gagal secara aman
// dan tidak menimpa active database dengan rollback atau menghapus rollback.
func TestCrashRecoveryActiveDBCorruptAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	// 1. Buat active database dengan konten acak/korup
	if err := os.WriteFile(dbPath, []byte("konten acak yang merusak header sqlite"), 0600); err != nil {
		t.Fatal(err)
	}

	// 2. Buat rollback valid
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-valid2.db")
	createDummyDatabaseWithItem(t, rollbackPath, "Data Rollback Valid 2")

	// 3. Jalankan recovery
	err := database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus gagal ketika active database korup")
	}
	if !strings.Contains(err.Error(), "korup") && !strings.Contains(err.Error(), "tidak valid") {
		t.Logf("Pesan error: %v", err)
	}

	// 4. Verifikasi active database tidak tertimpa
	content, readErr := os.ReadFile(dbPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "konten acak yang merusak header sqlite" {
		t.Fatal("Active database korup tidak boleh tertimpa secara otomatis")
	}

	// 5. Verifikasi rollback tetap aman
	if _, err := os.Stat(rollbackPath); err != nil {
		t.Fatalf("Berkas rollback harus tetap dipertahankan untuk penanganan manual: %v", err)
	}
}

// TestCrashRecoveryActiveDBForeignSchemaAbortsStartup menguji temuan QA-111-03:
// Jika file database aktif adalah database SQLite yang valid namun dari aplikasi asing
// (tidak memiliki tabel schema_migrations dan struktur Compliance Workspace),
// startup harus digagalkan demi keselamatan data dan integritas aplikasi.
func TestCrashRecoveryActiveDBForeignSchemaAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	// Buat database SQLite yang valid tapi hanya memiliki tabel aplikasi lain
	foreignDB, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = foreignDB.Exec("CREATE TABLE foreign_app_table (id INTEGER PRIMARY KEY, note TEXT);")
	foreignDB.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus menolak active database dengan skema asing")
	}
	if !strings.Contains(err.Error(), "schema_migrations") && !strings.Contains(err.Error(), "struktur") {
		t.Logf("Pesan error: %v", err)
	}
}

// TestCrashRecoveryActiveDBIncompleteSchemaAbortsStartup menguji temuan QA-111-03:
// Jika database aktif memiliki tabel schema_migrations tetapi kehilangan tabel inti atau kolom domain kunci,
// startup harus digagalkan dengan error terperinci.
func TestCrashRecoveryActiveDBIncompleteSchemaAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	// Buat database dengan skema parsial yang rusak (hanya work_items tanpa kolom wajib)
	incompleteDB, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = incompleteDB.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT);
		INSERT INTO schema_migrations (version) VALUES (1);
		CREATE TABLE work_items (id INTEGER PRIMARY KEY, deskripsi TEXT);
		CREATE TABLE activity_logs (id INTEGER PRIMARY KEY);
	`)
	incompleteDB.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus menolak active database yang kehilangan kolom inti")
	}
	if !strings.Contains(err.Error(), "kolom") && !strings.Contains(err.Error(), "tidak valid") {
		t.Logf("Pesan error: %v", err)
	}
}

// TestCrashRecoveryWindowsJunctionDirectoryRejected menguji temuan QA-111-02:
// Pada platform Windows, verifikasi bahwa junction directory (NTFS reparse point tanpa hak admin)
// ditolak baik pada parent data directory maupun pada artefak pemulihan.
func TestCrashRecoveryWindowsJunctionDirectoryRejected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Pengujian junction hanya relevan untuk sistem operasi Windows")
	}

	root := t.TempDir()
	targetDir := filepath.Join(root, "real_target_dir")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Subtest: Junction pada parent data directory
	t.Run("ParentDirectoryJunction", func(t *testing.T) {
		junctionDataDir := filepath.Join(root, "junction_data_dir")
		cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", junctionDataDir, targetDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Skipf("mklink /J gagal dijalankan pada lingkungan ini: %s (%v)", string(out), err)
		}
		defer func() {
			_ = os.Remove(junctionDataDir)
		}()

		dbPath := filepath.Join(junctionDataDir, "compliance.db")
		err = database.RecoverInterruptedRestore(dbPath)
		if err == nil {
			t.Fatal("RecoverInterruptedRestore harus menolak data directory yang merupakan junction / reparse point")
		}
		if !strings.Contains(err.Error(), "symbolic link") && !strings.Contains(err.Error(), "reparse point") {
			t.Errorf("Pesan error harus memuat indikasi symbolic link/reparse point, dapat: %v", err)
		}

		// Pastikan database kosong TIDAK dibuat
		if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
			t.Fatal("Database kosong tidak boleh dibuat pada parent direktori berupa junction")
		}
	})

	// 2. Subtest: Junction pada artefak rollback di dalam direktori data sah
	t.Run("RollbackArtifactJunction", func(t *testing.T) {
		validDataDir := filepath.Join(root, "valid_data_dir_rb")
		if err := os.MkdirAll(validDataDir, 0755); err != nil {
			t.Fatal(err)
		}
		junctionArtifactDir := filepath.Join(validDataDir, ".restore-rollback-junction.db")
		cmd2 := exec.Command("cmd.exe", "/c", "mklink", "/J", junctionArtifactDir, targetDir)
		out2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			t.Skipf("mklink /J untuk artefak rollback gagal dijalankan: %s (%v)", string(out2), err2)
		}
		defer func() {
			_ = os.Remove(junctionArtifactDir)
		}()

		dbPath2 := filepath.Join(validDataDir, "compliance.db")
		err2 = database.RecoverInterruptedRestore(dbPath2)
		if err2 == nil {
			t.Fatal("RecoverInterruptedRestore harus menolak artefak rollback berupa junction")
		}
		if !strings.Contains(err2.Error(), "direktori") && !strings.Contains(err2.Error(), "symbolic link") && !strings.Contains(err2.Error(), "tidak aman") {
			t.Errorf("Pesan error harus memuat indikasi artefak tidak aman, dapat: %v", err2)
		}

		// Pastikan database kosong TIDAK dibuat
		if _, statErr := os.Stat(dbPath2); !os.IsNotExist(statErr) {
			t.Fatal("Database kosong tidak boleh dibuat ketika artefak rollback berupa junction")
		}
	})

	// 3. Subtest: Junction pada artefak staging di dalam direktori data sah
	t.Run("StagingArtifactJunction", func(t *testing.T) {
		validDataDir := filepath.Join(root, "valid_data_dir_st")
		if err := os.MkdirAll(validDataDir, 0755); err != nil {
			t.Fatal(err)
		}
		junctionStagingDir := filepath.Join(validDataDir, ".restore-staging-junction.db")
		cmd3 := exec.Command("cmd.exe", "/c", "mklink", "/J", junctionStagingDir, targetDir)
		out3, err3 := cmd3.CombinedOutput()
		if err3 != nil {
			t.Skipf("mklink /J untuk artefak staging gagal dijalankan: %s (%v)", string(out3), err3)
		}
		defer func() {
			_ = os.Remove(junctionStagingDir)
		}()

		dbPath3 := filepath.Join(validDataDir, "compliance.db")
		err3 = database.RecoverInterruptedRestore(dbPath3)
		if err3 == nil {
			t.Fatal("RecoverInterruptedRestore harus menolak artefak staging berupa junction")
		}
		if !strings.Contains(err3.Error(), "direktori") && !strings.Contains(err3.Error(), "symbolic link") && !strings.Contains(err3.Error(), "tidak aman") {
			t.Errorf("Pesan error harus memuat indikasi artefak tidak aman, dapat: %v", err3)
		}

		// Pastikan database kosong TIDAK dibuat
		if _, statErr := os.Stat(dbPath3); !os.IsNotExist(statErr) {
			t.Fatal("Database kosong tidak boleh dibuat ketika artefak staging berupa junction")
		}
	})
}

// TestCrashRecoverySchemaV2MissingProgressPercentageAbortsStartup menguji temuan QA-111-03:
// Database dengan riwayat migrasi v2 tetapi tidak memiliki kolom work_items.progress_percentage
// harus ditolak demi integritas skema struktural aplikasi.
func TestCrashRecoverySchemaV2MissingProgressPercentageAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-noprog.db")

	db, err := database.OpenDB(rollbackPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT);
		INSERT INTO schema_migrations (version) VALUES (1), (2);
		CREATE TABLE work_items (
			id INTEGER PRIMARY KEY,
			title TEXT NOT NULL,
			work_code TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL,
			received_on TEXT NOT NULL
		);
		CREATE TABLE activity_logs (
			id INTEGER PRIMARY KEY,
			work_item_id INTEGER NOT NULL,
			entry_type TEXT NOT NULL,
			description TEXT NOT NULL,
			occurred_at TEXT NOT NULL
		);
		CREATE TABLE categories (id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL);
		CREATE TABLE subtasks (id INTEGER PRIMARY KEY, work_item_id INTEGER, title TEXT, is_completed INTEGER);
		CREATE TABLE work_templates (id INTEGER PRIMARY KEY, name TEXT, title_pattern TEXT);
		CREATE TABLE recurring_schedules (id INTEGER PRIMARY KEY, title TEXT, frequency TEXT, next_run_date TEXT, is_active INTEGER);
		CREATE TABLE import_batches (id INTEGER PRIMARY KEY, batch_code TEXT, status TEXT);
		CREATE TABLE import_items (id INTEGER PRIMARY KEY, batch_id INTEGER, status TEXT);
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus menolak skema v2 tanpa kolom progress_percentage")
	}
	if !strings.Contains(err.Error(), "progress_percentage") {
		t.Errorf("Pesan error harus menyebutkan kolom progress_percentage, dapat: %v", err)
	}

	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("compliance.db kosong TIDAK boleh dibuat jika kolom progress_percentage tidak ada")
	}
}

// TestCrashRecoverySchemaV2MissingSubtaskColumnAbortsStartup menguji temuan QA-111-03:
// Database dengan riwayat migrasi v2 tetapi subtasks kehilangan kolom penting seperti is_completed
// harus ditolak demi integritas skema.
func TestCrashRecoverySchemaV2MissingSubtaskColumnAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-nosubcol.db")

	db, err := database.OpenDB(rollbackPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT);
		INSERT INTO schema_migrations (version) VALUES (1), (2);
		CREATE TABLE work_items (
			id INTEGER PRIMARY KEY,
			title TEXT NOT NULL,
			work_code TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL,
			received_on TEXT NOT NULL,
			progress_percentage INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE activity_logs (
			id INTEGER PRIMARY KEY,
			work_item_id INTEGER NOT NULL,
			entry_type TEXT NOT NULL,
			description TEXT NOT NULL,
			occurred_at TEXT NOT NULL
		);
		CREATE TABLE categories (id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL);
		CREATE TABLE subtasks (id INTEGER PRIMARY KEY, work_item_id INTEGER, title TEXT);
		CREATE TABLE work_templates (id INTEGER PRIMARY KEY, name TEXT, title_pattern TEXT);
		CREATE TABLE recurring_schedules (id INTEGER PRIMARY KEY, title TEXT, frequency TEXT, next_run_date TEXT, is_active INTEGER);
		CREATE TABLE import_batches (id INTEGER PRIMARY KEY, batch_code TEXT, status TEXT);
		CREATE TABLE import_items (id INTEGER PRIMARY KEY, batch_id INTEGER, status TEXT);
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus menolak skema v2 tanpa kolom subtasks.is_completed")
	}
	if !strings.Contains(err.Error(), "is_completed") {
		t.Errorf("Pesan error harus menyebutkan kolom is_completed, dapat: %v", err)
	}

	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("compliance.db kosong TIDAK boleh dibuat jika kolom subtasks tidak lengkap")
	}
}

// TestCrashRecoverySchemaMigrationGapAbortsStartup menguji temuan QA-111-03:
// Database dengan riwayat migrasi yang memiliki gap (misal ada versi 1 dan 3, tetapi versi 2 hilang)
// harus ditolak karena riwayat migrasinya tidak konsisten.
func TestCrashRecoverySchemaMigrationGapAbortsStartup(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")
	rollbackPath := filepath.Join(dataDir, ".restore-rollback-gap.db")

	db, err := database.OpenDB(rollbackPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT);
		INSERT INTO schema_migrations (version) VALUES (1), (3);
		CREATE TABLE work_items (
			id INTEGER PRIMARY KEY,
			title TEXT NOT NULL,
			work_code TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL,
			received_on TEXT NOT NULL,
			progress_percentage INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE activity_logs (
			id INTEGER PRIMARY KEY,
			work_item_id INTEGER NOT NULL,
			entry_type TEXT NOT NULL,
			description TEXT NOT NULL,
			occurred_at TEXT NOT NULL
		);
		CREATE TABLE categories (id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL);
		CREATE TABLE subtasks (id INTEGER PRIMARY KEY, work_item_id INTEGER, title TEXT, is_completed INTEGER);
		CREATE TABLE work_templates (id INTEGER PRIMARY KEY, name TEXT, title_pattern TEXT);
		CREATE TABLE recurring_schedules (id INTEGER PRIMARY KEY, title TEXT, frequency TEXT, next_run_date TEXT, is_active INTEGER);
		CREATE TABLE import_batches (id INTEGER PRIMARY KEY, batch_code TEXT, status TEXT);
		CREATE TABLE import_items (id INTEGER PRIMARY KEY, batch_id INTEGER, status TEXT);
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = database.RecoverInterruptedRestore(dbPath)
	if err == nil {
		t.Fatal("RecoverInterruptedRestore harus menolak skema dengan gap riwayat migrasi")
	}
	if !strings.Contains(err.Error(), "tidak konsisten") {
		t.Errorf("Pesan error harus menyebutkan riwayat migrasi tidak konsisten, dapat: %v", err)
	}

	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("compliance.db kosong TIDAK boleh dibuat jika riwayat migrasi memiliki gap")
	}
}

// TestStartupIntegrityCheckPerformanceAndTimeout mengevaluasi performa startup dan batas waktu (timeout)
// dari PRAGMA integrity_check serta validasi skema pada basis data sintetis berukuran representatif.
func TestStartupIntegrityCheckPerformanceAndTimeout(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "compliance.db")

	// 1. Inisialisasi basis data awal dan migrasikan hingga skema v6
	db, err := database.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB: %v", err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrasi gagal: %v", err)
	}

	// 2. Populasi data sintetis representatif (500 items, 2500 logs, 1000 subtasks)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmtWork, err := tx.Prepare(`
		INSERT INTO work_items (
			title, description, category, requested_by, received_on, due_on,
			priority, status, created_at, updated_at, work_code, progress_percentage, provenance
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmtWork.Close()

	stmtLog, err := tx.Prepare(`
		INSERT INTO activity_logs (
			work_item_id, entry_type, description, occurred_at, recorded_at
		) VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmtLog.Close()

	stmtSub, err := tx.Prepare(`
		INSERT INTO subtasks (
			work_item_id, title, is_completed, sort_order
		) VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmtSub.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for i := 1; i <= 500; i++ {
		code := fmt.Sprintf("CMP-2026-%06d", i)
		title := fmt.Sprintf("Uji Integritas Sintetis Kepatuhan #%d", i)
		res, err := stmtWork.Exec(
			title, "Deskripsi pengujian performa startup", "Kepatuhan Internal",
			"Direksi", "2026-10-01", "2026-10-31", "normal", "in_progress",
			now, now, code, 50, "manual",
		)
		if err != nil {
			t.Fatal(err)
		}
		workID, _ := res.LastInsertId()

		for j := 1; j <= 5; j++ {
			logDesc := fmt.Sprintf("Log aktivitas #%d untuk item %s", j, code)
			if _, err := stmtLog.Exec(workID, "activity", logDesc, now, now); err != nil {
				t.Fatal(err)
			}
		}

		for k := 1; k <= 2; k++ {
			if _, err := stmtSub.Exec(workID, fmt.Sprintf("Subtask #%d", k), 0, k); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db.Close()

	fi, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sizeKB := float64(fi.Size()) / 1024.0

	// 3. Evaluasi performa PRAGMA integrity_check secara terisolasi
	dbRO, err := database.OpenReadOnlyDB(dbPath)
	if err != nil {
		t.Fatalf("Gagal membuka DB read-only: %v", err)
	}
	t0Pragma := time.Now()
	errPragma := database.CheckIntegrity(context.Background(), dbRO)
	durPragma := time.Since(t0Pragma)
	dbRO.Close()
	if errPragma != nil {
		t.Fatalf("PRAGMA integrity_check gagal: %v", errPragma)
	}

	// 4. Evaluasi performa penuh startup RecoverInterruptedRestore (termasuk validasi skema)
	t0Full := time.Now()
	errFull := database.RecoverInterruptedRestore(dbPath)
	durFull := time.Since(t0Full)
	if errFull != nil {
		t.Fatalf("RecoverInterruptedRestore gagal: %v", errFull)
	}

	t.Logf("=== HASIL EVALUASI STARTUP INTEGRITY CHECK ===")
	t.Logf("Volume Data Sintetis         : 500 work items, 2,500 activity logs, 1,000 subtasks (~4,000 baris total)")
	t.Logf("Ukuran Basis Data di Disk    : %.2f KB (%.2f MB)", sizeKB, sizeKB/1024.0)
	t.Logf("Durasi PRAGMA integrity_check: %v", durPragma)
	t.Logf("Durasi Total Validasi Startup: %v (Batas Timeout: 10s)", durFull)

	// Pastikan durasi startup jauh di bawah batas timeout 10 detik (misal harus < 2 detik)
	if durFull > 2*time.Second {
		t.Errorf("Durasi validasi startup melebihi ambang wajar 2 detik: %v", durFull)
	}

	// 5. Verifikasi bahwa timeout 10s dihormati saat context dibatalkan/berakhir
	dbROTimeout, err := database.OpenReadOnlyDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dbROTimeout.Close()

	ctxExpired, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	time.Sleep(1 * time.Millisecond) // Pastikan deadline terlampaui
	cancel()

	t0Timeout := time.Now()
	errTimeout := database.CheckIntegrity(ctxExpired, dbROTimeout)
	durTimeout := time.Since(t0Timeout)

	if errTimeout == nil {
		t.Errorf("CheckIntegrity harus gagal saat context deadline terlampaui")
	}
	t.Logf("Respons context deadline     : %v (dalam %v, timeout dihormati)", errTimeout, durTimeout)
}
