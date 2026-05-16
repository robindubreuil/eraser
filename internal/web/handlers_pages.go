package web

import (
	"net/http"

	"github.com/robindubreuil/eraser/internal/history"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if s.config == nil || s.config.Profile.FirstName == "" {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	data := map[string]interface{}{
		"Title":         "Dashboard",
		"Profile":       s.config.Profile,
		"BrokerCount":   len(s.brokerDB.Brokers),
		"RecentHistory": s.getRecentHistory(10),
		"Stats":         s.getStats(),
		"PipelineStats": s.getPipelineStats(),
	}

	s.renderWithCSRF(w, r, "dashboard.html", data)
}

func (s *Server) handleBrokers(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	region := r.URL.Query().Get("region")
	status := r.URL.Query().Get("status")

	brokers := s.getBrokersWithStatus(search, category, region, status)

	data := map[string]interface{}{
		"Title":      "Data Brokers",
		"Brokers":    brokers,
		"Categories": s.getUniqueCategories(),
		"Regions":    s.getUniqueRegions(),
		"Search":     search,
		"Category":   category,
		"Region":     region,
		"Status":     status,
		"Total":      len(s.brokerDB.Brokers),
		"Filtered":   len(brokers),
	}
	s.renderWithCSRF(w, r, "brokers.html", data)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	allHistory := s.getRecentHistory(1000)

	var filteredHistory []history.Record
	if statusFilter == "sent" || statusFilter == "failed" {
		for _, h := range allHistory {
			if string(h.Status) == statusFilter {
				filteredHistory = append(filteredHistory, h)
			}
		}
	} else {
		filteredHistory = allHistory
	}

	data := map[string]interface{}{
		"Title":        "History",
		"History":      filteredHistory,
		"StatusFilter": statusFilter,
	}
	s.renderWithCSRF(w, r, "history.html", data)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) { //nolint:revive
	data := map[string]interface{}{
		"Title":  "Settings",
		"Config": s.config,
	}
	s.renderWithCSRF(w, r, "settings.html", data)
}

func (s *Server) handlePipeline(w http.ResponseWriter, r *http.Request) {
	if s.config == nil || s.config.Profile.FirstName == "" {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	pipelineStats := s.getPipelineStats()

	var recentResponses []history.BrokerResponse
	if s.historyStore != nil {
		recentResponses, _ = s.historyStore.GetBrokerResponses("", false, 20)
	}

	var pendingTasks []history.PendingTask
	if s.historyStore != nil {
		pendingTasks, _ = s.historyStore.GetPendingTasks("", "pending")
	}

	data := map[string]interface{}{
		"Title":           "Pipeline Status",
		"PipelineStats":   pipelineStats,
		"RecentResponses": recentResponses,
		"PendingTasks":    pendingTasks,
		"InboxConfigured": s.config.Inbox.Enabled,
	}

	s.renderWithCSRF(w, r, "pipeline.html", data)
}

func (s *Server) handleForms(w http.ResponseWriter, r *http.Request) { //nolint:revive
	http.Redirect(w, r, "/tasks", http.StatusFound)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if s.config == nil || s.config.Profile.FirstName == "" {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	taskType := r.URL.Query().Get("type")
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}

	var tasks []history.PendingTask
	var completedTasksList []history.PendingTask
	var forms []history.FormWithStatus
	var reviewItems []history.BrokerResponse
	if s.historyStore != nil {
		tasks, _ = s.historyStore.GetPendingTasks(history.TaskType(taskType), "pending")
		completedTasksList, _ = s.historyStore.GetPendingTasks(history.TaskType(taskType), "completed")
		forms, _ = s.historyStore.GetFormsWithStatus()
		reviewItems, _ = s.historyStore.GetBrokerResponses("", true, 1000)
	}

	pendingTasks, completedTasksCount, skippedTasks := 0, 0, 0
	if s.historyStore != nil {
		pendingTasks, completedTasksCount, skippedTasks, _ = s.historyStore.GetPendingTaskStats()
	}

	pendingForms, filledForms, captchaForms, failedForms, skippedForms := 0, 0, 0, 0, 0
	if s.historyStore != nil {
		pendingForms, filledForms, captchaForms, failedForms, skippedForms, _ = s.historyStore.GetFormStats()
	}

	formsNeedingAction := pendingForms
	totalActionItems := pendingForms + pendingTasks
	needsReviewCount := len(reviewItems)

	data := map[string]interface{}{
		"Title":              "Action Needed",
		"Tasks":              tasks,
		"CompletedTasksList": completedTasksList,
		"Forms":              forms,
		"ReviewItems":        reviewItems,
		"TaskType":           taskType,
		"Status":             status,
		"PendingTasks":       pendingTasks,
		"CompletedTasks":     completedTasksCount,
		"SkippedTasks":       skippedTasks,
		"PendingForms":       pendingForms,
		"FilledForms":        filledForms,
		"CaptchaForms":       captchaForms,
		"FailedForms":        failedForms,
		"SkippedForms":       skippedForms,
		"NeedsReview":        needsReviewCount,
		"FormsNeedingAction": formsNeedingAction,
		"TotalActionItems":   totalActionItems + needsReviewCount,
	}

	s.renderWithCSRF(w, r, "tasks.html", data)
}
