package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/robindubreuil/eraser/internal/config"
	"github.com/robindubreuil/eraser/internal/email"
)

func countryToLocale(country string) string {
	c := strings.ToLower(strings.TrimSpace(country))
	switch c {
	case "france", "fr":
		return "fr"
	case "belgium", "be", "belgique":
		return "fr-be"
	case "switzerland", "ch", "suisse":
		return "fr-ch"
	case "luxembourg", "lu":
		return "fr-lu"
	case "germany", "de", "allemagne":
		return "de"
	case "spain", "es", "espagne":
		return "es"
	case "italy", "it", "italie":
		return "it"
	case "netherlands", "nl", "pays-bas":
		return "nl"
	case "united kingdom", "uk", "gb", "royaume-uni":
		return "en-gb"
	case "united states", "us", "usa", "etats-unis":
		return "en-us"
	case "canada":
		return "en-ca"
	case "poland", "pl", "pologne":
		return "pl"
	case "sweden", "se", "suede":
		return "sv"
	case "ireland", "ie", "irlande":
		return "en-ie"
	case "portugal", "pt":
		return "pt"
	case "brazil", "br", "bresil":
		return "pt-br"
	default:
		return ""
	}
}

func (s *Server) handleSetupWelcome(w http.ResponseWriter, r *http.Request) { //nolint:revive
	data := map[string]interface{}{
		"Title": "Setup",
		"Step":  "welcome",
	}
	s.renderWithCSRF(w, r, "setup/welcome.html", data)
}

func (s *Server) handleSetupProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		profile := config.Profile{
			FirstName:   strings.TrimSpace(r.FormValue("first_name")),
			LastName:    strings.TrimSpace(r.FormValue("last_name")),
			Email:       strings.TrimSpace(r.FormValue("email")),
			Address:     strings.TrimSpace(r.FormValue("address")),
			City:        strings.TrimSpace(r.FormValue("city")),
			State:       strings.TrimSpace(r.FormValue("state")),
			ZipCode:     strings.TrimSpace(r.FormValue("zip_code")),
			Country:     strings.TrimSpace(r.FormValue("country")),
			Phone:       strings.TrimSpace(r.FormValue("phone")),
			DateOfBirth: strings.TrimSpace(r.FormValue("dob")),
		}

		errors := make(map[string]string)
		if profile.FirstName == "" {
			errors["first_name"] = "First name is required"
		}
		if profile.LastName == "" {
			errors["last_name"] = "Last name is required"
		}
		if profile.Email == "" {
			errors["email"] = "Email is required"
		} else if err := email.ValidateEmail(profile.Email); err != nil {
			errors["email"] = "Please enter a valid email address"
		}

		if len(errors) > 0 {
			data := map[string]interface{}{
				"Title":   "Setup - Profile",
				"Step":    "profile",
				"Profile": profile,
				"Errors":  errors,
			}
			s.renderWithCSRF(w, r, "setup/profile.html", data)
			return
		}

		session := s.getOrCreateSession(w, r)
		if session == nil {
			http.Error(w, "Session error", http.StatusInternalServerError)
			return
		}
		s.updateSession(r, func(sess *Session) {
			sess.Step = "email"
			sess.Profile = profile
		})
		http.Redirect(w, r, "/setup/email", http.StatusFound)
		return
	}

	session := s.getSession(r)
	var profile config.Profile
	if session != nil {
		profile = session.Profile
	}
	data := map[string]interface{}{
		"Title":   "Setup - Profile",
		"Step":    "profile",
		"Profile": profile,
	}
	s.renderWithCSRF(w, r, "setup/profile.html", data)
}

func (s *Server) handleSetupEmail(w http.ResponseWriter, r *http.Request) {
	session := s.getSession(r)

	if session == nil || session.Profile.FirstName == "" {
		http.Redirect(w, r, "/setup/profile", http.StatusFound)
		return
	}

	if r.Method == "POST" {
		emailCfg := config.EmailConfig{
			Provider: "smtp",
			From:     session.Profile.Email,
		}

		errors := make(map[string]string)

		provider := r.FormValue("email_provider")
		emailCfg.SMTP.Host = strings.TrimSpace(r.FormValue("smtp_host"))
		if _, err := fmt.Sscanf(r.FormValue("smtp_port"), "%d", &emailCfg.SMTP.Port); err != nil {
			emailCfg.SMTP.Port = 0
		}
		emailCfg.SMTP.Username = strings.TrimSpace(r.FormValue("smtp_username"))
		emailCfg.SMTP.Password = strings.TrimSpace(r.FormValue("smtp_password"))
		emailCfg.SMTP.UseTLS = r.FormValue("smtp_tls") == "on"

		if emailCfg.SMTP.Host == "" {
			switch provider {
			case "orange":
				emailCfg.SMTP.Host = "smtp.orange.fr"
			case "sfr":
				emailCfg.SMTP.Host = "smtp.sfr.fr"
			case "free":
				emailCfg.SMTP.Host = "smtp.free.fr"
			case "bouygues":
				emailCfg.SMTP.Host = "smtp.bbox.fr"
			case "outlook":
				emailCfg.SMTP.Host = "smtp-mail.outlook.com"
			default:
				emailCfg.SMTP.Host = "smtp.gmail.com"
			}
		}
		if emailCfg.SMTP.Port == 0 {
			emailCfg.SMTP.Port = 465
		}
		if !emailCfg.SMTP.UseTLS {
			emailCfg.SMTP.UseTLS = true
		}

		if emailCfg.SMTP.Host == "" {
			errors["smtp_host"] = "SMTP host is required"
		}
		if emailCfg.SMTP.Port == 0 {
			errors["smtp_port"] = "SMTP port is required"
		}
		if emailCfg.SMTP.Username == "" {
			errors["smtp_username"] = "Email address is required"
		}
		if emailCfg.SMTP.Password == "" {
			errors["smtp_password"] = "Password is required"
		}

		if len(errors) > 0 {
			data := map[string]interface{}{
				"Title":   "Setup - Gmail",
				"Step":    "email",
				"Profile": session.Profile,
				"Email":   emailCfg,
				"Errors":  errors,
			}
			s.renderWithCSRF(w, r, "setup/email.html", data)
			return
		}

		s.updateSession(r, func(sess *Session) {
			sess.Email = emailCfg
			sess.Step = "test"
		})
		http.Redirect(w, r, "/setup/test", http.StatusFound)
		return
	}

	emailCfg := session.Email
	if emailCfg.SMTP.Host == "" {
		emailCfg.SMTP.Host = "smtp.gmail.com"
		emailCfg.SMTP.Port = 465
		emailCfg.SMTP.UseTLS = true
	}

	data := map[string]interface{}{
		"Title":   "Setup - Gmail",
		"Step":    "email",
		"Profile": session.Profile,
		"Email":   emailCfg,
	}
	s.renderWithCSRF(w, r, "setup/email.html", data)
}

func (s *Server) handleSetupTest(w http.ResponseWriter, r *http.Request) {
	session := s.getSession(r)

	if session == nil || session.Profile.FirstName == "" {
		http.Redirect(w, r, "/setup/profile", http.StatusFound)
		return
	}
	if session.Email.Provider == "" {
		http.Redirect(w, r, "/setup/email", http.StatusFound)
		return
	}

	data := map[string]interface{}{
		"Title":   "Setup - Test",
		"Step":    "test",
		"Profile": session.Profile,
		"Email":   session.Email,
	}
	s.renderWithCSRF(w, r, "setup/test.html", data)
}

func (s *Server) handleSetupTestSend(w http.ResponseWriter, r *http.Request) {
	session := s.getSession(r)

	if session == nil || session.Email.Provider == "" {
		w.WriteHeader(http.StatusBadRequest)
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "red",
			"BorderColor": "red",
			"TextColor":   "red-600",
			"Title":       "Email not configured. Please go back to the email step.",
		})
		return
	}

	sender, err := email.NewSender(session.Email)
	if err != nil {
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "red",
			"BorderColor": "red",
			"TextColor":   "red-700",
			"Title":       "Configuration error:",
			"Message":     err.Error(),
			"HTML":        template.HTML(`Please check your email settings and try again.`),
		})
		return
	}

	testMsg := email.Message{
		To:      session.Profile.Email,
		From:    session.Email.From,
		Subject: "Eraser Test Email",
		Body: fmt.Sprintf(`Hello %s,

This is a test email from Eraser to verify your email configuration is working correctly.

If you received this email, your setup is complete and you're ready to start sending data removal requests!

Best,
Eraser`, session.Profile.FirstName),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result := sender.Send(ctx, testMsg)
	if !result.Success {
		errMsg := "Unknown error"
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		s.renderPartial(w, "partials/alert.html", map[string]interface{}{
			"BgColor":     "red",
			"BorderColor": "red",
			"TextColor":   "red-700",
			"Title":       "Test failed:",
			"Message":     errMsg,
			"HTML":        template.HTML(`Please check your email configuration and try again.`),
			"Footer":      template.HTML(`<a href="/setup/email" class="text-indigo-600 hover:text-indigo-800 font-medium">Back to Email Settings</a>`),
		})
		return
	}

	s.renderPartial(w, "partials/alert.html", map[string]interface{}{
		"BgColor":     "green",
		"BorderColor": "green",
		"TextColor":   "green-700",
		"Title":       "Success!",
		"Message":     "Test email sent to your address. Check your inbox (and spam folder) for the test message.",
		"Footer":      template.HTML(`<a href="/setup/complete" class="inline-flex items-center px-6 py-3 bg-indigo-600 text-white font-medium rounded-md hover:bg-indigo-700">Complete Setup</a>`),
	})
}

func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	session := s.getSession(r)

	if session == nil || session.Profile.FirstName == "" || session.Email.Provider == "" {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	cfg := &config.Config{
		Profile: session.Profile,
		Email:   session.Email,
		Options: config.Options{
			Template:    "auto",
			Locale:      countryToLocale(session.Profile.Country),
			RateLimitMs: 2000,
		},
	}

	if err := config.Save(s.configPath, cfg); err != nil {
		data := map[string]interface{}{
			"Title": "Setup - Error",
			"Error": err.Error(),
		}
		s.renderWithCSRF(w, r, "setup/complete.html", data)
		return
	}

	s.config = cfg

	s.clearSession(w, r)

	data := map[string]interface{}{
		"Title":   "Setup Complete",
		"Step":    "complete",
		"Profile": session.Profile,
	}
	s.renderWithCSRF(w, r, "setup/complete.html", data)
}

func (s *Server) getOrCreateSession(w http.ResponseWriter, r *http.Request) *Session {
	cookie, err := r.Cookie("eraser_session")
	if err == nil && cookie.Value != "" {
		session := s.sessions.Get(cookie.Value)
		if session != nil {
			return session
		}
	}

	sessionID, err := s.sessions.Create()
	if err != nil {
		return nil
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "eraser_session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   1800,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	return s.sessions.Get(sessionID)
}

func (s *Server) getSession(r *http.Request) *Session {
	cookie, err := r.Cookie("eraser_session")
	if err != nil || cookie.Value == "" {
		return nil
	}
	return s.sessions.Get(cookie.Value)
}

func (s *Server) updateSession(r *http.Request, updateFn func(*Session)) bool {
	cookie, err := r.Cookie("eraser_session")
	if err != nil || cookie.Value == "" {
		return false
	}
	return s.sessions.Update(cookie.Value, updateFn)
}

func (s *Server) clearSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("eraser_session")
	if err == nil && cookie.Value != "" {
		s.sessions.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "eraser_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}
