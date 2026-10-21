package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/service"
)

const maxMultipartMemory = 1 << 20

func parseImportMultipart(w http.ResponseWriter, r *http.Request) bool {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || strings.TrimSpace(params["boundary"]) == "" {
		errorResponse(w, http.StatusUnsupportedMediaType, "Content-Type harus multipart/form-data dengan boundary yang valid")
		return false
	}
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			errorResponse(w, http.StatusRequestEntityTooLarge, "Ukuran permintaan impor melebihi batas maksimal")
		} else {
			errorResponse(w, http.StatusBadRequest, "Formulir unggahan rusak atau tidak valid")
		}
		return false
	}
	return true
}

func safeUploadFilename(filename string) (string, error) {
	filename = strings.TrimSpace(strings.ReplaceAll(filename, "\\", "/"))
	filename = filepath.Base(filename)
	if filename == "" || filename == "." || len(filename) > 255 {
		return "", errors.New("nama berkas unggahan tidak valid")
	}
	return filename, nil
}

// -----------------------------------------------------------------------------
// Data Import Center Handlers (Sprint 3)
// -----------------------------------------------------------------------------

// handleImportPreview memproses file unggahan secara dry-run tanpa mengubah database
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	if s.importService == nil {
		errorResponse(w, http.StatusServiceUnavailable, "Layanan impor belum tersedia")
		return
	}

	if !parseImportMultipart(w, r) {
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "Berkas unggahan ('file') tidak ditemukan dalam permintaan")
		return
	}
	defer file.Close()
	filename, err := safeUploadFilename(fileHeader.Filename)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if fileHeader.Size > domain.MaxImportFileSize {
		errorResponse(w, http.StatusRequestEntityTooLarge, "Ukuran berkas melebihi batas maksimal 10 MB")
		return
	}

	fileBytes, err := io.ReadAll(io.LimitReader(file, domain.MaxImportFileSize+1))
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal membaca berkas: %v", err))
		return
	}
	if len(fileBytes) > domain.MaxImportFileSize {
		errorResponse(w, http.StatusBadRequest, "Ukuran berkas melebihi batas maksimal 10 MB")
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
		if err := json.Unmarshal([]byte(mappingJSON), &mapping); err != nil {
			errorResponse(w, http.StatusBadRequest, "Pemetaan kolom JSON tidak valid")
			return
		}
	}

	preview, err := s.importService.PreviewImport(r.Context(), fileBytes, filename, "", worksheet, mapping)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, preview)
}

// handleImportCommit mengeksekusi commit impor secara atomik
func (s *Server) handleImportCommit(w http.ResponseWriter, r *http.Request) {
	if s.importService == nil {
		errorResponse(w, http.StatusServiceUnavailable, "Layanan impor belum tersedia")
		return
	}

	if !parseImportMultipart(w, r) {
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "Berkas unggahan ('file') wajib disertakan saat menyimpan impor")
		return
	}
	defer file.Close()
	filename, err := safeUploadFilename(fileHeader.Filename)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if fileHeader.Size > domain.MaxImportFileSize {
		errorResponse(w, http.StatusRequestEntityTooLarge, "Ukuran berkas melebihi batas maksimal 10 MB")
		return
	}

	fileBytes, err := io.ReadAll(io.LimitReader(file, domain.MaxImportFileSize+1))
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal membaca berkas: %v", err))
		return
	}
	if len(fileBytes) > domain.MaxImportFileSize {
		errorResponse(w, http.StatusBadRequest, "Ukuran berkas melebihi batas maksimal 10 MB")
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
		if err := json.Unmarshal([]byte(mappingJSON), &mapping); err != nil {
			errorResponse(w, http.StatusBadRequest, "Pemetaan kolom JSON tidak valid")
			return
		}
	}

	commitReq := domain.CommitImportRequest{
		FileName:      filename,
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
		errorResponse(w, http.StatusServiceUnavailable, "Layanan impor belum tersedia")
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
		errorResponse(w, http.StatusServiceUnavailable, "Layanan impor belum tersedia")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID proses impor tidak valid")
		return
	}

	batch, err := s.importService.GetBatchByID(r.Context(), id)
	if err != nil {
		errorResponse(w, http.StatusNotFound, "Proses impor tidak ditemukan")
		return
	}

	items, err := s.importService.GetBatchItems(r.Context(), id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal mengambil rincian proses impor: %v", err))
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
		errorResponse(w, http.StatusServiceUnavailable, "Layanan impor belum tersedia")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID proses impor tidak valid")
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
		errorResponse(w, http.StatusBadRequest, fmt.Sprintf("Gagal membatalkan proses impor: %v", err))
		return
	}

	response := map[string]interface{}{
		"status":            domain.ImportStatusRolledBack,
		"batch_id":          id,
		"compensated_count": compensatedCount,
		"message":           fmt.Sprintf("Proses impor berhasil dibatalkan. %d pekerjaan diarsipkan.", compensatedCount),
	}
	jsonResponse(w, http.StatusOK, response)
}

// handleGetImportErrorReport mengunduh laporan error / skip dalam format CSV ber-BOM UTF-8
func (s *Server) handleGetImportErrorReport(w http.ResponseWriter, r *http.Request) {
	if s.importService == nil {
		errorResponse(w, http.StatusServiceUnavailable, "Layanan impor belum tersedia")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID proses impor tidak valid")
		return
	}

	csvBytes, err := s.importService.GenerateErrorReportCSV(r.Context(), id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal membuat laporan kegagalan: %v", err))
		return
	}

	filename := fmt.Sprintf("laporan_kegagalan_impor_%d.csv", id)
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
