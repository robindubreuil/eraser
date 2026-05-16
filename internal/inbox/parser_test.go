package inbox

import (
	"strings"
	"testing"
)

func TestExtractURLsFromText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantURLs []string
	}{
		{
			name:     "single http url",
			input:    "Visit http://example.com for more info",
			wantURLs: []string{"http://example.com"},
		},
		{
			name:     "single https url",
			input:    "Click https://broker.com/opt-out?email=test@test.com to unsubscribe",
			wantURLs: []string{"https://broker.com/opt-out?email=test@test.com"},
		},
		{
			name:  "multiple urls",
			input: "Links: https://a.com/remove and https://b.com/confirm?token=abc123 end",
			wantURLs: []string{
				"https://a.com/remove",
				"https://b.com/confirm?token=abc123",
			},
		},
		{
			name:     "url with fragment",
			input:    "See https://example.com/page#section",
			wantURLs: []string{"https://example.com/page#section"},
		},
		{
			name:     "no urls",
			input:    "This is just plain text with no links.",
			wantURLs: nil,
		},
		{
			name:     "url in angle brackets",
			input:    `Click <https://example.com/optout> now`,
			wantURLs: []string{"https://example.com/optout"},
		},
		{
			name:     "url with port",
			input:    "Go to https://localhost:8080/remove-me",
			wantURLs: []string{"https://localhost:8080/remove-me"},
		},
		{
			name:     "url with path and query",
			input:    "https://broker.io/form/removal-request?id=42&email=a@b.com",
			wantURLs: []string{"https://broker.io/form/removal-request?id=42&email=a@b.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractURLsFromText(tt.input)
			if len(got) != len(tt.wantURLs) {
				t.Errorf("extractURLsFromText() got %d URLs, want %d: %v", len(got), len(tt.wantURLs), got)
				return
			}
			for i, u := range got {
				if u != tt.wantURLs[i] {
					t.Errorf("extractURLsFromText()[%d] = %q, want %q", i, u, tt.wantURLs[i])
				}
			}
		})
	}
}

func TestExtractURLsFromHTML(t *testing.T) {
	tests := []struct {
		name         string
		html         string
		wantAtLeast  int
		wantContains []string
	}{
		{
			name:         "anchor href",
			html:         `<html><body><a href="https://broker.com/opt-out">Click here</a></body></html>`,
			wantAtLeast:  1,
			wantContains: []string{"https://broker.com/opt-out"},
		},
		{
			name: "multiple anchors",
			html: `<html><body>
				<a href="https://a.com/remove">Remove</a>
				<a href="https://b.com/unsubscribe">Unsub</a>
			</body></html>`,
			wantAtLeast:  2,
			wantContains: []string{"https://a.com/remove", "https://b.com/unsubscribe"},
		},
		{
			name:         "plain url in text",
			html:         `<p>Visit https://example.com/confirm?token=xyz</p>`,
			wantAtLeast:  1,
			wantContains: []string{"https://example.com/confirm?token=xyz"},
		},
		{
			name:         "no links",
			html:         `<html><body><p>No links here</p></body></html>`,
			wantAtLeast:  0,
			wantContains: nil,
		},
		{
			name:         "invalid html falls back to regex",
			html:         `broken <<>> html https://example.com/optout more text`,
			wantAtLeast:  1,
			wantContains: []string{"https://example.com/optout"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractURLsFromHTML(tt.html)
			if len(got) < tt.wantAtLeast {
				t.Errorf("extractURLsFromHTML() got %d URLs, want at least %d: %v", len(got), tt.wantAtLeast, got)
			}
			for _, want := range tt.wantContains {
				found := false
				for _, u := range got {
					if strings.Contains(u, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("extractURLsFromHTML() missing expected URL containing %q, got: %v", want, got)
				}
			}
		})
	}
}

func TestCleanURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "valid https",
			input: "https://example.com/path",
			want:  "https://example.com/path",
		},
		{
			name:  "trailing period stripped",
			input: "https://example.com/path.",
			want:  "https://example.com/path",
		},
		{
			name:  "trailing paren stripped",
			input: "https://example.com/path)",
			want:  "https://example.com/path",
		},
		{
			name:  "multiple trailing punctuation",
			input: "https://example.com/path.,;)",
			want:  "https://example.com/path",
		},
		{
			name:  "ftp scheme rejected",
			input: "ftp://example.com/file",
			want:  "",
		},
		{
			name:  "no host rejected",
			input: "https://",
			want:  "",
		},
		{
			name:  "javascript scheme rejected",
			input: "javascript:alert(1)",
			want:  "",
		},
		{
			name:  "http valid",
			input: "http://example.com",
			want:  "http://example.com",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "url with query and fragment",
			input: "https://broker.com/remove?email=a@b.com#confirm",
			want:  "https://broker.com/remove?email=a@b.com#confirm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanURL(tt.input)
			if got != tt.want {
				t.Errorf("cleanURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsTrackingURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "tracking pixel gif",
			input: "https://mailer.com/track/open.gif",
			want:  true,
		},
		{
			name:  "pixel png",
			input: "https://mailer.com/pixel/1x1.png",
			want:  true,
		},
		{
			name:  "beacon url",
			input: "https://analytics.com/beacon/log",
			want:  true,
		},
		{
			name:  "spacer gif",
			input: "https://img.com/spacer.gif",
			want:  true,
		},
		{
			name:  "unsubscribe tracking",
			input: "https://mailer.com/unsubscribe-tracking/abc",
			want:  true,
		},
		{
			name:  "1x1 pixel",
			input: "https://tracker.com/1x1/img",
			want:  true,
		},
		{
			name:  "normal url not tracking",
			input: "https://broker.com/remove-me",
			want:  false,
		},
		{
			name:  "unsubscribe url not tracking",
			input: "https://mailer.com/unsubscribe?email=test@test.com",
			want:  false,
		},
		{
			name:  "open gif",
			input: "https://mailer.com/open.gif",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTrackingURL(tt.input)
			if got != tt.want {
				t.Errorf("isTrackingURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestScoreFormURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantAbove int
		wantBelow int
	}{
		{
			name:      "strong pattern opt-out",
			url:       "https://broker.com/opt-out?email=test@test.com",
			wantAbove: 0,
		},
		{
			name:      "ccpa request form",
			url:       "https://broker.com/ccpa-request",
			wantAbove: 0,
		},
		{
			name:      "dsar form",
			url:       "https://broker.com/dsar",
			wantAbove: 0,
		},
		{
			name:      "removal form strong",
			url:       "https://broker.com/removal-form",
			wantAbove: 0,
		},
		{
			name:      "moderate suppress",
			url:       "https://broker.com/suppress",
			wantAbove: 0,
		},
		{
			name:      "privacy policy excluded",
			url:       "https://broker.com/privacy-policy",
			wantBelow: 0,
		},
		{
			name:      "terms of service excluded",
			url:       "https://broker.com/terms-of-service",
			wantBelow: 0,
		},
		{
			name:      "login excluded",
			url:       "https://broker.com/login",
			wantBelow: 0,
		},
		{
			name:      "facebook excluded",
			url:       "https://facebook.com/remove",
			wantBelow: 0,
		},
		{
			name:      "pdf excluded",
			url:       "https://broker.com/remove.pdf",
			wantBelow: 0,
		},
		{
			name:      "about page excluded",
			url:       "https://broker.com/about",
			wantBelow: 0,
		},
		{
			name:      "manage preferences excluded",
			url:       "https://broker.com/manage-preferences",
			wantBelow: 0,
		},
		{
			name:      "neutral url zero score",
			url:       "https://broker.com/page",
			wantAbove: -1,
			wantBelow: 1,
		},
		{
			name:      "remove-me path strong",
			url:       "https://broker.com/remove-me",
			wantAbove: 0,
		},
		{
			name:      "gdpr moderate",
			url:       "https://broker.com/gdpr/form",
			wantAbove: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scoreFormURL(strings.ToLower(tt.url))
			if tt.wantAbove > 0 && got <= tt.wantAbove {
				t.Errorf("scoreFormURL(%q) = %d, want > %d", tt.url, got, tt.wantAbove)
			}
			if tt.wantBelow < 0 && got >= tt.wantBelow {
				t.Errorf("scoreFormURL(%q) = %d, want < %d", tt.url, got, tt.wantBelow)
			}
			if tt.wantAbove == 0 && tt.wantBelow == 0 {
				if tt.name != "" && strings.Contains(tt.name, "excluded") {
					if got >= 0 {
						t.Errorf("scoreFormURL(%q) = %d, want negative", tt.url, got)
					}
				}
			}
		})
	}
}

func TestIsFormURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "opt-out url is form",
			url:  "https://broker.com/opt-out",
			want: true,
		},
		{
			name: "remove-my-info is form",
			url:  "https://broker.com/remove-my-info",
			want: true,
		},
		{
			name: "privacy-policy is not form",
			url:  "https://broker.com/privacy-policy",
			want: false,
		},
		{
			name: "login is not form",
			url:  "https://broker.com/login",
			want: false,
		},
		{
			name: "generic page is not form",
			url:  "https://broker.com/home",
			want: false,
		},
		{
			name: "ccpa-request is form",
			url:  "https://broker.com/ccpa-request",
			want: true,
		},
		{
			name: "do-not-sell is form",
			url:  "https://broker.com/do-not-sell",
			want: true,
		},
		{
			name: "google.com excluded",
			url:  "https://google.com/remove",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isFormURL(strings.ToLower(tt.url))
			if got != tt.want {
				t.Errorf("isFormURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsConfirmationURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "confirm in url",
			url:  "https://broker.com/confirm?token=abc",
			want: true,
		},
		{
			name: "verify in url",
			url:  "https://broker.com/verify?code=123",
			want: true,
		},
		{
			name: "token param",
			url:  "https://broker.com/page?token=xyz",
			want: true,
		},
		{
			name: "code param",
			url:  "https://broker.com/page?code=abc",
			want: true,
		},
		{
			name: "activate in url",
			url:  "https://broker.com/activate",
			want: true,
		},
		{
			name: "no confirmation pattern",
			url:  "https://broker.com/remove-me",
			want: false,
		},
		{
			name: "approve in url",
			url:  "https://broker.com/approve?id=1",
			want: true,
		},
		{
			name: "click-here in url",
			url:  "https://broker.com/click-here",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConfirmationURL(strings.ToLower(tt.url))
			if got != tt.want {
				t.Errorf("isConfirmationURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestParseEmailURLs(t *testing.T) {
	tests := []struct {
		name                string
		email               *Email
		wantFormCount       int
		wantConfirmCount    int
		wantUnsubCount      int
		wantAllMin          int
		wantFormContains    string
		wantConfirmContains string
	}{
		{
			name: "plain text with form and confirm urls",
			email: &Email{
				Body: "Click https://broker.com/opt-out?email=test@test.com to remove. Confirm at https://broker.com/confirm?token=abc",
			},
			wantFormCount:       1,
			wantConfirmCount:    1,
			wantAllMin:          2,
			wantFormContains:    "opt-out",
			wantConfirmContains: "confirm",
		},
		{
			name: "html body with links",
			email: &Email{
				HTMLBody: `<html><body>
					<a href="https://broker.com/removal-form">Remove me</a>
					<a href="https://broker.com/privacy-policy">Privacy</a>
					<a href="https://broker.com/unsubscribe">Unsub</a>
				</body></html>`,
			},
			wantFormCount:  1,
			wantUnsubCount: 1,
			wantAllMin:     3,
		},
		{
			name: "tracking pixels filtered",
			email: &Email{
				Body: "https://broker.com/opt-out https://tracker.com/track/open.gif https://broker.com/confirm?token=x",
			},
			wantFormCount:    1,
			wantConfirmCount: 1,
			wantAllMin:       2,
		},
		{
			name: "deduplication",
			email: &Email{
				Body:     "https://broker.com/opt-out",
				HTMLBody: `<a href="https://broker.com/opt-out">Remove</a>`,
			},
			wantFormCount: 1,
			wantAllMin:    1,
		},
		{
			name: "empty email",
			email: &Email{
				Body: "",
			},
			wantAllMin: 0,
		},
		{
			name: "mixed body and html",
			email: &Email{
				Body:     "Plain: https://a.com/remove",
				HTMLBody: `<a href="https://b.com/opt-out">Opt out</a>`,
			},
			wantFormCount: 2,
			wantAllMin:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseEmailURLs(tt.email)
			if len(got.FormURLs) != tt.wantFormCount {
				t.Errorf("FormURLs count = %d, want %d: %v", len(got.FormURLs), tt.wantFormCount, got.FormURLs)
			}
			if len(got.ConfirmationURLs) != tt.wantConfirmCount {
				t.Errorf("ConfirmationURLs count = %d, want %d: %v", len(got.ConfirmationURLs), tt.wantConfirmCount, got.ConfirmationURLs)
			}
			if len(got.UnsubscribeURLs) != tt.wantUnsubCount {
				t.Errorf("UnsubscribeURLs count = %d, want %d: %v", len(got.UnsubscribeURLs), tt.wantUnsubCount, got.UnsubscribeURLs)
			}
			if len(got.AllURLs) < tt.wantAllMin {
				t.Errorf("AllURLs count = %d, want at least %d: %v", len(got.AllURLs), tt.wantAllMin, got.AllURLs)
			}
			if tt.wantFormContains != "" {
				found := false
				for _, u := range got.FormURLs {
					if strings.Contains(u, tt.wantFormContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("FormURLs missing URL containing %q, got: %v", tt.wantFormContains, got.FormURLs)
				}
			}
			if tt.wantConfirmContains != "" {
				found := false
				for _, u := range got.ConfirmationURLs {
					if strings.Contains(u, tt.wantConfirmContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("ConfirmationURLs missing URL containing %q, got: %v", tt.wantConfirmContains, got.ConfirmationURLs)
				}
			}
		})
	}
}

func TestExtractBouncedRecipient(t *testing.T) {
	tests := []struct {
		name  string
		email *Email
		want  string
	}{
		{
			name: "gmail ndr permanent failure",
			email: &Email{
				From:    "mailer-daemon@google.com",
				Subject: "Delivery Status Notification (Failure)",
				Body: `Delivery to the following recipient failed permanently:

    john.doe@example.com

Technical details of permanent failure:`,
			},
			want: "john.doe@example.com",
		},
		{
			name: "exchange ndr undeliverable",
			email: &Email{
				From:    "postmaster@exchange.example.com",
				Subject: "Undeliverable: Test message",
				Body:    `Undeliverable to: jane.smith@company.org The email account does not exist.`,
			},
			want: "jane.smith@company.org",
		},
		{
			name: "original recipient rfc822",
			email: &Email{
				From:    "MAILER-DAEMON@mx.example.com",
				Subject: "Returned mail: see transcript for details",
				Body: `Original-recipient: rfc822;user@domain.net
Action: failed
Status: 5.1.1 User unknown`,
			},
			want: "user@domain.net",
		},
		{
			name: "failed recipient pattern",
			email: &Email{
				From:    "postmaster@mail.example.com",
				Subject: "Mail delivery failed",
				Body:    `Failed recipient: bounced@target.com Reason: mailbox unavailable`,
			},
			want: "bounced@target.com",
		},
		{
			name: "message could not be delivered",
			email: &Email{
				From:    "mailer-daemon@relay.com",
				Subject: "Failure Notice",
				Body:    `Message could not be delivered to noone@nowhere.org because the recipient doesn't exist.`,
			},
			want: "noone@nowhere.org",
		},
		{
			name: "address in angle brackets with failed",
			email: &Email{
				From:    "MAILER-DAEMON@mta.example.com",
				Subject: "Returned mail",
				Body:    `<missing@user.com>: host said: 550 5.1.1 User unknown (failed)`,
			},
			want: "missing@user.com",
		},
		{
			name: "html body bounce",
			email: &Email{
				From:     "mailer-daemon@example.com",
				Subject:  "Delivery failure",
				HTMLBody: `<html><body><p>Delivery to the following recipient failed permanently:</p><p><b>html.user@test.com</b></p></body></html>`,
			},
			want: "html.user@test.com",
		},
		{
			name: "subject contains address",
			email: &Email{
				From:    "postmaster@example.com",
				Subject: "Returned mail: user@domain.com",
				Body:    `The following address had permanent fatal errors.`,
			},
			want: "user@domain.com",
		},
		{
			name: "fallback excludes system addresses",
			email: &Email{
				From:    "mailer-daemon@example.com",
				Subject: "Bounce",
				Body:    `mailer-daemon@example.com postmaster@example.com real.user@target.com`,
			},
			want: "real.user@target.com",
		},
		{
			name: "no valid recipient returns empty",
			email: &Email{
				From:    "noreply@example.com",
				Subject: "Auto-reply",
				Body:    "I am out of the office.",
			},
			want: "",
		},
		{
			name: "fallback excludes common free providers",
			email: &Email{
				From:    "postmaster@corp.com",
				Subject: "Delivery failure",
				Body:    "Delivery failed for someone@gmail.com and noreply@corp.com but real@customer.biz was the target.",
			},
			want: "real@customer.biz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractBouncedRecipient(tt.email)
			if got != tt.want {
				t.Errorf("ExtractBouncedRecipient() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripHTMLSimple(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple tag removal",
			input: "<p>Hello <b>world</b></p>",
			want:  " Hello  world  ",
		},
		{
			name:  "no tags",
			input: "plain text",
			want:  "plain text",
		},
		{
			name:  "self-closing tags",
			input: "line1<br/>line2",
			want:  "line1 line2",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "nested tags",
			input: `<div><a href="https://example.com">link</a></div>`,
			want:  "  link  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTMLSimple(tt.input)
			if got != tt.want {
				t.Errorf("stripHTMLSimple(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractConfirmationToken(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "token query param",
			url:  "https://broker.com/verify?token=abc123def456",
			want: "abc123def456",
		},
		{
			name: "code query param",
			url:  "https://broker.com/confirm?code=xyz789",
			want: "xyz789",
		},
		{
			name: "verify param",
			url:  "https://broker.com/verify?verify=verifytoken123",
			want: "verifytoken123",
		},
		{
			name: "confirmation param",
			url:  "https://broker.com/confirm?confirmation=confvalue",
			want: "confvalue",
		},
		{
			name: "key param",
			url:  "https://broker.com/verify?key=keyvalue",
			want: "keyvalue",
		},
		{
			name: "id param",
			url:  "https://broker.com/confirm?id=myid123",
			want: "myid123",
		},
		{
			name: "token in path after confirm",
			url:  "https://broker.com/confirm/abcdefghij12345",
			want: "abcdefghij12345",
		},
		{
			name: "token in path after verify",
			url:  "https://broker.com/verify/verylongtoken12345",
			want: "verylongtoken12345",
		},
		{
			name: "no token returns empty",
			url:  "https://broker.com/page?other=value",
			want: "",
		},
		{
			name: "invalid url returns empty",
			url:  "://invalid",
			want: "",
		},
		{
			name: "path token too short ignored",
			url:  "https://broker.com/confirm/short",
			want: "",
		},
		{
			name: "multiple params takes first match",
			url:  "https://broker.com/verify?token=first&code=second",
			want: "first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractConfirmationToken(tt.url)
			if got != tt.want {
				t.Errorf("ExtractConfirmationToken(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestGetPrimaryFormURL(t *testing.T) {
	tests := []struct {
		name         string
		urls         ExtractedURLs
		brokerDomain string
		want         string
	}{
		{
			name:         "empty urls",
			urls:         ExtractedURLs{},
			brokerDomain: "broker.com",
			want:         "",
		},
		{
			name: "single form url",
			urls: ExtractedURLs{
				FormURLs: []string{"https://broker.com/opt-out"},
			},
			brokerDomain: "broker.com",
			want:         "https://broker.com/opt-out",
		},
		{
			name: "prefers broker domain match",
			urls: ExtractedURLs{
				FormURLs: []string{
					"https://other.com/opt-out",
					"https://broker.com/opt-out",
				},
			},
			brokerDomain: "broker.com",
			want:         "https://broker.com/opt-out",
		},
		{
			name: "no form urls returns empty",
			urls: ExtractedURLs{
				FormURLs: []string{},
			},
			brokerDomain: "broker.com",
			want:         "",
		},
		{
			name: "higher scoring pattern wins",
			urls: ExtractedURLs{
				FormURLs: []string{
					"https://broker.com/remove",
					"https://broker.com/opt-out",
				},
			},
			brokerDomain: "broker.com",
			want:         "https://broker.com/opt-out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetPrimaryFormURL(tt.urls, tt.brokerDomain)
			if got != tt.want {
				t.Errorf("GetPrimaryFormURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetPrimaryConfirmationURL(t *testing.T) {
	tests := []struct {
		name         string
		urls         ExtractedURLs
		brokerDomain string
		want         string
	}{
		{
			name:         "empty urls",
			urls:         ExtractedURLs{},
			brokerDomain: "broker.com",
			want:         "",
		},
		{
			name: "prefers broker domain match",
			urls: ExtractedURLs{
				ConfirmationURLs: []string{
					"https://other.com/confirm?token=abc",
					"https://broker.com/confirm?token=xyz",
				},
			},
			brokerDomain: "broker.com",
			want:         "https://broker.com/confirm?token=xyz",
		},
		{
			name: "falls back to first confirm url",
			urls: ExtractedURLs{
				ConfirmationURLs: []string{
					"https://other.com/verify?code=123",
				},
			},
			brokerDomain: "broker.com",
			want:         "https://other.com/verify?code=123",
		},
		{
			name: "no confirmation urls returns empty",
			urls: ExtractedURLs{
				ConfirmationURLs: []string{},
			},
			brokerDomain: "broker.com",
			want:         "",
		},
		{
			name: "first domain match wins",
			urls: ExtractedURLs{
				ConfirmationURLs: []string{
					"https://other.com/confirm",
					"https://exact.broker.com/verify",
					"https://broker.com/confirm",
				},
			},
			brokerDomain: "broker.com",
			want:         "https://exact.broker.com/verify",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetPrimaryConfirmationURL(tt.urls, tt.brokerDomain)
			if got != tt.want {
				t.Errorf("GetPrimaryConfirmationURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
