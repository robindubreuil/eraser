package broker

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func isValidURL(rawURL string) bool {
	if rawURL == "" {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme == "http" || scheme == "https"
}

func sanitizeBroker(b *Broker) {
	if !isValidURL(b.OptOutURL) {
		b.OptOutURL = ""
	}
	if !isValidURL(b.Website) {
		b.Website = ""
	}
	if strings.ContainsAny(b.Email, " \t\n\r") {
		b.Email = ""
	}
}

type Broker struct {
	ID           string   `yaml:"id"`
	Name         string   `yaml:"name"`
	Email        string   `yaml:"email"`
	Website      string   `yaml:"website,omitempty"`
	OptOutURL    string   `yaml:"opt_out_url,omitempty"`
	Region       string   `yaml:"region"`             // "us", "eu", "uk", "global"
	Category     string   `yaml:"category,omitempty"` // "people-search", "marketing", "background-check", "credit", etc.
	Notes        string   `yaml:"notes,omitempty"`
	RequiresID   bool     `yaml:"requires_id,omitempty"` // If they require ID verification
	Tags         []string `yaml:"tags,omitempty"`
	LastVerified string   `yaml:"last_verified,omitempty"` // ISO 8601 date when data was last verified
	Source       string   `yaml:"source,omitempty"`        // "cppa", "manual", "verified", etc.
}

type BrokerDatabase struct {
	Brokers []Broker `yaml:"brokers"`
}

func LoadFromFile(path string) (*BrokerDatabase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read broker file: %w", err)
	}

	var db BrokerDatabase
	if err := yaml.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("failed to parse broker file: %w", err)
	}

	for i := range db.Brokers {
		sanitizeBroker(&db.Brokers[i])
	}

	var filtered []Broker
	for _, b := range db.Brokers {
		if strings.TrimSpace(b.Email) == "" {
			fmt.Fprintf(os.Stderr, "WARNING: skipping broker %q (%s): empty email\n", b.Name, b.ID)
			continue
		}
		filtered = append(filtered, b)
	}
	db.Brokers = filtered

	return &db, nil
}

func LoadFromDir(dir string) (*BrokerDatabase, error) {
	db := &BrokerDatabase{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read broker directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		partialDB, err := LoadFromFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", entry.Name(), err)
		}

		db.Brokers = append(db.Brokers, partialDB.Brokers...)
	}

	return db, nil
}

func toSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[strings.ToLower(s)] = true
	}
	return m
}

func (db *BrokerDatabase) Filter(regions []string, excluded []string) []Broker {
	regionSet, excludedSet := toSet(regions), toSet(excluded)

	var result []Broker
	for _, b := range db.Brokers {
		if excludedSet[strings.ToLower(b.ID)] || excludedSet[strings.ToLower(b.Name)] {
			continue
		}
		if len(regionSet) > 0 {
			r := strings.ToLower(b.Region)
			if !regionSet[r] && !regionSet["global"] && r != "global" {
				continue
			}
		}
		result = append(result, b)
	}
	return result
}

func (db *BrokerDatabase) FindByID(id string) *Broker {
	id = strings.ToLower(id)
	for i := range db.Brokers {
		if strings.ToLower(db.Brokers[i].ID) == id {
			return &db.Brokers[i]
		}
	}
	return nil
}

func (db *BrokerDatabase) Save(path string) error {
	data, err := yaml.Marshal(db)
	if err != nil {
		return fmt.Errorf("failed to serialize brokers: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func (db *BrokerDatabase) Add(broker Broker) error {
	if db.FindByID(broker.ID) != nil {
		return fmt.Errorf("broker with ID %q already exists", broker.ID)
	}
	db.Brokers = append(db.Brokers, broker)
	return nil
}

func (db *BrokerDatabase) removeBy(match func(broker Broker) bool) *Broker {
	for i := range db.Brokers {
		if match(db.Brokers[i]) {
			removed := db.Brokers[i]
			db.Brokers = append(db.Brokers[:i], db.Brokers[i+1:]...)
			return &removed
		}
	}
	return nil
}

func (db *BrokerDatabase) FindByEmail(email string) *Broker {
	email = strings.ToLower(email)
	for i := range db.Brokers {
		if strings.ToLower(db.Brokers[i].Email) == email {
			return &db.Brokers[i]
		}
	}
	return nil
}

func (db *BrokerDatabase) RemoveByEmail(email string) *Broker {
	e := strings.ToLower(email)
	return db.removeBy(func(b Broker) bool { return strings.ToLower(b.Email) == e })
}

func (db *BrokerDatabase) RemoveByID(id string) *Broker {
	lo := strings.ToLower(id)
	return db.removeBy(func(b Broker) bool { return strings.ToLower(b.ID) == lo })
}

// SaveWithBackup saves the database to file, creating a backup first
func (db *BrokerDatabase) SaveWithBackup(path string) error {
	// Create backup
	if _, err := os.Stat(path); err == nil {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file for backup: %w", err)
		}
		backupPath := path + ".bak"
		if err := os.WriteFile(backupPath, data, 0644); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
	}

	return db.Save(path)
}
