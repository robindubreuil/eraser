package inbox

import (
	"context"
	"strings"
	"testing"

	"github.com/robindubreuil/eraser/internal/broker"
	"github.com/robindubreuil/eraser/internal/config"
)

func TestNewMonitor(t *testing.T) {
	cfg := config.InboxConfig{
		Server:   "imap.example.com",
		Port:     993,
		Email:    "test@example.com",
		Password: "secret",
		Folder:   "INBOX",
	}
	brokers := []broker.Broker{
		{ID: "b1", Name: "Broker One", Email: "privacy@broker1.com", Website: "https://www.broker1.com"},
		{ID: "b2", Name: "Broker Two", Email: "optout@broker2.com"},
		{ID: "b3", Name: "Broker Three", Website: "https://broker3.net/opt-out"},
		{ID: "b4", Name: "No Contact"},
	}

	m := NewMonitor(cfg, brokers)
	if m == nil {
		t.Fatal("NewMonitor returned nil")
	}
	if m.config.Email != "test@example.com" {
		t.Errorf("config.Email = %q, want %q", m.config.Email, "test@example.com")
	}
	if len(m.brokers) == 0 {
		t.Error("brokers map empty, expected entries")
	}

	if _, ok := m.brokers["broker1.com"]; !ok {
		t.Error("missing broker1.com in brokers map")
	}
	if _, ok := m.brokers["broker2.com"]; !ok {
		t.Error("missing broker2.com in brokers map")
	}
	if _, ok := m.brokers["broker3.net"]; !ok {
		t.Error("missing broker3.net in brokers map")
	}
}

func TestNewMonitorEmptyBrokers(t *testing.T) {
	cfg := config.InboxConfig{Server: "imap.test.com", Port: 993}
	m := NewMonitor(cfg, nil)
	if m == nil {
		t.Fatal("NewMonitor returned nil")
	}
	if len(m.brokers) != 0 {
		t.Errorf("brokers map = %d entries, want 0", len(m.brokers))
	}
}

func TestNewMonitorBrokerDomainOverlap(t *testing.T) {
	cfg := config.InboxConfig{}
	brokers := []broker.Broker{
		{ID: "b1", Name: "Broker A", Email: "privacy@broker.com"},
		{ID: "b2", Name: "Broker B", Website: "https://www.broker.com"},
	}
	m := NewMonitor(cfg, brokers)
	if m == nil {
		t.Fatal("NewMonitor returned nil")
	}
	b, ok := m.brokers["broker.com"]
	if !ok {
		t.Fatal("missing broker.com in brokers map")
	}
	if b.ID != "b2" {
		t.Errorf("broker.com mapped to %s, want b2 (website overwrites email)", b.ID)
	}
}

func TestNewMonitorBrokerWithEmptyWebsite(t *testing.T) {
	cfg := config.InboxConfig{}
	brokers := []broker.Broker{
		{ID: "b1", Name: "Empty Website", Website: ""},
	}
	m := NewMonitor(cfg, brokers)
	if len(m.brokers) != 0 {
		t.Errorf("brokers map = %d entries, want 0", len(m.brokers))
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"https url", "https://www.example.com/path", "example.com"},
		{"http url", "http://broker.io/page", "broker.io"},
		{"with www", "https://www.broker.net/opt-out", "broker.net"},
		{"no scheme no www", "broker.org/remove", "broker.org"},
		{"with path and query", "https://broker.com/form?id=1&x=2", "broker.com"},
		{"empty string", "", ""},
		{"just domain", "example.com", "example.com"},
		{"http without www", "http://mysite.co.uk/deep/path", "mysite.co.uk"},
		{"http with www", "http://www.example.com/path", "example.com"},
		{"just www", "www.example.com", "example.com"},
		{"trailing slash", "https://broker.com/", "broker.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDomain(tt.url)
			if got != tt.want {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestDisconnectNilClient(t *testing.T) {
	m := &Monitor{client: nil}
	err := m.Disconnect()
	if err != nil {
		t.Errorf("Disconnect() on nil client = %v, want nil", err)
	}
}

func TestFetchRecentEmailsNilClient(t *testing.T) {
	m := &Monitor{client: nil}
	_, err := m.FetchRecentEmails(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("FetchRecentEmails() nil client err = %v, want not connected error", err)
	}
}

func TestFetchBrokerEmailsNilClient(t *testing.T) {
	m := &Monitor{client: nil}
	_, err := m.FetchBrokerEmails(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("FetchBrokerEmails() nil client err = %v, want not connected error", err)
	}
}

func TestFetchBounceEmailsNilClient(t *testing.T) {
	m := &Monitor{client: nil}
	_, err := m.FetchBounceEmails(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("FetchBounceEmails() nil client err = %v, want not connected error", err)
	}
}

func TestWatchForNewEmailsNilClient(t *testing.T) {
	m := &Monitor{client: nil}
	err := m.WatchForNewEmails(context.Background(), func(Email) {})
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("WatchForNewEmails() nil client err = %v, want not connected error", err)
	}
}

func TestEnsureFolderExistsNilClient(t *testing.T) {
	m := &Monitor{client: nil}
	err := m.EnsureFolderExists("Archive")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("EnsureFolderExists() nil client err = %v, want not connected error", err)
	}
}

func TestMoveToFolderNilClient(t *testing.T) {
	m := &Monitor{client: nil}
	err := m.MoveToFolder(1, "Archive")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("MoveToFolder() nil client err = %v, want not connected error", err)
	}
}

func TestArchiveEmailsNilClient(t *testing.T) {
	m := &Monitor{client: nil}
	err := m.ArchiveEmails([]uint32{1, 2}, "Archive")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("ArchiveEmails() nil client err = %v, want not connected error", err)
	}
}
