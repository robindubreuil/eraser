package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/robindubreuil/eraser/internal/email"
	"github.com/robindubreuil/eraser/internal/history"
)

func TestHandleSettingsInbox_InvalidEmail(t *testing.T) {
	s := newTestServer(t)
	form := url.Values{"inbox_email": {"not-an-email"}, "inbox_password": {"pass"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/inbox", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	s.handleSettingsInbox(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Invalid email") {
		t.Errorf("body should contain 'Invalid email', got: %s", body[:min(200, len(body))])
	}
}

func TestHandleSettingsInbox_NilConfig(t *testing.T) {
	s := newTestServerWithConfig(t, nil)
	form := url.Values{"inbox_email": {"test@gmail.com"}, "inbox_password": {"app-pass"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/inbox", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	s.handleSettingsInbox(rr, req)
	if s.config == nil {
		t.Error("config should have been created")
	}
	if s.config.Inbox.Email != "test@gmail.com" {
		t.Errorf("inbox email = %q, want %q", s.config.Inbox.Email, "test@gmail.com")
	}
}

func TestHandleSettingsInbox_ParseFormError(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/settings/inbox", strings.NewReader("%%bad%%"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	s.handleSettingsInbox(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleAPISendOne_EmailNotConfigured(t *testing.T) {
	s := newTestServer(t)
	s.config.Email.Provider = ""
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/send/acxiom", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", "acxiom")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleAPISendOne(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleAPISendOne_NilConfig(t *testing.T) {
	s := newTestServerWithConfig(t, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/send/acxiom", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", "acxiom")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleAPISendOne(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleAPISendOne_SendFailure(t *testing.T) {
	s := newTestServer(t)
	s.config.Email.SMTP.Host = "127.0.0.1"
	s.config.Email.SMTP.Port = 1
	s.config.Email.SMTP.UseTLS = false
	s.config.Email.SMTP.Username = ""

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/send/acxiom", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", "acxiom")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleAPISendOne(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "failed") && !strings.Contains(body, "error") {
		t.Logf("body: %s", body)
	}
}

func TestHandleAPISendAll_EmailNotConfigured(t *testing.T) {
	s := newTestServer(t)
	s.config.Email.Provider = ""
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/send-all", nil)
	s.handleAPISendAll(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	var result map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &result)
	if !strings.Contains(result["error"].(string), "Email not configured") {
		t.Errorf("error = %v, want email not configured", result["error"])
	}
}

func TestHandleAPIDeleteFailed_DBError(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	store, err := history.NewStore(fmt.Sprintf("%s/test.db", dir))
	if err != nil {
		t.Fatal(err)
	}
	s.historyStore = store
	store.Close()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/history/failed", nil)
	s.handleAPIDeleteFailed(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestHandleTaskComplete_StatusValidation(t *testing.T) {
	tests := []struct {
		name           string
		status         string
		wantInRedirect string
	}{
		{"completed default", "completed", "/tasks/1/helper"},
		{"skipped status", "skipped", "/tasks/1/helper"},
		{"failed status", "failed", "/tasks/1/helper"},
		{"invalid status defaults to completed", "invalid_status", "/tasks/1/helper"},
		{"empty status defaults to completed", "", "/tasks/1/helper"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			s.historyStore.Add(&history.Record{
				BrokerID: "acxiom", BrokerName: "Acxiom", Email: "p@a.com",
				Template: "generic", Status: history.StatusSent, SentAt: time.Now(),
			})

			var body string
			if tt.status != "" {
				body = fmt.Sprintf("status=%s", tt.status)
			}
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/tasks/1/complete", strings.NewReader(body))
			if body != "" {
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("taskID", "1")
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			s.handleTaskComplete(rr, req)

			if rr.Code != http.StatusFound {
				t.Logf("body: %s", rr.Body.String())
				t.Errorf("status = %d, want %d", rr.Code, http.StatusFound)
			}
		})
	}
}

func TestHandleTaskComplete_DBError(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	store, _ := history.NewStore(fmt.Sprintf("%s/test.db", dir))
	s.historyStore = store
	store.Close()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks/1/complete", strings.NewReader("status=completed"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleTaskComplete(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestHandleTaskSkip_Success(t *testing.T) {
	s := newTestServer(t)
	s.historyStore.Add(&history.Record{
		BrokerID: "acxiom", BrokerName: "Acxiom", Email: "p@a.com",
		Template: "generic", Status: history.StatusSent, SentAt: time.Now(),
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks/1/skip", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleTaskSkip(rr, req)
	if rr.Code != http.StatusFound {
		t.Logf("body: %s", rr.Body.String())
		t.Errorf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if loc := rr.Header().Get("Location"); loc != "/tasks/1/helper" {
		t.Errorf("Location = %q, want %q", loc, "/tasks/1/helper")
	}
}

func TestHandleTaskSkip_InvalidID(t *testing.T) {
	s := newTestServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks/abc/skip", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", "abc")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleTaskSkip(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleFormComplete_DBError(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	store, _ := history.NewStore(fmt.Sprintf("%s/test.db", dir))
	s.historyStore = store
	store.Close()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/forms/acxiom/complete", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", "acxiom")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleFormComplete(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Logf("body: %s", rr.Body.String())
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestHandleFormComplete_NormalRequest(t *testing.T) {
	s := newTestServer(t)
	s.historyStore.Add(&history.Record{
		BrokerID: "acxiom", BrokerName: "Acxiom", Email: "p@a.com",
		Template: "generic", Status: history.StatusSent, SentAt: time.Now(),
	})
	s.historyStore.UpdatePipelineStatus("acxiom", history.PipelineFormRequired)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/forms/acxiom/complete", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", "acxiom")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleFormComplete(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if loc := rr.Header().Get("Location"); loc != "/tasks" {
		t.Errorf("Location = %q, want %q", loc, "/tasks")
	}
}

func TestHandleFormSkip_DBError(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	store, _ := history.NewStore(fmt.Sprintf("%s/test.db", dir))
	s.historyStore = store
	store.Close()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/forms/acxiom/skip", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", "acxiom")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleFormSkip(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Logf("body: %s", rr.Body.String())
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestHandleFormSkip_NormalRequest(t *testing.T) {
	s := newTestServer(t)
	s.historyStore.Add(&history.Record{
		BrokerID: "acxiom", BrokerName: "Acxiom", Email: "p@a.com",
		Template: "generic", Status: history.StatusSent, SentAt: time.Now(),
	})
	s.historyStore.UpdatePipelineStatus("acxiom", history.PipelineFormRequired)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/forms/acxiom/skip", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", "acxiom")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleFormSkip(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if loc := rr.Header().Get("Location"); loc != "/tasks" {
		t.Errorf("Location = %q, want %q", loc, "/tasks")
	}
}

func TestHandleAPISendAll_SenderError(t *testing.T) {
	s := newTestServer(t)
	s.config.Email.SMTP.Host = ""
	s.config.Email.SMTP.Port = 0
	s.config.Email.Provider = "unknown-provider"

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/send-all", nil)
	s.handleAPISendAll(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Logf("body: %s", rr.Body.String())
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestHandleAPISendOne_NilHistoryStore(t *testing.T) {
	s := newTestServer(t)
	s.historyStore = nil
	s.config.Email.SMTP.Host = "127.0.0.1"
	s.config.Email.SMTP.Port = 1
	s.config.Email.SMTP.UseTLS = false
	s.config.Email.SMTP.Username = ""

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/send/acxiom", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", "acxiom")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleAPISendOne(rr, req)
}

func newTestSMTPServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				scanner := bufio.NewScanner(c)
				fmt.Fprintf(c, "220 ready\r\n")
				for scanner.Scan() {
					line := strings.ToUpper(strings.TrimSpace(scanner.Text()))
					switch {
					case strings.HasPrefix(line, "EHLO") || strings.HasPrefix(line, "HELO"):
						fmt.Fprintf(c, "250-localhost\r\n250 OK\r\n")
					case strings.HasPrefix(line, "MAIL FROM"):
						fmt.Fprintf(c, "250 OK\r\n")
					case strings.HasPrefix(line, "RCPT TO"):
						fmt.Fprintf(c, "250 OK\r\n")
					case strings.HasPrefix(line, "DATA"):
						fmt.Fprintf(c, "354 Go ahead\r\n")
						for scanner.Scan() {
							if strings.TrimSpace(scanner.Text()) == "." {
								fmt.Fprintf(c, "250 OK\r\n")
								break
							}
						}
					case strings.HasPrefix(line, "QUIT"):
						fmt.Fprintf(c, "221 Bye\r\n")
						return
					default:
						fmt.Fprintf(c, "500 huh\r\n")
					}
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func TestHandleAPISendOne_Success(t *testing.T) {
	s := newTestServer(t)
	addr := newTestSMTPServer(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	s.config.Email.SMTP.Host = host
	s.config.Email.SMTP.Port = port
	s.config.Email.SMTP.UseTLS = false
	s.config.Email.SMTP.Username = ""
	s.config.Email.From = "sender@example.com"

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/send/acxiom", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", "acxiom")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleAPISendOne(rr, req)

	body := rr.Body.String()
	if !strings.Contains(strings.ToLower(body), "sent") {
		t.Errorf("expected 'sent' in body, got: %s", body[:min(200, len(body))])
	}
}

func TestHandleAPISendOne_ConnectionRefused(t *testing.T) {
	s := newTestServer(t)
	s.config.Email.SMTP.Host = "127.0.0.1"
	s.config.Email.SMTP.Port = 1
	s.config.Email.SMTP.UseTLS = false
	s.config.Email.SMTP.Username = ""
	s.config.Email.From = "sender@example.com"

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/send/acxiom", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", "acxiom")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleAPISendOne(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "failed") && !strings.Contains(body, "error") {
		t.Errorf("expected 'failed' or 'error' in body, got: %s", body[:min(200, len(body))])
	}
}

func TestHandleAPISendOne_SenderError(t *testing.T) {
	s := newTestServer(t)
	s.config.Email.Provider = "bad-provider"

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/send/acxiom", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", "acxiom")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleAPISendOne(rr, req)

	body := rr.Body.String()
	if !strings.Contains(strings.ToLower(body), "error") {
		t.Errorf("expected 'error' in body for bad provider, got: %s", body[:min(200, len(body))])
	}
}

func TestHandleTaskSkip_DBError(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	store, _ := history.NewStore(fmt.Sprintf("%s/test.db", dir))
	s.historyStore = store
	store.Close()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks/1/skip", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.handleTaskSkip(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Logf("body: %s", rr.Body.String())
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestHandleSettingsInbox_InvalidEmailInjection(t *testing.T) {
	s := newTestServer(t)
	form := url.Values{"inbox_email": {"test@example.com\r\nBcc:evil@bad.com"}, "inbox_password": {"pass"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/inbox", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	s.handleSettingsInbox(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Invalid email") {
		t.Error("should show Invalid email for CRLF injection")
	}
}

func TestHandleSettingsInbox_PasswordOnly(t *testing.T) {
	s := newTestServer(t)
	form := url.Values{"inbox_email": {""}, "inbox_password": {"pass"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/inbox", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	s.handleSettingsInbox(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "required") {
		t.Error("should show required error for empty email")
	}
}

func TestHandleSettingsInbox_EmailOnly(t *testing.T) {
	s := newTestServer(t)
	form := url.Values{"inbox_email": {"test@gmail.com"}, "inbox_password": {""}}
	req := httptest.NewRequest(http.MethodPost, "/settings/inbox", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	s.handleSettingsInbox(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "required") {
		t.Error("should show required error for empty password")
	}
}

func TestHandleAPISendAll_SaveError(t *testing.T) {
	s := newTestServer(t)
	addr := newTestSMTPServer(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	s.config.Email.SMTP.Host = host
	s.config.Email.SMTP.Port = port
	s.config.Email.SMTP.UseTLS = false
	s.config.Email.SMTP.Username = ""
	s.config.Email.From = "sender@example.com"
	s.jobPersistence = NewJobPersistence("/nonexistent/path/that/cannot/be/created/because/permissions")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/send-all", nil)
	s.handleAPISendAll(rr, req)
	if rr.Code != http.StatusOK {
		t.Logf("body: %s", rr.Body.String())
		t.Errorf("status = %d, want %d (should still succeed even if persistence fails)", rr.Code, http.StatusOK)
	}
}

func TestHandleAPIInboxScan_Disabled(t *testing.T) {
	s := newTestServer(t)
	s.config.Inbox.Enabled = false

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/inbox/scan", nil)
	s.handleAPIInboxScan(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleAPIInboxRescan_Disabled(t *testing.T) {
	s := newTestServer(t)
	s.config.Inbox.Enabled = false

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/inbox/rescan", nil)
	s.handleAPIInboxRescan(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleAPIReclassify_NilStore2(t *testing.T) {
	s := newTestServer(t)
	s.historyStore = nil

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/inbox/reclassify", nil)
	s.handleAPIReclassify(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestCheckPendingJob_NoState2(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	s.jobPersistence = NewJobPersistence(filepath.Join(dir, "job-state.json"))

	s.checkPendingJob()
}

func TestCheckPendingJob_LoadError(t *testing.T) {
	s := newTestServer(t)
	s.jobPersistence = NewJobPersistence("/nonexistent/dir/job-state.json")

	s.checkPendingJob()
}

func TestResumePendingJob_NoConfig(t *testing.T) {
	s := newTestServer(t)
	s.config.Email.Provider = ""

	state := &PersistentJobState{
		ID:               "test-id",
		Status:           JobStatusRunning,
		Sent:             1,
		Failed:           0,
		Total:            3,
		RemainingBrokers: []string{"acxiom", "spokeo"},
	}
	s.resumePendingJob(state)
}

func TestResumePendingJob_BadProvider(t *testing.T) {
	s := newTestServer(t)
	s.config.Email.Provider = "nonexistent-provider"
	s.config.Email.SMTP.Host = "bad"
	s.config.Email.From = "test@test.com"

	state := &PersistentJobState{
		ID:               "test-id",
		Status:           JobStatusRunning,
		Sent:             1,
		Failed:           0,
		Total:            3,
		RemainingBrokers: []string{"acxiom"},
	}
	s.resumePendingJob(state)
}

func TestResumePendingJob_NoMatchingBrokers(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	s.jobPersistence = NewJobPersistence(filepath.Join(dir, "job-state.json"))
	s.config.Email.Provider = "smtp"
	s.config.Email.SMTP.Host = "localhost"
	s.config.Email.SMTP.Port = 25
	s.config.Email.From = "test@test.com"

	state := &PersistentJobState{
		ID:               "test-id",
		Status:           JobStatusRunning,
		Sent:             1,
		Failed:           0,
		Total:            3,
		RemainingBrokers: []string{"nonexistent-broker-id"},
	}
	s.resumePendingJob(state)
}

type mockSender struct {
	success bool
}

func (m *mockSender) Send(ctx context.Context, msg email.Message) email.Result {
	return email.Result{Success: m.success, MessageID: "test-id"}
}

func (m *mockSender) Name() string { return "mock" }

func TestProcessSendJob_Cancel(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	s.jobPersistence = NewJobPersistence(filepath.Join(dir, "job-state.json"))

	brokers := []BrokerWithStatus{
		{Broker: s.brokerDB.Brokers[0], Status: "never"},
		{Broker: s.brokerDB.Brokers[1], Status: "never"},
	}
	sender := &mockSender{}

	job := s.jobManager.Create(2)
	job.Cancel()

	s.processSendJob(job, brokers, sender)
	if job.Sent != 0 && job.Failed != 0 {
		t.Errorf("expected no processing after cancel, sent=%d failed=%d", job.Sent, job.Failed)
	}
}

type authFailSender struct{}

func (a *authFailSender) Send(ctx context.Context, msg email.Message) email.Result {
	return email.Result{Success: false, Error: fmt.Errorf("535 auth authentication failed")}
}

func (a *authFailSender) Name() string { return "auth-fail" }

func TestProcessSendJob_AuthFailure(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	s.jobPersistence = NewJobPersistence(filepath.Join(dir, "job-state.json"))

	brokers := []BrokerWithStatus{}
	for i := 0; i < len(s.brokerDB.Brokers); i++ {
		brokers = append(brokers, BrokerWithStatus{Broker: s.brokerDB.Brokers[i], Status: "never"})
	}
	sender := &authFailSender{}

	job := s.jobManager.Create(len(brokers))

	s.processSendJob(job, brokers, sender)
	if job.ErrorType != "auth" {
		t.Errorf("expected auth error type, got %q status=%s", job.ErrorType, job.Status)
	}
}

func TestHandleAPIReclassify_EmptyResponses(t *testing.T) {
	s := newTestServer(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/inbox/reclassify", nil)
	s.handleAPIReclassify(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleAPIInboxScan_ConnectFail(t *testing.T) {
	s := newTestServer(t)
	s.config.Inbox.Enabled = true
	s.config.Inbox.Server = "127.0.0.1:1993"

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/inbox/scan", nil)
	s.handleAPIInboxScan(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleAPIInboxRescan_ConnectFail(t *testing.T) {
	s := newTestServer(t)
	s.config.Inbox.Enabled = true
	s.config.Inbox.Server = "127.0.0.1:1993"

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/inbox/rescan", nil)
	s.handleAPIInboxRescan(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleAPIInboxRescan_ClearError(t *testing.T) {
	s := newTestServer(t)
	s.config.Inbox.Enabled = true
	s.config.Inbox.Server = "127.0.0.1:1993"

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/inbox/rescan?clear=true", nil)
	s.handleAPIInboxRescan(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}
