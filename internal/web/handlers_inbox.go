package web

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/robindubreuil/eraser/internal/history"
	"github.com/robindubreuil/eraser/internal/inbox"
)

func (s *Server) handleAPIInboxScan(w http.ResponseWriter, r *http.Request) {
	if s.config == nil || !s.config.Inbox.Enabled {
		s.renderPartial(w, "partials/inbox-not-configured.html", nil)
		return
	}

	monitor := inbox.NewMonitor(s.config.Inbox, s.brokerDB.Brokers)

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	if err := monitor.Connect(ctx); err != nil {
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "red",
			"BorderColor": "red",
			"TextColor":   "red-700",
			"Title":       "Failed to connect to inbox",
			"Message":     "Check your IMAP settings and try again.",
		})
		return
	}
	defer monitor.Disconnect() //nolint:errcheck

	emails, err := monitor.FetchBrokerEmails(ctx, 7)
	if err != nil {
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "red",
			"BorderColor": "red",
			"TextColor":   "red-700",
			"Title":       "Failed to fetch emails",
			"Message":     "An error occurred while fetching emails. Please try again.",
		})
		return
	}

	if s.config.Inbox.ArchiveFolder != "" {
		archiveEmails, err := monitor.FetchBrokerEmailsFromFolder(ctx, s.config.Inbox.ArchiveFolder, 7)
		if err != nil {
			log.Printf("Warning: failed to fetch from archive folder %s: %v", s.config.Inbox.ArchiveFolder, err)
		} else {
			emails = append(emails, archiveEmails...)
		}
	}

	if len(emails) == 0 {
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "blue",
			"BorderColor": "blue",
			"TextColor":   "blue-700",
			"Title":       "No new broker emails found.",
			"Message":     "No emails from known data brokers in the last 7 days.",
		})
		return
	}

	var success, formRequired, confirmRequired, rejected, unknown int
	var processedUIDs []uint32
	for _, email := range emails {
		classified := inbox.ClassifyResponse(&email)
		processedUIDs = append(processedUIDs, email.UID)

		bodyContent := email.Body
		if bodyContent == "" {
			bodyContent = email.HTMLBody
		}

		brokerResp := &history.BrokerResponse{
			BrokerID:     email.BrokerID,
			BrokerName:   email.BrokerName,
			ResponseType: string(classified.Type),
			EmailFrom:    email.From,
			EmailSubject: email.Subject,
			EmailBody:    bodyContent,
			FormURL:      classified.FormURL,
			ConfirmURL:   classified.ConfirmURL,
			Confidence:   classified.Confidence,
			NeedsReview:  classified.NeedsReview,
			ReceivedAt:   email.ReceivedAt,
		}

		if s.historyStore != nil {
			s.historyStore.AddBrokerResponse(brokerResp) //nolint:errcheck
		}

		switch classified.Type {
		case inbox.ResponseSuccess:
			success++
		case inbox.ResponseFormRequired:
			formRequired++
		case inbox.ResponseConfirmationRequired:
			confirmRequired++
		case inbox.ResponseRejected:
			rejected++
		default:
			unknown++
		}
	}

	var archived int
	if s.config.Inbox.AutoArchive && len(processedUIDs) > 0 {
		if err := monitor.ArchiveEmails(processedUIDs, s.config.Inbox.ArchiveFolder); err != nil {
			log.Printf("Warning: failed to archive emails: %v", err)
		} else {
			archived = len(processedUIDs)
			log.Printf("Archived %d emails to %s folder", archived, s.config.Inbox.ArchiveFolder)
		}
	}

	s.renderPartial(w, "partials/scan-result.html", map[string]interface{}{
		"Total":           len(emails),
		"Success":         success,
		"FormRequired":    formRequired,
		"ConfirmRequired": confirmRequired,
		"Rejected":        rejected,
		"Unknown":         unknown,
		"Link":            "/tasks",
		"RefreshLink":     "/pipeline",
	})
}

func (s *Server) handleAPIInboxRescan(w http.ResponseWriter, r *http.Request) {
	if s.config == nil || !s.config.Inbox.Enabled {
		s.renderPartial(w, "partials/inbox-not-configured.html", nil)
		return
	}

	clearFirst := r.URL.Query().Get("clear") == "true"
	if clearFirst && s.historyStore != nil {
		if err := s.historyStore.ClearBrokerResponses(); err != nil {
			s.renderPartial(w, "partials/alert.html", map[string]interface{}{
				"BgColor":     "red",
				"BorderColor": "red",
				"TextColor":   "red-700",
				"Title":       "Failed to clear responses",
				"Message":     "An error occurred. Please try again.",
			})
			return
		}
	}

	monitor := inbox.NewMonitor(s.config.Inbox, s.brokerDB.Brokers)

	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()

	if err := monitor.Connect(ctx); err != nil {
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "red",
			"BorderColor": "red",
			"TextColor":   "red-700",
			"Title":       "Failed to connect to inbox",
			"Message":     "Check your IMAP settings and try again.",
		})
		return
	}
	defer monitor.Disconnect() //nolint:errcheck

	emails, err := monitor.FetchBrokerEmails(ctx, 30)
	if err != nil {
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "red",
			"BorderColor": "red",
			"TextColor":   "red-700",
			"Title":       "Failed to fetch emails",
			"Message":     "An error occurred while fetching emails. Please try again.",
		})
		return
	}

	if s.config.Inbox.ArchiveFolder != "" {
		archiveEmails, err := monitor.FetchBrokerEmailsFromFolder(ctx, s.config.Inbox.ArchiveFolder, 30)
		if err != nil {
			log.Printf("Warning: failed to fetch from archive folder %s: %v", s.config.Inbox.ArchiveFolder, err)
		} else {
			emails = append(emails, archiveEmails...)
		}
	}

	if len(emails) == 0 {
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "blue",
			"BorderColor": "blue",
			"TextColor":   "blue-700",
			"Title":       "No broker emails found.",
			"Message":     "No emails from known data brokers in the last 30 days.",
		})
		return
	}

	var success, formRequired, confirmRequired, rejected, pending, unknown int
	var updated, inserted int
	for _, email := range emails {
		classified := inbox.ClassifyResponse(&email)

		bodyContent := email.Body
		if bodyContent == "" {
			bodyContent = email.HTMLBody
		}

		if s.historyStore != nil {
			existing, _ := s.historyStore.FindBrokerResponseBySubject(email.BrokerID, email.Subject)
			if existing != nil {
				err := s.historyStore.UpdateBrokerResponseClassification(
					existing.ID,
					string(classified.Type),
					classified.FormURL,
					classified.ConfirmURL,
					classified.Confidence,
					classified.NeedsReview,
				)
				if err == nil {
					updated++
				}
				if existing.EmailBody == "" && bodyContent != "" {
					s.historyStore.UpdateBrokerResponseBody(existing.ID, bodyContent) //nolint:errcheck
				}
			} else {
				brokerResp := &history.BrokerResponse{
					BrokerID:     email.BrokerID,
					BrokerName:   email.BrokerName,
					ResponseType: string(classified.Type),
					EmailFrom:    email.From,
					EmailSubject: email.Subject,
					EmailBody:    bodyContent,
					FormURL:      classified.FormURL,
					ConfirmURL:   classified.ConfirmURL,
					Confidence:   classified.Confidence,
					NeedsReview:  classified.NeedsReview,
					ReceivedAt:   email.ReceivedAt,
				}
				if err := s.historyStore.AddBrokerResponse(brokerResp); err == nil { //nolint:errcheck
					inserted++
				}
			}
		}

		switch classified.Type {
		case inbox.ResponseSuccess:
			success++
		case inbox.ResponseFormRequired:
			formRequired++
		case inbox.ResponseConfirmationRequired:
			confirmRequired++
		case inbox.ResponseRejected:
			rejected++
		case inbox.ResponsePending:
			pending++
		default:
			unknown++
		}
	}

	s.renderPartial(w, "partials/rescan-result.html", map[string]interface{}{
		"Total":           len(emails),
		"Updated":         updated,
		"Inserted":        inserted,
		"Success":         success,
		"FormRequired":    formRequired,
		"ConfirmRequired": confirmRequired,
		"Pending":         pending,
		"Rejected":        rejected,
		"Unknown":         unknown,
	})
}

func (s *Server) handleAPIReclassify(w http.ResponseWriter, r *http.Request) {
	if s.historyStore == nil {
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "red",
			"BorderColor": "red",
			"TextColor":   "red-700",
			"Title":       "Database not available.",
		})
		return
	}

	responses, err := s.historyStore.GetAllBrokerResponses()
	if err != nil {
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "red",
			"BorderColor": "red",
			"TextColor":   "red-700",
			"Title":       "Failed to get responses",
			"Message":     "An error occurred. Please try again.",
		})
		return
	}

	if len(responses) == 0 {
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "blue",
			"BorderColor": "blue",
			"TextColor":   "blue-700",
			"Title":       "No responses to reclassify.",
		})
		return
	}

	var missingBodies int
	for _, resp := range responses {
		if resp.EmailBody == "" {
			missingBodies++
		}
	}

	var bodiesUpdated int
	if missingBodies > 0 && s.config.Inbox.Server != "" && s.brokerDB != nil {
		log.Printf("Found %d records missing email bodies, fetching from IMAP...", missingBodies)

		monitor := inbox.NewMonitor(s.config.Inbox, s.brokerDB.Brokers)

		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		if err := monitor.Connect(ctx); err != nil {
			log.Printf("Warning: failed to connect to IMAP for body fetch: %v", err)
		} else {
			defer monitor.Disconnect() //nolint:errcheck

			var allEmails []inbox.Email

			emails, err := monitor.FetchRecentEmails(ctx, 30)
			if err != nil {
				log.Printf("Warning: failed to fetch from INBOX: %v", err)
			} else {
				allEmails = append(allEmails, emails...)
			}

			if s.config.Inbox.ArchiveFolder != "" {
				archiveEmails, err := monitor.FetchBrokerEmailsFromFolder(ctx, s.config.Inbox.ArchiveFolder, 30)
				if err != nil {
					log.Printf("Warning: failed to fetch from archive folder: %v", err)
				} else {
					allEmails = append(allEmails, archiveEmails...)
				}
			}

			log.Printf("Fetched %d emails from IMAP", len(allEmails))

			emailBodies := make(map[string]string)
			for _, email := range allEmails {
				if email.BrokerID == "" {
					continue
				}
				key := email.BrokerID + "|" + email.Subject
				body := email.Body
				if body == "" {
					body = email.HTMLBody
				}
				if body != "" {
					emailBodies[key] = body
				}
			}

			for _, resp := range responses {
				if resp.EmailBody != "" {
					continue
				}
				key := resp.BrokerID + "|" + resp.EmailSubject
				if body, ok := emailBodies[key]; ok {
					err := s.historyStore.UpdateBrokerResponseBody(resp.ID, body)
					if err == nil {
						bodiesUpdated++
						for i := range responses {
							if responses[i].ID == resp.ID {
								responses[i].EmailBody = body
								break
							}
						}
					}
				}
			}
			log.Printf("Updated %d records with email bodies from IMAP", bodiesUpdated)
		}
	}

	var updated, unchanged int
	var pending, rejected, success, formRequired, confirmRequired, unknown int

	for _, resp := range responses {
		var newType inbox.ResponseType
		var confidence float64
		var needsReview bool
		var formURL, confirmURL string

		if resp.EmailBody != "" {
			email := &inbox.Email{
				From:    resp.EmailFrom,
				Subject: resp.EmailSubject,
				Body:    resp.EmailBody,
			}
			classified := inbox.ClassifyResponse(email)
			newType = classified.Type
			confidence = classified.Confidence
			needsReview = classified.NeedsReview
			formURL = classified.FormURL
			confirmURL = classified.ConfirmURL
		} else {
			newType, confidence, needsReview = inbox.ClassifyBySubjectOnly(resp.EmailSubject)
			formURL = resp.FormURL
			confirmURL = resp.ConfirmURL
		}

		if string(newType) != resp.ResponseType || (resp.ResponseType == "unknown" && newType != inbox.ResponseUnknown) {
			err := s.historyStore.UpdateBrokerResponseClassification(
				resp.ID,
				string(newType),
				formURL,
				confirmURL,
				confidence,
				needsReview,
			)
			if err == nil {
				updated++
			}
		} else {
			unchanged++
		}

		switch newType {
		case inbox.ResponseSuccess:
			success++
		case inbox.ResponseFormRequired:
			formRequired++
		case inbox.ResponseConfirmationRequired:
			confirmRequired++
		case inbox.ResponseRejected:
			rejected++
		case inbox.ResponsePending:
			pending++
		default:
			unknown++
		}
	}

	s.renderPartial(w, "partials/reclassify-result.html", map[string]interface{}{
		"Total":           len(responses),
		"Updated":         updated,
		"Unchanged":       unchanged,
		"Pending":         pending,
		"Rejected":        rejected,
		"Success":         success,
		"FormRequired":    formRequired,
		"ConfirmRequired": confirmRequired,
		"Unknown":         unknown,
	})
}
