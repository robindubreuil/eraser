package inbox

import (
	"testing"
)

func TestGetClassificationReason(t *testing.T) {
	tests := []struct {
		name         string
		responseType ResponseType
		want         string
	}{
		{"success", ResponseSuccess, "Email indicates removal request was completed successfully"},
		{"form_required", ResponseFormRequired, "Email contains link to opt-out form that needs to be filled"},
		{"confirmation_required", ResponseConfirmationRequired, "Email contains confirmation link that needs to be clicked"},
		{"rejected", ResponseRejected, "Broker rejected or could not process the removal request"},
		{"pending", ResponsePending, "Request is being processed, follow-up may be needed"},
		{"unknown", ResponseUnknown, "Could not automatically classify this response"},
		{"bounced", ResponseBounced, "Unknown classification"},
		{"empty_type", ResponseType(""), "Unknown classification"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getClassificationReason(tt.responseType, 0)
			if got != tt.want {
				t.Errorf("getClassificationReason(%q) = %q, want %q", tt.responseType, got, tt.want)
			}
		})
	}
}

func TestClassifyBatch(t *testing.T) {
	emails := []Email{
		{Subject: "Eraser Test Email", Body: "test"},
		{Subject: "Re: Request", Body: "we have removed your data"},
		{Subject: "Unknown subject", Body: "some random text"},
	}
	results := ClassifyBatch(emails)
	if len(results) != 3 {
		t.Fatalf("ClassifyBatch returned %d results, want 3", len(results))
	}
	if results[0].Type != ResponseSuccess {
		t.Errorf("results[0].Type = %s, want %s", results[0].Type, ResponseSuccess)
	}
	if results[1].Type != ResponseSuccess {
		t.Errorf("results[1].Type = %s, want %s", results[1].Type, ResponseSuccess)
	}
	if results[2].Type != ResponseUnknown {
		t.Errorf("results[2].Type = %s, want %s", results[2].Type, ResponseUnknown)
	}
}

func TestClassifyBatchEmpty(t *testing.T) {
	results := ClassifyBatch(nil)
	if len(results) != 0 {
		t.Errorf("ClassifyBatch(nil) returned %d results, want 0", len(results))
	}
}

func TestClassifyBySubjectOnly(t *testing.T) {
	tests := []struct {
		name        string
		subject     string
		wantType    ResponseType
		wantMinConf float64
		wantMaxConf float64
		wantReview  bool
	}{
		{"automatic reply strong", "Automatic reply: your request", ResponsePending, 0.7, 1.0, false},
		{"out of office", "Out of Office: Re: Removal Request", ResponsePending, 0.7, 1.0, false},
		{"not found rejection", "Not Found - Data Request", ResponseRejected, 0.7, 1.0, false},
		{"opt-out completed", "Opt-out Has Been Completed", ResponseSuccess, 0.7, 1.0, false},
		{"removal instructions form", "Opt-out Instructions", ResponseFormRequired, 0.7, 1.0, false},
		{"weak match", "We will respond to your inquiry soon", ResponsePending, 0.4, 0.49, true},
		{"no match", "Hello world", ResponseUnknown, 0.0, 0.0, true},
		{"ticket number", "#REQ-123456 Request Update", ResponsePending, 0.7, 1.0, false},
		{"thank you for privacy", "Thank you for your privacy request", ResponsePending, 0.7, 1.0, false},
		{"unable to process", "Unable to process your request", ResponseRejected, 0.7, 1.0, false},
		{"request received", "Your request has been received", ResponsePending, 0.7, 1.0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotConf, gotReview := ClassifyBySubjectOnly(tt.subject)
			if gotType != tt.wantType {
				t.Errorf("type = %s, want %s", gotType, tt.wantType)
			}
			if gotConf < tt.wantMinConf || gotConf > tt.wantMaxConf {
				t.Errorf("confidence = %.2f, want between %.2f and %.2f", gotConf, tt.wantMinConf, tt.wantMaxConf)
			}
			if gotReview != tt.wantReview {
				t.Errorf("needsReview = %v, want %v", gotReview, tt.wantReview)
			}
		})
	}
}

func TestFilterByType(t *testing.T) {
	responses := []ClassifiedResponse{
		{Type: ResponseSuccess},
		{Type: ResponsePending},
		{Type: ResponseSuccess},
		{Type: ResponseRejected},
		{Type: ResponseFormRequired},
	}
	tests := []struct {
		name    string
		rType   ResponseType
		wantLen int
	}{
		{"filter success", ResponseSuccess, 2},
		{"filter pending", ResponsePending, 1},
		{"filter rejected", ResponseRejected, 1},
		{"filter bounced none", ResponseBounced, 0},
		{"filter form", ResponseFormRequired, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterByType(responses, tt.rType)
			if len(got) != tt.wantLen {
				t.Errorf("FilterByType(%s) = %d results, want %d", tt.rType, len(got), tt.wantLen)
			}
		})
	}
}

func TestFilterByTypeEmpty(t *testing.T) {
	got := FilterByType(nil, ResponseSuccess)
	if len(got) != 0 {
		t.Errorf("FilterByType(nil) = %d results, want 0", len(got))
	}
}

func TestGetActionableResponses(t *testing.T) {
	responses := []ClassifiedResponse{
		{Type: ResponseSuccess},
		{Type: ResponseFormRequired, FormURL: "https://example.com/form"},
		{Type: ResponseConfirmationRequired, ConfirmURL: "https://example.com/confirm"},
		{Type: ResponsePending},
		{Type: ResponseRejected},
	}
	got := GetActionableResponses(responses)
	if len(got) != 2 {
		t.Errorf("GetActionableResponses() = %d results, want 2", len(got))
	}
	for _, r := range got {
		if r.Type != ResponseFormRequired && r.Type != ResponseConfirmationRequired {
			t.Errorf("unexpected type %s in actionable responses", r.Type)
		}
	}
}

func TestGetActionableResponsesEmpty(t *testing.T) {
	got := GetActionableResponses(nil)
	if len(got) != 0 {
		t.Errorf("GetActionableResponses(nil) = %d, want 0", len(got))
	}
}

func TestSummarizeResponses(t *testing.T) {
	responses := []ClassifiedResponse{
		{Type: ResponseSuccess, Confidence: 0.9},
		{Type: ResponseFormRequired, Confidence: 0.8},
		{Type: ResponseConfirmationRequired, Confidence: 0.85},
		{Type: ResponseRejected, Confidence: 0.9},
		{Type: ResponsePending, Confidence: 0.7},
		{Type: ResponseBounced, Confidence: 0.95},
		{Type: ResponseUnknown, NeedsReview: true},
		{Type: ResponseSuccess, Confidence: 0.3, NeedsReview: true},
	}
	summary := SummarizeResponses(responses)
	if summary.Total != 8 {
		t.Errorf("Total = %d, want 8", summary.Total)
	}
	if summary.Success != 2 {
		t.Errorf("Success = %d, want 2", summary.Success)
	}
	if summary.FormRequired != 1 {
		t.Errorf("FormRequired = %d, want 1", summary.FormRequired)
	}
	if summary.ConfirmRequired != 1 {
		t.Errorf("ConfirmRequired = %d, want 1", summary.ConfirmRequired)
	}
	if summary.Rejected != 1 {
		t.Errorf("Rejected = %d, want 1", summary.Rejected)
	}
	if summary.Pending != 1 {
		t.Errorf("Pending = %d, want 1", summary.Pending)
	}
	if summary.Bounced != 1 {
		t.Errorf("Bounced = %d, want 1", summary.Bounced)
	}
	if summary.Unknown != 1 {
		t.Errorf("Unknown = %d, want 1", summary.Unknown)
	}
	if summary.NeedReview != 2 {
		t.Errorf("NeedReview = %d, want 2", summary.NeedReview)
	}
}

func TestSummarizeResponsesEmpty(t *testing.T) {
	summary := SummarizeResponses(nil)
	if summary.Total != 0 {
		t.Errorf("Total = %d, want 0", summary.Total)
	}
}

func TestGetBouncedResponses(t *testing.T) {
	responses := []ClassifiedResponse{
		{Type: ResponseSuccess},
		{Type: ResponseBounced, BouncedRecipient: "a@b.com"},
		{Type: ResponseBounced, BouncedRecipient: "c@d.com"},
		{Type: ResponsePending},
	}
	got := GetBouncedResponses(responses)
	if len(got) != 2 {
		t.Fatalf("GetBouncedResponses() = %d, want 2", len(got))
	}
	for _, r := range got {
		if r.Type != ResponseBounced {
			t.Errorf("unexpected type %s in bounced responses", r.Type)
		}
	}
}

func TestIsTestEmail(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		content string
		want    bool
	}{
		{"test email in subject", "Eraser Test Email", "", true},
		{"test email from eraser in body", "Re: Test", "test email from eraser", true},
		{"this is a test email in body", "", "This is a test email for verification", true},
		{"not test email", "Re: Removal Request", "We have received your request", false},
		{"empty both", "", "", false},
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

func TestIsBounceEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   *Email
		subject string
		content string
		want    bool
	}{
		{
			"bounce sender with pattern",
			&Email{From: "mailer-daemon@google.com", FromName: "Mail Delivery System"},
			"Delivery Status Notification (Failure)",
			"delivery to the following recipient failed",
			true,
		},
		{
			"bounce sender from name",
			&Email{From: "noreply@example.com", FromName: "Mail Delivery Subsystem"},
			"Undeliverable",
			"your message could not be delivered",
			true,
		},
		{
			"strong bounce patterns no sender",
			&Email{From: "some-broker@example.com", FromName: "Support"},
			"Delivery Status Notification (Failure)",
			"undeliverable mail delivery failed permanent error",
			true,
		},
		{
			"not bounce",
			&Email{From: "support@broker.com", FromName: "Support"},
			"Re: Data Removal Request",
			"we have received your request",
			false,
		},
		{
			"bounce sender no patterns",
			&Email{From: "mailer-daemon@example.com", FromName: "Mailer"},
			"Re: Something",
			"normal content here",
			false,
		},
		{
			"postmaster sender with bounce content",
			&Email{From: "postmaster@exchange.com", FromName: "Postmaster"},
			"Undeliverable: Test",
			"address rejected user unknown",
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBounceEmail(tt.email, tt.subject, tt.content)
			if got != tt.want {
				t.Errorf("isBounceEmail() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyResponseBounceEmail(t *testing.T) {
	email := &Email{
		From:     "mailer-daemon@google.com",
		FromName: "Mail Delivery System",
		Subject:  "Delivery Status Notification (Failure)",
		Body:     "Delivery to the following recipient failed permanently: user@broker.com",
	}
	result := ClassifyResponse(email)
	if result.Type != ResponseBounced {
		t.Errorf("Type = %s, want %s", result.Type, ResponseBounced)
	}
	if result.Confidence < 0.9 {
		t.Errorf("Confidence = %.2f, want >= 0.9", result.Confidence)
	}
	if result.NeedsReview {
		t.Errorf("NeedsReview = true, want false")
	}
	if result.BouncedRecipient == "" {
		t.Errorf("BouncedRecipient empty, want non-empty")
	}
}

func TestClassifyResponseSuccessEmail(t *testing.T) {
	tests := []struct {
		name     string
		subject  string
		body     string
		wantType ResponseType
	}{
		{"success removed", "Re: Request", "your data has been removed from our system", ResponseSuccess},
		{"success opted out complete", "Re: Request", "your opt-out request is confirmed", ResponseSuccess},
		{"success no longer hold", "Re: Privacy", "we no longer hold your information", ResponseSuccess},
		{"success deleted", "Re: Removal", "we have deleted your data successfully", ResponseSuccess},
		{"success subject opt-out completed", "Opt-out Has Been Completed", "done", ResponseSuccess},
		{"success subject removed", "Your data has been removed", "noted", ResponseSuccess},
		{"success subject request processed", "Request has been processed", "ok", ResponseSuccess},
		{"success subject ticket solved", "Ticket for removal solved", "done", ResponseSuccess},
		{"success body successfully removed", "Re: Update", "successfully removed your information", ResponseSuccess},
		{"success body no longer store", "Re: Privacy", "we no longer store your data", ResponseSuccess},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := &Email{Subject: tt.subject, Body: tt.body}
			result := ClassifyResponse(email)
			if result.Type != tt.wantType {
				t.Errorf("Type = %s, want %s (confidence: %.2f)", result.Type, tt.wantType, result.Confidence)
			}
		})
	}
}

func TestClassifyResponseUnknownEmail(t *testing.T) {
	email := &Email{
		Subject: "Hello",
		Body:    "This is just a random email with no classification signals.",
	}
	result := ClassifyResponse(email)
	if result.Type != ResponseUnknown {
		t.Errorf("Type = %s, want %s", result.Type, ResponseUnknown)
	}
	if !result.NeedsReview {
		t.Errorf("NeedsReview = false for unknown, want true")
	}
}

func TestClassifyResponseURLInference(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantType ResponseType
	}{
		{
			"confirm url only infers confirmation",
			"Check this out https://broker.com/confirm?token=abc123def456",
			ResponseConfirmationRequired,
		},
		{
			"form url only infers form",
			"See https://broker.com/opt-out for details",
			ResponseFormRequired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := &Email{Subject: "Hello", Body: tt.body}
			result := ClassifyResponse(email)
			if result.Type != tt.wantType {
				t.Errorf("Type = %s, want %s (confidence: %.2f)", result.Type, tt.wantType, result.Confidence)
			}
		})
	}
}

func TestClassifyResponseHTMLBodyFallback(t *testing.T) {
	email := &Email{
		Subject:  "Re: Request",
		HTMLBody: "<html><body>We have <b>removed</b> your data successfully</body></html>",
	}
	result := ClassifyResponse(email)
	if result.Type != ResponseSuccess {
		t.Errorf("Type = %s, want %s", result.Type, ResponseSuccess)
	}
}

func TestClassifyResponseWithBrokerDomain(t *testing.T) {
	email := &Email{
		Subject:    "Re: Request",
		Body:       "Please complete the form at https://broker.com/opt-out",
		FromDomain: "broker.com",
	}
	result := ClassifyResponse(email)
	if result.Type != ResponseFormRequired {
		t.Errorf("Type = %s, want %s", result.Type, ResponseFormRequired)
	}
	if result.FormURL == "" {
		t.Errorf("FormURL empty, expected a form URL")
	}
}

func TestClassifyResponseConfidenceCalculation(t *testing.T) {
	tests := []struct {
		name        string
		subject     string
		body        string
		wantMinConf float64
		wantReview  bool
	}{
		{
			"high confidence single type",
			"Re: Request",
			"we have removed your data",
			0.8,
			false,
		},
		{
			"low confidence unknown",
			"Hello",
			"random content",
			0.0,
			true,
		},
		{
			"competing types lower confidence",
			"Out of Office: Re: Removal Request",
			"we do not have any record of your data. Thank you for your inquiry.",
			0.0,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := &Email{Subject: tt.subject, Body: tt.body}
			result := ClassifyResponse(email)
			if result.Confidence < tt.wantMinConf {
				t.Errorf("Confidence = %.2f, want >= %.2f", result.Confidence, tt.wantMinConf)
			}
			if result.NeedsReview != tt.wantReview {
				t.Errorf("NeedsReview = %v, want %v", result.NeedsReview, tt.wantReview)
			}
		})
	}
}

func TestClassifyResponseFormWithFormURLs(t *testing.T) {
	email := &Email{
		Subject:    "Opt-out Instructions",
		Body:       "Please visit our opt-out page at https://broker.com/opt-out",
		FromDomain: "broker.com",
	}
	result := ClassifyResponse(email)
	if result.Type != ResponseFormRequired {
		t.Errorf("Type = %s, want %s", result.Type, ResponseFormRequired)
	}
	if result.Confidence < 0.85 {
		t.Errorf("Confidence = %.2f, want >= 0.85 (backed by form URLs)", result.Confidence)
	}
}

func TestClassifyResponseConfirmWithConfirmURLs(t *testing.T) {
	email := &Email{
		Subject:    "Confirm your request",
		Body:       "Click here to confirm your email address https://broker.com/confirm?token=abc123def456",
		FromDomain: "broker.com",
	}
	result := ClassifyResponse(email)
	if result.Type != ResponseConfirmationRequired {
		t.Errorf("Type = %s, want %s", result.Type, ResponseConfirmationRequired)
	}
	if result.Confidence < 0.85 {
		t.Errorf("Confidence = %.2f, want >= 0.85 (backed by confirm URLs)", result.Confidence)
	}
}

func TestClassifyBySubjectOnlyAllCategories(t *testing.T) {
	tests := []struct {
		subject  string
		wantType ResponseType
	}{
		{"Opt-out Has Been Completed", ResponseSuccess},
		{"Opt-out Instructions for your data", ResponseFormRequired},
		{"Your data has been removed", ResponseSuccess},
		{"No record found for your email", ResponseRejected},
		{"Thank you for reaching out", ResponsePending},
		{"Request denied", ResponseRejected},
		{"How to remove your data", ResponseFormRequired},
		{"auto-reply: your message", ResponsePending},
		{"office closed until Monday", ResponsePending},
		{"response to your email", ResponsePending},
		{"we have received your ticket", ResponsePending},
		{"Ticket (259135) Update", ResponsePending},
		{"support request #12345", ResponsePending},
		{"legal request received", ResponsePending},
		{"I have now left the company", ResponsePending},
		{"no longer with the organization", ResponsePending},
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

func TestIsBounceEmail550Code(t *testing.T) {
	email := &Email{From: "mailer-daemon@mail.com", FromName: "Mail System"}
	tests := []struct {
		name    string
		subject string
		content string
		want    bool
	}{
		{"550 rejected from bounce sender", "Delivery failure", "550 5.1.1 user rejected", true},
		{"strong bounce patterns no sender", "Delivery Status Notification (Failure)", "undeliverable delivery failed permanent error 550 rejected", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBounceEmail(email, tt.subject, tt.content)
			if got != tt.want {
				t.Errorf("isBounceEmail() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple tags", "<p>Hello <b>world</b></p>", "Hello world"},
		{"script removed", `<script>alert("xss")</script><p>Safe</p>`, "Safe"},
		{"style removed", `<style>body{color:red}</style><p>Text</p>`, "Text"},
		{"entities decoded", "&nbsp;test&amp;less&lt;than&gt;great&quot;quote", "test&less<than>great\"quote"},
		{"whitespace collapsed", "<p>a   b   c</p>", "a b c"},
		{"empty input", "", ""},
		{"complex html", `<div><h1>Title</h1><p>Content</p></div>`, "Title Content"},
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
