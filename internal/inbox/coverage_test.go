package inbox

import (
	"testing"

	"github.com/robindubreuil/eraser/internal/broker"
	"github.com/robindubreuil/eraser/internal/config"
)

func TestArchiveEmailsEmptyUIDs(t *testing.T) {
	m := &Monitor{client: nil}
	err := m.ArchiveEmails(nil, "Archive")
	if err == nil {
		t.Error("expected error with nil client")
	}
}

func TestNewMonitor_BrokerWithEmptyEmail(t *testing.T) {
	cfg := config.InboxConfig{}
	brokers := []broker.Broker{
		{ID: "b1", Email: ""},
		{ID: "b2", Email: "no-at-sign"},
	}
	m := NewMonitor(cfg, brokers)
	if len(m.brokers) != 0 {
		t.Errorf("brokers = %d entries, want 0 (all invalid emails)", len(m.brokers))
	}
}

func TestNewMonitor_BrokerWithAtSign(t *testing.T) {
	cfg := config.InboxConfig{}
	brokers := []broker.Broker{
		{ID: "b2", Email: "@"},
	}
	m := NewMonitor(cfg, brokers)
	if len(m.brokers) != 1 {
		t.Errorf("brokers = %d entries, @ splits into ['', ''] and lower('') maps to empty string", len(m.brokers))
	}
}

func TestSummarizeResponses_SingleEntry(t *testing.T) {
	responses := []ClassifiedResponse{
		{Type: ResponseSuccess, Confidence: 0.9},
	}
	summary := SummarizeResponses(responses)
	if summary.Total != 1 {
		t.Errorf("Total = %d, want 1", summary.Total)
	}
	if summary.Success != 1 {
		t.Errorf("Success = %d, want 1", summary.Success)
	}
}

func TestGetBouncedResponses_Empty(t *testing.T) {
	got := GetBouncedResponses(nil)
	if len(got) != 0 {
		t.Errorf("GetBouncedResponses(nil) = %d, want 0", len(got))
	}
}

func TestGetBouncedResponses_NoBounces(t *testing.T) {
	responses := []ClassifiedResponse{
		{Type: ResponseSuccess},
		{Type: ResponsePending},
	}
	got := GetBouncedResponses(responses)
	if len(got) != 0 {
		t.Errorf("GetBouncedResponses() = %d, want 0", len(got))
	}
}

func TestIsTestEmail_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		content string
		want    bool
	}{
		{"case insensitive subject", "ERASER TEST EMAIL", "", true},
		{"case insensitive body", "", "TEST EMAIL FROM ERASER", true},
		{"partial match not test", "Test your email settings", "", false},
		{"test email in middle of content", "", "This is a test email from Eraser to verify", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestEmail(tt.subject, tt.content)
			if got != tt.want {
				t.Errorf("isTestEmail(%q, %q) = %v, want %v", tt.subject, tt.content, got, tt.want)
			}
		})
	}
}

func TestStripHTML_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"nested script", `<script type="text/javascript">var x=1;</script>Content`, "Content"},
		{"nested style", `<style type="text/css">body{}</style>Text`, "Text"},
		{"multiple entities", "&nbsp;&nbsp;&amp;&lt;", "&<"},
		{"img tag", `<img src="test.jpg" alt="test"/>`, ""},
		{"br tags", "line1<br>line2<br/>line3", "line1 line2 line3"},
		{"attributes with quotes", `<a href="http://test.com" class="btn">Link</a>`, "Link"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTML(tt.input)
			if got != tt.want {
				t.Errorf("stripHTML() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyBatch_SingleElement(t *testing.T) {
	emails := []Email{
		{Subject: "Eraser Test Email", Body: "test"},
	}
	results := ClassifyBatch(emails)
	if len(results) != 1 {
		t.Fatalf("ClassifyBatch returned %d results, want 1", len(results))
	}
	if results[0].Type != ResponseSuccess {
		t.Errorf("results[0].Type = %s, want %s", results[0].Type, ResponseSuccess)
	}
	if results[0].Confidence != 1.0 {
		t.Errorf("results[0].Confidence = %.2f, want 1.0", results[0].Confidence)
	}
}

func TestClassifyBySubjectOnly_WeakMatches(t *testing.T) {
	tests := []struct {
		subject  string
		wantType ResponseType
	}{
		{"Automatic reply: your request", ResponsePending},
		{"Your removal request has been received", ResponsePending},
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			gotType, _, _ := ClassifyBySubjectOnly(tt.subject)
			if gotType != tt.wantType {
				t.Errorf("ClassifyBySubjectOnly(%q) type = %s, want %s", tt.subject, gotType, tt.wantType)
			}
		})
	}
}

func TestFilterByType_AllMatching(t *testing.T) {
	responses := []ClassifiedResponse{
		{Type: ResponseBounced},
		{Type: ResponseBounced},
	}
	got := FilterByType(responses, ResponseBounced)
	if len(got) != 2 {
		t.Errorf("FilterByType() = %d, want 2", len(got))
	}
}

func TestGetActionableResponses_WithConfirmOnly(t *testing.T) {
	responses := []ClassifiedResponse{
		{Type: ResponseConfirmationRequired, ConfirmURL: "https://example.com/confirm"},
	}
	got := GetActionableResponses(responses)
	if len(got) != 1 {
		t.Errorf("GetActionableResponses() = %d, want 1", len(got))
	}
}

func TestIsBounceEmail_554Code(t *testing.T) {
	email := &Email{From: "mailer-daemon@mail.com", FromName: "Mail System"}
	got := isBounceEmail(email, "Delivery failure", "554 rejected message")
	if !got {
		t.Error("isBounceEmail() = false for 554 rejection, want true")
	}
}

func TestIsBounceEmail_BounceSenderNoContent(t *testing.T) {
	email := &Email{From: "postmaster@exchange.com", FromName: "Postmaster"}
	got := isBounceEmail(email, "Normal subject", "normal content")
	if got {
		t.Error("isBounceEmail() should be false for bounce sender without bounce patterns")
	}
}
