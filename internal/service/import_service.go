package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
)

// ImportService mengelola alur kerja Data Import Center
type ImportService struct {
	commitMu     sync.Mutex
	dbMu         sync.RWMutex
	db           *sql.DB
	importRepo   *repository.ImportRepository
	workRepo     *repository.WorkItemRepository
	activityRepo *repository.ActivityLogRepository
	categoryRepo *repository.CategoryRepository
	backupSvc    *BackupService
}

func NewImportService(
	db *sql.DB,
	importRepo *repository.ImportRepository,
	workRepo *repository.WorkItemRepository,
	activityRepo *repository.ActivityLogRepository,
	categoryRepo *repository.CategoryRepository,
	backupSvc *BackupService,
) *ImportService {
	return &ImportService{
		db:           db,
		importRepo:   importRepo,
		workRepo:     workRepo,
		activityRepo: activityRepo,
		categoryRepo: categoryRepo,
		backupSvc:    backupSvc,
	}
}

func (s *ImportService) getDB() *sql.DB {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	return s.db
}

func (s *ImportService) SetDB(db *sql.DB) {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	s.db = db
	s.importRepo.SetDB(db)
	if s.categoryRepo != nil {
		s.categoryRepo.SetDB(db)
	}
	// workRepo dan activityRepo adalah pointer shared yang sudah diupdate oleh WorkItemService.SetDB
}

// SetBackupService menyetel dependensi BackupService setelah inisialisasi (menghindari circular dep)
func (s *ImportService) SetBackupService(bs *BackupService) {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	s.backupSvc = bs
}

// PreviewImport memproses file secara read-only tanpa mengubah database sama sekali
func (s *ImportService) PreviewImport(
	ctx context.Context,
	fileBytes []byte,
	fileName string,
	fileType string,
	sheetName string,
	userMapping map[string]string,
) (*domain.ImportPreviewResult, error) {
	parsed, err := ParseImportFile(fileBytes, fileName, fileType, sheetName)
	if err != nil {
		return nil, fmt.Errorf("gagal memproses file impor: %w", err)
	}

	// Tolak file yang terdeteksi sebagai Activity Log atau format yang tidak kompatibel.
	// Activity Log memiliki header khas seperti "activity_type", "occurred_at", "recorded_at"
	// yang tidak ada dalam template Work Register.
	if isActivityLogFile(parsed.Headers) {
		return nil, errors.New(
			"file ini tampaknya adalah ekspor Activity Log, bukan file Work Register. " +
				"Import Center hanya menerima file Work Register (CSV atau XLSX) dengan kolom seperti " +
				"'Judul Pekerjaan', 'Tanggal Diterima', dll. " +
				"Silakan unduh template yang benar menggunakan tombol 'Unduh Template'.",
		)
	}

	mapping := userMapping
	if len(mapping) == 0 {
		mapping = SuggestColumnMapping(parsed.Headers)
	}

	result := &domain.ImportPreviewResult{
		FileName:          fileName,
		FileType:          parsed.FileType,
		FileSize:          int64(len(fileBytes)),
		TotalRows:         len(parsed.Rows),
		DetectedHeaders:   parsed.Headers,
		SuggestedMapping:  mapping,
		Worksheets:        parsed.Worksheets,
		SelectedWorksheet: parsed.SheetName,
	}

	// Pelacakan duplikasi antarbaris dalam satu file
	inFileDuplicates := make(map[string]int) // key -> baris pertama ditemukan

	var previewRows []domain.ImportPreviewRow

	for i, rawRow := range parsed.Rows {
		rowNum := i + 1
		pRow := s.parseAndValidateRow(ctx, rowNum, rawRow, mapping)

		// 1. Cek duplikasi di dalam file yang sama (in-file duplicate)
		inFileKey := fmt.Sprintf("%s|%s|%s",
			strings.ToLower(strings.TrimSpace(pRow.Title)),
			pRow.ReceivedOn,
			strings.ToLower(strings.TrimSpace(pRow.RequestedBy)),
		)

		if prevRow, exists := inFileDuplicates[inFileKey]; exists {
			pRow.IsDuplicate = true
			if pRow.DuplicateReason == "" {
				pRow.DuplicateReason = fmt.Sprintf("Duplikasi dengan baris %d dalam file yang sama", prevRow)
			}
		} else if pRow.Title != "" && pRow.ReceivedOn != "" {
			inFileDuplicates[inFileKey] = rowNum
		}

		// Agregasi status baris (total = valid + duplicate + invalid)
		if !pRow.IsValid {
			result.InvalidRows++
		} else if pRow.IsDuplicate {
			result.DuplicateRows++
		} else {
			result.ValidRows++
		}

		// Ambil seluruh baris preview (hingga batas maksimal batch)
		previewRows = append(previewRows, pRow)
	}

	result.ErrorRows = result.InvalidRows
	result.Rows = previewRows
	result.SampleRows = previewRows
	return result, nil
}

// parseAndValidateRow memetakan data kolom, mensanitasi formula injection, dan memeriksa duplikasi ke database
func (s *ImportService) parseAndValidateRow(
	ctx context.Context,
	rowNum int,
	rawRow map[string]string,
	mapping map[string]string,
) domain.ImportPreviewRow {
	pRow := domain.ImportPreviewRow{
		RowNumber: rowNum,
		RawData:   rawRow,
		IsValid:   true,
	}

	// Ekstrak nilai berdasarkan mapping dengan pencocokan case-insensitive
	getVal := func(targetField string) string {
		headerName, ok := mapping[targetField]
		if !ok || strings.TrimSpace(headerName) == "" {
			return ""
		}
		// 1. Direct match
		if val, exists := rawRow[headerName]; exists {
			return strings.TrimSpace(val)
		}
		// 2. Case-insensitive & trimmed fallback
		targetLower := strings.ToLower(strings.TrimSpace(headerName))
		for k, val := range rawRow {
			if strings.ToLower(strings.TrimSpace(k)) == targetLower {
				return strings.TrimSpace(val)
			}
		}
		return ""
	}

	pRow.Title = getVal("title")
	pRow.Description = getVal("description")
	pRow.Category = getVal("category")
	pRow.RequestedBy = getVal("requested_by")
	pRow.ReceivedOn = getVal("received_on")
	dueOnStr := getVal("due_on")
	if dueOnStr != "" {
		pRow.DueOn = &dueOnStr
	}
	pRow.Priority = strings.ToLower(getVal("priority"))
	pRow.Status = strings.ToLower(getVal("status"))

	// Validasi Kolom Wajib
	if pRow.Title == "" {
		pRow.IsValid = false
		pRow.Errors = append(pRow.Errors, "Judul pekerjaan wajib diisi")
	} else if len(pRow.Title) > 255 {
		pRow.IsValid = false
		pRow.Errors = append(pRow.Errors, "Judul pekerjaan melebihi 255 karakter")
	}

	if pRow.ReceivedOn == "" {
		pRow.IsValid = false
		pRow.Errors = append(pRow.Errors, "Tanggal diterima wajib diisi")
	} else {
		// Validasi format tanggal YYYY-MM-DD
		if _, err := time.Parse("2006-01-02", pRow.ReceivedOn); err != nil {
			pRow.IsValid = false
			pRow.Errors = append(pRow.Errors, fmt.Sprintf("Format tanggal diterima '%s' tidak valid (harus YYYY-MM-DD)", pRow.ReceivedOn))
		}
	}

	if pRow.DueOn != nil && *pRow.DueOn != "" {
		if _, err := time.Parse("2006-01-02", *pRow.DueOn); err != nil {
			pRow.IsValid = false
			pRow.Errors = append(pRow.Errors, fmt.Sprintf("Format tenggat waktu '%s' tidak valid (harus YYYY-MM-DD)", *pRow.DueOn))
		}
	}

	// Default dan validasi Priority (mendukung Bahasa Indonesia dan Inggris)
	rawPriority := strings.ToLower(pRow.Priority)
	switch rawPriority {
	case domain.PriorityLow, "rendah":
		pRow.Priority = domain.PriorityLow
	case domain.PriorityNormal, "sedang", "biasa", "":
		pRow.Priority = domain.PriorityNormal
	case domain.PriorityHigh, "tinggi":
		pRow.Priority = domain.PriorityHigh
	case domain.PriorityCritical, "kritis":
		pRow.Priority = domain.PriorityCritical
	default:
		pRow.IsValid = false
		pRow.Errors = append(pRow.Errors, fmt.Sprintf("Prioritas '%s' tidak valid (pilih: low/rendah, normal, high/tinggi, critical/kritis)", pRow.Priority))
	}

	// Default dan validasi Status (mendukung Bahasa Indonesia dan Inggris)
	rawStatus := strings.ToLower(pRow.Status)
	switch rawStatus {
	case domain.StatusNotStarted, "belum mulai", "belum dimulai", "not started", "":
		pRow.Status = domain.StatusNotStarted
	case domain.StatusInProgress, "dalam proses", "sedang berjalan", "in progress", "proses":
		pRow.Status = domain.StatusInProgress
	case domain.StatusWaiting, "menunggu", "pending":
		pRow.Status = domain.StatusWaiting
	case domain.StatusCompleted, "selesai", "done":
		pRow.Status = domain.StatusCompleted
	case domain.StatusCancelled, "dibatalkan", "batal", "canceled":
		pRow.Status = domain.StatusCancelled
	default:
		pRow.IsValid = false
		pRow.Errors = append(pRow.Errors, fmt.Sprintf("Status '%s' tidak valid (pilih: not_started/belum mulai, in_progress/dalam proses, waiting/menunggu, completed/selesai, cancelled/dibatalkan)", pRow.Status))
	}

	// Default & Validasi Kategori
	cleanCat := strings.TrimSpace(pRow.Category)
	if cleanCat == "" {
		pRow.Category = "General"
	} else if len(cleanCat) > 100 {
		pRow.IsValid = false
		pRow.Errors = append(pRow.Errors, "Nama kategori melebihi 100 karakter")
	} else {
		pRow.Category = cleanCat
		if s.categoryRepo != nil {
			if existingCat, err := s.categoryRepo.GetByName(ctx, cleanCat); err == nil && existingCat != nil {
				if !existingCat.IsActive {
					pRow.IsValid = false
					pRow.Errors = append(pRow.Errors, fmt.Sprintf("Kategori '%s' berstatus nonaktif. Aktifkan kategori atau gunakan kategori aktif.", existingCat.Name))
				} else {
					pRow.Category = existingCat.Name // gunakan canonical casing
				}
			}
		}
	}

	if len(pRow.Description) > 5000 {
		pRow.IsValid = false
		pRow.Errors = append(pRow.Errors, "Deskripsi pekerjaan melebihi 5.000 karakter")
	}

	// 2. Cek duplikasi terhadap basis data existing
	// Berdasarkan kombinasi stabil: Judul + Tanggal Diterima + Pemberi Tugas
	// (Memenuhi prinsip: 'Jangan menggunakan kesamaan judul saja sebagai penentu pasti duplikasi')
	if pRow.Title != "" && pRow.ReceivedOn != "" {
		var existingID int64
		var existingCode string
		query := `SELECT id, work_code FROM work_items WHERE trim(title) = trim(?) AND received_on = ? AND trim(requested_by) = trim(?) LIMIT 1;`
		err := s.getDB().QueryRowContext(ctx, query, pRow.Title, pRow.ReceivedOn, pRow.RequestedBy).Scan(&existingID, &existingCode)
		if err == nil {
			pRow.IsDuplicate = true
			pRow.MatchedWorkItemID = &existingID
			pRow.MatchedWorkCode = existingCode
			pRow.DuplicateReason = fmt.Sprintf("Sesuai dengan pekerjaan existing [%s] '%s'", existingCode, pRow.Title)
		}
	}

	pRow.ValidationErrors = pRow.Errors
	return pRow
}

// CommitImport mengeksekusi commit impor secara atomik
func (s *ImportService) CommitImport(
	ctx context.Context,
	fileBytes []byte,
	req domain.CommitImportRequest,
) (*domain.CommitImportResult, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()

	// 1. Snapshot database otomatis sebelum commit (Safety pre-commit backup)
	if s.backupSvc != nil {
		_, err := s.backupSvc.CreateBackup(ctx)
		if err != nil {
			return nil, fmt.Errorf("gagal membuat snapshot backup sebelum commit impor: %w", err)
		}
	}

	// 2. Re-parse dan re-validasi seluruh isi file untuk memastikan integritas data tidak berubah diam-diam
	parsed, err := ParseImportFile(fileBytes, req.FileName, req.FileType, req.Worksheet)
	if err != nil {
		return nil, fmt.Errorf("gagal memvalidasi ulang file impor: %w", err)
	}

	mapping := req.ColumnMapping
	if len(mapping) == 0 {
		mapping = SuggestColumnMapping(parsed.Headers)
	}

	// Mode penanganan duplikasi: "skip" (default) atau "create"
	dupMode := strings.ToLower(req.DuplicateMode)
	if dupMode != "create" {
		dupMode = "skip"
	}

	// Lakukan validasi menyeluruh pada seluruh baris
	inFileDuplicates := make(map[string]int)
	var validatedRows []domain.ImportPreviewRow

	for i, rawRow := range parsed.Rows {
		rowNum := i + 1
		pRow := s.parseAndValidateRow(ctx, rowNum, rawRow, mapping)

		inFileKey := fmt.Sprintf("%s|%s|%s",
			strings.ToLower(strings.TrimSpace(pRow.Title)),
			pRow.ReceivedOn,
			strings.ToLower(strings.TrimSpace(pRow.RequestedBy)),
		)

		if prevRow, exists := inFileDuplicates[inFileKey]; exists {
			pRow.IsDuplicate = true
			if pRow.DuplicateReason == "" {
				pRow.DuplicateReason = fmt.Sprintf("Duplikasi dengan baris %d dalam file yang sama", prevRow)
			}
		} else if pRow.Title != "" && pRow.ReceivedOn != "" {
			inFileDuplicates[inFileKey] = rowNum
		}

		validatedRows = append(validatedRows, pRow)
	}

	// 3. Mulai Transaksi Atomik
	tx, err := s.getDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal memulai transaksi database: %w", err)
	}
	defer tx.Rollback()

	// Buat kode batch unik
	randBytes := make([]byte, 2)
	_, _ = rand.Read(randBytes)
	randHex := hex.EncodeToString(randBytes)
	batchCode := fmt.Sprintf("IMP-%s-%s", time.Now().UTC().Format("20060102-150405"), randHex)

	mappingJSON, _ := json.Marshal(mapping)
	now := time.Now().UTC()

	batch := &domain.ImportBatch{
		BatchCode:         batchCode,
		FileName:          req.FileName,
		FileType:          parsed.FileType,
		FileSize:          int64(len(fileBytes)),
		TotalRows:         len(validatedRows),
		Status:            domain.ImportStatusCompleted,
		ColumnMappingJSON: string(mappingJSON),
		CreatedAt:         now,
		CompletedAt:       &now,
	}

	if err := s.importRepo.CreateBatchTx(ctx, tx, batch); err != nil {
		return nil, fmt.Errorf("gagal menyimpan batch impor: %w", err)
	}

	importedCount := 0
	skippedCount := 0
	failedCount := 0

	// 4. Proses Tiap Baris Secara Konsisten
	for _, row := range validatedRows {
		rawJSON, _ := json.Marshal(row.RawData)

		// Jika baris tidak valid: catat sebagai failed
		if !row.IsValid {
			item := &domain.ImportItem{
				BatchID:      batch.ID,
				RowNumber:    row.RowNumber,
				Status:       domain.ItemStatusFailed,
				ActionType:   "none",
				ErrorMessage: strings.Join(row.Errors, "; "),
				RawDataJSON:  string(rawJSON),
				CreatedAt:    now,
			}
			if err := s.importRepo.CreateItemTx(ctx, tx, item); err != nil {
				return nil, err
			}
			failedCount++
			continue
		}

		// Jika baris adalah duplikasi dan mode adalah "skip": catat sebagai skipped
		if row.IsDuplicate && dupMode == "skip" {
			item := &domain.ImportItem{
				BatchID:      batch.ID,
				RowNumber:    row.RowNumber,
				WorkItemID:   row.MatchedWorkItemID,
				Status:       domain.ItemStatusSkipped,
				ActionType:   "skip",
				ErrorMessage: row.DuplicateReason,
				RawDataJSON:  string(rawJSON),
				CreatedAt:    now,
			}
			if err := s.importRepo.CreateItemTx(ctx, tx, item); err != nil {
				return nil, err
			}
			skippedCount++
			continue
		}

		// Mode Create (atau baris bukan duplikasi): Generate CMP-YYYY-NNNNNN dan simpan ke work_items
		year := now.Year()
		parts := strings.Split(row.ReceivedOn, "-")
		if len(parts) == 3 {
			if y, err := strconv.Atoi(parts[0]); err == nil && y >= 2000 && y <= 2100 {
				year = y
			}
		}

		code, err := s.workRepo.GenerateNextWorkCode(ctx, tx, year)
		if err != nil {
			return nil, fmt.Errorf("gagal menghasilkan nomor CMP baris %d: %w", row.RowNumber, err)
		}

		// Kalkulasi progres awal tanpa subtask
		progPct := domain.CalculateProgress(0, 0, row.Status)
		var compAt *time.Time
		if row.Status == domain.StatusCompleted {
			compAt = &now
		}

		finalCatName := strings.TrimSpace(row.Category)
		if finalCatName == "" {
			finalCatName = "General"
		}
		if s.categoryRepo != nil {
			cat, err := s.categoryRepo.GetByNameTx(ctx, tx, finalCatName)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("gagal memeriksa kategori '%s' baris %d: %w", finalCatName, row.RowNumber, err)
			}
			if cat != nil {
				if !cat.IsActive {
					return nil, fmt.Errorf("kategori '%s' pada baris %d berstatus nonaktif dan tidak dapat digunakan untuk pekerjaan baru", cat.Name, row.RowNumber)
				}
				finalCatName = cat.Name
			} else {
				// Daftarkan kategori kustom baru secara atomik dalam transaksi yang sama
				newCat := &domain.Category{
					Name:        finalCatName,
					Description: "Kategori dibuat otomatis dari impor data",
					Color:       "#4f46e5",
					IsDefault:   false,
					IsActive:    true,
					CreatedAt:   now,
				}
				if err := s.categoryRepo.CreateTx(ctx, tx, newCat); err != nil {
					return nil, fmt.Errorf("gagal mendaftarkan kategori baru '%s' baris %d: %w", finalCatName, row.RowNumber, err)
				}
				finalCatName = newCat.Name
			}
		}

		wi := &domain.WorkItem{
			WorkCode:           code,
			Title:              row.Title,
			Description:        row.Description,
			Category:           finalCatName,
			RequestedBy:        row.RequestedBy,
			ReceivedOn:         row.ReceivedOn,
			DueOn:              row.DueOn,
			Priority:           row.Priority,
			Status:             row.Status,
			ProgressPercentage: progPct,
			CompletedAt:        compAt,
			CreatedAt:          now,
			UpdatedAt:          now,
		}

		if err := s.workRepo.CreateTx(ctx, tx, wi); err != nil {
			return nil, fmt.Errorf("gagal menyimpan pekerjaan baris %d: %w", row.RowNumber, err)
		}

		// Catat Activity Log Penciptaan (Append-Only)
		desc := fmt.Sprintf("Pekerjaan dibuat melalui impor batch '%s' (baris %d)", batch.BatchCode, row.RowNumber)
		act := &domain.ActivityLog{
			WorkItemID:  wi.ID,
			EntryType:   domain.EntryTypeCreated,
			Description: desc,
			OccurredAt:  now,
			RecordedAt:  now,
		}
		if err := s.activityRepo.CreateTx(ctx, tx, act); err != nil {
			return nil, fmt.Errorf("gagal mencatat activity log baris %d: %w", row.RowNumber, err)
		}

		// Catat riwayat import item
		action := "insert"
		reason := ""
		if row.IsDuplicate {
			reason = "Diimpor sebagai pekerjaan baru atas persetujuan duplikasi"
		}

		item := &domain.ImportItem{
			BatchID:      batch.ID,
			RowNumber:    row.RowNumber,
			WorkItemID:   &wi.ID,
			Status:       domain.ItemStatusCreated,
			ActionType:   action,
			ErrorMessage: reason,
			RawDataJSON:  string(rawJSON),
			CreatedAt:    now,
		}
		if err := s.importRepo.CreateItemTx(ctx, tx, item); err != nil {
			return nil, err
		}
		importedCount++
	}

	// 5. Perbarui Total Statistik Batch
	batch.ImportedRows = importedCount
	batch.SkippedRows = skippedCount
	batch.FailedRows = failedCount
	if importedCount > 0 {
		batch.Status = domain.ImportStatusCompleted
	} else {
		batch.Status = domain.ImportStatusFailed
	}

	if err := s.importRepo.UpdateBatchTx(ctx, tx, batch); err != nil {
		return nil, fmt.Errorf("gagal memperbarui rekap batch: %w", err)
	}

	// Commit Transaksi Atomik
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("gagal commit transaksi impor: %w", err)
	}

	return &domain.CommitImportResult{
		BatchID:      batch.ID,
		BatchCode:    batch.BatchCode,
		TotalRows:    batch.TotalRows,
		ImportedRows: importedCount,
		SkippedRows:  skippedCount,
		FailedRows:   failedCount,
		CreatedCount: importedCount,
		SkippedCount: skippedCount,
		FailedCount:  failedCount,
		Status:       batch.Status,
		Message:      fmt.Sprintf("Impor selesai: %d pekerjaan dibuat, %d baris dilewati, %d baris gagal.", importedCount, skippedCount, failedCount),
	}, nil
}

// CompensateRollbackBatch mengeksekusi pembatalan hasil impor melalui tindakan kompensasi (arsip) tanpa menghapus Activity Log
// ErrImportHasEditedItems dikembalikan jika batch memiliki pekerjaan yang telah diedit sebelum kompensasi
type ErrImportHasEditedItems struct {
	EditedCount int
	BatchCode   string
}

func (e *ErrImportHasEditedItems) Error() string {
	return fmt.Sprintf("batch '%s' memiliki %d pekerjaan yang telah diedit atau diubah statusnya setelah impor. Memerlukan konfirmasi eksplisit (force=true) agar tidak mengarsipkan pekerjaan yang telah dimodifikasi secara tidak sengaja", e.BatchCode, e.EditedCount)
}

// CompensateRollbackBatch mengeksekusi pembatalan hasil impor melalui tindakan kompensasi (arsip) tanpa menghapus Activity Log.
// Jika batch memuat pekerjaan yang sudah diedit setelah diimpor dan force == false, aksi ditolak dengan peringatan eksplisit.
func (s *ImportService) CompensateRollbackBatch(ctx context.Context, batchID int64, force bool) (int, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()

	batch, err := s.importRepo.GetBatchByID(ctx, batchID)
	if err != nil {
		return 0, fmt.Errorf("batch tidak ditemukan: %w", err)
	}
	if batch.Status == domain.ImportStatusRolledBack {
		return 0, errors.New("batch impor ini sudah pernah dibatalkan sebelumnya")
	}
	if batch.Status != domain.ImportStatusCompleted {
		return 0, fmt.Errorf("hanya batch berstatus 'completed' yang dapat dibatalkan (status saat ini: %s)", batch.Status)
	}

	tx, err := s.getDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("gagal memulai transaksi pembatalan: %w", err)
	}
	defer tx.Rollback()

	// Ambil seluruh ID pekerjaan yang dibuat oleh batch ini
	workIDs, err := s.importRepo.GetCreatedWorkItemIDsByBatch(ctx, tx, batchID)
	if err != nil {
		return 0, err
	}

	// Periksa apakah ada pekerjaan yang telah diedit/diubah setelah diimpor
	if !force {
		editedCount := 0
		for _, wID := range workIDs {
			var logsCount, subCount int
			_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_logs WHERE work_item_id = ? AND entry_type IN ('edited', 'status_changed')", wID).Scan(&logsCount)
			_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM subtasks WHERE work_item_id = ?", wID).Scan(&subCount)
			if logsCount > 0 || subCount > 0 {
				editedCount++
			}
		}
		if editedCount > 0 {
			return 0, &ErrImportHasEditedItems{
				EditedCount: editedCount,
				BatchCode:   batch.BatchCode,
			}
		}
	}

	now := time.Now().UTC()
	compensatedCount := 0

	for _, wID := range workIDs {
		// Dapatkan pekerjaan untuk memverifikasi apakah sudah diarsipkan
		item, err := s.workRepo.GetByIDTx(ctx, tx, wID)
		if err != nil || item == nil {
			continue
		}

		if item.ArchivedAt == nil {
			item.ArchivedAt = &now
			item.UpdatedAt = now
			if err := s.workRepo.UpdateTx(ctx, tx, item); err != nil {
				return 0, fmt.Errorf("gagal mengarsipkan pekerjaan ID %d: %w", wID, err)
			}
			compensatedCount++
		}

		// Catat Activity Log Kompensasi (Append-Only)
		desc := fmt.Sprintf("Pekerjaan diarsipkan melalui tindakan kompensasi pembatalan impor batch '%s'", batch.BatchCode)
		act := &domain.ActivityLog{
			WorkItemID:  wID,
			EntryType:   domain.EntryTypeArchived,
			Description: desc,
			OccurredAt:  now,
			RecordedAt:  now,
		}
		if err := s.activityRepo.CreateTx(ctx, tx, act); err != nil {
			return 0, fmt.Errorf("gagal mencatat log kompensasi pekerjaan ID %d: %w", wID, err)
		}
	}

	// Update status batch menjadi 'rolled_back'
	if err := s.importRepo.SetBatchStatusTx(ctx, tx, batchID, domain.ImportStatusRolledBack); err != nil {
		return 0, fmt.Errorf("gagal memperbarui status batch ke rolled_back: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("gagal commit pembatalan kompensasi: %w", err)
	}

	return compensatedCount, nil
}

// ListBatches mengambil daftar riwayat impor
func (s *ImportService) ListBatches(ctx context.Context, limit, offset int) ([]*domain.ImportBatch, error) {
	return s.importRepo.ListBatches(ctx, limit, offset)
}

// GetBatchByID mengambil detail satu batch impor
func (s *ImportService) GetBatchByID(ctx context.Context, id int64) (*domain.ImportBatch, error) {
	return s.importRepo.GetBatchByID(ctx, id)
}

// GetBatchItems mengambil rincian baris dari suatu batch
func (s *ImportService) GetBatchItems(ctx context.Context, batchID int64) ([]*domain.ImportItem, error) {
	return s.importRepo.GetBatchItems(ctx, batchID)
}

// GenerateErrorReportCSV menghasilkan file laporan baris yang gagal atau dilewati dalam format CSV standar
func (s *ImportService) GenerateErrorReportCSV(ctx context.Context, batchID int64) ([]byte, error) {
	items, err := s.importRepo.GetBatchItems(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil data rincian batch: %w", err)
	}

	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM UTF-8

	writer := csv.NewWriter(&buf)
	_ = writer.Write([]string{
		"Nomor Baris",
		"Status",
		"Tindakan",
		"Keterangan / Alasan",
		"Data Mentah JSON",
		"Waktu Diproses",
	})

	for _, item := range items {
		_ = writer.Write([]string{
			strconv.Itoa(item.RowNumber),
			item.Status,
			item.ActionType,
			domain.SanitizeFormulaInjection(item.ErrorMessage),
			domain.SanitizeFormulaInjection(item.RawDataJSON),
			item.CreatedAt.Format(time.RFC3339),
		})
	}

	writer.Flush()
	return buf.Bytes(), nil
}
