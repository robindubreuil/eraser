package broker

import (
	"os"
	"path/filepath"
	"testing"
)

const validYAML = `brokers:
  - id: acme-corp
    name: Acme Corp
    email: privacy@acme.example
    website: https://acme.example
    region: us
    category: people-search
    tags:
      - test
  - id: eu-broker
    name: EU Broker
    email: dpo@eubroker.example
    website: https://eubroker.example
    opt_out_url: https://eubroker.example/optout
    region: eu
  - id: global-biz
    name: Global Biz
    email: privacy@globalbiz.example
    website: https://globalbiz.example
    region: global
  - id: bad-urls
    name: Bad URLs
    email: privacy@badurls.example
    website: ftp://bad.example
    opt_out_url: javascript:alert(1)
    region: us
`

func makeDB(t *testing.T) *BrokerDatabase {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "brokers.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0644); err != nil {
		t.Fatal(err)
	}
	db, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLoadFromFile(t *testing.T) {
	t.Run("valid YAML", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "brokers.yaml")
		if err := os.WriteFile(path, []byte(validYAML), 0644); err != nil {
			t.Fatal(err)
		}
		db, err := LoadFromFile(path)
		if err != nil {
			t.Fatalf("LoadFromFile() error = %v", err)
		}
		if len(db.Brokers) != 4 {
			t.Fatalf("expected 4 brokers, got %d", len(db.Brokers))
		}
		first := db.Brokers[0]
		if first.ID != "acme-corp" || first.Email != "privacy@acme.example" || first.Region != "us" {
			t.Errorf("unexpected first broker: %+v", first)
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		_, err := LoadFromFile("/nonexistent/path/brokers.yaml")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("invalid YAML", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.yaml")
		if err := os.WriteFile(path, []byte("::invalid::yaml{{"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadFromFile(path)
		if err == nil {
			t.Fatal("expected error for invalid YAML")
		}
	})
}

func TestLoadFromDir(t *testing.T) {
	t.Run("loads multiple YAML files", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(`brokers:
  - id: a1
    name: A1
    email: a1@test.com
    region: us
`), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "b.yml"), []byte(`brokers:
  - id: b1
    name: B1
    email: b1@test.com
    region: eu
`), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0755); err != nil {
			t.Fatal(err)
		}

		db, err := LoadFromDir(dir)
		if err != nil {
			t.Fatalf("LoadFromDir() error = %v", err)
		}
		if len(db.Brokers) != 2 {
			t.Fatalf("expected 2 brokers, got %d", len(db.Brokers))
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		_, err := LoadFromDir("/nonexistent/dir")
		if err == nil {
			t.Fatal("expected error for missing directory")
		}
	})
}

func TestFilter(t *testing.T) {
	db := makeDB(t)

	tests := []struct {
		name            string
		regions         []string
		excluded        []string
		expectedCount   int
		expectedFirstID string
	}{
		{
			name:          "no filters returns all",
			regions:       nil,
			excluded:      nil,
			expectedCount: 4,
		},
		{
			name:            "filter by us region includes global",
			regions:         []string{"us"},
			excluded:        nil,
			expectedCount:   3,
			expectedFirstID: "acme-corp",
		},
		{
			name:          "filter by eu region includes global",
			regions:       []string{"eu"},
			excluded:      nil,
			expectedCount: 2,
		},
		{
			name:          "global filter includes all",
			regions:       []string{"global"},
			excluded:      nil,
			expectedCount: 4,
		},
		{
			name:          "exclude by id",
			regions:       nil,
			excluded:      []string{"acme-corp"},
			expectedCount: 3,
		},
		{
			name:          "exclude by name case-insensitive",
			regions:       nil,
			excluded:      []string{"ACME CORP"},
			expectedCount: 3,
		},
		{
			name:          "combined region and exclude",
			regions:       []string{"us"},
			excluded:      []string{"bad-urls"},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := db.Filter(tt.regions, tt.excluded)
			if len(result) != tt.expectedCount {
				t.Errorf("Filter() returned %d brokers, want %d", len(result), tt.expectedCount)
			}
			if tt.expectedFirstID != "" && len(result) > 0 && result[0].ID != tt.expectedFirstID {
				t.Errorf("first broker ID = %q, want %q", result[0].ID, tt.expectedFirstID)
			}
		})
	}
}

func TestFindByID(t *testing.T) {
	db := makeDB(t)

	t.Run("found", func(t *testing.T) {
		b := db.FindByID("acme-corp")
		if b == nil {
			t.Fatal("expected broker, got nil")
		}
		if b.Name != "Acme Corp" {
			t.Errorf("Name = %q, want %q", b.Name, "Acme Corp")
		}
	})

	t.Run("found case-insensitive", func(t *testing.T) {
		b := db.FindByID("ACME-CORP")
		if b == nil {
			t.Fatal("expected broker with case-insensitive lookup")
		}
	})

	t.Run("not found", func(t *testing.T) {
		b := db.FindByID("nonexistent")
		if b != nil {
			t.Errorf("expected nil, got %+v", b)
		}
	})
}

func TestFindByEmail(t *testing.T) {
	db := makeDB(t)

	t.Run("found", func(t *testing.T) {
		b := db.FindByEmail("privacy@acme.example")
		if b == nil {
			t.Fatal("expected broker, got nil")
		}
		if b.ID != "acme-corp" {
			t.Errorf("ID = %q, want %q", b.ID, "acme-corp")
		}
	})

	t.Run("found case-insensitive", func(t *testing.T) {
		b := db.FindByEmail("PRIVACY@ACME.EXAMPLE")
		if b == nil {
			t.Fatal("expected broker with case-insensitive lookup")
		}
	})

	t.Run("not found", func(t *testing.T) {
		b := db.FindByEmail("nobody@example.com")
		if b != nil {
			t.Errorf("expected nil, got %+v", b)
		}
	})
}

func TestAdd(t *testing.T) {
	db := makeDB(t)

	t.Run("add new broker", func(t *testing.T) {
		b := Broker{ID: "new-broker", Name: "New Broker", Email: "new@test.com", Region: "us"}
		if err := db.Add(b); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
		found := db.FindByID("new-broker")
		if found == nil {
			t.Fatal("broker not found after Add()")
		}
	})

	t.Run("duplicate detection", func(t *testing.T) {
		b := Broker{ID: "acme-corp", Name: "Duplicate", Email: "dup@test.com", Region: "us"}
		err := db.Add(b)
		if err == nil {
			t.Fatal("expected error for duplicate ID")
		}
	})
}

func TestRemoveByEmail(t *testing.T) {
	db := makeDB(t)

	t.Run("remove existing", func(t *testing.T) {
		removed := db.RemoveByEmail("privacy@acme.example")
		if removed == nil {
			t.Fatal("expected removed broker, got nil")
		}
		if removed.ID != "acme-corp" {
			t.Errorf("removed ID = %q, want %q", removed.ID, "acme-corp")
		}
		if db.FindByID("acme-corp") != nil {
			t.Error("broker still present after RemoveByEmail")
		}
	})

	t.Run("remove nonexistent", func(t *testing.T) {
		removed := db.RemoveByEmail("nobody@example.com")
		if removed != nil {
			t.Errorf("expected nil, got %+v", removed)
		}
	})
}

func TestRemoveByID(t *testing.T) {
	db := makeDB(t)

	t.Run("remove existing", func(t *testing.T) {
		removed := db.RemoveByID("eu-broker")
		if removed == nil {
			t.Fatal("expected removed broker, got nil")
		}
		if removed.ID != "eu-broker" {
			t.Errorf("removed ID = %q, want %q", removed.ID, "eu-broker")
		}
		if db.FindByID("eu-broker") != nil {
			t.Error("broker still present after RemoveByID")
		}
	})

	t.Run("remove nonexistent", func(t *testing.T) {
		removed := db.RemoveByID("nonexistent")
		if removed != nil {
			t.Errorf("expected nil, got %+v", removed)
		}
	})
}

func TestSaveWithBackup(t *testing.T) {
	db := makeDB(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "brokers.yaml")

	if err := db.SaveWithBackup(path); err != nil {
		t.Fatalf("SaveWithBackup() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if len(data) == 0 {
		t.Error("saved file is empty")
	}

	if err := db.SaveWithBackup(path); err != nil {
		t.Fatalf("second SaveWithBackup() error = %v", err)
	}

	backup := path + ".bak"
	bakData, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("backup file not found: %v", err)
	}
	if len(bakData) == 0 {
		t.Error("backup file is empty")
	}
}

func TestIsValidURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"empty", "", true},
		{"https", "https://example.com", true},
		{"http", "http://example.com", true},
		{"HTTP case", "HTTP://EXAMPLE.COM", true},
		{"ftp", "ftp://example.com", false},
		{"javascript", "javascript:alert(1)", false},
		{"no scheme", "example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidURL(tt.url); got != tt.want {
				t.Errorf("isValidURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestSanitizeBroker(t *testing.T) {
	t.Run("strips invalid URLs", func(t *testing.T) {
		db := makeDB(t)
		bad := db.FindByID("bad-urls")
		if bad == nil {
			t.Fatal("bad-urls broker not found")
		}
		if bad.Website != "" {
			t.Errorf("Website should be sanitized to empty, got %q", bad.Website)
		}
		if bad.OptOutURL != "" {
			t.Errorf("OptOutURL should be sanitized to empty, got %q", bad.OptOutURL)
		}
	})

	t.Run("keeps valid URLs", func(t *testing.T) {
		db := makeDB(t)
		eu := db.FindByID("eu-broker")
		if eu == nil {
			t.Fatal("eu-broker not found")
		}
		if eu.Website != "https://eubroker.example" {
			t.Errorf("Website = %q, want %q", eu.Website, "https://eubroker.example")
		}
		if eu.OptOutURL != "https://eubroker.example/optout" {
			t.Errorf("OptOutURL = %q, want %q", eu.OptOutURL, "https://eubroker.example/optout")
		}
	})
}
