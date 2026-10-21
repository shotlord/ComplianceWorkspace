package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
)

// TestBatch09_HTMLModalSimplification verifies that index.html contains the simplified
// permanent delete and restore modals, without spinner/preview/code input dependencies.
func TestBatch09_HTMLModalSimplification(t *testing.T) {
	htmlPath := filepath.Join("..", "web", "static", "index.html")
	if _, err := os.Stat(htmlPath); err != nil {
		htmlPath = filepath.Join("web", "static", "index.html")
	}

	contentBytes, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("Gagal membaca index.html: %v", err)
	}
	html := string(contentBytes)

	// 1. Verifikasi Modal Permanent Delete
	t.Run("ModalPermanentDelete_SimplifiedStructure", func(t *testing.T) {
		if !strings.Contains(html, `id="modalPermanentDeleteWork"`) {
			t.Fatal("modalPermanentDeleteWork harus ada di index.html")
		}
		if !strings.Contains(html, `id="deleteWorkCodeDisplay"`) {
			t.Error("deleteWorkCodeDisplay harus ada di index.html")
		}
		if !strings.Contains(html, `id="deleteWorkTitleDisplay"`) {
			t.Error("deleteWorkTitleDisplay harus ada di index.html")
		}
		if !strings.Contains(html, `id="checkAcknowledgeDeleteWork"`) {
			t.Error("checkAcknowledgeDeleteWork harus ada di index.html")
		}
		expectedDelAck := "Saya memahami bahwa pekerjaan ini akan dihapus dari daftar operasional, subtask terkait dihapus, dan riwayat aktivitas tetap dipertahankan untuk kebutuhan audit. Saya menyetujui tindakan ini."
		if !strings.Contains(html, expectedDelAck) {
			t.Errorf("Teks klausul persetujuan hapus permanen tidak cocok persis. Ekspektasi: %s", expectedDelAck)
		}

		// Elemen lama yang WAJIB DIBERSIHKAN
		if strings.Contains(html, `id="deleteWorkPreviewInfo"`) {
			t.Error("deleteWorkPreviewInfo seharusnya sudah dihapus dari index.html")
		}
		if strings.Contains(html, `id="confirmDeleteWorkCode"`) {
			t.Error("confirmDeleteWorkCode seharusnya sudah dihapus dari index.html")
		}
	})

	// 2. Verifikasi Modal Restore
	t.Run("ModalRestoreConfirm_SimplifiedStructure", func(t *testing.T) {
		if !strings.Contains(html, `id="modalRestoreConfirm"`) {
			t.Fatal("modalRestoreConfirm harus ada di index.html")
		}
		if !strings.Contains(html, `Pemulihan Database`) {
			t.Error("Judul modal harus disederhanakan menjadi 'Pemulihan Database'")
		}
		if !strings.Contains(html, `id="restoreBackupFilenameDisplay"`) {
			t.Error("restoreBackupFilenameDisplay harus ada di index.html")
		}
		if !strings.Contains(html, `id="checkAcknowledgeRestoreRisk"`) {
			t.Error("checkAcknowledgeRestoreRisk harus ada di index.html")
		}
		expectedRstAck := "Saya memahami bahwa pemulihan akan mengganti database aktif dan tidak dapat dibatalkan melalui aplikasi. Salinan pengaman otomatis akan dibuat sebelum pemulihan."
		if !strings.Contains(html, expectedRstAck) {
			t.Errorf("Teks klausul persetujuan pemulihan cadangan tidak cocok persis. Ekspektasi: %s", expectedRstAck)
		}

		// Elemen lama yang WAJIB DIBERSIHKAN
		if strings.Contains(html, `id="restoreInspectionSummary"`) {
			t.Error("restoreInspectionSummary seharusnya sudah dihapus dari index.html")
		}
		if strings.Contains(html, `id="restoreComparisonTableBody"`) {
			t.Error("restoreComparisonTableBody seharusnya sudah dihapus dari index.html")
		}
	})
}

// TestBatch09_PermanentDeleteAndRestoreDirectFlow menguji flow langsung backend
// sesuai penyederhanaan modal UAT Batch 09 tanpa memanggil endpoint inspect/preview.
func TestBatch09_PermanentDeleteAndRestoreDirectFlow(t *testing.T) {
	_, _, _, workSvc, _, backupSvc, _, _, baseURL, cleanup := setupBatch08Environment(t)
	defer cleanup()

	client, csrfToken, csrfCookie := getAuthorizedCSRFClient(t, baseURL)
	ctx := context.Background()

	// 1. Buat pekerjaan
	dueOn := "2026-12-31"
	created, err := workSvc.CreateWorkItem(ctx, domain.CreateWorkItemInput{
		Title:       "Pekerjaan Audit Batch 09",
		Description: "Uji simplifikasi permanent delete dan restore langsung",
		Category:    "Audit",
		RequestedBy: "UAT Lead",
		ReceivedOn:  "2026-10-18",
		DueOn:       &dueOn,
		Priority:    domain.PriorityHigh,
	})
	if err != nil {
		t.Fatalf("Gagal membuat pekerjaan: %v", err)
	}

	if created.ID == 0 || created.WorkCode == "" {
		t.Fatalf("Pekerjaan baru tidak valid: %+v", created)
	}

	// Buat backup awal (sebelum arsip dan hapus)
	backupRes, err := backupSvc.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("Gagal membuat backup awal: %v", err)
	}

	// 2. Arsipkan pekerjaan agar memenuhi syarat penghapusan permanen
	_, err = workSvc.ArchiveWorkItem(ctx, created.ID)
	if err != nil {
		t.Fatalf("Gagal arsip pekerjaan: %v", err)
	}

	// 3. Eksekusi DELETE /api/work-items/{id} langsung tanpa delete-preview
	delPayload := map[string]string{
		"confirm_code": created.WorkCode,
	}
	delBody, _ := json.Marshal(delPayload)
	delReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/work-items/%d", baseURL, created.ID), bytes.NewReader(delBody))
	delReq.Header.Set("Content-Type", "application/json")
	delReq.Header.Set("X-CSRF-Token", csrfToken)
	if csrfCookie != nil {
		delReq.AddCookie(csrfCookie)
	}
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Fatalf("Gagal DELETE pekerjaan: %v", err)
	}
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("Ekspektasi 200 OK saat delete permanen langsung, didapat: %d", delResp.StatusCode)
	}
	delResp.Body.Close()

	// Verifikasi pekerjaan terhapus dari database operasional
	workRepo := repository.NewWorkItemRepository(workSvc.GetDB())
	_, err = workRepo.GetByID(ctx, created.ID)
	if err == nil {
		t.Error("Pekerjaan seharusnya sudah tidak ada di database operasional")
	}

	// 4. Eksekusi POST /api/backups/restore langsung tanpa inspect-backup
	restorePayload := map[string]string{
		"filename": backupRes.Filename,
	}
	rstBody, _ := json.Marshal(restorePayload)
	rstReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/backups/restore", bytes.NewReader(rstBody))
	rstReq.Header.Set("Content-Type", "application/json")
	rstReq.Header.Set("X-CSRF-Token", csrfToken)
	if csrfCookie != nil {
		rstReq.AddCookie(csrfCookie)
	}
	rstResp, err := client.Do(rstReq)
	if err != nil {
		t.Fatalf("Gagal POST /api/backups/restore: %v", err)
	}
	if rstResp.StatusCode != http.StatusOK {
		t.Fatalf("Ekspektasi 200 OK saat pemulihan langsung, didapat: %d", rstResp.StatusCode)
	}
	rstResp.Body.Close()

	// Verifikasi pekerjaan pulih kembali dari backup
	restoredRepo := repository.NewWorkItemRepository(workSvc.GetDB())
	restoredItem, err := restoredRepo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("Pekerjaan seharusnya telah pulih setelah restore: %v", err)
	}
	if restoredItem.WorkCode != created.WorkCode {
		t.Errorf("Kode pekerjaan pulih tidak cocok: ekspektasi %s, didapat %s", created.WorkCode, restoredItem.WorkCode)
	}
}
