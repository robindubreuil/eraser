package email

import (
	"testing"

	"github.com/robindubreuil/eraser/internal/config"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{"valid simple", "user@example.com", false},
		{"valid with dots", "first.last@example.com", false},
		{"valid with plus", "user+tag@example.com", false},
		{"valid with subdomain", "user@mail.example.com", false},
		{"CRLF injection", "user@example.com\r\nBcc: evil@evil.com", true},
		{"LF injection", "user@example.com\nBcc: evil@evil.com", true},
		{"CR injection", "user@example.com\revil@evil.com", true},
		{"comma injection", "user@example.com,evil@evil.com", true},
		{"semicolon injection", "user@example.com;evil@evil.com", true},
		{"empty string", "", true},
		{"missing @", "userexample.com", true},
		{"missing domain", "user@", true},
		{"missing local", "@example.com", true},
		{"spaces", "user @example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmail(tt.email)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEmail(%q) error = %v, wantErr %v", tt.email, err, tt.wantErr)
			}
		})
	}
}

func TestNewSender(t *testing.T) {
	t.Run("smtp provider", func(t *testing.T) {
		cfg := config.EmailConfig{
			Provider: "smtp",
			From:     "test@example.com",
			SMTP: config.SMTPConfig{
				Host:     "smtp.example.com",
				Port:     587,
				Username: "test@example.com",
				Password: "secret",
				UseTLS:   true,
			},
		}
		sender, err := NewSender(cfg)
		if err != nil {
			t.Fatalf("NewSender() error = %v", err)
		}
		if sender.Name() != "smtp" {
			t.Errorf("Name() = %q, want %q", sender.Name(), "smtp")
		}
	})

	t.Run("empty provider defaults to smtp", func(t *testing.T) {
		cfg := config.EmailConfig{
			Provider: "",
			From:     "test@example.com",
			SMTP: config.SMTPConfig{
				Host: "smtp.example.com",
				Port: 587,
			},
		}
		sender, err := NewSender(cfg)
		if err != nil {
			t.Fatalf("NewSender() error = %v", err)
		}
		if sender.Name() != "smtp" {
			t.Errorf("Name() = %q, want %q", sender.Name(), "smtp")
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		cfg := config.EmailConfig{
			Provider: "carrier-pigeon",
			From:     "test@example.com",
		}
		_, err := NewSender(cfg)
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
	})
}

func TestSMTPSender_Name(t *testing.T) {
	s := NewSMTPSender(config.SMTPConfig{}, "test@example.com")
	if s.Name() != "smtp" {
		t.Errorf("Name() = %q, want %q", s.Name(), "smtp")
	}
}
