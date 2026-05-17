package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const validConfigYAML = `profile:
  first_name: Jane
  last_name: Doe
  email: jane@example.com
  address: "123 Main St"
  city: Springfield
  state: IL
  zip_code: "62701"
  country: US

email:
  provider: smtp
  from: jane@example.com
  smtp:
    host: smtp.example.com
    port: 587
    username: jane@example.com
    password: secret123
    use_tls: true

options:
  template: gdpr
  dry_run: false
  rate_limit_ms: 1000
  regions:
    - us
    - eu
`

func TestLoad(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte(validConfigYAML), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Profile.FirstName != "Jane" {
			t.Errorf("FirstName = %q, want %q", cfg.Profile.FirstName, "Jane")
		}
		if cfg.Email.Provider != "smtp" {
			t.Errorf("Provider = %q, want %q", cfg.Email.Provider, "smtp")
		}
		if cfg.Options.Template != "gdpr" {
			t.Errorf("Template = %q, want %q", cfg.Options.Template, "gdpr")
		}
		if cfg.Options.RateLimitMs != 1000 {
			t.Errorf("RateLimitMs = %d, want %d", cfg.Options.RateLimitMs, 1000)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := Load("/nonexistent/config.yaml")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("default template and rate limit", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		yaml := `profile:
  first_name: A
  last_name: B
  email: a@b.com
email:
  provider: smtp
  from: a@b.com
  smtp:
    host: smtp.example.com
    port: 587
`
		if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Options.Template != "auto" {
			t.Errorf("default Template = %q, want %q", cfg.Options.Template, "auto")
		}
		if cfg.Options.RateLimitMs != defaultRateLimitMs {
			t.Errorf("default RateLimitMs = %d, want %d", cfg.Options.RateLimitMs, defaultRateLimitMs)
		}
	})

	t.Run("locale field parsed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		yaml := `profile:
  first_name: Jean
  last_name: Dupont
  email: jean@example.com
email:
  provider: smtp
  from: jean@example.com
  smtp:
    host: smtp.example.com
    port: 587
options:
  template: auto
  locale: fr
  regions:
    - eu
`
		if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Options.Locale != "fr" {
			t.Errorf("Locale = %q, want %q", cfg.Options.Locale, "fr")
		}
		if cfg.Options.Template != "auto" {
			t.Errorf("Template = %q, want %q", cfg.Options.Template, "auto")
		}
	})

	t.Run("insecure permissions rejected on linux", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("permission check only on linux")
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte(validConfigYAML), 0644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err == nil {
			t.Fatal("Load() with insecure perms should fail, got nil error")
		}
		if cfg != nil {
			t.Error("expected nil config on insecure permissions")
		}
	})

	t.Run("inbox defaults for gmail", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		yaml := validConfigYAML + `
inbox:
  enabled: true
  provider: gmail
  email: jane@gmail.com
  password: apppass
`
		if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Inbox.Server != "imap.gmail.com" {
			t.Errorf("Server = %q, want %q", cfg.Inbox.Server, "imap.gmail.com")
		}
		if cfg.Inbox.Port != 993 {
			t.Errorf("Port = %d, want %d", cfg.Inbox.Port, 993)
		}
		if cfg.Inbox.Folder != "INBOX" {
			t.Errorf("Folder = %q, want %q", cfg.Inbox.Folder, "INBOX")
		}
		if cfg.Inbox.ArchiveFolder != "Eraser" {
			t.Errorf("ArchiveFolder = %q, want %q", cfg.Inbox.ArchiveFolder, "Eraser")
		}
	})

	t.Run("inbox defaults for outlook", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		yaml := validConfigYAML + `
inbox:
  enabled: true
  provider: outlook
  email: jane@outlook.com
  password: apppass
`
		if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Inbox.Server != "outlook.office365.com" {
			t.Errorf("Server = %q, want %q", cfg.Inbox.Server, "outlook.office365.com")
		}
		if cfg.Inbox.Port != 993 {
			t.Errorf("Port = %d, want %d", cfg.Inbox.Port, 993)
		}
	})

	t.Run("pipeline defaults", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte(validConfigYAML), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Pipeline.BrowserTimeoutSec != 30 {
			t.Errorf("BrowserTimeoutSec = %d, want %d", cfg.Pipeline.BrowserTimeoutSec, 30)
		}
		if !cfg.Pipeline.BrowserHeadless {
			t.Error("BrowserHeadless should default to true")
		}
	})
}

func TestValidate(t *testing.T) {
	validConfig := func() *Config {
		return &Config{
			Profile: Profile{
				FirstName: "Jane",
				LastName:  "Doe",
				Email:     "jane@example.com",
			},
			Email: EmailConfig{
				Provider: "smtp",
				From:     "jane@example.com",
				SMTP: SMTPConfig{
					Host: "smtp.example.com",
					Port: 587,
				},
			},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:   "all fields valid",
			mutate: func(_ *Config) {},
		},
		{
			name:    "missing first name",
			mutate:  func(c *Config) { c.Profile.FirstName = "" },
			wantErr: "first_name and last_name",
		},
		{
			name:    "missing last name",
			mutate:  func(c *Config) { c.Profile.LastName = "" },
			wantErr: "first_name and last_name",
		},
		{
			name:    "missing email",
			mutate:  func(c *Config) { c.Profile.Email = "" },
			wantErr: "email is required",
		},
		{
			name:    "missing provider",
			mutate:  func(c *Config) { c.Email.Provider = "" },
			wantErr: "provider is required",
		},
		{
			name:    "missing from",
			mutate:  func(c *Config) { c.Email.From = "" },
			wantErr: "from address is required",
		},
		{
			name:    "unknown provider",
			mutate:  func(c *Config) { c.Email.Provider = "carrier-pigeon" },
			wantErr: "unknown provider",
		},
		{
			name:    "smtp missing host",
			mutate:  func(c *Config) { c.Email.SMTP.Host = "" },
			wantErr: "host is required",
		},
		{
			name:    "smtp missing port",
			mutate:  func(c *Config) { c.Email.SMTP.Port = 0 },
			wantErr: "port is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q", tt.wantErr)
				} else if !contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestValidateInbox(t *testing.T) {
	tests := []struct {
		name    string
		inbox   InboxConfig
		wantErr string
	}{
		{
			name:    "not enabled",
			inbox:   InboxConfig{Enabled: false},
			wantErr: "not enabled",
		},
		{
			name:    "missing email",
			inbox:   InboxConfig{Enabled: true, Password: "x", Server: "imap.test.com", Port: 993},
			wantErr: "email address is required",
		},
		{
			name:    "missing password",
			inbox:   InboxConfig{Enabled: true, Email: "a@b.com", Server: "imap.test.com", Port: 993},
			wantErr: "password",
		},
		{
			name:    "missing server",
			inbox:   InboxConfig{Enabled: true, Email: "a@b.com", Password: "x", Port: 993},
			wantErr: "IMAP server",
		},
		{
			name:    "missing port",
			inbox:   InboxConfig{Enabled: true, Email: "a@b.com", Password: "x", Server: "imap.test.com"},
			wantErr: "IMAP port",
		},
		{
			name:  "valid inbox",
			inbox: InboxConfig{Enabled: true, Email: "a@b.com", Password: "x", Server: "imap.test.com", Port: 993},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Inbox: tt.inbox}
			err := cfg.ValidateInbox()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateInbox() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateInbox() expected error containing %q", tt.wantErr)
				} else if !contains(err.Error(), tt.wantErr) {
					t.Errorf("ValidateInbox() error = %q, want containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestDefaultConfigPath(t *testing.T) {
	p := DefaultConfigPath()
	if p == "" {
		t.Error("DefaultConfigPath() returned empty string")
	}
	if filepath.Base(p) != "config.yaml" {
		t.Errorf("DefaultConfigPath() = %q, want ending in config.yaml", p)
	}
}

func TestSave(t *testing.T) {
	t.Run("creates directory and file with 0600", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "subdir", "config.yaml")

		cfg := &Config{
			Profile: Profile{FirstName: "Jane", LastName: "Doe", Email: "jane@example.com"},
			Email:   EmailConfig{Provider: "smtp", From: "jane@example.com"},
		}
		if err := Save(path, cfg); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("config file not created")
		}

		if runtime.GOOS != "windows" {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); perm != 0600 {
				t.Errorf("file permissions = %04o, want 0600", perm)
			}
		}

		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("Load() after Save() error = %v", err)
		}
		if loaded.Profile.FirstName != "Jane" {
			t.Errorf("roundtrip FirstName = %q, want %q", loaded.Profile.FirstName, "Jane")
		}
	})
}

func TestProfile_FullName(t *testing.T) {
	p := Profile{FirstName: "Jane", LastName: "Doe"}
	if got := p.FullName(); got != "Jane Doe" {
		t.Errorf("FullName() = %q, want %q", got, "Jane Doe")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
