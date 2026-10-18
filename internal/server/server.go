package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"compliance-workspace/internal/domain"
	"compliance-workspace/internal/repository"
	"compliance-workspace/internal/service"
	"compliance-workspace/web"
)

// Server membungkus HTTP server dan dependencies
type Server struct {
	httpServer      *http.Server
	listener        net.Listener
	workItemService *service.WorkItemService
	backupService   *service.BackupService
	exportService   *service.ExportService
	taskMgmtService *service.TaskManagementService
	importService   *service.ImportService
}

func NewServer(
	addr string,
	workItemService *service.WorkItemService,
	backupService *service.BackupService,
	exportService *service.ExportService,
	optionalServices ...interface{},
) (*Server, error) {
	// Pastikan binding hanya ke 127.0.0.1
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		return nil, errors.New("keamanan: server hanya diizinkan mengikat ke 127.0.0.1")
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("gagal mendengarkan pada %s: %w", addr, err)
	}

	var tmSvc *service.TaskManagementService
	var impSvc *service.ImportService

	for _, opt := range optionalServices {
		switch s := opt.(type) {
		case *service.TaskManagementService:
			tmSvc = s
		case *service.ImportService:
			impSvc = s
		}
	}

	s := &Server{
		listener:        ln,
		workItemService: workItemService,
		backupService:   backupService,
		exportService:   exportService,
		taskMgmtService: tmSvc,
		importService:   impSvc,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// Pasang pipeline middleware keamanan
	handler := HostValidatorMiddleware(SecurityHeadersMiddleware(CSRFAndOriginMiddleware(mux)))

	s.httpServer = &http.Server{
		Handler: handler,
	}

	return s, nil
}

// Addr mengembalikan alamat aktif server (termasuk port dinamis yang dialokasikan OS)
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Start menjalankan listening HTTP
func (s *Server) Start() error {
	return s.httpServer.Serve(s.listener)
}

// Shutdown mematikan server dengan anggun (graceful shutdown) dan menutup koneksi basis data
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.httpServer.Shutdown(ctx)
	if s.workItemService != nil {
		if db := s.workItemService.GetDB(); db != nil {
			_ = db.Close()
		}
	}
	return err
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Endpoint token CSRF
	mux.HandleFunc("GET /api/csrf-token", s.handleGetCSRFToken)

	// Endpoint Work Item
	mux.HandleFunc("POST /api/work-items", s.handleCreateWorkItem)
	mux.HandleFunc("GET /api/work-items", s.handleListWorkItems)
	mux.HandleFunc("GET /api/work-items/{id}", s.handleGetWorkItem)
	mux.HandleFunc("PUT /api/work-items/{id}", s.handleUpdateWorkItem)
	mux.HandleFunc("POST /api/work-items/{id}/archive", s.handleArchiveWorkItem)
	mux.HandleFunc("POST /api/work-items/{id}/restore", s.handleRestoreWorkItem)
	mux.HandleFunc("GET /api/work-items/{id}/delete-preview", s.handleGetWorkItemDeletePreview)
	mux.HandleFunc("DELETE /api/work-items/{id}", s.handlePermanentDeleteWorkItem)

	// Endpoint Activity Log
	mux.HandleFunc("POST /api/work-items/{id}/activities", s.handleAddActivity)
	mux.HandleFunc("GET /api/work-items/{id}/activities", s.handleListActivities)

	// Endpoint Subtask, Checklist, Kategori, Template & Recurring (Sprint 2)
	if s.taskMgmtService != nil {
		// Subtask & Checklist
		mux.HandleFunc("GET /api/work-items/{id}/subtasks", s.handleListSubtasks)
		mux.HandleFunc("POST /api/work-items/{id}/subtasks", s.handleCreateSubtask)
		mux.HandleFunc("PUT /api/subtasks/{id}", s.handleUpdateSubtask)
		mux.HandleFunc("DELETE /api/subtasks/{id}", s.handleDeleteSubtask)

		// Categories
		mux.HandleFunc("GET /api/categories", s.handleListCategories)
		mux.HandleFunc("POST /api/categories", s.handleCreateCategory)
		mux.HandleFunc("PUT /api/categories/{id}", s.handleUpdateCategory)
		mux.HandleFunc("DELETE /api/categories/{id}", s.handleDeleteCategory)

		// Work Templates
		mux.HandleFunc("GET /api/templates", s.handleListTemplates)
		mux.HandleFunc("GET /api/templates/{id}", s.handleGetTemplate)
		mux.HandleFunc("POST /api/templates", s.handleCreateTemplate)
		mux.HandleFunc("PUT /api/templates/{id}", s.handleUpdateTemplate)
		mux.HandleFunc("DELETE /api/templates/{id}", s.handleDeleteTemplate)
		mux.HandleFunc("POST /api/templates/{id}/apply", s.handleApplyTemplate)

		// Recurring Tasks
		mux.HandleFunc("GET /api/recurring-schedules", s.handleListRecurringSchedules)
		mux.HandleFunc("POST /api/recurring-schedules", s.handleCreateRecurringSchedule)
		mux.HandleFunc("PUT /api/recurring-schedules/{id}", s.handleUpdateRecurringSchedule)
		mux.HandleFunc("DELETE /api/recurring-schedules/{id}", s.handleDeleteRecurringSchedule)
		mux.HandleFunc("POST /api/recurring-schedules/trigger", s.handleTriggerRecurring)
	}

	// Endpoint Backup & Restore
	if s.backupService != nil {
		mux.HandleFunc("GET /api/backups", s.handleListBackups)
		mux.HandleFunc("POST /api/backups", s.handleCreateBackup)
		mux.HandleFunc("POST /api/backups/restore", s.handleRestoreBackup)
		mux.HandleFunc("POST /api/backup/restore", s.handleRestoreBackup)
		mux.HandleFunc("GET /api/backup/inspect", s.handleInspectBackup)
		mux.HandleFunc("GET /api/backups/inspect", s.handleInspectBackup)
		mux.HandleFunc("DELETE /api/backup/{filename}", s.handleDeleteBackup)
		mux.HandleFunc("DELETE /api/backups/{filename}", s.handleDeleteBackup)
		mux.HandleFunc("POST /api/backup/delete-batch", s.handleBatchDeleteBackups)
		mux.HandleFunc("POST /api/backups/delete-batch", s.handleBatchDeleteBackups)
	}

	// Endpoint Ekspor CSV
	if s.exportService != nil {
		mux.HandleFunc("GET /api/export/work-items", s.handleExportWorkItems)
		mux.HandleFunc("GET /api/export/activity-logs", s.handleExportActivityLogs)
	}

	// Endpoint Data Import Center (Sprint 3)
	if s.importService != nil {
		mux.HandleFunc("POST /api/import/preview", s.handleImportPreview)
		mux.HandleFunc("POST /api/import/commit", s.handleImportCommit)
		mux.HandleFunc("GET /api/import/history", s.handleListImportHistory)
		mux.HandleFunc("GET /api/import/batches/{id}", s.handleGetImportBatch)
		mux.HandleFunc("POST /api/import/batches/{id}/compensate", s.handleCompensateImportBatch)
		mux.HandleFunc("GET /api/import/batches/{id}/error-report", s.handleGetImportErrorReport)
		mux.HandleFunc("GET /api/import/template/csv", s.handleGetCSVTemplate)
		mux.HandleFunc("GET /api/import/template/xlsx", s.handleGetXLSXTemplate)
	}

	// Aset Frontend Statis (Embedded)
	sub, err := fs.Sub(web.StaticFS, "static")
	if err == nil {
		mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
			icoData, err := fs.ReadFile(sub, "favicon.ico")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/x-icon")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			_, _ = w.Write(icoData)
		})
		mux.Handle("/", http.FileServer(http.FS(sub)))
	}
}

// Helper untuk mengirim respon JSON
func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// Helper untuk mengirim respon error JSON
func errorResponse(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}

func (s *Server) handleGetCSRFToken(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(CSRFCookieName)
	if err == nil && cookie.Value != "" {
		jsonResponse(w, http.StatusOK, map[string]string{"csrf_token": cookie.Value})
		return
	}
	errorResponse(w, http.StatusInternalServerError, "Gagal mendapatkan token CSRF")
}

func (s *Server) handleCreateWorkItem(w http.ResponseWriter, r *http.Request) {
	var in domain.CreateWorkItemInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errorResponse(w, http.StatusBadRequest, fmt.Sprintf("Format JSON tidak valid: %v", err))
		return
	}

	item, err := s.workItemService.CreateWorkItem(r.Context(), in)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusCreated, item)
}

func (s *Server) handleListWorkItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := repository.WorkItemFilter{
		Status:          q.Get("status"),
		Priority:        q.Get("priority"),
		Category:        q.Get("category"),
		Search:          q.Get("search"),
		IncludeArchived: q.Get("include_archived") == "true" || q.Get("include_archived") == "1",
		Provenance:      q.Get("provenance"),
		SortBy:          q.Get("sort_by"),
		SortOrder:       q.Get("sort_order"),
	}

	items, err := s.workItemService.ListWorkItems(r.Context(), filter)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal mengambil data pekerjaan: %v", err))
		return
	}

	if items == nil {
		items = []*domain.WorkItem{}
	}

	jsonResponse(w, http.StatusOK, items)
}

func (s *Server) handleGetWorkItem(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID pekerjaan tidak valid")
		return
	}

	item, err := s.workItemService.GetWorkItem(r.Context(), id)
	if err != nil {
		errorResponse(w, http.StatusNotFound, "Pekerjaan tidak ditemukan")
		return
	}

	jsonResponse(w, http.StatusOK, item)
}

func (s *Server) handleUpdateWorkItem(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID pekerjaan tidak valid")
		return
	}

	var in domain.UpdateWorkItemInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errorResponse(w, http.StatusBadRequest, fmt.Sprintf("Format JSON tidak valid: %v", err))
		return
	}

	updated, err := s.workItemService.UpdateWorkItem(r.Context(), id, in)
	if err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			errorResponse(w, http.StatusNotFound, err.Error())
		} else {
			errorResponse(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	jsonResponse(w, http.StatusOK, updated)
}

func (s *Server) handleArchiveWorkItem(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID pekerjaan tidak valid")
		return
	}

	archived, err := s.workItemService.ArchiveWorkItem(r.Context(), id)
	if err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, archived)
}

func (s *Server) handleRestoreWorkItem(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID pekerjaan tidak valid")
		return
	}

	restored, err := s.workItemService.RestoreWorkItem(r.Context(), id)
	if err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, restored)
}

func (s *Server) handleAddActivity(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID pekerjaan tidak valid")
		return
	}

	var in domain.AddActivityInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errorResponse(w, http.StatusBadRequest, fmt.Sprintf("Format JSON tidak valid: %v", err))
		return
	}

	log, err := s.workItemService.AddActivity(r.Context(), id, in)
	if err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			errorResponse(w, http.StatusNotFound, err.Error())
		} else {
			errorResponse(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	jsonResponse(w, http.StatusCreated, log)
}

func (s *Server) handleListActivities(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID pekerjaan tidak valid")
		return
	}

	logs, err := s.workItemService.ListActivities(r.Context(), id)
	if err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}

	if logs == nil {
		logs = []*domain.ActivityLog{}
	}

	jsonResponse(w, http.StatusOK, logs)
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	items, err := s.backupService.ListBackups()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal mengambil daftar cadangan: %v", err))
		return
	}
	if items == nil {
		items = []*service.BackupItem{}
	}
	jsonResponse(w, http.StatusOK, items)
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	item, err := s.backupService.CreateBackup(r.Context())
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal membuat cadangan database: %v", err))
		return
	}
	jsonResponse(w, http.StatusCreated, item)
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Format JSON tidak valid")
		return
	}

	if strings.TrimSpace(req.Filename) == "" {
		errorResponse(w, http.StatusBadRequest, "Nama file cadangan wajib disertakan")
		return
	}

	if err := s.backupService.RestoreBackup(r.Context(), req.Filename); err != nil {
		errorResponse(w, http.StatusBadRequest, fmt.Sprintf("Gagal memulihkan database: %v", err))
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"message": fmt.Sprintf("Database berhasil dipulihkan dari cadangan '%s'", req.Filename),
	})
}

func (s *Server) handleExportWorkItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := repository.WorkItemFilter{
		Status:          q.Get("status"),
		Priority:        q.Get("priority"),
		Category:        q.Get("category"),
		Search:          q.Get("search"),
		IncludeArchived: q.Get("include_archived") == "true" || q.Get("include_archived") == "1",
		SortBy:          q.Get("sort_by"),
		SortOrder:       q.Get("sort_order"),
	}

	timestamp := time.Now().UTC().Format("20060102_150405")
	filename := fmt.Sprintf("compliance_work_register_%s.csv", timestamp)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	if err := s.exportService.ExportWorkItemsCSV(r.Context(), w, filter); err != nil {
		log.Printf("[ERROR] Ekspor CSV pekerjaan gagal: %v", err)
	}
}

func (s *Server) handleExportActivityLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var workItemID int64
	if idStr := q.Get("work_item_id"); idStr != "" {
		workItemID, _ = strconv.ParseInt(idStr, 10, 64)
	}

	timestamp := time.Now().UTC().Format("20060102_150405")
	filename := fmt.Sprintf("compliance_activity_logs_%s.csv", timestamp)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	if err := s.exportService.ExportActivityLogsCSV(r.Context(), w, workItemID); err != nil {
		log.Printf("[ERROR] Ekspor CSV log aktivitas gagal: %v", err)
	}
}

func (s *Server) handleGetWorkItemDeletePreview(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID pekerjaan tidak valid")
		return
	}

	preview, err := s.workItemService.GetDeletePreview(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			errorResponse(w, http.StatusNotFound, err.Error())
		} else {
			errorResponse(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	jsonResponse(w, http.StatusOK, preview)
}

func (s *Server) handlePermanentDeleteWorkItem(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID pekerjaan tidak valid")
		return
	}

	var req struct {
		ConfirmCode string `json:"confirm_code"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.ConfirmCode == "" {
		req.ConfirmCode = r.URL.Query().Get("confirm_code")
	}

	deletedItem, err := s.workItemService.PermanentDeleteWorkItem(r.Context(), id, req.ConfirmCode)
	if err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			errorResponse(w, http.StatusNotFound, err.Error())
		} else {
			errorResponse(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":   fmt.Sprintf("Pekerjaan '%s' (%s) berhasil dihapus secara permanen.", deletedItem.Title, deletedItem.WorkCode),
		"work_code": deletedItem.WorkCode,
	})
}

func (s *Server) handleInspectBackup(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("filename")
	if strings.TrimSpace(filename) == "" {
		errorResponse(w, http.StatusBadRequest, "Parameter filename wajib disertakan")
		return
	}

	inspection, err := s.backupService.InspectBackup(r.Context(), filename)
	if err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			errorResponse(w, http.StatusNotFound, err.Error())
		} else {
			errorResponse(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	jsonResponse(w, http.StatusOK, inspection)
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if strings.TrimSpace(filename) == "" {
		filename = r.URL.Query().Get("filename")
	}
	if strings.TrimSpace(filename) == "" {
		errorResponse(w, http.StatusBadRequest, "Nama berkas cadangan wajib disertakan")
		return
	}

	if err := s.backupService.DeleteBackup(r.Context(), filename); err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			errorResponse(w, http.StatusNotFound, err.Error())
		} else {
			errorResponse(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"message":  fmt.Sprintf("Berkas cadangan '%s' berhasil dihapus.", filename),
		"filename": filename,
	})
}

func (s *Server) handleBatchDeleteBackups(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filenames []string `json:"filenames"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Format JSON tidak valid")
		return
	}

	result, err := s.backupService.DeleteBackups(r.Context(), req.Filenames)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, result)
}

