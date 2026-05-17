package template

import (
	"strings"
	"testing"

	"github.com/robindubreuil/eraser/internal/broker"
	"github.com/robindubreuil/eraser/internal/config"
)

func testProfile() config.Profile {
	return config.Profile{
		FirstName:   "Jane",
		LastName:    "Doe",
		Email:       "jane@example.com",
		Address:     "123 Main St",
		City:        "Springfield",
		State:       "IL",
		ZipCode:     "62701",
		Country:     "US",
		Phone:       "555-1234",
		DateOfBirth: "1990-01-15",
	}
}

func testBroker() broker.Broker {
	return broker.Broker{
		ID:      "test-broker",
		Name:    "Test Broker Inc",
		Email:   "privacy@testbroker.example",
		Website: "https://testbroker.example",
		Region:  "us",
	}
}

func TestNewEngine(t *testing.T) {
	t.Run("loads all templates", func(t *testing.T) {
		engine, err := NewEngine()
		if err != nil {
			t.Fatalf("NewEngine() error = %v", err)
		}
		avail := engine.AvailableTemplates()
		if len(avail) != 4 {
			t.Errorf("AvailableTemplates() returned %d, want 4", len(avail))
		}
		names := map[string]bool{}
		for _, n := range avail {
			names[n] = true
		}
		for _, expected := range []string{"gdpr", "gdpr-fr", "ccpa", "generic"} {
			if !names[expected] {
				t.Errorf("missing template %q", expected)
			}
		}
	})
}

func TestRender(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	tests := []struct {
		name         string
		templateName string
		wantSubject  string
		wantContains []string
		wantErr      bool
	}{
		{
			name:         "gdpr template",
			templateName: "gdpr",
			wantSubject:  "GDPR Data Erasure Request - Article 17 Right to Erasure",
			wantContains: []string{"GDPR", "Article 17", "Jane Doe", "jane@example.com", "Test Broker Inc"},
		},
		{
			name:         "ccpa template",
			templateName: "ccpa",
			wantSubject:  "CCPA Data Deletion Request - Right to Delete Personal Information",
			wantContains: []string{"CCPA", "California", "Jane Doe", "jane@example.com", "Test Broker Inc"},
		},
		{
			name:         "generic template",
			templateName: "generic",
			wantSubject:  "Personal Data Removal Request",
			wantContains: []string{"Jane Doe", "jane@example.com", "Test Broker Inc", "personal information"},
		},
		{
			name:         "gdpr-fr template",
			templateName: "gdpr-fr",
			wantSubject:  "Demande d'effacement de donnees - Article 17 RGPD",
			wantContains: []string{"RGPD", "article 17", "Jane Doe", "jane@example.com", "Test Broker Inc", "CNIL"},
		},
		{
			name:         "unknown template",
			templateName: "nonexistent",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email, err := engine.Render(tt.templateName, testProfile(), testBroker())
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if email.Subject != tt.wantSubject {
				t.Errorf("Subject = %q, want %q", email.Subject, tt.wantSubject)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(email.Body, want) {
					t.Errorf("Body missing %q", want)
				}
			}
		})
	}
}

func TestRender_ContainsProfileData(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	email, err := engine.Render("generic", testProfile(), testBroker())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	fields := []string{
		"Jane Doe",
		"jane@example.com",
		"123 Main St",
		"Springfield",
		"IL",
		"62701",
		"US",
		"555-1234",
		"1990-01-15",
	}

	for _, field := range fields {
		if !strings.Contains(email.Body, field) {
			t.Errorf("Body missing profile field %q", field)
		}
	}
}

func TestRender_MinimalProfile(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	minimal := config.Profile{
		FirstName: "Jane",
		LastName:  "Doe",
		Email:     "jane@example.com",
	}

	email, err := engine.Render("gdpr", minimal, testBroker())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if strings.Contains(email.Body, "- Address:") {
		t.Error("minimal profile should not include Address field")
	}
	if !strings.Contains(email.Body, "Jane Doe") {
		t.Error("Body missing full name")
	}
}

func TestGetSubject(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	tests := []struct {
		name         string
		templateName string
		want         string
	}{
		{"gdpr", "gdpr", "GDPR Data Erasure Request - Article 17 Right to Erasure"},
		{"gdpr-fr", "gdpr-fr", "Demande d'effacement de donnees - Article 17 RGPD"},
		{"ccpa", "ccpa", "CCPA Data Deletion Request - Right to Delete Personal Information"},
		{"generic", "generic", "Personal Data Removal Request"},
		{"unknown", "other", "Personal Data Removal Request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.getSubject(tt.templateName)
			if got != tt.want {
				t.Errorf("getSubject(%q) = %q, want %q", tt.templateName, got, tt.want)
			}
		})
	}
}

func TestTemplateForRegion(t *testing.T) {
	tests := []struct {
		name           string
		brokerRegion   string
		userLocale     string
		configTemplate string
		want           string
	}{
		{"EU + FR locale", "eu", "fr", "auto", "gdpr-fr"},
		{"EU + fr-FR locale", "eu", "fr-FR", "auto", "gdpr-fr"},
		{"EU + EN locale", "eu", "en", "auto", "gdpr"},
		{"EU + no locale", "eu", "", "auto", "gdpr"},
		{"UK region", "uk", "", "auto", "gdpr"},
		{"UK + FR locale", "uk", "fr", "auto", "gdpr"},
		{"US region", "us", "", "auto", "ccpa"},
		{"US + FR locale", "us", "fr", "auto", "ccpa"},
		{"global region", "global", "", "auto", "generic"},
		{"unknown region", "xx", "", "auto", "generic"},
		{"empty region", "", "", "auto", "generic"},
		{"explicit gdpr override", "us", "en", "gdpr", "gdpr"},
		{"explicit ccpa override", "eu", "fr", "ccpa", "ccpa"},
		{"auto treated as auto", "eu", "fr", "auto", "gdpr-fr"},
		{"empty config treated as auto", "eu", "de", "", "gdpr"},
		{"generic config uses generic", "eu", "fr", "generic", "generic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TemplateForRegion(tt.brokerRegion, tt.userLocale, tt.configTemplate)
			if got != tt.want {
				t.Errorf("TemplateForRegion(%q, %q, %q) = %q, want %q",
					tt.brokerRegion, tt.userLocale, tt.configTemplate, got, tt.want)
			}
		})
	}
}

func TestGDPRTemplateContainsArticle21(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	email, err := engine.Render("gdpr", testProfile(), testBroker())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	for _, want := range []string{"Article 17", "Article 21", "Article 19", "Article 77"} {
		if !strings.Contains(email.Body, want) {
			t.Errorf("GDPR template missing %q", want)
		}
	}
}

func TestGDPRFRTemplateContainsFrenchLegalRefs(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	email, err := engine.Render("gdpr-fr", testProfile(), testBroker())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	for _, want := range []string{"article 17", "article 21", "CNIL", "Informatique et Libertes", "ePrivacy"} {
		if !strings.Contains(email.Body, want) {
			t.Errorf("GDPR-FR template missing %q", want)
		}
	}
}

func TestGenericTemplateGlobalLaws(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	email, err := engine.Render("generic", testProfile(), testBroker())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	for _, want := range []string{"GDPR", "LGPD", "PIPEDA", "UK GDPR"} {
		if !strings.Contains(email.Body, want) {
			t.Errorf("Generic template missing global law reference %q", want)
		}
	}
}
