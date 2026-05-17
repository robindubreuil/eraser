package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/robindubreuil/eraser/internal/email"
	"github.com/robindubreuil/eraser/internal/history"
	"github.com/robindubreuil/eraser/internal/template"
)

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) { //nolint:revive
	stats := s.getStats()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"total_brokers":%d,"sent":%d,"failed":%d}`, stats.TotalBrokers, stats.Sent, stats.Failed)
}

func (s *Server) handleAPIBrokers(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	region := r.URL.Query().Get("region")
	status := r.URL.Query().Get("status")

	brokers := s.getBrokersWithStatus(search, category, region, status)

	s.renderPartial(w, "partials/broker-list.html", map[string]interface{}{
		"Brokers":  brokers,
		"Filtered": len(brokers),
		"Total":    len(s.brokerDB.Brokers),
	})
}

func (s *Server) handleAPIHistory(w http.ResponseWriter, r *http.Request) { //nolint:revive
	s.renderPartial(w, "partials/history-list.html", map[string]interface{}{
		"History": s.getRecentHistory(50),
	})
}

func (s *Server) handleAPIDeleteFailed(w http.ResponseWriter, r *http.Request) { //nolint:revive
	w.Header().Set("Content-Type", "application/json")

	if s.historyStore == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Database not available"}) //nolint:errcheck
		return
	}

	deleted, err := s.historyStore.DeleteByStatus(history.StatusFailed)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to delete records"}) //nolint:errcheck
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"deleted": deleted,
		"message": fmt.Sprintf("Deleted %d failed records", deleted),
	})
}

func (s *Server) handleAPISendOne(w http.ResponseWriter, r *http.Request) {
	if !s.rateLimiter.Allow("send") {
		w.WriteHeader(http.StatusTooManyRequests)
		s.renderPartial(w, "partials/send-status.html", map[string]interface{}{
			"Status": "rate-limited",
		})
		return
	}

	brokerID := chi.URLParam(r, "brokerID")

	br := s.brokerDB.FindByID(brokerID)
	if br == nil {
		w.WriteHeader(http.StatusNotFound)
		s.renderPartial(w, "partials/send-status.html", map[string]interface{}{
			"Status": "not-found",
		})
		return
	}

	if s.config == nil || s.config.Email.Provider == "" {
		w.WriteHeader(http.StatusBadRequest)
		s.renderPartial(w, "partials/email-not-configured.html", nil)
		return
	}

	sender, err := email.NewSender(s.config.Email)
	if err != nil {
		s.renderPartial(w, "partials/send-status.html", map[string]interface{}{
			"Status": "error",
			"Error":  "Failed to create email sender",
		})
		return
	}

	tmplName := template.TemplateForRegion(br.Region, s.config.Options.Locale, s.config.Options.Template)
	rendered, err := s.tmplEngine.Render(tmplName, s.config.Profile, *br)
	if err != nil {
		s.renderPartial(w, "partials/send-status.html", map[string]interface{}{
			"Status": "template-error",
			"Error":  "Failed to render email template",
		})
		return
	}

	msg := email.Message{
		To:      br.Email,
		From:    s.config.Email.From,
		Subject: rendered.Subject,
		Body:    rendered.Body,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result := sender.Send(ctx, msg)

	record := &history.Record{
		BrokerID:   br.ID,
		BrokerName: br.Name,
		Email:      br.Email,
		Template:   tmplName,
		SentAt:     time.Now(),
	}

	if result.Success {
		record.Status = history.StatusSent
		record.MessageID = result.MessageID
	} else {
		record.Status = history.StatusFailed
		if result.Error != nil {
			record.Error = result.Error.Error()
		}
	}

	if s.historyStore != nil {
		s.historyStore.Add(record) //nolint:errcheck
	}

	if result.Success {
		s.renderPartial(w, "partials/send-status.html", map[string]interface{}{
			"Status": "sent",
		})
	} else {
		errMsg := "Unknown error"
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		s.renderPartial(w, "partials/send-status.html", map[string]interface{}{
			"Status": "failed",
			"Error":  errMsg,
		})
	}
}

func (s *Server) handleAPISendAll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !s.rateLimiter.Allow("send-all") {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "Rate limit exceeded. Please wait before sending another batch."}) //nolint:errcheck
		return
	}

	if activeJob := s.jobManager.GetActive(); activeJob != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"error":  "A send job is already in progress",
			"job_id": activeJob.ID,
		})
		return
	}

	if s.config == nil || s.config.Email.Provider == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Email not configured. Please configure email settings first."}) //nolint:errcheck
		return
	}

	search := r.FormValue("search")
	category := r.FormValue("category")
	region := r.FormValue("region")
	status := r.FormValue("status")

	if status == "" {
		status = "pending"
	}

	toSend := s.getBrokersWithStatus(search, category, region, status)

	if len(toSend) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No pending brokers to send to."}) //nolint:errcheck
		return
	}

	sender, err := email.NewSender(s.config.Email)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to initialize email sender"}) //nolint:errcheck
		return
	}

	job := s.jobManager.Create(len(toSend))

	brokerIDs := make([]string, len(toSend))
	for i, b := range toSend {
		brokerIDs[i] = b.ID
	}

	jobState := &PersistentJobState{
		ID:               job.ID,
		Status:           job.Status,
		Sent:             0,
		Failed:           0,
		Total:            len(toSend),
		StartedAt:        job.StartedAt,
		RemainingBrokers: brokerIDs,
		Search:           search,
		Category:         category,
		Region:           region,
		StatusFilter:     status,
	}
	if err := s.jobPersistence.Save(jobState); err != nil {
		log.Printf("Warning: failed to save job state: %v", err)
	}

	go s.processSendJob(job, toSend, sender)

	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"job_id": job.ID,
		"total":  len(toSend),
	})
}

func (s *Server) processSendJob(job *Job, toSend []BrokerWithStatus, sender email.Sender) {
	sent := 0
	failed := 0
	rateLimitMs := s.config.Options.RateLimitMs
	if rateLimitMs == 0 {
		rateLimitMs = 2000
	}

	dailyLimit := 250
	job.DailyLimit = dailyLimit

	remaining := make([]string, len(toSend))
	for i, b := range toSend {
		remaining[i] = b.ID
	}

	for i, b := range toSend {
		if job.IsCancelled() {
			break
		}

		if sent >= dailyLimit {
			job.DaySent = sent
			job.Status = JobStatusPaused
			job.Error = fmt.Sprintf("Daily limit of %d emails reached. Remaining %d brokers will be sent when you restart tomorrow.", dailyLimit, len(remaining))
			s.saveJobProgress(job, sent, failed, remaining)
			log.Printf("Job paused: daily limit of %d reached, %d remaining", dailyLimit, len(remaining))
			return
		}

		job.Update(sent, failed, b.Name)

		tmplName := template.TemplateForRegion(b.Region, s.config.Options.Locale, s.config.Options.Template)
		rendered, err := s.tmplEngine.Render(tmplName, s.config.Profile, b.Broker)
		if err != nil {
			failed++
			job.Update(sent, failed, b.Name)
			remaining = remaining[1:]
			s.saveJobProgress(job, sent, failed, remaining)
			continue
		}

		msg := email.Message{
			To:      b.Email,
			From:    s.config.Email.From,
			Subject: rendered.Subject,
			Body:    rendered.Body,
		}

		ctx, cancel := context.WithTimeout(job.Context(), 30*time.Second)
		result := sender.Send(ctx, msg)
		cancel()

		record := &history.Record{
			BrokerID:   b.ID,
			BrokerName: b.Name,
			Email:      b.Email,
			Template:   tmplName,
			SentAt:     time.Now(),
		}

		if result.Success {
			record.Status = history.StatusSent
			record.MessageID = result.MessageID
			sent++
			job.ResetAuthFailures()
		} else {
			record.Status = history.StatusFailed
			errMsg := ""
			if result.Error != nil {
				errMsg = result.Error.Error()
				record.Error = errMsg
			}
			failed++

			if strings.Contains(strings.ToLower(errMsg), "auth") {
				if job.RecordAuthFailure() {
					if s.historyStore != nil {
						s.historyStore.Add(record) //nolint:errcheck
					}
					remaining = remaining[1:]
					s.saveJobProgress(job, sent, failed, remaining)
					job.StopWithError("auth", "Stopped due to repeated authentication failures. Your email provider may have rate-limited or blocked your account. Please check your email settings and try again later.")
					log.Printf("Job stopped: repeated auth failures after %d sent, %d failed", sent, failed)
					return
				}
			}
		}

		if s.historyStore != nil {
			s.historyStore.Add(record) //nolint:errcheck
		}

		job.Update(sent, failed, b.Name)

		remaining = remaining[1:]
		s.saveJobProgress(job, sent, failed, remaining)

		if i < len(toSend)-1 && !job.IsCancelled() {
			time.Sleep(time.Duration(rateLimitMs) * time.Millisecond)
		}
	}

	job.Complete()
	if err := s.jobPersistence.Clear(); err != nil {
		log.Printf("Warning: failed to clear job state: %v", err)
	}
}

func (s *Server) saveJobProgress(job *Job, sent, failed int, remaining []string) {
	state := &PersistentJobState{
		ID:               job.ID,
		Status:           job.Status,
		Sent:             sent,
		Failed:           failed,
		Total:            job.Total,
		StartedAt:        job.StartedAt,
		RemainingBrokers: remaining,
	}
	if err := s.jobPersistence.Save(state); err != nil {
		log.Printf("Warning: failed to save job progress: %v", err)
	}
}

func (s *Server) handleAPIPipelineStats(w http.ResponseWriter, r *http.Request) { //nolint:revive
	stats := s.getPipelineStats()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{
		"email_sent": %d,
		"awaiting_response": %d,
		"form_required": %d,
		"form_filled": %d,
		"awaiting_captcha": %d,
		"captcha_solved": %d,
		"awaiting_confirmation": %d,
		"confirmed": %d,
		"rejected": %d,
		"failed": %d,
		"pending_tasks": %d,
		"needs_review": %d
	}`,
		stats.EmailSent, stats.AwaitingResponse, stats.FormRequired, stats.FormFilled,
		stats.AwaitingCaptcha, stats.CaptchaSolved, stats.AwaitingConfirmation,
		stats.Confirmed, stats.Rejected, stats.Failed, stats.PendingTasks, stats.NeedsReview)
}

func (s *Server) handleAPIResponses(w http.ResponseWriter, r *http.Request) {
	responseType := r.URL.Query().Get("type")
	needsReview := r.URL.Query().Get("needs_review") == "true"

	var responses []history.BrokerResponse
	if s.historyStore != nil {
		responses, _ = s.historyStore.GetBrokerResponses(responseType, needsReview, 50)
	}

	s.renderPartial(w, "partials/response-list.html", map[string]interface{}{
		"Responses": responses,
	})
}

func (s *Server) handleAPITasks(w http.ResponseWriter, r *http.Request) {
	taskType := r.URL.Query().Get("type")
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}

	var tasks []history.PendingTask
	if s.historyStore != nil {
		tasks, _ = s.historyStore.GetPendingTasks(history.TaskType(taskType), status)
	}

	s.renderPartial(w, "partials/task-list.html", map[string]interface{}{
		"Tasks":    tasks,
		"TaskType": taskType,
		"Status":   status,
	})
}

func (s *Server) handleAPIJobActive(w http.ResponseWriter, r *http.Request) { //nolint:revive
	w.Header().Set("Content-Type", "application/json")

	job := s.jobManager.GetActive()
	if job == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"job": nil}) //nolint:errcheck
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"job": job.ToJSON()}) //nolint:errcheck
}

func (s *Server) handleAPIJobStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	jobID := chi.URLParam(r, "jobID")
	job := s.jobManager.Get(jobID)

	if job == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "job not found"}) //nolint:errcheck
		return
	}

	json.NewEncoder(w).Encode(job.ToJSON()) //nolint:errcheck
}

func (s *Server) handleAPIJobCancel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	jobID := chi.URLParam(r, "jobID")
	job := s.jobManager.Get(jobID)

	if job == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "job not found"}) //nolint:errcheck
		return
	}

	job.Cancel()
	json.NewEncoder(w).Encode(map[string]string{"status": "canceled"}) //nolint:errcheck
}
