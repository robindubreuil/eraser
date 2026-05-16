package web

import (
	"net/http"

	"github.com/robindubreuil/eraser/internal/config"
	"github.com/robindubreuil/eraser/internal/email"
)

func (s *Server) handleSettingsInbox(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderSettingsWithMessage(w, r, "Failed to parse form", false)
		return
	}

	emailAddr := r.FormValue("inbox_email")
	password := r.FormValue("inbox_password")

	if emailAddr == "" || password == "" {
		s.renderSettingsWithMessage(w, r, "Email and password are required", false)
		return
	}

	if err := email.ValidateEmail(emailAddr); err != nil {
		s.renderSettingsWithMessage(w, r, "Invalid email address", false)
		return
	}

	if s.config == nil {
		s.config = &config.Config{}
	}

	s.config.Inbox = config.InboxConfig{
		Enabled:  true,
		Provider: "gmail",
		Email:    emailAddr,
		Password: password,
	}

	if err := config.Save(s.configPath, s.config); err != nil {
		s.renderSettingsWithMessage(w, r, "Failed to save configuration", false)
		return
	}

	s.renderSettingsWithMessage(w, r, "Inbox monitoring enabled successfully!", true)
}

func (s *Server) renderSettingsWithMessage(w http.ResponseWriter, r *http.Request, message string, success bool) {
	data := map[string]interface{}{
		"Title":        "Settings",
		"Config":       s.config,
		"InboxMessage": message,
		"InboxSuccess": success,
	}
	s.renderWithCSRF(w, r, "settings.html", data)
}
