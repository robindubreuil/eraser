package history

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNewStore(t *testing.T) {
	t.Run("creates database", func(t *testing.T) {
		store := newTestStore(t)
		if store == nil {
			t.Fatal("expected store, got nil")
		}
	})

	t.Run("creates parent directory", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "nested", "dir", "test.db")
		store, err := NewStore(dbPath)
		if err != nil {
			t.Fatalf("NewStore() error = %v", err)
		}
		store.Close()
	})
}

func TestAddAndGetRecentRequests(t *testing.T) {
	store := newTestStore(t)

	r1 := &Record{
		BrokerID:   "broker-a",
		BrokerName: "Broker A",
		Email:      "test@example.com",
		Template:   "gdpr",
		Status:     StatusSent,
		MessageID:  "msg-001",
		SentAt:     time.Now().Add(-2 * time.Hour),
	}
	if err := store.Add(r1); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if r1.ID == 0 {
		t.Error("expected ID to be set after Add()")
	}

	r2 := &Record{
		BrokerID:   "broker-b",
		BrokerName: "Broker B",
		Email:      "test@example.com",
		Template:   "ccpa",
		Status:     StatusFailed,
		Error:      "connection refused",
		SentAt:     time.Now().Add(-1 * time.Hour),
	}
	if err := store.Add(r2); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	records, err := store.GetRecentRequests(10)
	if err != nil {
		t.Fatalf("GetRecentRequests() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].BrokerID != "broker-b" {
		t.Errorf("first record BrokerID = %q, want %q (most recent first)", records[0].BrokerID, "broker-b")
	}
}

func TestGetStats(t *testing.T) {
	store := newTestStore(t)

	total, sent, failed, err := store.GetStats()
	if err != nil {
		t.Fatalf("GetStats() on empty DB error = %v", err)
	}
	if total != 0 || sent != 0 || failed != 0 {
		t.Errorf("empty DB: total=%d sent=%d failed=%d, want all 0", total, sent, failed)
	}

	store.Add(&Record{
		BrokerID: "a", BrokerName: "A", Email: "t@e.com",
		Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
	})
	store.Add(&Record{
		BrokerID: "b", BrokerName: "B", Email: "t@e.com",
		Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
	})
	store.Add(&Record{
		BrokerID: "c", BrokerName: "C", Email: "t@e.com",
		Template: "gdpr", Status: StatusFailed, Error: "timeout", SentAt: time.Now(),
	})

	total, sent, failed, err = store.GetStats()
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	if total != 3 || sent != 2 || failed != 1 {
		t.Errorf("total=%d sent=%d failed=%d, want total=3 sent=2 failed=1", total, sent, failed)
	}
}

func TestGetMonthlyStats(t *testing.T) {
	store := newTestStore(t)

	store.Add(&Record{
		BrokerID: "a", BrokerName: "A", Email: "t@e.com",
		Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
	})
	store.Add(&Record{
		BrokerID: "b", BrokerName: "B", Email: "t@e.com",
		Template: "gdpr", Status: StatusFailed, Error: "err", SentAt: time.Now(),
	})

	sent, failed, err := store.GetMonthlyStats()
	if err != nil {
		t.Fatalf("GetMonthlyStats() error = %v", err)
	}
	if sent != 1 {
		t.Errorf("sent = %d, want 1", sent)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
}

func TestUpdatePipelineStatusAndGetPipelineStats(t *testing.T) {
	store := newTestStore(t)

	store.Add(&Record{
		BrokerID: "a", BrokerName: "A", Email: "t@e.com",
		Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
	})

	if err := store.UpdatePipelineStatus("a", PipelineAwaitingResponse); err != nil {
		t.Fatalf("UpdatePipelineStatus() error = %v", err)
	}

	stats, err := store.GetPipelineStats()
	if err != nil {
		t.Fatalf("GetPipelineStats() error = %v", err)
	}
	if stats[PipelineAwaitingResponse] != 1 {
		t.Errorf("stats[awaiting_response] = %d, want 1", stats[PipelineAwaitingResponse])
	}

	if err := store.UpdatePipelineStatus("a", PipelineConfirmed); err != nil {
		t.Fatalf("second UpdatePipelineStatus() error = %v", err)
	}

	stats, err = store.GetPipelineStats()
	if err != nil {
		t.Fatalf("GetPipelineStats() error = %v", err)
	}
	if stats[PipelineConfirmed] != 1 {
		t.Errorf("stats[confirmed] = %d, want 1", stats[PipelineConfirmed])
	}
	if _, ok := stats[PipelineAwaitingResponse]; ok {
		t.Error("old pipeline status should not appear")
	}
}

func TestAddBrokerResponseAndGetBrokerResponses(t *testing.T) {
	store := newTestStore(t)

	resp := &BrokerResponse{
		BrokerID:     "broker-a",
		BrokerName:   "Broker A",
		ResponseType: "success",
		EmailFrom:    "privacy@broker-a.example",
		EmailSubject: "Re: Data Removal",
		FormURL:      "https://broker-a.example/form",
		ConfirmURL:   "",
		Confidence:   0.95,
		NeedsReview:  false,
		ReceivedAt:   time.Now(),
	}
	if err := store.AddBrokerResponse(resp); err != nil {
		t.Fatalf("AddBrokerResponse() error = %v", err)
	}
	if resp.ID == 0 {
		t.Error("expected ID to be set")
	}

	responses, err := store.GetBrokerResponses("", false, 10)
	if err != nil {
		t.Fatalf("GetBrokerResponses() error = %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	got := responses[0]
	if got.BrokerID != "broker-a" || got.ResponseType != "success" || got.Confidence != 0.95 {
		t.Errorf("unexpected response: %+v", got)
	}
}

func TestGetBrokerResponses_Filtering(t *testing.T) {
	store := newTestStore(t)

	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "a", BrokerName: "A", ResponseType: "success",
		EmailFrom: "x@y.com", EmailSubject: "s1", Confidence: 0.9, ReceivedAt: time.Now(),
	})
	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "b", BrokerName: "B", ResponseType: "form_required",
		EmailFrom: "x@y.com", EmailSubject: "s2", Confidence: 0.7, NeedsReview: true, ReceivedAt: time.Now(),
	})

	t.Run("filter by type", func(t *testing.T) {
		responses, err := store.GetBrokerResponses("success", false, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(responses) != 1 || responses[0].ResponseType != "success" {
			t.Errorf("expected 1 success response, got %d", len(responses))
		}
	})

	t.Run("filter by needs review", func(t *testing.T) {
		responses, err := store.GetBrokerResponses("", true, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(responses) != 1 || !responses[0].NeedsReview {
			t.Errorf("expected 1 review response, got %d", len(responses))
		}
	})

	t.Run("no filters", func(t *testing.T) {
		responses, err := store.GetBrokerResponses("", false, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(responses) != 2 {
			t.Errorf("expected 2 responses, got %d", len(responses))
		}
	})
}

func TestFindBrokerResponseBySubject(t *testing.T) {
	store := newTestStore(t)

	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "a", BrokerName: "A", ResponseType: "success",
		EmailFrom: "x@y.com", EmailSubject: "Re: Data Removal", Confidence: 0.9, ReceivedAt: time.Now(),
	})

	t.Run("found", func(t *testing.T) {
		resp, err := store.FindBrokerResponseBySubject("a", "Re: Data Removal")
		if err != nil {
			t.Fatal(err)
		}
		if resp == nil {
			t.Fatal("expected response, got nil")
		}
	})

	t.Run("not found", func(t *testing.T) {
		resp, err := store.FindBrokerResponseBySubject("a", "nonexistent subject")
		if err != nil {
			t.Fatal(err)
		}
		if resp != nil {
			t.Errorf("expected nil, got %+v", resp)
		}
	})
}

func TestUpdateBrokerResponseClassification(t *testing.T) {
	store := newTestStore(t)

	resp := &BrokerResponse{
		BrokerID: "a", BrokerName: "A", ResponseType: "unknown",
		EmailFrom: "x@y.com", EmailSubject: "test", Confidence: 0.3,
		NeedsReview: true, ReceivedAt: time.Now(),
	}
	store.AddBrokerResponse(resp)

	if err := store.UpdateBrokerResponseClassification(resp.ID, "success", "", "", 0.99, false); err != nil {
		t.Fatalf("UpdateBrokerResponseClassification() error = %v", err)
	}

	responses, _ := store.GetBrokerResponses("success", false, 10)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].Confidence != 0.99 {
		t.Errorf("Confidence = %f, want 0.99", responses[0].Confidence)
	}
}

func TestClearBrokerResponses(t *testing.T) {
	store := newTestStore(t)
	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "a", BrokerName: "A", ResponseType: "success",
		EmailFrom: "x@y.com", EmailSubject: "test", Confidence: 0.9, ReceivedAt: time.Now(),
	})
	if err := store.ClearBrokerResponses(); err != nil {
		t.Fatalf("ClearBrokerResponses() error = %v", err)
	}
	responses, _ := store.GetBrokerResponses("", false, 10)
	if len(responses) != 0 {
		t.Errorf("expected 0 responses after clear, got %d", len(responses))
	}
}

func TestPendingTasks_CRUD(t *testing.T) {
	store := newTestStore(t)

	task := &PendingTask{
		BrokerID:   "broker-a",
		BrokerName: "Broker A",
		TaskType:   TaskCaptcha,
		FormURL:    "https://example.com/form",
		Notes:      "needs manual solving",
	}
	if err := store.AddPendingTask(task); err != nil {
		t.Fatalf("AddPendingTask() error = %v", err)
	}
	if task.ID == 0 {
		t.Error("expected ID to be set")
	}

	t.Run("get by ID", func(t *testing.T) {
		got, err := store.GetPendingTaskByID(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil {
			t.Fatal("expected task, got nil")
		}
		if got.BrokerID != "broker-a" || got.TaskType != TaskCaptcha {
			t.Errorf("unexpected task: %+v", got)
		}
		if got.FormURL != "https://example.com/form" {
			t.Errorf("FormURL = %q, want %q", got.FormURL, "https://example.com/form")
		}
		if got.Notes != "needs manual solving" {
			t.Errorf("Notes = %q, want %q", got.Notes, "needs manual solving")
		}
	})

	t.Run("get pending by type", func(t *testing.T) {
		tasks, err := store.GetPendingTasks(TaskCaptcha, "pending")
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatalf("expected 1 task, got %d", len(tasks))
		}
	})

	t.Run("get all pending", func(t *testing.T) {
		tasks, err := store.GetPendingTasks("", "pending")
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatalf("expected 1 task, got %d", len(tasks))
		}
	})

	t.Run("complete task", func(t *testing.T) {
		if err := store.CompletePendingTask(task.ID, "completed"); err != nil {
			t.Fatalf("CompletePendingTask() error = %v", err)
		}
		got, _ := store.GetPendingTaskByID(task.ID)
		if got.Status != "completed" {
			t.Errorf("Status = %q, want %q", got.Status, "completed")
		}
		if !got.CompletedAt.Valid {
			t.Error("CompletedAt should be set")
		}
	})

	t.Run("mark opened", func(t *testing.T) {
		task2 := &PendingTask{
			BrokerID: "broker-b", BrokerName: "B", TaskType: TaskManualForm,
		}
		store.AddPendingTask(task2)
		if err := store.MarkTaskOpened(task2.ID); err != nil {
			t.Fatalf("MarkTaskOpened() error = %v", err)
		}
		got, _ := store.GetPendingTaskByID(task2.ID)
		if !got.OpenedAt.Valid {
			t.Error("OpenedAt should be set")
		}
	})

	t.Run("nonexistent task", func(t *testing.T) {
		got, err := store.GetPendingTaskByID(99999)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("expected nil for nonexistent task, got %+v", got)
		}
	})
}

func TestGetPendingTaskStats(t *testing.T) {
	store := newTestStore(t)

	store.AddPendingTask(&PendingTask{BrokerID: "a", BrokerName: "A", TaskType: TaskCaptcha})
	store.AddPendingTask(&PendingTask{BrokerID: "b", BrokerName: "B", TaskType: TaskManualForm})
	task := &PendingTask{BrokerID: "c", BrokerName: "C", TaskType: TaskReview}
	store.AddPendingTask(task)
	store.CompletePendingTask(task.ID, "completed")

	pending, completed, skipped, err := store.GetPendingTaskStats()
	if err != nil {
		t.Fatalf("GetPendingTaskStats() error = %v", err)
	}
	if pending != 2 {
		t.Errorf("pending = %d, want 2", pending)
	}
	if completed != 1 {
		t.Errorf("completed = %d, want 1", completed)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
}

func TestParseSQLiteTime(t *testing.T) {
	tests := []struct {
		name  string
		input sql.NullString
		want  bool
	}{
		{"null string", sql.NullString{Valid: false}, false},
		{"RFC3339", sql.NullString{String: "2024-01-15T10:30:00Z", Valid: true}, true},
		{"SQLite format", sql.NullString{String: "2024-01-15 10:30:00", Valid: true}, true},
		{"invalid", sql.NullString{String: "not-a-date", Valid: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSQLiteTime(tt.input)
			if tt.want && got.IsZero() {
				t.Error("expected non-zero time")
			}
			if !tt.want && !got.IsZero() {
				t.Errorf("expected zero time, got %v", got)
			}
		})
	}
}

func TestDeleteByStatus(t *testing.T) {
	store := newTestStore(t)

	store.Add(&Record{
		BrokerID: "a", BrokerName: "A", Email: "t@e.com",
		Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
	})
	store.Add(&Record{
		BrokerID: "b", BrokerName: "B", Email: "t@e.com",
		Template: "gdpr", Status: StatusFailed, Error: "err", SentAt: time.Now(),
	})

	deleted, err := store.DeleteByStatus(StatusFailed)
	if err != nil {
		t.Fatalf("DeleteByStatus() error = %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	total, _, _, _ := store.GetStats()
	if total != 1 {
		t.Errorf("total after delete = %d, want 1", total)
	}
}

func TestGetLastRequestForBroker(t *testing.T) {
	store := newTestStore(t)

	t.Run("no requests", func(t *testing.T) {
		got, err := store.GetLastRequestForBroker("nonexistent")
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("returns latest", func(t *testing.T) {
		store.Add(&Record{
			BrokerID: "a", BrokerName: "A", Email: "t@e.com",
			Template: "gdpr", Status: StatusSent, SentAt: time.Now().Add(-2 * time.Hour),
		})
		store.Add(&Record{
			BrokerID: "a", BrokerName: "A", Email: "t@e.com",
			Template: "ccpa", Status: StatusSent, SentAt: time.Now(),
		})

		got, err := store.GetLastRequestForBroker("a")
		if err != nil {
			t.Fatal(err)
		}
		if got == nil {
			t.Fatal("expected record, got nil")
		}
		if got.Template != "ccpa" {
			t.Errorf("Template = %q, want %q (latest)", got.Template, "ccpa")
		}
	})
}

func TestGetResponseStats(t *testing.T) {
	store := newTestStore(t)

	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "a", BrokerName: "A", ResponseType: "success",
		EmailFrom: "x@y.com", EmailSubject: "s1", Confidence: 0.9, ReceivedAt: time.Now(),
	})
	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "b", BrokerName: "B", ResponseType: "success",
		EmailFrom: "x@y.com", EmailSubject: "s2", Confidence: 0.8, ReceivedAt: time.Now(),
	})
	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "c", BrokerName: "C", ResponseType: "rejected",
		EmailFrom: "x@y.com", EmailSubject: "s3", Confidence: 0.7, ReceivedAt: time.Now(),
	})

	stats, err := store.GetResponseStats()
	if err != nil {
		t.Fatalf("GetResponseStats() error = %v", err)
	}
	if stats["success"] != 2 {
		t.Errorf("stats[success] = %d, want 2", stats["success"])
	}
	if stats["rejected"] != 1 {
		t.Errorf("stats[rejected] = %d, want 1", stats["rejected"])
	}
}

func TestGetAllBrokerResponses(t *testing.T) {
	store := newTestStore(t)

	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "a", BrokerName: "A", ResponseType: "success",
		EmailFrom: "x@y.com", EmailSubject: "s1", EmailBody: "body text",
		Confidence: 0.9, ReceivedAt: time.Now(),
	})

	responses, err := store.GetAllBrokerResponses()
	if err != nil {
		t.Fatalf("GetAllBrokerResponses() error = %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected 1, got %d", len(responses))
	}
	if responses[0].EmailBody != "body text" {
		t.Errorf("EmailBody = %q, want %q", responses[0].EmailBody, "body text")
	}
}

func TestUpdateBrokerResponseBody(t *testing.T) {
	store := newTestStore(t)

	resp := &BrokerResponse{
		BrokerID: "a", BrokerName: "A", ResponseType: "unknown",
		EmailFrom: "x@y.com", EmailSubject: "s1", Confidence: 0.5, ReceivedAt: time.Now(),
	}
	store.AddBrokerResponse(resp)

	if err := store.UpdateBrokerResponseBody(resp.ID, "updated body"); err != nil {
		t.Fatalf("UpdateBrokerResponseBody() error = %v", err)
	}

	all, _ := store.GetAllBrokerResponses()
	if len(all) != 1 || all[0].EmailBody != "updated body" {
		t.Errorf("EmailBody not updated")
	}
}

func TestDefaultDBPath(t *testing.T) {
	path := DefaultDBPath()
	if path == "" {
		t.Error("DefaultDBPath() returned empty string")
	}
	_, err := os.UserHomeDir()
	if err == nil && !strings.Contains(path, ".eraser") {
		t.Errorf("DefaultDBPath() = %q, expected to contain .eraser", path)
	}
}

func TestGetAllBrokerStatuses(t *testing.T) {
	store := newTestStore(t)

	t.Run("empty database", func(t *testing.T) {
		statuses, err := store.GetAllBrokerStatuses()
		if err != nil {
			t.Fatal(err)
		}
		if len(statuses) != 0 {
			t.Errorf("expected 0 statuses, got %d", len(statuses))
		}
	})

	t.Run("multiple brokers", func(t *testing.T) {
		store.Add(&Record{
			BrokerID: "a", BrokerName: "A", Email: "t@e.com",
			Template: "gdpr", Status: StatusSent, SentAt: time.Now().Add(-2 * time.Hour),
		})
		store.Add(&Record{
			BrokerID: "a", BrokerName: "A", Email: "t@e.com",
			Template: "gdpr", Status: StatusFailed, Error: "timeout", SentAt: time.Now(),
		})
		store.Add(&Record{
			BrokerID: "b", BrokerName: "B", Email: "t@e.com",
			Template: "ccpa", Status: StatusSent, SentAt: time.Now().Add(-30 * time.Minute),
		})

		statuses, err := store.GetAllBrokerStatuses()
		if err != nil {
			t.Fatal(err)
		}
		if len(statuses) != 2 {
			t.Fatalf("expected 2 brokers, got %d", len(statuses))
		}

		bsA := statuses["a"]
		if bsA.Status != StatusFailed {
			t.Errorf("broker a Status = %q, want %q", bsA.Status, StatusFailed)
		}
		if bsA.TotalSent != 2 {
			t.Errorf("broker a TotalSent = %d, want 2", bsA.TotalSent)
		}

		bsB := statuses["b"]
		if bsB.Status != StatusSent {
			t.Errorf("broker b Status = %q, want %q", bsB.Status, StatusSent)
		}
		if bsB.TotalSent != 1 {
			t.Errorf("broker b TotalSent = %d, want 1", bsB.TotalSent)
		}
	})
}

func TestGetBrokerResponses_CombinedFilter(t *testing.T) {
	store := newTestStore(t)

	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
		EmailFrom: "x@y.com", EmailSubject: "s1", Confidence: 0.7,
		NeedsReview: true, ReceivedAt: time.Now(),
	})
	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "b", BrokerName: "B", ResponseType: "form_required",
		EmailFrom: "x@y.com", EmailSubject: "s2", Confidence: 0.9,
		NeedsReview: false, ReceivedAt: time.Now(),
	})
	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "c", BrokerName: "C", ResponseType: "success",
		EmailFrom: "x@y.com", EmailSubject: "s3", Confidence: 0.95,
		NeedsReview: true, ReceivedAt: time.Now(),
	})

	tests := []struct {
		name         string
		responseType string
		needsReview  bool
		limit        int
		wantCount    int
	}{
		{"type + needs_review", "form_required", true, 10, 1},
		{"type only", "form_required", false, 10, 2},
		{"needs_review only", "", true, 10, 2},
		{"no filters", "", false, 10, 3},
		{"limit", "", false, 2, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses, err := store.GetBrokerResponses(tt.responseType, tt.needsReview, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			if len(responses) != tt.wantCount {
				t.Errorf("got %d responses, want %d", len(responses), tt.wantCount)
			}
		})
	}
}

func TestGetBrokerResponses_EmptyDB(t *testing.T) {
	store := newTestStore(t)

	responses, err := store.GetBrokerResponses("", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 0 {
		t.Errorf("expected 0 responses from empty DB, got %d", len(responses))
	}
}

func TestGetAllBrokerResponses_Empty(t *testing.T) {
	store := newTestStore(t)

	responses, err := store.GetAllBrokerResponses()
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 0 {
		t.Errorf("expected 0, got %d", len(responses))
	}
}

func TestGetResponseStats_Empty(t *testing.T) {
	store := newTestStore(t)

	stats, err := store.GetResponseStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %d entries", len(stats))
	}
}

func TestMarkTaskOpened_Idempotent(t *testing.T) {
	store := newTestStore(t)

	task := &PendingTask{BrokerID: "a", BrokerName: "A", TaskType: TaskCaptcha}
	store.AddPendingTask(task)

	if err := store.MarkTaskOpened(task.ID); err != nil {
		t.Fatalf("first MarkTaskOpened() error = %v", err)
	}
	got, _ := store.GetPendingTaskByID(task.ID)
	firstOpen := got.OpenedAt.Time

	time.Sleep(10 * time.Millisecond)

	if err := store.MarkTaskOpened(task.ID); err != nil {
		t.Fatalf("second MarkTaskOpened() error = %v", err)
	}
	got, _ = store.GetPendingTaskByID(task.ID)
	if !got.OpenedAt.Time.Equal(firstOpen) {
		t.Error("MarkTaskOpened should be idempotent - opened_at changed on second call")
	}
}

func TestGetPendingTasks_FilterCombos(t *testing.T) {
	store := newTestStore(t)

	t1 := &PendingTask{BrokerID: "a", BrokerName: "A", TaskType: TaskCaptcha}
	store.AddPendingTask(t1)
	t2 := &PendingTask{BrokerID: "b", BrokerName: "B", TaskType: TaskManualForm}
	store.AddPendingTask(t2)
	store.CompletePendingTask(t2.ID, "completed")

	tests := []struct {
		name      string
		taskType  TaskType
		status    string
		wantCount int
	}{
		{"no filters", "", "", 2},
		{"type only", TaskCaptcha, "", 1},
		{"status only", "", "pending", 1},
		{"type + status match", TaskManualForm, "completed", 1},
		{"type + status mismatch", TaskCaptcha, "completed", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks, err := store.GetPendingTasks(tt.taskType, tt.status)
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) != tt.wantCount {
				t.Errorf("got %d tasks, want %d", len(tasks), tt.wantCount)
			}
		})
	}
}

func TestAddPendingTask_SetsStatus(t *testing.T) {
	store := newTestStore(t)

	task := &PendingTask{
		BrokerID:       "a",
		BrokerName:     "A",
		TaskType:       TaskCaptcha,
		FormURL:        "https://example.com",
		ScreenshotPath: "/tmp/shot.png",
		BrowserState:   `{"cookies":[]}`,
		Notes:          "test note",
		Status:         "ignored",
	}
	store.AddPendingTask(task)

	got, _ := store.GetPendingTaskByID(task.ID)
	if got.Status != "pending" {
		t.Errorf("Status = %q, want %q (AddPendingTask always sets pending)", got.Status, "pending")
	}
	if got.BrowserState != `{"cookies":[]}` {
		t.Errorf("BrowserState = %q, unexpected", got.BrowserState)
	}
	if got.ScreenshotPath != "/tmp/shot.png" {
		t.Errorf("ScreenshotPath = %q, unexpected", got.ScreenshotPath)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestGetFormsWithStatus(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*Store)
		wantCount  int
		wantStatus string
		wantBroker string
	}{
		{
			name:      "empty database",
			setup:     func(s *Store) {},
			wantCount: 0,
		},
		{
			name: "pending form",
			setup: func(s *Store) {
				s.Add(&Record{
					BrokerID: "a", BrokerName: "A", Email: "t@e.com",
					Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
				})
				s.AddBrokerResponse(&BrokerResponse{
					BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
					EmailFrom: "x@y.com", EmailSubject: "form here", FormURL: "https://form.example.com",
					Confidence: 0.9, ReceivedAt: time.Now(),
				})
			},
			wantCount:  1,
			wantStatus: "pending",
			wantBroker: "a",
		},
		{
			name: "completed task shows filled",
			setup: func(s *Store) {
				s.Add(&Record{
					BrokerID: "a", BrokerName: "A", Email: "t@e.com",
					Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
				})
				s.AddBrokerResponse(&BrokerResponse{
					BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
					EmailFrom: "x@y.com", EmailSubject: "form", FormURL: "https://form.example.com",
					Confidence: 0.9, ReceivedAt: time.Now(),
				})
				task := &PendingTask{
					BrokerID: "a", BrokerName: "A", TaskType: TaskCaptcha,
					FormURL: "https://form.example.com",
				}
				s.AddPendingTask(task)
				s.CompletePendingTask(task.ID, "completed")
			},
			wantCount:  1,
			wantStatus: "filled",
			wantBroker: "a",
		},
		{
			name: "pending captcha task",
			setup: func(s *Store) {
				s.Add(&Record{
					BrokerID: "a", BrokerName: "A", Email: "t@e.com",
					Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
				})
				s.AddBrokerResponse(&BrokerResponse{
					BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
					EmailFrom: "x@y.com", EmailSubject: "form", FormURL: "https://form.example.com",
					Confidence: 0.9, ReceivedAt: time.Now(),
				})
				task := &PendingTask{
					BrokerID: "a", BrokerName: "A", TaskType: TaskCaptcha,
					FormURL: "https://form.example.com",
				}
				s.AddPendingTask(task)
			},
			wantCount:  1,
			wantStatus: "captcha",
			wantBroker: "a",
		},
		{
			name: "skipped task",
			setup: func(s *Store) {
				s.Add(&Record{
					BrokerID: "a", BrokerName: "A", Email: "t@e.com",
					Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
				})
				s.AddBrokerResponse(&BrokerResponse{
					BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
					EmailFrom: "x@y.com", EmailSubject: "form", FormURL: "https://form.example.com",
					Confidence: 0.9, ReceivedAt: time.Now(),
				})
				task := &PendingTask{
					BrokerID: "a", BrokerName: "A", TaskType: TaskManualForm,
					FormURL: "https://form.example.com",
				}
				s.AddPendingTask(task)
				s.CompletePendingTask(task.ID, "skipped")
			},
			wantCount:  1,
			wantStatus: "skipped",
			wantBroker: "a",
		},
		{
			name: "pipeline status filled",
			setup: func(s *Store) {
				s.Add(&Record{
					BrokerID: "a", BrokerName: "A", Email: "t@e.com",
					Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
				})
				s.UpdatePipelineStatus("a", PipelineFormFilled)
				s.AddBrokerResponse(&BrokerResponse{
					BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
					EmailFrom: "x@y.com", EmailSubject: "form", FormURL: "https://form.example.com",
					Confidence: 0.9, ReceivedAt: time.Now(),
				})
			},
			wantCount:  1,
			wantStatus: "filled",
			wantBroker: "a",
		},
		{
			name: "pipeline status confirmed",
			setup: func(s *Store) {
				s.Add(&Record{
					BrokerID: "a", BrokerName: "A", Email: "t@e.com",
					Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
				})
				s.UpdatePipelineStatus("a", PipelineConfirmed)
				s.AddBrokerResponse(&BrokerResponse{
					BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
					EmailFrom: "x@y.com", EmailSubject: "form", FormURL: "https://form.example.com",
					Confidence: 0.9, ReceivedAt: time.Now(),
				})
			},
			wantCount:  1,
			wantStatus: "filled",
			wantBroker: "a",
		},
		{
			name: "pipeline status failed",
			setup: func(s *Store) {
				s.Add(&Record{
					BrokerID: "a", BrokerName: "A", Email: "t@e.com",
					Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
				})
				s.UpdatePipelineStatus("a", PipelineFailed)
				s.AddBrokerResponse(&BrokerResponse{
					BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
					EmailFrom: "x@y.com", EmailSubject: "form", FormURL: "https://form.example.com",
					Confidence: 0.9, ReceivedAt: time.Now(),
				})
			},
			wantCount:  1,
			wantStatus: "failed",
			wantBroker: "a",
		},
		{
			name: "pipeline status rejected",
			setup: func(s *Store) {
				s.Add(&Record{
					BrokerID: "a", BrokerName: "A", Email: "t@e.com",
					Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
				})
				s.UpdatePipelineStatus("a", PipelineRejected)
				s.AddBrokerResponse(&BrokerResponse{
					BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
					EmailFrom: "x@y.com", EmailSubject: "form", FormURL: "https://form.example.com",
					Confidence: 0.9, ReceivedAt: time.Now(),
				})
			},
			wantCount:  1,
			wantStatus: "skipped",
			wantBroker: "a",
		},
		{
			name: "deduplicates by broker_id",
			setup: func(s *Store) {
				s.Add(&Record{
					BrokerID: "a", BrokerName: "A", Email: "t@e.com",
					Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
				})
				s.AddBrokerResponse(&BrokerResponse{
					BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
					EmailFrom: "x@y.com", EmailSubject: "form1", FormURL: "https://f1.com",
					Confidence: 0.9, ReceivedAt: time.Now(),
				})
				s.AddBrokerResponse(&BrokerResponse{
					BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
					EmailFrom: "x@y.com", EmailSubject: "form2", FormURL: "https://f2.com",
					Confidence: 0.8, ReceivedAt: time.Now(),
				})
			},
			wantCount:  1,
			wantStatus: "pending",
			wantBroker: "a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			tt.setup(store)

			forms, err := store.GetFormsWithStatus()
			if err != nil {
				t.Fatalf("GetFormsWithStatus() error = %v", err)
			}
			if len(forms) != tt.wantCount {
				t.Fatalf("got %d forms, want %d", len(forms), tt.wantCount)
			}
			if tt.wantCount > 0 {
				if forms[0].Status != tt.wantStatus {
					t.Errorf("Status = %q, want %q", forms[0].Status, tt.wantStatus)
				}
				if tt.wantBroker != "" && forms[0].BrokerID != tt.wantBroker {
					t.Errorf("BrokerID = %q, want %q", forms[0].BrokerID, tt.wantBroker)
				}
			}
		})
	}
}

func TestGetFormStats(t *testing.T) {
	store := newTestStore(t)

	store.Add(&Record{
		BrokerID: "a", BrokerName: "A", Email: "t@e.com",
		Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
	})
	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "a", BrokerName: "A", ResponseType: "form_required",
		EmailFrom: "x@y.com", EmailSubject: "form", FormURL: "https://f.com",
		Confidence: 0.9, ReceivedAt: time.Now(),
	})

	pending, filled, captcha, failed, skipped, err := store.GetFormStats()
	if err != nil {
		t.Fatalf("GetFormStats() error = %v", err)
	}
	if pending != 1 {
		t.Errorf("pending = %d, want 1", pending)
	}
	if filled != 0 {
		t.Errorf("filled = %d, want 0", filled)
	}
	if captcha != 0 {
		t.Errorf("captcha = %d, want 0", captcha)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
}

func TestGetFormStats_Empty(t *testing.T) {
	store := newTestStore(t)

	pending, filled, captcha, failed, skipped, err := store.GetFormStats()
	if err != nil {
		t.Fatalf("GetFormStats() error = %v", err)
	}
	if pending != 0 || filled != 0 || captcha != 0 || failed != 0 || skipped != 0 {
		t.Errorf("all stats should be 0: p=%d f=%d c=%d fa=%d s=%d", pending, filled, captcha, failed, skipped)
	}
}

func TestGetPendingTaskStats_Empty(t *testing.T) {
	store := newTestStore(t)

	pending, completed, skipped, err := store.GetPendingTaskStats()
	if err != nil {
		t.Fatalf("GetPendingTaskStats() error = %v", err)
	}
	if pending != 0 || completed != 0 || skipped != 0 {
		t.Errorf("empty DB: p=%d c=%d s=%d, want all 0", pending, completed, skipped)
	}
}

func TestDeleteByStatus_NoMatch(t *testing.T) {
	store := newTestStore(t)

	deleted, err := store.DeleteByStatus(StatusSent)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0 on empty DB", deleted)
	}
}

func TestGetRecentRequests_Empty(t *testing.T) {
	store := newTestStore(t)

	records, err := store.GetRecentRequests(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestGetMonthlyStats_Empty(t *testing.T) {
	store := newTestStore(t)

	sent, failed, err := store.GetMonthlyStats()
	if err != nil {
		t.Fatal(err)
	}
	if sent != 0 || failed != 0 {
		t.Errorf("sent=%d failed=%d, want both 0", sent, failed)
	}
}

func TestGetPipelineStats_Empty(t *testing.T) {
	store := newTestStore(t)

	stats, err := store.GetPipelineStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %d entries", len(stats))
	}
}

func TestAddBrokerResponse_NeedsReviewTrue(t *testing.T) {
	store := newTestStore(t)

	resp := &BrokerResponse{
		BrokerID: "a", BrokerName: "A", ResponseType: "unknown",
		EmailFrom: "x@y.com", EmailSubject: "s1", Confidence: 0.3,
		NeedsReview: true, ReceivedAt: time.Now(),
	}
	store.AddBrokerResponse(resp)

	got, _ := store.GetBrokerResponses("", true, 10)
	if len(got) != 1 || !got[0].NeedsReview {
		t.Error("NeedsReview should be true")
	}
}

func TestConcurrentAccess(t *testing.T) {
	store := newTestStore(t)

	store.Add(&Record{
		BrokerID: "broker", BrokerName: "Broker", Email: "t@e.com",
		Template: "gdpr", Status: StatusSent, SentAt: time.Now(),
	})
	store.AddBrokerResponse(&BrokerResponse{
		BrokerID: "a", BrokerName: "A", ResponseType: "success",
		EmailFrom: "x@y.com", EmailSubject: "s", Confidence: 0.9, ReceivedAt: time.Now(),
	})
	task := &PendingTask{BrokerID: "a", BrokerName: "A", TaskType: TaskCaptcha}
	store.AddPendingTask(task)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.GetStats()
			store.GetRecentRequests(5)
			store.GetAllBrokerStatuses()
			store.GetBrokerResponses("", false, 10)
			store.GetPendingTasks("", "")
			store.GetPipelineStats()
			store.GetResponseStats()
		}()
	}
	wg.Wait()

	total, sent, failed, err := store.GetStats()
	if err != nil {
		t.Fatalf("GetStats() after concurrent reads: %v", err)
	}
	if total != 1 || sent != 1 || failed != 0 {
		t.Errorf("total=%d sent=%d failed=%d, want 1/1/0", total, sent, failed)
	}
}
