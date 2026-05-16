package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/robindubreuil/eraser/internal/broker"
	"github.com/robindubreuil/eraser/internal/config"
	"github.com/robindubreuil/eraser/internal/history"
	emaTemplate "github.com/robindubreuil/eraser/internal/template"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return newTestServerWithConfig(t, testConfig())
}

func newTestServerWithConfig(t *testing.T, cfg *config.Config) *Server {
	t.Helper()

	brokerDB := &broker.BrokerDatabase{
		Brokers: []broker.Broker{
			{ID: "acxiom", Name: "Acxiom", Email: "privacy@acxiom.com", Region: "us", Category: "people-search"},
			{ID: "corelogic", Name: "CoreLogic", Email: "optout@corelogic.com", Region: "us", Category: "background-check"},
			{ID: "eu-broker", Name: "EU Data Co", Email: "dpo@eudata.eu", Region: "eu", Category: "marketing"},
		},
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "history.db")
	historyStore, err := history.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	t.Cleanup(func() { historyStore.Close() })

	tmplEngine, err := emaTemplate.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	configPath := filepath.Join(dir, "config.yaml")

	csrfKey := make([]byte, 32)
	for i := range csrfKey {
		csrfKey[i] = byte(i)
	}

	s := &Server{
		config:         cfg,
		configPath:     configPath,
		brokerDB:       brokerDB,
		historyStore:   historyStore,
		tmplEngine:     tmplEngine,
		port:           0,
		csrfKey:        csrfKey,
		sessions:       NewSessionStore(context.Background(), 30*time.Minute),
		rateLimiter:    NewRateLimiter(context.Background(), 30, time.Minute),
		jobManager:     NewJobManager(),
		jobPersistence: NewJobPersistence(dir),
	}

	tmpl, err := s.parseTemplates()
	if err != nil {
		t.Fatalf("parseTemplates() error = %v", err)
	}
	s.templates = tmpl

	return s
}

func testConfig() *config.Config {
	return &config.Config{
		Profile: config.Profile{
			FirstName: "Jane",
			LastName:  "Doe",
			Email:     "jane@example.com",
			Address:   "123 Main St",
			City:      "Springfield",
			State:     "IL",
			ZipCode:   "62701",
			Country:   "US",
		},
		Email: config.EmailConfig{
			Provider: "smtp",
			From:     "jane@example.com",
			SMTP: config.SMTPConfig{
				Host:     "smtp.example.com",
				Port:     587,
				Username: "jane@example.com",
				Password: "secret",
				UseTLS:   true,
			},
		},
		Options: config.Options{
			Template:    "generic",
			RateLimitMs: 2000,
		},
	}
}

func executeRequest(t *testing.T, s *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	return executeRequestWithBody(t, s, method, path, nil)
}

func executeRequestWithBody(t *testing.T, s *Server, method, path string, body url.Values) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(body.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}

	rr := httptest.NewRecorder()
	router := s.setupRouter()
	router.ServeHTTP(rr, req)
	return rr
}

func TestHandleDashboard(t *testing.T) {
	tests := []struct {
		name         string
		config       *config.Config
		wantStatus   int
		wantRedirect bool
	}{
		{
			name:         "redirects to /setup when config is nil",
			config:       nil,
			wantStatus:   http.StatusFound,
			wantRedirect: true,
		},
		{
			name: "redirects when profile first name is empty",
			config: &config.Config{
				Profile: config.Profile{FirstName: ""},
			},
			wantStatus:   http.StatusFound,
			wantRedirect: true,
		},
		{
			name:       "returns 200 with valid config",
			config:     testConfig(),
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServerWithConfig(t, tt.config)
			rr := executeRequest(t, s, http.MethodGet, "/")

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
			if tt.wantRedirect {
				loc := rr.Header().Get("Location")
				if loc != "/setup" {
					t.Errorf("Location = %q, want %q", loc, "/setup")
				}
			}
		})
	}
}

func TestHandleBrokers(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/brokers")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Acxiom") {
		t.Error("response should contain broker name Acxiom")
	}
}

func TestHandleBrokers_SearchFilter(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/brokers?search=acxiom")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Acxiom") {
		t.Error("filtered response should contain Acxiom")
	}
	if strings.Contains(body, "CoreLogic") {
		t.Error("filtered response should not contain CoreLogic")
	}
}

func TestHandleBrokers_CategoryFilter(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/brokers?category=people-search")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Acxiom") {
		t.Error("category filtered response should contain Acxiom")
	}
	if strings.Contains(body, "CoreLogic") {
		t.Error("category filtered response should not contain CoreLogic")
	}
}

func TestHandleHistory(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/history")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleHistory_WithStatusFilter(t *testing.T) {
	s := newTestServer(t)

	s.historyStore.Add(&history.Record{
		BrokerID: "acxiom", BrokerName: "Acxiom", Email: "privacy@acxiom.com",
		Template: "generic", Status: history.StatusSent, SentAt: time.Now(),
	})
	s.historyStore.Add(&history.Record{
		BrokerID: "corelogic", BrokerName: "CoreLogic", Email: "optout@corelogic.com",
		Template: "generic", Status: history.StatusFailed, Error: "timeout", SentAt: time.Now(),
	})

	rr := executeRequest(t, s, http.MethodGet, "/history?status=sent")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleSettings(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/settings")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Settings") {
		t.Error("response should contain Settings title")
	}
}

func TestHandleAPIStats(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/api/stats")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}

	totalBrokers, ok := result["total_brokers"].(float64)
	if !ok {
		t.Fatal("total_brokers not found or wrong type")
	}
	if totalBrokers != 3 {
		t.Errorf("total_brokers = %v, want 3", totalBrokers)
	}
}

func TestHandleAPIBrokers(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/api/brokers")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Acxiom") {
		t.Error("response should contain broker name Acxiom")
	}
}

func TestHandleAPIBrokers_WithFilters(t *testing.T) {
	tests := []struct {
		name            string
		query           string
		wantContains    string
		wantNotContains string
	}{
		{
			name:            "filter by region eu",
			query:           "/api/brokers?region=eu",
			wantContains:    "EU Data Co",
			wantNotContains: "Acxiom",
		},
		{
			name:            "filter by status pending",
			query:           "/api/brokers?status=pending",
			wantContains:    "Acxiom",
			wantNotContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			rr := executeRequest(t, s, http.MethodGet, tt.query)

			if rr.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
			}
			body := rr.Body.String()
			if tt.wantContains != "" && !strings.Contains(body, tt.wantContains) {
				t.Errorf("response should contain %q", tt.wantContains)
			}
			if tt.wantNotContains != "" && strings.Contains(body, tt.wantNotContains) {
				t.Errorf("response should not contain %q", tt.wantNotContains)
			}
		})
	}
}

func TestHandleAPIDeleteFailed(t *testing.T) {
	t.Run("nil store returns error", func(t *testing.T) {
		s := newTestServer(t)
		s.historyStore = nil
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/history/failed", nil)
		s.handleAPIDeleteFailed(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatalf("json unmarshal error: %v", err)
		}
		if _, ok := result["error"]; !ok {
			t.Error("response should contain error key")
		}
	})

	t.Run("deletes failed records", func(t *testing.T) {
		s := newTestServer(t)

		s.historyStore.Add(&history.Record{
			BrokerID: "acxiom", BrokerName: "Acxiom", Email: "privacy@acxiom.com",
			Template: "generic", Status: history.StatusSent, SentAt: time.Now(),
		})
		s.historyStore.Add(&history.Record{
			BrokerID: "corelogic", BrokerName: "CoreLogic", Email: "optout@corelogic.com",
			Template: "generic", Status: history.StatusFailed, Error: "timeout", SentAt: time.Now(),
		})

		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/history/failed", nil)
		s.handleAPIDeleteFailed(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatalf("json unmarshal error: %v", err)
		}
		deleted, _ := result["deleted"].(float64)
		if deleted != 1 {
			t.Errorf("deleted = %v, want 1", deleted)
		}
	})
}

func TestHandleAPIPipelineStats(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/api/pipeline/stats")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}

	expectedKeys := []string{
		"email_sent", "awaiting_response", "form_required", "form_filled",
		"awaiting_captcha", "captcha_solved", "awaiting_confirmation",
		"confirmed", "rejected", "failed", "pending_tasks", "needs_review",
	}
	for _, key := range expectedKeys {
		if _, ok := result[key]; !ok {
			t.Errorf("missing key %q in pipeline stats response", key)
		}
	}
}

func TestHandleAPIPipelineStats_WithData(t *testing.T) {
	s := newTestServer(t)

	s.historyStore.Add(&history.Record{
		BrokerID: "acxiom", BrokerName: "Acxiom", Email: "privacy@acxiom.com",
		Template: "generic", Status: history.StatusSent, SentAt: time.Now(),
	})
	s.historyStore.UpdatePipelineStatus("acxiom", history.PipelineConfirmed)

	rr := executeRequest(t, s, http.MethodGet, "/api/pipeline/stats")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var result map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &result)
	confirmed, _ := result["confirmed"].(float64)
	if confirmed != 1 {
		t.Errorf("confirmed = %v, want 1", confirmed)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/api/stats")

	tests := []struct {
		header string
		want   string
	}{
		{"X-Frame-Options", "DENY"},
		{"X-Content-Type-Options", "nosniff"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
		{"Content-Security-Policy", "default-src 'self'"},
		{"Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()"},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			got := rr.Header().Get(tt.header)
			if got == "" {
				t.Errorf("header %q not set", tt.header)
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("%s = %q, want containing %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestSecurityHeaders_CacheControlOnPages(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/settings")

	cc := rr.Header().Get("Cache-Control")
	if !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want containing no-store for non-static paths", cc)
	}
}

func TestSecurityHeaders_CSPContainsDirectives(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/settings")

	csp := rr.Header().Get("Content-Security-Policy")

	directives := []string{
		"script-src",
		"style-src",
		"img-src",
		"font-src",
		"connect-src",
		"frame-ancestors 'none'",
		"form-action 'self'",
		"base-uri 'self'",
	}
	for _, d := range directives {
		if !strings.Contains(csp, d) {
			t.Errorf("CSP missing directive %q; CSP = %q", d, csp)
		}
	}
}

func TestSetupWizard_Welcome(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/setup/")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestSetupWizard_Profile_GET(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/setup/profile")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "first_name") {
		t.Error("profile form should contain first_name field")
	}
}

func TestSetupWizard_Profile_POST_Validation(t *testing.T) {
	tests := []struct {
		name       string
		form       url.Values
		wantInBody string
	}{
		{
			name:       "missing first name shows error",
			form:       url.Values{"first_name": {""}, "last_name": {"Doe"}, "email": {"jane@example.com"}},
			wantInBody: "First name is required",
		},
		{
			name:       "missing last name shows error",
			form:       url.Values{"first_name": {"Jane"}, "last_name": {""}, "email": {"jane@example.com"}},
			wantInBody: "Last name is required",
		},
		{
			name:       "missing email shows error",
			form:       url.Values{"first_name": {"Jane"}, "last_name": {"Doe"}, "email": {""}},
			wantInBody: "Email is required",
		},
		{
			name:       "invalid email shows error",
			form:       url.Values{"first_name": {"Jane"}, "last_name": {"Doe"}, "email": {"not-an-email"}},
			wantInBody: "valid email",
		},
		{
			name:       "email with injection chars shows error",
			form:       url.Values{"first_name": {"Jane"}, "last_name": {"Doe"}, "email": {"test@example.com\r\nBcc:evil@bad.com"}},
			wantInBody: "valid email",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/setup/profile", strings.NewReader(tt.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			s.handleSetupProfile(rr, req)

			if rr.Code != http.StatusOK {
				t.Logf("body: %s", rr.Body.String())
				t.Errorf("status = %d, want %d (should re-render form with errors)", rr.Code, http.StatusOK)
			}
			body := rr.Body.String()
			if !strings.Contains(body, tt.wantInBody) {
				t.Errorf("body missing %q", tt.wantInBody)
			}
		})
	}
}

func TestSetupWizard_Profile_POST_Valid(t *testing.T) {
	s := newTestServer(t)

	sessionID, _ := s.sessions.Create()

	form := url.Values{
		"first_name": {"Jane"},
		"last_name":  {"Doe"},
		"email":      {"jane@example.com"},
		"address":    {"123 Main St"},
		"city":       {"Springfield"},
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup/profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "eraser_session", Value: sessionID})
	s.handleSetupProfile(rr, req)

	if rr.Code != http.StatusFound {
		t.Logf("body: %s", rr.Body.String())
		t.Errorf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	loc := rr.Header().Get("Location")
	if loc != "/setup/email" {
		t.Errorf("Location = %q, want %q", loc, "/setup/email")
	}
}

func TestGetBrokersWithStatus(t *testing.T) {
	s := newTestServer(t)

	t.Run("returns all brokers without filters", func(t *testing.T) {
		brokers := s.getBrokersWithStatus("", "", "", "")
		if len(brokers) != 3 {
			t.Errorf("got %d brokers, want 3", len(brokers))
		}
	})

	t.Run("filters by search term", func(t *testing.T) {
		brokers := s.getBrokersWithStatus("acxiom", "", "", "")
		if len(brokers) != 1 {
			t.Fatalf("got %d brokers, want 1", len(brokers))
		}
		if brokers[0].Name != "Acxiom" {
			t.Errorf("broker name = %q, want %q", brokers[0].Name, "Acxiom")
		}
	})

	t.Run("filters by region", func(t *testing.T) {
		brokers := s.getBrokersWithStatus("", "", "eu", "")
		if len(brokers) != 1 {
			t.Fatalf("got %d brokers, want 1", len(brokers))
		}
		if brokers[0].ID != "eu-broker" {
			t.Errorf("broker ID = %q, want %q", brokers[0].ID, "eu-broker")
		}
	})

	t.Run("filters by category", func(t *testing.T) {
		brokers := s.getBrokersWithStatus("", "people-search", "", "")
		if len(brokers) != 1 {
			t.Fatalf("got %d brokers, want 1", len(brokers))
		}
	})

	t.Run("search is case insensitive", func(t *testing.T) {
		brokers := s.getBrokersWithStatus("CORELOGIC", "", "", "")
		if len(brokers) != 1 {
			t.Fatalf("got %d brokers, want 1", len(brokers))
		}
	})
}

func TestGetStats(t *testing.T) {
	s := newTestServer(t)

	t.Run("empty history returns pending equal to total brokers", func(t *testing.T) {
		stats := s.getStats()
		if stats.TotalBrokers != 3 {
			t.Errorf("TotalBrokers = %d, want 3", stats.TotalBrokers)
		}
		if stats.Pending != 3 {
			t.Errorf("Pending = %d, want 3", stats.Pending)
		}
		if stats.Sent != 0 || stats.Failed != 0 {
			t.Errorf("Sent = %d, Failed = %d, want both 0", stats.Sent, stats.Failed)
		}
	})

	t.Run("counts sent and failed from history", func(t *testing.T) {
		s.historyStore.Add(&history.Record{
			BrokerID: "acxiom", BrokerName: "Acxiom", Email: "p@a.com",
			Template: "generic", Status: history.StatusSent, SentAt: time.Now(),
		})
		s.historyStore.Add(&history.Record{
			BrokerID: "corelogic", BrokerName: "CoreLogic", Email: "o@c.com",
			Template: "generic", Status: history.StatusFailed, Error: "err", SentAt: time.Now(),
		})

		stats := s.getStats()
		if stats.Sent != 1 {
			t.Errorf("Sent = %d, want 1", stats.Sent)
		}
		if stats.Failed != 1 {
			t.Errorf("Failed = %d, want 1", stats.Failed)
		}
		if stats.Pending != 1 {
			t.Errorf("Pending = %d, want 1", stats.Pending)
		}
	})
}

func TestGetUniqueValues(t *testing.T) {
	s := newTestServer(t)

	categories := s.getUniqueCategories()
	if len(categories) < 2 {
		t.Errorf("got %d categories, want at least 2", len(categories))
	}

	regions := s.getUniqueRegions()
	if len(regions) < 2 {
		t.Errorf("got %d regions, want at least 2", len(regions))
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(context.Background(), 3, time.Minute)

	if !rl.Allow("key1") {
		t.Error("first request should be allowed")
	}
	if !rl.Allow("key1") {
		t.Error("second request should be allowed")
	}
	if !rl.Allow("key1") {
		t.Error("third request should be allowed")
	}
	if rl.Allow("key1") {
		t.Error("fourth request should be rate limited")
	}
	if !rl.Allow("key2") {
		t.Error("different key should not be affected")
	}
}

func TestSessionStore(t *testing.T) {
	store := NewSessionStore(context.Background(), 30*time.Minute)

	t.Run("create and get session", func(t *testing.T) {
		id, err := store.Create()
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if id == "" {
			t.Fatal("session ID should not be empty")
		}

		session := store.Get(id)
		if session == nil {
			t.Fatal("expected session, got nil")
		}
		if session.ID != id {
			t.Errorf("session ID = %q, want %q", session.ID, id)
		}
	})

	t.Run("get nonexistent session", func(t *testing.T) {
		session := store.Get("nonexistent")
		if session != nil {
			t.Error("expected nil for nonexistent session")
		}
	})

	t.Run("get empty ID", func(t *testing.T) {
		session := store.Get("")
		if session != nil {
			t.Error("expected nil for empty ID")
		}
	})

	t.Run("update session", func(t *testing.T) {
		id, _ := store.Create()
		ok := store.Update(id, func(s *Session) {
			s.Step = "email"
			s.Profile.FirstName = "Jane"
		})
		if !ok {
			t.Fatal("Update() returned false")
		}

		session := store.Get(id)
		if session.Step != "email" {
			t.Errorf("Step = %q, want %q", session.Step, "email")
		}
		if session.Profile.FirstName != "Jane" {
			t.Errorf("FirstName = %q, want %q", session.Profile.FirstName, "Jane")
		}
	})

	t.Run("delete session", func(t *testing.T) {
		id, _ := store.Create()
		store.Delete(id)
		session := store.Get(id)
		if session != nil {
			t.Error("expected nil after delete")
		}
	})

	t.Run("update nonexistent session", func(t *testing.T) {
		ok := store.Update("nonexistent", func(s *Session) {})
		if ok {
			t.Error("Update() should return false for nonexistent session")
		}
	})

	t.Run("count sessions", func(t *testing.T) {
		store := NewSessionStore(context.Background(), 30*time.Minute)
		store.Create()
		store.Create()
		store.Create()
		if count := store.Count(); count != 3 {
			t.Errorf("Count() = %d, want 3", count)
		}
	})
}

func TestJobManager(t *testing.T) {
	jm := NewJobManager()

	t.Run("create job", func(t *testing.T) {
		job := jm.Create(10)
		if job == nil {
			t.Fatal("expected job, got nil")
		}
		if job.ID == "" {
			t.Error("job ID should not be empty")
		}
		if job.Status != JobStatusRunning {
			t.Errorf("Status = %q, want %q", job.Status, JobStatusRunning)
		}
		if job.Total != 10 {
			t.Errorf("Total = %d, want 10", job.Total)
		}
	})

	t.Run("get job by ID", func(t *testing.T) {
		job := jm.Create(5)
		got := jm.Get(job.ID)
		if got == nil || got.ID != job.ID {
			t.Error("Get() should return the same job")
		}
	})

	t.Run("get nonexistent job", func(t *testing.T) {
		got := jm.Get("nonexistent")
		if got != nil {
			t.Error("expected nil for nonexistent job")
		}
	})

	t.Run("get active job", func(t *testing.T) {
		jm := NewJobManager()
		job := jm.Create(5)
		active := jm.GetActive()
		if active == nil || active.ID != job.ID {
			t.Error("GetActive() should return the running job")
		}
	})

	t.Run("no active job when none running", func(t *testing.T) {
		jm := NewJobManager()
		if active := jm.GetActive(); active != nil {
			t.Error("expected nil when no jobs exist")
		}
	})

	t.Run("cancel job", func(t *testing.T) {
		job := jm.Create(5)
		job.Cancel()
		if !job.IsCancelled() {
			t.Error("job should be canceled")
		}
	})

	t.Run("complete job", func(t *testing.T) {
		job := jm.Create(5)
		job.Complete()
		if job.Status != JobStatusCompleted {
			t.Errorf("Status = %q, want %q", job.Status, JobStatusCompleted)
		}
		if job.Progress != 100 {
			t.Errorf("Progress = %d, want 100", job.Progress)
		}
	})

	t.Run("job update progress", func(t *testing.T) {
		job := jm.Create(10)
		job.Update(5, 2, "BrokerX")
		if job.Sent != 5 {
			t.Errorf("Sent = %d, want 5", job.Sent)
		}
		if job.Failed != 2 {
			t.Errorf("Failed = %d, want 2", job.Failed)
		}
		if job.Progress != 70 {
			t.Errorf("Progress = %d, want 70", job.Progress)
		}
		if job.CurrentBroker != "BrokerX" {
			t.Errorf("CurrentBroker = %q, want %q", job.CurrentBroker, "BrokerX")
		}
	})

	t.Run("auth failure tracking", func(t *testing.T) {
		job := jm.Create(10)
		if job.RecordAuthFailure() {
			t.Error("first auth failure should not trigger stop")
		}
		if job.RecordAuthFailure() {
			t.Error("second auth failure should not trigger stop")
		}
		if !job.RecordAuthFailure() {
			t.Error("third auth failure should trigger stop")
		}
		job.ResetAuthFailures()
		if job.RecordAuthFailure() {
			t.Error("after reset, first failure should not trigger stop")
		}
	})

	t.Run("ToJSON", func(t *testing.T) {
		job := jm.Create(10)
		job.Update(3, 1, "TestBroker")
		data := job.ToJSON()
		if data["id"] != job.ID {
			t.Errorf("id mismatch")
		}
		if data["sent"] != 3 {
			t.Errorf("sent = %v, want 3", data["sent"])
		}
		if data["failed"] != 1 {
			t.Errorf("failed = %v, want 1", data["failed"])
		}
	})
}

func TestJobPersistence(t *testing.T) {
	dir := t.TempDir()
	jp := NewJobPersistence(dir)

	t.Run("load when no file returns nil", func(t *testing.T) {
		state, err := jp.Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if state != nil {
			t.Error("expected nil when no file exists")
		}
	})

	t.Run("save and load roundtrip", func(t *testing.T) {
		state := &PersistentJobState{
			ID:               "test-job-1",
			Status:           JobStatusRunning,
			Sent:             5,
			Failed:           1,
			Total:            10,
			StartedAt:        time.Now(),
			RemainingBrokers: []string{"b1", "b2"},
		}

		if err := jp.Save(state); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		loaded, err := jp.Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if loaded == nil {
			t.Fatal("expected state, got nil")
		}
		if loaded.ID != state.ID {
			t.Errorf("ID = %q, want %q", loaded.ID, state.ID)
		}
		if loaded.Sent != state.Sent {
			t.Errorf("Sent = %d, want %d", loaded.Sent, state.Sent)
		}
		if len(loaded.RemainingBrokers) != 2 {
			t.Errorf("RemainingBrokers len = %d, want 2", len(loaded.RemainingBrokers))
		}
	})

	t.Run("clear removes file", func(t *testing.T) {
		jp.Save(&PersistentJobState{ID: "x", Total: 1, StartedAt: time.Now()})
		if err := jp.Clear(); err != nil {
			t.Fatalf("Clear() error = %v", err)
		}
		state, _ := jp.Load()
		if state != nil {
			t.Error("expected nil after clear")
		}
	})

	t.Run("clear nonexistent file is no-op", func(t *testing.T) {
		if err := jp.Clear(); err != nil {
			t.Fatalf("Clear() on nonexistent file should not error: %v", err)
		}
	})
}

func TestHandleAPIJobActive(t *testing.T) {
	s := newTestServer(t)

	t.Run("no active job", func(t *testing.T) {
		rr := executeRequest(t, s, http.MethodGet, "/api/job/active")

		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
		}

		var result map[string]interface{}
		json.Unmarshal(rr.Body.Bytes(), &result)
		if result["job"] != nil {
			t.Errorf("job should be nil when no active job, got %v", result["job"])
		}
	})

	t.Run("with active job", func(t *testing.T) {
		job := s.jobManager.Create(10)
		job.Update(3, 0, "TestBroker")

		rr := executeRequest(t, s, http.MethodGet, "/api/job/active")

		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
		}

		var result map[string]interface{}
		json.Unmarshal(rr.Body.Bytes(), &result)
		jobData, ok := result["job"].(map[string]interface{})
		if !ok {
			t.Fatal("job should be a map")
		}
		if jobData["id"] != job.ID {
			t.Errorf("job id mismatch")
		}
	})
}

func TestHandleAPIJobStatus(t *testing.T) {
	s := newTestServer(t)
	job := s.jobManager.Create(5)

	rr := executeRequest(t, s, http.MethodGet, "/api/job/"+job.ID+"/status")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var result map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &result)
	if result["id"] != job.ID {
		t.Errorf("id = %v, want %q", result["id"], job.ID)
	}
}

func TestHandleAPIJobStatus_NotFound(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/api/job/nonexistent/status")

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestHandleAPIJobCancel(t *testing.T) {
	s := newTestServer(t)
	job := s.jobManager.Create(5)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/job/"+job.ID+"/cancel", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", job.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleAPIJobCancel(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var result map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &result)
	if result["status"] != "canceled" {
		t.Errorf("status = %v, want %q", result["status"], "canceled")
	}

	if !job.IsCancelled() {
		t.Error("job should be canceled")
	}
}

func TestHandleAPIJobCancel_NotFound(t *testing.T) {
	s := newTestServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/job/nonexistent/cancel", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleAPIJobCancel(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestHandlePipeline_RedirectsWhenNoConfig(t *testing.T) {
	s := newTestServerWithConfig(t, nil)
	rr := executeRequest(t, s, http.MethodGet, "/pipeline")

	if rr.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if loc := rr.Header().Get("Location"); loc != "/setup" {
		t.Errorf("Location = %q, want %q", loc, "/setup")
	}
}

func TestHandleTasks_RedirectsWhenNoConfig(t *testing.T) {
	s := newTestServerWithConfig(t, nil)
	rr := executeRequest(t, s, http.MethodGet, "/tasks")

	if rr.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusFound)
	}
}

func TestHandleTasks_WithConfig(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/tasks")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleForms_RedirectsToTasks(t *testing.T) {
	s := newTestServer(t)
	rr := executeRequest(t, s, http.MethodGet, "/forms")

	if rr.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if loc := rr.Header().Get("Location"); loc != "/tasks" {
		t.Errorf("Location = %q, want %q", loc, "/tasks")
	}
}

func TestGetStats_NilHistoryStore(t *testing.T) {
	s := newTestServer(t)
	s.historyStore = nil

	stats := s.getStats()
	if stats.TotalBrokers != 3 {
		t.Errorf("TotalBrokers = %d, want 3", stats.TotalBrokers)
	}
	if stats.Sent != 0 || stats.Failed != 0 {
		t.Errorf("Sent = %d, Failed = %d, want both 0 with nil store", stats.Sent, stats.Failed)
	}
}

func TestGetRecentHistory_NilStore(t *testing.T) {
	s := newTestServer(t)
	s.historyStore = nil

	records := s.getRecentHistory(10)
	if records != nil {
		t.Errorf("expected nil with nil store, got %v", records)
	}
}

func TestGetPipelineStats_NilStore(t *testing.T) {
	s := newTestServer(t)
	s.historyStore = nil

	stats := s.getPipelineStats()
	if stats != (PipelineStats{}) {
		t.Errorf("expected zero PipelineStats with nil store, got %+v", stats)
	}
}

func TestRender_NonexistentTemplate(t *testing.T) {
	s := newTestServer(t)
	rr := httptest.NewRecorder()

	s.render(rr, "nonexistent.html", nil)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestRenderPartial_NonexistentTemplate(t *testing.T) {
	s := newTestServer(t)
	rr := httptest.NewRecorder()

	s.renderPartial(rr, "nonexistent.html", nil)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}
