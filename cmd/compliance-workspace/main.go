package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"compliance-workspace/internal/config"
	"compliance-workspace/internal/database"
	"compliance-workspace/internal/repository"
	"compliance-workspace/internal/server"
	"compliance-workspace/internal/service"
)

func main() {
	portFlag := flag.Int("port", 0, "Nomor port HTTP (0 untuk alokasi port dinamis oleh OS)")
	flag.Parse()

	log.Println("[INFO] Menginisialisasi Compliance Workspace v1.0...")

	cfg, err := config.ResolveConfig("")
	if err != nil {
		log.Fatalf("[FATAL] Kesalahan konfigurasi direktori: %v", err)
	}

	log.Printf("[INFO] Lokasi basis data: %s", cfg.DBPath)

	db, err := database.OpenDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("[FATAL] Gagal membuka basis data: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := database.Migrate(ctx, db); err != nil {
		log.Fatalf("[FATAL] Gagal menjalankan migrasi basis data: %v", err)
	}
	log.Println("[INFO] Migrasi basis data berhasil diterapkan.")

	workRepo := repository.NewWorkItemRepository(db)
	activityRepo := repository.NewActivityLogRepository(db)
	workItemService := service.NewWorkItemService(db, workRepo, activityRepo)
	exportService := service.NewExportService(workRepo, activityRepo)
	backupService := service.NewBackupService(workItemService, cfg.DBPath, cfg.BackupDir)

	bindAddr := fmt.Sprintf("127.0.0.1:%d", *portFlag)
	srv, err := server.NewServer(bindAddr, workItemService, backupService, exportService)
	if err != nil {
		log.Fatalf("[FATAL] Gagal menginisialisasi server: %v", err)
	}

	log.Printf("[INFO] Server aktif dan mendengarkan pada: http://%s", srv.Addr())
	log.Println("[INFO] Tekan Ctrl+C untuk menghentikan aplikasi.")

	// Jalankan server di background goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Tangani sinyal OS untuk graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Fatalf("[FATAL] Kesalahan pada server HTTP: %v", err)
	case sig := <-quit:
		log.Printf("[INFO] Menerima sinyal %s, mematikan server dengan aman...", sig)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[ERROR] Kesalahan saat mematikan server: %v", err)
	}

	log.Println("[INFO] Compliance Workspace telah dimatikan.")
}
