package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/robindubreuil/eraser/internal/history"
)

func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskID")
	var taskID int64
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}

	task, err := s.historyStore.GetPendingTaskByID(taskID)
	if err != nil || task == nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	data := map[string]interface{}{
		"Title": "Task Detail",
		"Task":  task,
	}

	s.renderWithCSRF(w, r, "task-detail.html", data)
}

func (s *Server) handleTaskComplete(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskID")
	var taskID int64
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	status := r.FormValue("status")
	if status == "" {
		status = "completed"
	}
	validStatuses := map[string]bool{"completed": true, "skipped": true, "failed": true}
	if !validStatuses[status] {
		status = "completed"
	}

	if s.historyStore == nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`<span class="text-red-600">Database not available</span>`)) //nolint:errcheck
		return
	}

	if err := s.historyStore.CompletePendingTask(taskID, status); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf(`<span class="text-red-600">Error: %s</span>`, template.HTMLEscapeString(err.Error())))) //nolint:errcheck
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/tasks/%d/helper", taskID), http.StatusFound)
}

func (s *Server) handleTaskSkip(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskID")
	var taskID int64
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	if s.historyStore == nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`<span class="text-red-600">Database not available</span>`)) //nolint:errcheck
		return
	}

	if err := s.historyStore.CompletePendingTask(taskID, "skipped"); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf(`<span class="text-red-600">Error: %s</span>`, template.HTMLEscapeString(err.Error())))) //nolint:errcheck
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/tasks/%d/helper", taskID), http.StatusFound)
}

func (s *Server) handleTaskHelper(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskID")
	var taskID int64
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}

	task, err := s.historyStore.GetPendingTaskByID(taskID)
	if err != nil || task == nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	s.historyStore.MarkTaskOpened(taskID) //nolint:errcheck

	task, _ = s.historyStore.GetPendingTaskByID(taskID)

	profileData := make(map[string]string)
	if task.BrowserState != "" {
		json.Unmarshal([]byte(task.BrowserState), &profileData) //nolint:errcheck
	}

	orderedFields := []struct {
		Key   string
		Label string
	}{
		{"email", "Email"},
		{"firstName", "First Name"},
		{"lastName", "Last Name"},
		{"phone", "Phone"},
		{"address", "Address"},
		{"city", "City"},
		{"state", "State"},
		{"zipCode", "ZIP Code"},
		{"country", "Country"},
	}

	orderedProfile := make([]map[string]string, 0)
	for _, field := range orderedFields {
		if val, ok := profileData[field.Key]; ok && val != "" {
			orderedProfile = append(orderedProfile, map[string]string{
				"key":   field.Label,
				"value": val,
			})
		}
	}

	data := map[string]interface{}{
		"Title":          fmt.Sprintf("CAPTCHA Task: %s", task.BrokerName),
		"Task":           task,
		"ProfileData":    profileData,
		"OrderedProfile": orderedProfile,
	}

	s.renderWithCSRF(w, r, "task-helper.html", data)
}

func (s *Server) handleFormComplete(w http.ResponseWriter, r *http.Request) {
	brokerID := chi.URLParam(r, "brokerID")

	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}

	if err := s.historyStore.UpdatePipelineStatus(brokerID, history.PipelineConfirmed); err != nil {
		http.Error(w, "Failed to update status", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/tasks")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/tasks", http.StatusFound)
}

func (s *Server) handleFormSkip(w http.ResponseWriter, r *http.Request) {
	brokerID := chi.URLParam(r, "brokerID")

	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}

	if err := s.historyStore.UpdatePipelineStatus(brokerID, history.PipelineRejected); err != nil {
		http.Error(w, "Failed to update status", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/tasks")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/tasks", http.StatusFound)
}

func (s *Server) getPipelineStats() PipelineStats {
	stats := PipelineStats{}

	if s.historyStore == nil {
		return stats
	}

	pipelineStats, err := s.historyStore.GetPipelineStats()
	if err == nil {
		stats.EmailSent = pipelineStats[history.PipelineEmailSent]
		stats.AwaitingResponse = pipelineStats[history.PipelineAwaitingResponse]
		stats.FormRequired = pipelineStats[history.PipelineFormRequired]
		stats.FormFilled = pipelineStats[history.PipelineFormFilled]
		stats.AwaitingCaptcha = pipelineStats[history.PipelineAwaitingCaptcha]
		stats.CaptchaSolved = pipelineStats[history.PipelineCaptchaSolved]
		stats.AwaitingConfirmation = pipelineStats[history.PipelineAwaitingConfirmation]
		stats.Confirmed = pipelineStats[history.PipelineConfirmed]
		stats.Rejected = pipelineStats[history.PipelineRejected]
		stats.Failed = pipelineStats[history.PipelineFailed]
	}

	pendingTaskCount, _, _, err := s.historyStore.GetPendingTaskStats()
	if err == nil {
		stats.PendingTasks = pendingTaskCount
	}

	responses, err := s.historyStore.GetBrokerResponses("", true, 1000)
	if err == nil {
		stats.NeedsReview = len(responses)
	}

	pendingForms, _, _, _, _, _ := s.historyStore.GetFormStats()

	stats.PendingTasks = pendingForms + pendingTaskCount + stats.NeedsReview

	return stats
}
