package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config menyimpan konfigurasi direktori dan path basis data
type Config struct {
	BaseDir   string
	DataDir   string
	BackupDir string
	DBPath    string
}

// GetExecutableDir mengembalikan direktori tempat biner executable berada.
func GetExecutableDir() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("gagal mendapatkan path executable: %w", err)
	}
	// Evaluasi symlink jika ada
	realPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		realPath = execPath
	}
	return filepath.Dir(realPath), nil
}

// CheckWritable memverifikasi apakah sebuah direktori memiliki izin tulis.
// Menulis probe file temporer untuk memastikan izin tulis filesystem.
func CheckWritable(dir string) error {
	// Pastikan direktori ada terlebih dahulu
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("direktori tidak dapat dibuat atau tidak memiliki izin tulis: %s (%w). Harap letakkan aplikasi di lokasi yang memiliki hak akses tulis", dir, err)
	}

	testFile := filepath.Join(dir, fmt.Sprintf(".write_probe_%d.tmp", os.Getpid()))
	f, err := os.OpenFile(testFile, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("direktori tidak memiliki izin tulis: %s (%w). Harap pindahkan aplikasi ke direktori yang memiliki hak akses tulis", dir, err)
	}
	_ = f.Close()
	_ = os.Remove(testFile)
	return nil
}

// ResolveConfig menginisialisasi path direktori dan data/compliance.db relatif terhadap baseDir.
// Jika baseDir kosong, lokasi executable akan digunakan.
func ResolveConfig(baseDir string) (*Config, error) {
	if baseDir == "" {
		execDir, err := GetExecutableDir()
		if err != nil {
			return nil, err
		}
		baseDir = execDir
	}

	dataDir := filepath.Join(baseDir, "data")
	if err := CheckWritable(dataDir); err != nil {
		return nil, err
	}

	backupDir := filepath.Join(baseDir, "backups")
	if err := CheckWritable(backupDir); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataDir, "compliance.db")
	return &Config{
		BaseDir:   baseDir,
		DataDir:   dataDir,
		BackupDir: backupDir,
		DBPath:    dbPath,
	}, nil
}
