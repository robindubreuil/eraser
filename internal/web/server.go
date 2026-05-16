package web

import (
	"context"
	"crypto/rand"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/csrf"

	"github.com/robindubreuil/eraser/internal/broker"
	"github.com/robindubreuil/eraser/internal/config"
	"github.com/robindubreuil/eraser/internal/email"
	"github.com/robindubreuil/eraser/internal/history"
	emaTemplate "github.com/robindubreuil/eraser/internal/template"
)

//go:embed static/*
var staticFS embed.FS

//go:embed templates/*
var templatesFS embed.FS

const (
	defaultRateLimit  = 30
	defaultRateWindow = time.Minute
	defaultSessionTTL = 30 * time.Minute
)

type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func NewRateLimiter(ctx context.Context, limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
	go rl.cleanupLoop(ctx)
	return rl
}

func (rl *RateLimiter) filterRecent(times []time.Time, windowStart time.Time) []time.Time {
	n := 0
	for _, t := range times {
		if t.After(windowStart) {
			times[n] = t
			n++
		}
	}
	return times[:n]
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	recent := rl.filterRecent(rl.requests[key], now.Add(-rl.window))

	if len(recent) >= rl.limit {
		rl.requests[key] = recent
		return false
	}
	rl.requests[key] = append(recent, now)
	return true
}

func (rl *RateLimiter) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.mu.Lock()
			windowStart := time.Now().Add(-rl.window)
			for key, times := range rl.requests {
				recent := rl.filterRecent(times, windowStart)
				if len(recent) == 0 {
					delete(rl.requests, key)
				} else {
					rl.requests[key] = recent
				}
			}
			rl.mu.Unlock()
		}
	}
}

type Server struct {
	config         *config.Config
	configPath     string
	brokerDB       *broker.BrokerDatabase
	historyStore   *history.Store
	tmplEngine     *emaTemplate.Engine
	templates      map[string]*template.Template
	httpServer     *http.Server
	port           int
	csrfKey        []byte
	sessions       *SessionStore
	rateLimiter    *RateLimiter
	jobManager     *JobManager
	jobPersistence *JobPersistence
}

func NewServer(ctx context.Context, port int, cfg *config.Config, configPath string, brokerDB *broker.BrokerDatabase, historyStore *history.Store, tmplEngine *emaTemplate.Engine) (*Server, error) {
	csrfKey := make([]byte, 32)
	if _, err := rand.Read(csrfKey); err != nil {
		return nil, fmt.Errorf("failed to generate CSRF key: %w", err)
	}

	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".eraser")

	s := &Server{
		config:         cfg,
		configPath:     configPath,
		brokerDB:       brokerDB,
		historyStore:   historyStore,
		tmplEngine:     tmplEngine,
		port:           port,
		csrfKey:        csrfKey,
		sessions:       NewSessionStore(ctx, defaultSessionTTL),
		rateLimiter:    NewRateLimiter(ctx, defaultRateLimit, defaultRateWindow),
		jobManager:     NewJobManager(),
		jobPersistence: NewJobPersistence(dataDir),
	}

	tmpl, err := s.parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}
	s.templates = tmpl
	return s, nil
}

func (s *Server) parseTemplates() (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.Format("Jan 2, 2006 3:04 PM")
		},
		"formatDate": func(t time.Time) string {
			return t.Format("Jan 2, 2006")
		},
		"add": func(a, b int) int {
			return a + b
		},
	}

	layoutContent, err := templatesFS.ReadFile("templates/layout.html")
	if err != nil {
		return nil, fmt.Errorf("failed to read layout template: %w", err)
	}

	var partials []string
	partialTemplates := make(map[string]string)
	err = fs.WalkDir(templatesFS, "templates/partials", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		content, err := templatesFS.ReadFile(path)
		if err != nil {
			return err
		}
		partials = append(partials, string(content))
		name := path[len("templates/"):]
		partialTemplates[name] = string(content)
		return nil
	})
	if err != nil && !strings.Contains(err.Error(), "file does not exist") {
		return nil, fmt.Errorf("failed to read partials: %w", err)
	}

	templates := make(map[string]*template.Template)

	err = fs.WalkDir(templatesFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.Contains(path, "/partials/") || path == "templates/layout.html" {
			return nil
		}
		if !strings.HasSuffix(path, ".html") {
			return nil
		}

		content, err := templatesFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", path, err)
		}

		name := path[len("templates/"):]
		pageTmpl := template.New(name).Funcs(funcs)

		_, err = pageTmpl.Parse(string(layoutContent))
		if err != nil {
			return fmt.Errorf("failed to parse layout for %s: %w", name, err)
		}

		for _, partial := range partials {
			_, err = pageTmpl.Parse(partial)
			if err != nil {
				return fmt.Errorf("failed to parse partial for %s: %w", name, err)
			}
		}

		_, err = pageTmpl.Parse(string(content))
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", name, err)
		}

		templates[name] = pageTmpl

		return nil
	})

	if err != nil {
		return nil, err
	}

	for name, content := range partialTemplates {
		partialTmpl := template.New(name).Funcs(funcs)
		_, err = partialTmpl.Parse(content)
		if err != nil {
			return nil, fmt.Errorf("failed to parse partial %s: %w", name, err)
		}
		templates[name] = partialTmpl
	}

	return templates, nil
}

func (s *Server) Start() error {
	router := s.setupRouter()

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.checkPendingJob()

	go func() {
		time.Sleep(500 * time.Millisecond)
		url := fmt.Sprintf("http://localhost:%d", s.port)
		openBrowser(url)
	}()

	fmt.Printf("Starting Eraser web UI at http://localhost:%d\n", s.port)
	fmt.Println("Press Ctrl+C to stop")

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) checkPendingJob() {
	state, err := s.jobPersistence.Load()
	if err != nil {
		log.Printf("Warning: failed to load pending job: %v", err)
		return
	}

	if state == nil || len(state.RemainingBrokers) == 0 {
		return
	}

	fmt.Printf("\nFound incomplete send job: %d of %d brokers remaining\n", len(state.RemainingBrokers), state.Total)
	fmt.Printf("Already sent: %d, failed: %d\n", state.Sent, state.Failed)

	go s.resumePendingJob(state)
}

func (s *Server) resumePendingJob(state *PersistentJobState) {
	time.Sleep(2 * time.Second)

	if s.config == nil || s.config.Email.Provider == "" {
		log.Printf("Cannot resume job: email not configured")
		s.jobPersistence.Clear() //nolint:errcheck
		return
	}

	sender, err := email.NewSender(s.config.Email)
	if err != nil {
		log.Printf("Cannot resume job: failed to create email sender: %v", err)
		s.jobPersistence.Clear() //nolint:errcheck
		return
	}

	brokerMap := make(map[string]broker.Broker)
	for _, b := range s.brokerDB.Brokers {
		brokerMap[b.ID] = b
	}

	var toSend []BrokerWithStatus
	for _, id := range state.RemainingBrokers {
		if b, ok := brokerMap[id]; ok {
			toSend = append(toSend, BrokerWithStatus{Broker: b, Status: "never"})
		}
	}

	if len(toSend) == 0 {
		log.Printf("No valid brokers remaining in pending job")
		s.jobPersistence.Clear() //nolint:errcheck
		return
	}

	job := s.jobManager.Create(state.Total)
	job.Sent = state.Sent
	job.Failed = state.Failed
	job.Progress = ((state.Sent + state.Failed) * 100) / state.Total

	fmt.Printf("Resuming send job: %d brokers remaining...\n", len(toSend))

	s.processSendJob(job, toSend, sender)
}

func (s *Server) setupRouter() *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(securityHeaders)

	csrfMiddleware := csrf.Protect(
		s.csrfKey,
		csrf.Secure(false),
		csrf.Path("/"),
		csrf.HttpOnly(true),
		csrf.SameSite(csrf.SameSiteLaxMode),
		csrf.RequestHeader("X-CSRF-Token"),
		csrf.TrustedOrigins([]string{"localhost", "127.0.0.1", fmt.Sprintf("localhost:%d", s.port), fmt.Sprintf("127.0.0.1:%d", s.port)}),
	)
	r.Use(csrfMiddleware)

	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	r.Get("/", s.handleDashboard)
	r.Get("/brokers", s.handleBrokers)
	r.Get("/history", s.handleHistory)
	r.Get("/settings", s.handleSettings)
	r.Post("/settings/inbox", s.handleSettingsInbox)
	r.Get("/pipeline", s.handlePipeline)
	r.Get("/tasks", s.handleTasks)
	r.Get("/tasks/{taskID}", s.handleTaskDetail)
	r.Get("/tasks/{taskID}/helper", s.handleTaskHelper)
	r.Post("/tasks/{taskID}/complete", s.handleTaskComplete)
	r.Post("/tasks/{taskID}/skip", s.handleTaskSkip)
	r.Get("/forms", s.handleForms)
	r.Post("/forms/{brokerID}/complete", s.handleFormComplete)
	r.Post("/forms/{brokerID}/skip", s.handleFormSkip)

	r.Route("/setup", func(r chi.Router) {
		r.Get("/", s.handleSetupWelcome)
		r.Get("/profile", s.handleSetupProfile)
		r.Post("/profile", s.handleSetupProfile)
		r.Get("/email", s.handleSetupEmail)
		r.Post("/email", s.handleSetupEmail)
		r.Get("/test", s.handleSetupTest)
		r.Post("/test/send", s.handleSetupTestSend)
		r.Get("/complete", s.handleSetupComplete)
	})

	r.Route("/api", func(r chi.Router) {
		r.Get("/stats", s.handleAPIStats)
		r.Get("/brokers", s.handleAPIBrokers)
		r.Get("/history", s.handleAPIHistory)
		r.Delete("/history/failed", s.handleAPIDeleteFailed)
		r.Post("/send/{brokerID}", s.handleAPISendOne)
		r.Post("/send-all", s.handleAPISendAll)
		r.Get("/job/active", s.handleAPIJobActive)
		r.Get("/job/{jobID}/status", s.handleAPIJobStatus)
		r.Post("/job/{jobID}/cancel", s.handleAPIJobCancel)
		r.Get("/pipeline/stats", s.handleAPIPipelineStats)
		r.Get("/pipeline/responses", s.handleAPIResponses)
		r.Get("/pipeline/tasks", s.handleAPITasks)
		r.Post("/inbox/scan", s.handleAPIInboxScan)
		r.Post("/inbox/rescan", s.handleAPIInboxRescan)
		r.Post("/inbox/reclassify", s.handleAPIReclassify)
	})

	return r
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		csp := "default-src 'self'; " +
			"script-src 'self' 'unsafe-inline' 'unsafe-eval' https://cdn.tailwindcss.com https://unpkg.com; " +
			"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
			"img-src 'self' data:; " +
			"font-src 'self' https://fonts.gstatic.com; " +
			"connect-src 'self'; " +
			"frame-ancestors 'none'; " +
			"form-action 'self'; " +
			"base-uri 'self'"
		w.Header().Set("Content-Security-Policy", csp)

		if !strings.HasPrefix(r.URL.Path, "/static/") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}

		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")

		next.ServeHTTP(w, r)
	})
}

func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	default:
		return
	}

	exec.Command(cmd, args...).Start() //nolint:errcheck
}

type Stats struct {
	TotalBrokers int
	Sent         int
	Failed       int
	Pending      int
}

type BrokerWithStatus struct {
	broker.Broker
	Status    string
	LastSent  string
	TotalSent int
}

func (s *Server) getBrokersWithStatus(search, category, region, statusFilter string) []BrokerWithStatus {
	var brokerStatuses map[string]history.BrokerStatus
	if s.historyStore != nil {
		brokerStatuses, _ = s.historyStore.GetAllBrokerStatuses()
	}
	if brokerStatuses == nil {
		brokerStatuses = make(map[string]history.BrokerStatus)
	}

	search = strings.ToLower(strings.TrimSpace(search))
	category = strings.ToLower(strings.TrimSpace(category))
	region = strings.ToLower(strings.TrimSpace(region))
	statusFilter = strings.ToLower(strings.TrimSpace(statusFilter))

	var result []BrokerWithStatus
	for _, b := range s.brokerDB.Brokers {
		if search != "" {
			name := strings.ToLower(b.Name)
			email := strings.ToLower(b.Email)
			if !strings.Contains(name, search) && !strings.Contains(email, search) {
				continue
			}
		}

		if category != "" && strings.ToLower(b.Category) != category {
			continue
		}

		if region != "" && strings.ToLower(b.Region) != region {
			continue
		}

		bws := BrokerWithStatus{
			Broker: b,
			Status: "never",
		}

		if status, ok := brokerStatuses[b.ID]; ok {
			bws.Status = string(status.Status)
			bws.TotalSent = status.TotalSent
			if !status.LastSent.IsZero() {
				bws.LastSent = status.LastSent.Format("Jan 2, 2006")
			}
		}

		if statusFilter != "" {
			if statusFilter == "pending" && bws.Status != "never" {
				continue
			} else if statusFilter == "sent" && bws.Status != "sent" {
				continue
			} else if statusFilter == "failed" && bws.Status != "failed" {
				continue
			}
		}

		result = append(result, bws)
	}

	return result
}

func (s *Server) getUniqueValues(getter func(broker.Broker) string) []string {
	seen := make(map[string]bool)
	var vals []string
	for _, b := range s.brokerDB.Brokers {
		if v := getter(b); v != "" && !seen[v] {
			seen[v] = true
			vals = append(vals, v)
		}
	}
	return vals
}

func (s *Server) getUniqueCategories() []string {
	return s.getUniqueValues(func(b broker.Broker) string { return b.Category })
}

func (s *Server) getUniqueRegions() []string {
	return s.getUniqueValues(func(b broker.Broker) string { return b.Region })
}

func (s *Server) getStats() Stats {
	stats := Stats{
		TotalBrokers: len(s.brokerDB.Brokers),
	}

	if s.historyStore != nil {
		_, sent, failed, err := s.historyStore.GetStats()
		if err == nil {
			stats.Sent = sent
			stats.Failed = failed
		}
	}

	stats.Pending = stats.TotalBrokers - stats.Sent - stats.Failed
	if stats.Pending < 0 {
		stats.Pending = 0
	}

	return stats
}

func (s *Server) getRecentHistory(limit int) []history.Record {
	if s.historyStore == nil {
		return nil
	}
	records, _ := s.historyStore.GetRecentRequests(limit)
	return records
}

func (s *Server) render(w http.ResponseWriter, name string, data interface{}) {
	tmpl, ok := s.templates[name]
	if !ok {
		http.Error(w, "Template not found: "+name, http.StatusInternalServerError)
		return
	}
	err := tmpl.ExecuteTemplate(w, "layout", data)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) renderPartial(w http.ResponseWriter, name string, data interface{}) {
	tmpl, ok := s.templates[name]
	if !ok {
		http.Error(w, "Template not found: "+name, http.StatusInternalServerError)
		return
	}
	err := tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) renderWithCSRF(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	data["CSRFToken"] = csrf.Token(r)
	data["CSRFField"] = template.HTML(fmt.Sprintf(`<input type="hidden" name="gorilla.csrf.Token" value="%s">`, csrf.Token(r)))

	tmpl, ok := s.templates[name]
	if !ok {
		http.Error(w, "Template not found: "+name, http.StatusInternalServerError)
		return
	}
	err := tmpl.ExecuteTemplate(w, "layout", data)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

type PipelineStats struct {
	EmailSent            int
	AwaitingResponse     int
	FormRequired         int
	FormFilled           int
	AwaitingCaptcha      int
	CaptchaSolved        int
	AwaitingConfirmation int
	Confirmed            int
	Rejected             int
	Failed               int
	PendingTasks         int
	NeedsReview          int
}
