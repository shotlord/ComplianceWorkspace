package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"compliance-workspace/internal/domain"
)

// -----------------------------------------------------------------------------
// Subtask Handlers
// -----------------------------------------------------------------------------

func (s *Server) handleListSubtasks(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	workItemID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || workItemID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID pekerjaan tidak valid")
		return
	}

	subtasks, err := s.taskMgmtService.ListSubtasks(r.Context(), workItemID)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal mengambil subtask: %v", err))
		return
	}
	if subtasks == nil {
		subtasks = []*domain.Subtask{}
	}
	jsonResponse(w, http.StatusOK, subtasks)
}

func (s *Server) handleCreateSubtask(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	workItemID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || workItemID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID pekerjaan tidak valid")
		return
	}

	var in domain.CreateSubtaskInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errorResponse(w, http.StatusBadRequest, "Format JSON tidak valid")
		return
	}

	subtask, err := s.taskMgmtService.AddSubtask(r.Context(), workItemID, in)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusCreated, subtask)
}

func (s *Server) handleUpdateSubtask(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	subtaskID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || subtaskID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID subtask tidak valid")
		return
	}

	var in domain.UpdateSubtaskInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errorResponse(w, http.StatusBadRequest, "Format JSON tidak valid")
		return
	}

	subtask, err := s.taskMgmtService.UpdateSubtask(r.Context(), subtaskID, in)
	if err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			errorResponse(w, http.StatusNotFound, err.Error())
		} else {
			errorResponse(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	jsonResponse(w, http.StatusOK, subtask)
}

func (s *Server) handleDeleteSubtask(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	subtaskID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || subtaskID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID subtask tidak valid")
		return
	}

	if err := s.taskMgmtService.DeleteSubtask(r.Context(), subtaskID); err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			errorResponse(w, http.StatusNotFound, err.Error())
		} else {
			errorResponse(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": "Subtask berhasil dihapus"})
}

// -----------------------------------------------------------------------------
// Category Handlers
// -----------------------------------------------------------------------------

func (s *Server) handleListCategories(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"
	onlyActive := !all
	cats, err := s.taskMgmtService.ListCategories(r.Context(), onlyActive)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal mengambil kategori: %v", err))
		return
	}
	if cats == nil {
		cats = []*domain.Category{}
	}
	jsonResponse(w, http.StatusOK, cats)
}

func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var in domain.CreateCategoryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errorResponse(w, http.StatusBadRequest, "Format JSON tidak valid")
		return
	}

	cat, err := s.taskMgmtService.CreateCategory(r.Context(), in)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusCreated, cat)
}

func (s *Server) handleUpdateCategory(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	catID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || catID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID kategori tidak valid")
		return
	}

	var in domain.UpdateCategoryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errorResponse(w, http.StatusBadRequest, "Format JSON tidak valid")
		return
	}

	cat, err := s.taskMgmtService.UpdateCategory(r.Context(), catID, in)
	if err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			errorResponse(w, http.StatusNotFound, err.Error())
		} else {
			errorResponse(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	jsonResponse(w, http.StatusOK, cat)
}

func (s *Server) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	catID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || catID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID kategori tidak valid")
		return
	}

	if err := s.taskMgmtService.DeleteCategory(r.Context(), catID); err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": "Kategori berhasil dihapus secara permanen"})
}

// -----------------------------------------------------------------------------
// Work Template Handlers
// -----------------------------------------------------------------------------

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	tpls, err := s.taskMgmtService.ListTemplates(r.Context())
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal mengambil template: %v", err))
		return
	}
	if tpls == nil {
		tpls = []*domain.WorkTemplate{}
	}
	jsonResponse(w, http.StatusOK, tpls)
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	tplID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || tplID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID template tidak valid")
		return
	}

	tpl, err := s.taskMgmtService.GetTemplate(r.Context(), tplID)
	if err != nil {
		errorResponse(w, http.StatusNotFound, "Template tidak ditemukan")
		return
	}
	jsonResponse(w, http.StatusOK, tpl)
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var in domain.CreateWorkTemplateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errorResponse(w, http.StatusBadRequest, "Format JSON tidak valid")
		return
	}

	tpl, err := s.taskMgmtService.CreateTemplate(r.Context(), in)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusCreated, tpl)
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	tplID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || tplID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID template tidak valid")
		return
	}

	var in domain.UpdateWorkTemplateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errorResponse(w, http.StatusBadRequest, "Format JSON tidak valid")
		return
	}

	tpl, err := s.taskMgmtService.UpdateTemplate(r.Context(), tplID, in)
	if err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			errorResponse(w, http.StatusNotFound, err.Error())
		} else {
			errorResponse(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	jsonResponse(w, http.StatusOK, tpl)
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	tplID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || tplID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID template tidak valid")
		return
	}

	if err := s.taskMgmtService.DeleteTemplate(r.Context(), tplID); err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": "Template berhasil dihapus"})
}

func (s *Server) handleApplyTemplate(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	tplID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || tplID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID template tidak valid")
		return
	}

	var req struct {
		ReceivedOn string `json:"received_on"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	item, err := s.taskMgmtService.ApplyTemplate(r.Context(), tplID, req.ReceivedOn)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusCreated, item)
}

// -----------------------------------------------------------------------------
// Recurring Tasks Handlers
// -----------------------------------------------------------------------------

func (s *Server) handleListRecurringSchedules(w http.ResponseWriter, r *http.Request) {
	schs, err := s.taskMgmtService.ListRecurringSchedules(r.Context())
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal mengambil jadwal berulang: %v", err))
		return
	}
	if schs == nil {
		schs = []*domain.RecurringSchedule{}
	}
	jsonResponse(w, http.StatusOK, schs)
}

func (s *Server) handleCreateRecurringSchedule(w http.ResponseWriter, r *http.Request) {
	var in domain.CreateRecurringScheduleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errorResponse(w, http.StatusBadRequest, "Format JSON tidak valid")
		return
	}

	sch, err := s.taskMgmtService.CreateRecurringSchedule(r.Context(), in)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusCreated, sch)
}

func (s *Server) handleUpdateRecurringSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	schID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || schID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID jadwal tidak valid")
		return
	}

	var in domain.UpdateRecurringScheduleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errorResponse(w, http.StatusBadRequest, "Format JSON tidak valid")
		return
	}

	sch, err := s.taskMgmtService.UpdateRecurringSchedule(r.Context(), schID, in)
	if err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			errorResponse(w, http.StatusNotFound, err.Error())
		} else {
			errorResponse(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	jsonResponse(w, http.StatusOK, sch)
}

func (s *Server) handleDeleteRecurringSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	schID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || schID <= 0 {
		errorResponse(w, http.StatusBadRequest, "ID jadwal tidak valid")
		return
	}

	if err := s.taskMgmtService.DeleteRecurringSchedule(r.Context(), schID); err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": "Jadwal berulang berhasil dihapus"})
}

func (s *Server) handleTriggerRecurring(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AsOfDate string `json:"as_of_date"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	createdItems, err := s.taskMgmtService.TriggerRecurringGeneration(r.Context(), req.AsOfDate)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Gagal mengevaluasi jadwal: %v", err))
		return
	}
	if createdItems == nil {
		createdItems = []*domain.WorkItem{}
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":       fmt.Sprintf("Pemeriksaan selesai. %d pekerjaan baru dihasilkan.", len(createdItems)),
		"created_count": len(createdItems),
		"items":         createdItems,
	})
}
