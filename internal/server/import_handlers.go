package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/service"
)

// -----------------------------------------------------------------------------
// Data Import Center Handlers (Sprint 3)
// -----------------------------------------------------------------------------

// handleImportPreview memproses file unggahan secara dry-run tanpa mengubah database
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	if s.importService == nil {
		errorResponse(w, http.StatusServiceUnavailable, "Layanan Import Center belum diinisialisasi")
		return
	}

	// Batasi ukuran form maksimal 10 MB + buffer header
	if err := r.ParseMultipartForm(domain.MaxImportFileSize + 1024*1024); err != nil {
		errorResponse(w, http.StatusBadRequest, fmt.Sprintf("Ukuran file melebihi batas atau form rusak: %v", err))
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "File unggahan ('file') tidak ditemukan dalam request")
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(io.LimitReader(file, domain.MaxImportFileSize+1))
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal membaca file: %v", err))
		return
	}
	if len(fileBytes) > domain.MaxImportFileSize {
		errorResponse(w, http.StatusBadRequest, "Ukuran file melebihi batas maksimal 10 MB")
		return
	}

	worksheet := r.FormValue("worksheet")
	if worksheet == "" {
		worksheet = r.FormValue("sheet_name")
	}
	mappingJSON := r.FormValue("column_mapping")
	if mappingJSON == "" {
		mappingJSON = r.FormValue("mapping")
	}
	var mapping map[string]string
	if mappingJSON != "" {
		_ = json.Unmarshal([]byte(mappingJSON), &mapping)
	}

	preview, err := s.importService.PreviewImport(r.Context(), fileBytes, fileHeader.Filename, "", worksheet, mapping)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, preview)
}

// handleImportCommit mengeksekusi commit impor secara atomik
func (s *Server) handleImportCommit(w http.ResponseWriter, r *http.Request) {
	if s.importService == nil {
		errorResponse(w, http.StatusServiceUnavailable, "Layanan Import Center belum diinisialisasi")
		return
	}

	if err := r.ParseMultipartForm(domain.MaxImportFileSize + 1024*1024); err != nil {
		errorResponse(w, http.StatusBadRequest, fmt.Sprintf("Gagal membaca payload form: %v", err))
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "File unggahan ('file') wajib disertakan saat commit")
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(io.LimitReader(file, domain.MaxImportFileSize+1))
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal membaca file: %v", err))
		return
	}
	if len(fileBytes) > domain.MaxImportFileSize {
		errorResponse(w, http.StatusBadRequest, "Ukuran file melebihi batas maksimal 10 MB")
		return
	}

	worksheet := r.FormValue("worksheet")
	if worksheet == "" {
		worksheet = r.FormValue("sheet_name")
	}
	dupMode := r.FormValue("duplicate_mode")
	mappingJSON := r.FormValue("column_mapping")
	if mappingJSON == "" {
		mappingJSON = r.FormValue("mapping")
	}
	var mapping map[string]string
	if mappingJSON != "" {
		_ = json.Unmarshal([]byte(mappingJSON), &mapping)
	}

	commitReq := domain.CommitImportRequest{
		FileName:      fileHeader.Filename,
		Worksheet:     worksheet,
		ColumnMapping: mapping,
		DuplicateMode: dupMode,
	}

	result, err := s.importService.CommitImport(r.Context(), fileBytes, commitReq)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusCreated, result)
}

// handleListImportHistory mengambil daftar riwayat batch impor
func (s *Server) handleListImportHistory(w http.ResponseWriter, r *http.Request) {
	if s.importService == nil {
		errorResponse(w, http.StatusServiceUnavailable, "Layanan Import Center belum diinisialisasi")
		return
	}

	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if val, err := strconv.Atoi(o); err == nil && val >= 0 {
			offset = val
		}
	}

	batches, err := s.importService.ListBatches(r.Context(), limit, offset)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal mengambil riwayat impor: %v", err))
		return
	}
	if batches == nil {
		batches = []*domain.ImportBatch{}
	}
	jsonResponse(w, http.StatusOK, batches)
}

// handleGetImportBatch mengambil rincian satu batch beserta baris itemnya
func (s *Server) handleGetImportBatch(w http.ResponseWriter, r *http.Request) {
	if s.importService == nil {
		errorResponse(w, http.StatusServiceUnavailable, "Layanan Import Center belum diinisialisasi")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID batch tidak valid")
		return
	}

	batch, err := s.importService.GetBatchByID(r.Context(), id)
	if err != nil {
		errorResponse(w, http.StatusNotFound, "Batch impor tidak ditemukan")
		return
	}

	items, err := s.importService.GetBatchItems(r.Context(), id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal mengambil rincian item batch: %v", err))
		return
	}
	if items == nil {
		items = []*domain.ImportItem{}
	}

	response := map[string]interface{}{
		"batch": batch,
		"items": items,
	}
	jsonResponse(w, http.StatusOK, response)
}

// handleCompensateImportBatch membatalkan hasil impor batch melalui tindakan kompensasi (arsip)
func (s *Server) handleCompensateImportBatch(w http.ResponseWriter, r *http.Request) {
	if s.importService == nil {
		errorResponse(w, http.StatusServiceUnavailable, "Layanan Import Center belum diinisialisasi")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID batch tidak valid")
		return
	}

	force := r.URL.Query().Get("force") == "true" || r.FormValue("force") == "true"

	compensatedCount, err := s.importService.CompensateRollbackBatch(r.Context(), id, force)
	if err != nil {
		var editedErr *service.ErrImportHasEditedItems
		if errors.As(err, &editedErr) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":            editedErr.Error(),
				"has_edited_items": true,
				"edited_count":     editedErr.EditedCount,
				"batch_code":       editedErr.BatchCode,
			})
			return
		}
		errorResponse(w, http.StatusBadRequest, fmt.Sprintf("Gagal melakukan kompensasi pembatalan batch: %v", err))
		return
	}

	response := map[string]interface{}{
		"status":            domain.ImportStatusRolledBack,
		"batch_id":          id,
		"compensated_count": compensatedCount,
		"message":           fmt.Sprintf("Batch berhasil dibatalkan melalui kompensasi: %d pekerjaan diarsipkan.", compensatedCount),
	}
	jsonResponse(w, http.StatusOK, response)
}

// handleGetImportErrorReport mengunduh laporan error / skip dalam format CSV ber-BOM UTF-8
func (s *Server) handleGetImportErrorReport(w http.ResponseWriter, r *http.Request) {
	if s.importService == nil {
		errorResponse(w, http.StatusServiceUnavailable, "Layanan Import Center belum diinisialisasi")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID batch tidak valid")
		return
	}

	csvBytes, err := s.importService.GenerateErrorReportCSV(r.Context(), id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal menghasilkan laporan error: %v", err))
		return
	}

	filename := fmt.Sprintf("laporan_error_batch_%d.csv", id)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(csvBytes)
}

// handleGetCSVTemplate mengunduh file template CSV acuan
func (s *Server) handleGetCSVTemplate(w http.ResponseWriter, r *http.Request) {
	csvBytes := service.GenerateCSVTemplate()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"template_work_register.csv\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(csvBytes)
}

// handleGetXLSXTemplate mengunduh file template Microsoft Excel (.xlsx) acuan
func (s *Server) handleGetXLSXTemplate(w http.ResponseWriter, r *http.Request) {
	xlsxBytes, err := service.GenerateXLSXTemplate()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal membuat template Excel: %v", err))
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\"template_work_register.xlsx\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(xlsxBytes)
}
