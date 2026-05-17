package template

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/robindubreuil/eraser/internal/broker"
	"github.com/robindubreuil/eraser/internal/config"
)

//go:embed templates/*.tmpl
var embeddedTemplates embed.FS

var frenchMonths = []string{
	"janvier", "février", "mars", "avril", "mai", "juin",
	"juillet", "août", "septembre", "octobre", "novembre", "décembre",
}

func formatFrenchDate(t time.Time) string {
	return fmt.Sprintf("%d %s %d", t.Day(), frenchMonths[t.Month()-1], t.Year())
}

// isEULocale returns true if the locale indicates an EU/EEA resident
func isEULocale(locale string) bool {
	l := strings.ToLower(strings.TrimSpace(locale))
	prefixes := []string{
		"fr", "de", "es", "it", "nl", "be", "pt", "pl", "sv",
		"da", "fi", "el", "cs", "ro", "bg", "hr", "et", "lv",
		"lt", "hu", "mt", "sk", "sl", "cy", "lu", "ie",
		"at", "en-ie",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

func isUKLocale(locale string) bool {
	l := strings.ToLower(strings.TrimSpace(locale))
	return strings.HasPrefix(l, "en-gb") || strings.HasPrefix(l, "uk")
}

func isUSLocale(locale string) bool {
	l := strings.ToLower(strings.TrimSpace(locale))
	return strings.HasPrefix(l, "en-us") || (strings.HasPrefix(l, "en") && !strings.Contains(l, "-"))
}

// EmailData contains all data available to email templates
type EmailData struct {
	FirstName   string
	LastName    string
	FullName    string
	Email       string
	Address     string
	City        string
	State       string
	ZipCode     string
	Country     string
	Phone       string
	DateOfBirth string

	BrokerName    string
	BrokerEmail   string
	BrokerWebsite string
	BrokerOptOut  string

	Date     string
	Year     int
	Month    string
	Template string
	Locale   string
}

type Email struct {
	Subject string
	Body    string
}

type Engine struct {
	templates map[string]*template.Template
}

func NewEngine() (*Engine, error) {
	e := &Engine{
		templates: make(map[string]*template.Template),
	}

	templateNames := []string{"gdpr", "gdpr-fr", "ccpa", "generic"}
	for _, name := range templateNames {
		content, err := embeddedTemplates.ReadFile("templates/" + name + ".tmpl")
		if err != nil {
			return nil, fmt.Errorf("failed to read embedded template %s: %w", name, err)
		}

		tmpl, err := template.New(name).Parse(string(content))
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s: %w", name, err)
		}

		e.templates[name] = tmpl
	}

	return e, nil
}

func (e *Engine) Render(templateName string, profile config.Profile, b broker.Broker) (*Email, error) {
	tmpl, ok := e.templates[templateName]
	if !ok {
		return nil, fmt.Errorf("unknown template: %s", templateName)
	}

	now := time.Now()
	dateStr := now.Format("January 2, 2006")
	if templateName == "gdpr-fr" {
		dateStr = formatFrenchDate(now)
	}

	data := EmailData{
		FirstName:     profile.FirstName,
		LastName:      profile.LastName,
		FullName:      profile.FullName(),
		Email:         profile.Email,
		Address:       profile.Address,
		City:          profile.City,
		State:         profile.State,
		ZipCode:       profile.ZipCode,
		Country:       profile.Country,
		Phone:         profile.Phone,
		DateOfBirth:   profile.DateOfBirth,
		BrokerName:    b.Name,
		BrokerEmail:   b.Email,
		BrokerWebsite: b.Website,
		BrokerOptOut:  b.OptOutURL,
		Date:          dateStr,
		Year:          now.Year(),
		Month:         now.Format("January"),
		Template:      templateName,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to render template: %w", err)
	}

	subject := e.getSubject(templateName)

	return &Email{
		Subject: subject,
		Body:    buf.String(),
	}, nil
}

func (e *Engine) getSubject(templateName string) string {
	switch templateName {
	case "gdpr":
		return "GDPR Data Erasure Request - Article 17 Right to Erasure"
	case "gdpr-fr":
		return "Demande d'effacement de données - Article 17 RGPD"
	case "ccpa":
		return "CCPA Data Deletion Request - Right to Delete Personal Information"
	default:
		return "Personal Data Removal Request"
	}
}

func (e *Engine) AvailableTemplates() []string {
	templates := make([]string, 0, len(e.templates))
	for name := range e.templates {
		templates = append(templates, name)
	}
	return templates
}

// TemplateForLocale selects the best email template based on the user's
// jurisdiction (locale). GDPR applies to ALL brokers when the user is an
// EU resident, regardless of where the broker is located. Similarly, CCPA
// only applies to California residents.
//
// Selection logic:
//   - EU/EEA resident + French locale → gdpr-fr (for ALL brokers)
//   - EU/EEA/UK resident → gdpr (for ALL brokers)
//   - US resident → ccpa (for ALL brokers)
//   - Other/unknown → generic
//
// If the user has explicitly set a template in config (not "auto"), that
// takes precedence.
func TemplateForLocale(userLocale, configTemplate string) string {
	if configTemplate != "" && configTemplate != "auto" {
		return configTemplate
	}

	l := strings.ToLower(strings.TrimSpace(userLocale))

	if isEULocale(l) {
		if strings.HasPrefix(l, "fr") {
			return "gdpr-fr"
		}
		return "gdpr"
	}
	if isUKLocale(l) {
		return "gdpr"
	}
	if isUSLocale(l) {
		return "ccpa"
	}
	return "generic"
}
