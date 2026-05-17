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
			wantSubject:  "Demande d'effacement de données - Article 17 RGPD",
			wantContains: []string{"RGPD", "article 17", "Jane Doe", "jane@example.com", "Test Broker Inc", "CNIL", "données"},
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
		{"gdpr-fr", "gdpr-fr", "Demande d'effacement de données - Article 17 RGPD"},
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

func TestTemplateForLocale(t *testing.T) {
	tests := []struct {
		name           string
		userLocale     string
		configTemplate string
		want           string
	}{
		{"FR locale gets gdpr-fr", "fr", "auto", "gdpr-fr"},
		{"fr-FR locale gets gdpr-fr", "fr-FR", "auto", "gdpr-fr"},
		{"fr-BE locale gets gdpr-fr", "fr-be", "auto", "gdpr-fr"},
		{"DE locale gets gdpr", "de", "auto", "gdpr"},
		{"ES locale gets gdpr", "es", "auto", "gdpr"},
		{"IT locale gets gdpr", "it", "auto", "gdpr"},
		{"NL locale gets gdpr", "nl", "auto", "gdpr"},
		{"PL locale gets gdpr", "pl", "auto", "gdpr"},
		{"SV locale gets gdpr", "sv", "auto", "gdpr"},
		{"IE locale gets gdpr", "en-ie", "auto", "gdpr"},
		{"UK locale gets gdpr", "en-gb", "auto", "gdpr"},
		{"US locale gets ccpa", "en-us", "auto", "ccpa"},
		{"generic EN gets ccpa", "en", "auto", "ccpa"},
		{"no locale gets generic", "", "auto", "generic"},
		{"unknown locale gets generic", "xx", "auto", "generic"},
		{"FR locale sends GDPR to ALL brokers (US included)", "fr", "auto", "gdpr-fr"},
		{"explicit gdpr override", "en-us", "gdpr", "gdpr"},
		{"explicit ccpa override", "fr", "ccpa", "ccpa"},
		{"empty config uses locale", "de", "", "gdpr"},
		{"generic config uses generic", "fr", "generic", "generic"},
		{"auto is locale-driven", "fr", "auto", "gdpr-fr"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TemplateForLocale(tt.userLocale, tt.configTemplate)
			if got != tt.want {
				t.Errorf("TemplateForLocale(%q, %q) = %q, want %q",
					tt.userLocale, tt.configTemplate, got, tt.want)
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

	for _, want := range []string{"article 17", "article 21", "CNIL", "Informatique et Libertés", "ePrivacy", "données", "à l'effacement", "Règlement"} {
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

func TestIsEULocale(t *testing.T) {
	tests := []struct {
		locale string
		want   bool
	}{
		{"fr", true},
		{"de", true},
		{"es", true},
		{"it", true},
		{"nl", true},
		{"pl", true},
		{"sv", true},
		{"en-ie", true},
		{"pt", true},
		{"el", true},
		{"en-us", false},
		{"en-gb", false},
		{"en", false},
		{"", false},
		{"xx", false},
	}

	for _, tt := range tests {
		t.Run(tt.locale, func(t *testing.T) {
			got := isEULocale(tt.locale)
			if got != tt.want {
				t.Errorf("isEULocale(%q) = %v, want %v", tt.locale, got, tt.want)
			}
		})
	}
}

func TestFrenchDateFormat(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	email, err := engine.Render("gdpr-fr", testProfile(), testBroker())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	frenchMonths := []string{"janvier", "février", "mars", "avril", "mai", "juin",
		"juillet", "août", "septembre", "octobre", "novembre", "décembre"}

	found := false
	for _, m := range frenchMonths {
		if strings.Contains(email.Body, m) {
			found = true
			break
		}
	}
	if !found {
		t.Error("GDPR-FR template does not contain a French month name in the date")
	}

	if strings.Contains(email.Body, "January") {
		t.Error("GDPR-FR template contains English month name")
	}
}

func TestEnglishDateFormat(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	email, err := engine.Render("gdpr", testProfile(), testBroker())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	englishMonths := []string{"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December"}

	found := false
	for _, m := range englishMonths {
		if strings.Contains(email.Body, m) {
			found = true
			break
		}
	}
	if !found {
		t.Error("GDPR template does not contain an English month name in the date")
	}
}
