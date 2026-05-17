package main

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestBrokersYAML(t *testing.T, brokers string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "brokers.yaml")
	if err := os.WriteFile(p, []byte(brokers), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeTestCPPACSV(t *testing.T, records [][]string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cppa.csv")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	for _, r := range records {
		if err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	return p
}

func TestLoadBrokers(t *testing.T) {
	p := writeTestBrokersYAML(t, `
brokers:
  - id: test1
    name: TestBroker
    email: privacy@testbroker.com
    website: https://testbroker.com
    opt_out_url: https://testbroker.com/optout
    region: us
  - id: test2
    name: AnotherBroker
    email: dpo@another.com
    region: eu
`)
	db, err := loadBrokers(p)
	if err != nil {
		t.Fatalf("loadBrokers: %v", err)
	}
	if len(db.Brokers) != 2 {
		t.Fatalf("expected 2 brokers, got %d", len(db.Brokers))
	}
	if db.Brokers[0].ID != "test1" {
		t.Errorf("broker[0].ID = %q, want test1", db.Brokers[0].ID)
	}
	if db.Brokers[1].Region != "eu" {
		t.Errorf("broker[1].Region = %q, want eu", db.Brokers[1].Region)
	}
}

func TestLoadBrokersInvalidPath(t *testing.T) {
	_, err := loadBrokers("/nonexistent/path/brokers.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadBrokersInvalidYAML(t *testing.T) {
	p := writeTestBrokersYAML(t, `{{invalid yaml`)
	_, err := loadBrokers(p)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseCPPA(t *testing.T) {
	row := make([]string, 20)
	row[0] = "Acme Corp"
	row[1] = "Acme DBA"
	row[2] = "https://acme.com"
	row[3] = "privacy@acme.com"
	row[14] = "https://acme.com/optout"

	records := [][]string{
		{"metadata line"},
		{"col0", "col1", "col2", "col3", "col4", "col5", "col6", "col7", "col8", "col9", "col10", "col11", "col12", "col13", "col14", "col15"},
		row,
	}
	p := writeTestCPPACSV(t, records)

	entries, err := parseCPPA(p)
	if err != nil {
		t.Fatalf("parseCPPA: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Email != "privacy@acme.com" {
		t.Errorf("Email = %q, want privacy@acme.com", e.Email)
	}
	if e.Name != "acme corp" {
		t.Errorf("Name = %q, want acme corp (normalized)", e.Name)
	}
	if !strings.Contains(e.OptOut, "acme.com/optout") {
		t.Errorf("OptOut = %q, want acme.com/optout", e.OptOut)
	}
}

func TestParseCPPAShortRow(t *testing.T) {
	records := [][]string{
		{"metadata"},
		{"h0", "h1"},
		{"short"}, // < 15 fields → skipped
	}
	p := writeTestCPPACSV(t, records)
	entries, err := parseCPPA(p)
	if err != nil {
		t.Fatalf("parseCPPA: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for short row, got %d", len(entries))
	}
}

func TestParseCPPAEmptyName(t *testing.T) {
	row := make([]string, 20)
	row[0] = "   "
	row[3] = "test@test.com"
	records := [][]string{
		{"meta"},
		make([]string, 20),
		row,
	}
	p := writeTestCPPACSV(t, records)
	entries, err := parseCPPA(p)
	if err != nil {
		t.Fatalf("parseCPPA: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for blank name, got %d", len(entries))
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Acme, Inc.", "acme inc"},
		{"Acme Inc.", "acme inc"},
		{"Acme Inc", "acme inc"},
		{"Acme Corp.", "acme corp"},
		{"Big Corporation", "big corporation"},
		{"Some LLC", "some llc"},
		{"Foo Ltd.", "foo ltd"},
		{"Bar Co.", "bar co"},
		{"Hello-World_Test", "hello world test"},
		{"  Multiple   Spaces  ", "multiple spaces"},
		{"simple name", "simple name"},
	}
	for _, tt := range tests {
		got := normalizeName(tt.in)
		if got != tt.want {
			t.Errorf("normalizeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"https://www.Example.com/", "https://example.com"},
		{"example.com", "https://example.com"},
		{"http://WWW.Foo.COM/path/", "http://foo.com/path"},
	}
	for _, tt := range tests {
		got := normalizeURL(tt.in)
		if got != tt.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"https://www.example.com/path", "example.com"},
		{"http://foo.bar.com", "foo.bar.com"},
	}
	for _, tt := range tests {
		got := extractDomain(tt.in)
		if got != tt.want {
			t.Errorf("extractDomain(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEmailDomain(t *testing.T) {
	tests := []struct{ in, want string }{
		{"user@example.com", "example.com"},
		{"", ""},
		{"no-at-sign", ""},
	}
	for _, tt := range tests {
		got := emailDomain(tt.in)
		if got != tt.want {
			t.Errorf("emailDomain(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPct(t *testing.T) {
	if p := pct(1, 4); p != 25.0 {
		t.Errorf("pct(1,4) = %v, want 25", p)
	}
	if p := pct(0, 0); p != 0 {
		t.Errorf("pct(0,0) = %v, want 0", p)
	}
	if p := pct(3, 10); p != 30.0 {
		t.Errorf("pct(3,10) = %v, want 30", p)
	}
}

func TestRunQualityChecks(t *testing.T) {
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "a", Name: "Broker A", Email: "priv@a.com", OptOutURL: "https://a.com/optout", Website: "https://a.com"},
			{ID: "b", Name: "Gmail Broker", Email: "test@gmail.com"},
			{ID: "c", Name: "Yahoo Broker", Email: "test@yahoo.com"},
			{ID: "d", Name: "Broker D", Email: "priv@d.com", OptOutURL: "https://d.com/optout"},
			{ID: "d2", Name: "Broker D", Email: "priv@d.com"},
		},
	}
	rpt := &AuditReport{}
	runQualityChecks(db, rpt)

	if len(rpt.GmailBrokers) != 2 {
		t.Errorf("GmailBrokers = %d, want 2", len(rpt.GmailBrokers))
	}
	if len(rpt.MissingOptOut) != 3 {
		t.Errorf("MissingOptOut = %d, want 3", len(rpt.MissingOptOut))
	}
	if len(rpt.MissingWebsite) != 4 {
		t.Errorf("MissingWebsite = %d, want 4", len(rpt.MissingWebsite))
	}
	if len(rpt.DuplicateNames) != 1 {
		t.Errorf("DuplicateNames = %d, want 1", len(rpt.DuplicateNames))
	}
	if len(rpt.DuplicateEmails) != 1 {
		t.Errorf("DuplicateEmails = %d, want 1", len(rpt.DuplicateEmails))
	}
}

func TestRunQualityChecksHotmail(t *testing.T) {
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "h", Name: "Hotmail", Email: "x@hotmail.com"},
			{ID: "o", Name: "Outlook", Email: "x@outlook.com"},
		},
	}
	rpt := &AuditReport{}
	runQualityChecks(db, rpt)
	if len(rpt.GmailBrokers) != 2 {
		t.Errorf("GmailBrokers = %d, want 2 (hotmail+outlook)", len(rpt.GmailBrokers))
	}
}

func TestCrossReferenceCPPA(t *testing.T) {
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "a", Name: "Acme", Email: "p@acme.com", Website: "https://acme.com"},
			{ID: "b", Name: "Unknown", Email: "p@unknown.com", Website: "https://unknown.com"},
		},
	}
	cppa := []CPPAEntry{
		{Name: "acme", Email: "p@acme.com", Website: "https://acme.com"},
		{Name: "newbroker", Email: "x@newbroker.com", Website: "https://newbroker.com", OptOut: "https://newbroker.com/optout"},
	}
	rpt := &AuditReport{}
	crossReferenceCPPA(db, cppa, rpt)

	if rpt.CPPAMatched != 1 {
		t.Errorf("CPPAMatched = %d, want 1", rpt.CPPAMatched)
	}
	if rpt.CPPAMissing != 1 {
		t.Errorf("CPPAMissing = %d, want 1", rpt.CPPAMissing)
	}
	if len(rpt.CPPAUnmatched) != 1 {
		t.Fatalf("CPPAUnmatched len = %d, want 1", len(rpt.CPPAUnmatched))
	}
	if rpt.CPPAUnmatched[0].Name != "newbroker" {
		t.Errorf("CPPAUnmatched[0].Name = %q, want newbroker", rpt.CPPAUnmatched[0].Name)
	}
	if len(rpt.OurUnmatched) != 1 || rpt.OurUnmatched[0].Name != "Unknown" {
		t.Errorf("OurUnmatched = %v, want [Unknown]", rpt.OurUnmatched)
	}
}

func TestCrossReferenceCPPAEmailMismatch(t *testing.T) {
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "a", Name: "Acme", Email: "wrong@otherdomain.com", Website: "https://acme.com"},
		},
	}
	cppa := []CPPAEntry{
		{Name: "acme", Email: "right@acme.com", Website: "https://acme.com"},
	}
	rpt := &AuditReport{}
	crossReferenceCPPA(db, cppa, rpt)

	if len(rpt.EmailMismatches) != 1 {
		t.Fatalf("EmailMismatches len = %d, want 1", len(rpt.EmailMismatches))
	}
	m := rpt.EmailMismatches[0]
	if m.OurEmail != "wrong@otherdomain.com" || m.CPPAEmail != "right@acme.com" {
		t.Errorf("mismatch: ours=%q cppa=%q", m.OurEmail, m.CPPAEmail)
	}
}

func TestCrossReferenceCPPAOptOutSuggestion(t *testing.T) {
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "a", Name: "Acme", Email: "p@acme.com", Website: "https://acme.com"},
		},
	}
	cppa := []CPPAEntry{
		{Name: "acme", Email: "p@acme.com", Website: "https://acme.com", OptOut: "https://acme.com/optout"},
	}
	rpt := &AuditReport{}
	crossReferenceCPPA(db, cppa, rpt)

	if len(rpt.Suggestions) != 1 {
		t.Fatalf("Suggestions len = %d, want 1", len(rpt.Suggestions))
	}
	if !strings.Contains(rpt.Suggestions[0], "opt_out_url") {
		t.Errorf("Suggestion = %q, want opt_out_url suggestion", rpt.Suggestions[0])
	}
}

func TestCrossReferenceCPPAByDBA(t *testing.T) {
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "a", Name: "Doing Business As", Email: "p@dba.com"},
		},
	}
	cppa := []CPPAEntry{
		{Name: "legal name", DBA: "doing business as", Email: "p@dba.com"},
	}
	rpt := &AuditReport{}
	crossReferenceCPPA(db, cppa, rpt)
	if rpt.CPPAMatched != 1 {
		t.Errorf("CPPAMatched = %d, want 1 (DBA match)", rpt.CPPAMatched)
	}
}

func TestCrossReferenceCPPAByDomain(t *testing.T) {
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "a", Name: "Foo", Email: "p@foo.com", Website: "https://foo.com"},
		},
	}
	cppa := []CPPAEntry{
		{Name: "totally different name", Email: "other@bar.com", Website: "https://foo.com"},
	}
	rpt := &AuditReport{}
	crossReferenceCPPA(db, cppa, rpt)
	if rpt.CPPAMatched != 1 {
		t.Errorf("CPPAMatched = %d, want 1 (domain match)", rpt.CPPAMatched)
	}
}

func TestCrossReferenceCPPAByEmailDomain(t *testing.T) {
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "a", Name: "Foo", Email: "x@uniquecorp.net"},
		},
	}
	cppa := []CPPAEntry{
		{Name: "other name", Email: "y@uniquecorp.net"},
	}
	rpt := &AuditReport{}
	crossReferenceCPPA(db, cppa, rpt)
	if rpt.CPPAMatched != 1 {
		t.Errorf("CPPAMatched = %d, want 1 (email domain match)", rpt.CPPAMatched)
	}
}

func TestAuditReportJSON(t *testing.T) {
	ts := time.Now().UTC().Format(time.RFC3339)
	rpt := &AuditReport{
		Timestamp:       ts,
		OurBrokerCount:  5,
		EUBrokerCount:   2,
		CPPABrokerCount: 100,
		CPPAMatched:     3,
		CPPAMissing:     97,
		GmailBrokers:    []string{"BadBroker"},
		MissingOptOut:   []string{"BrokerX"},
		DuplicateEmails: []string{"dup@example.com"},
		MXNoRecords:     []string{"BadBroker"},
		MXFailThreshold: false,
		Suggestions:     []string{"fix something"},
	}
	data, err := json.MarshalIndent(rpt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	var back AuditReport
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.OurBrokerCount != 5 {
		t.Errorf("OurBrokerCount = %d, want 5", back.OurBrokerCount)
	}
	if back.Timestamp != ts {
		t.Errorf("Timestamp mismatch: %q vs %q", back.Timestamp, ts)
	}
	if len(back.GmailBrokers) != 1 {
		t.Errorf("GmailBrokers len = %d, want 1", len(back.GmailBrokers))
	}
}

func TestValidateMXRecords(t *testing.T) {
	if os.Getenv("CI") == "" && testing.Short() {
		t.Skip("skipping DNS lookup in short mode")
	}
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "a", Name: "Google", Email: "test@google.com"},
			{ID: "b", Name: "FakeBroker", Email: "test@thisdomaindoesnotexist12345.invalid"},
		},
	}
	rpt := &AuditReport{}
	validateMXRecords(db, rpt)

	if len(rpt.MXDomainsChecked) != 2 {
		t.Fatalf("MXDomainsChecked len = %d, want 2", len(rpt.MXDomainsChecked))
	}

	found := false
	for _, d := range rpt.MXDomainsChecked {
		if d.Domain == "google.com" && d.HasMX {
			found = true
		}
	}
	if !found {
		t.Error("google.com should have MX records")
	}

	if len(rpt.MXNoRecords) == 0 {
		t.Error("expected at least one broker with no MX (invalid domain)")
	}
}

func TestValidateMXRecordsAllValid(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DNS lookup in short mode")
	}
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "g", Name: "Google", Email: "test@google.com"},
		},
	}
	rpt := &AuditReport{}
	validateMXRecords(db, rpt)
	if rpt.MXFailThreshold {
		t.Error("MXFailThreshold should be false for valid domains")
	}
}

func TestCheckEntryMatchNoMismatch(t *testing.T) {
	b := &Broker{Email: "p@acme.com", Website: "https://acme.com", OptOutURL: "https://acme.com/optout"}
	e := &CPPAEntry{Email: "p@acme.com", Website: "https://acme.com", OptOut: "https://acme.com/optout"}
	rpt := &AuditReport{}
	checkEntryMatch(b, e, rpt)
	if len(rpt.EmailMismatches) != 0 {
		t.Errorf("EmailMismatches = %d, want 0", len(rpt.EmailMismatches))
	}
	if len(rpt.WebsiteMismatches) != 0 {
		t.Errorf("WebsiteMismatches = %d, want 0", len(rpt.WebsiteMismatches))
	}
}

func TestCheckEntryMatchWebsiteMismatch(t *testing.T) {
	b := &Broker{Email: "p@acme.com", Website: "https://acme.com", OptOutURL: "https://acme.com/optout"}
	e := &CPPAEntry{Email: "p@acme.com", Website: "https://totallydifferent.com", OptOut: ""}
	rpt := &AuditReport{}
	checkEntryMatch(b, e, rpt)
	if len(rpt.WebsiteMismatches) != 1 {
		t.Fatalf("WebsiteMismatches = %d, want 1", len(rpt.WebsiteMismatches))
	}
}

func TestPrintReportNoPanic(t *testing.T) {
	rpt := &AuditReport{
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		OurBrokerCount:  2,
		EUBrokerCount:   1,
		CPPABrokerCount: 10,
		CPPAMatched:     1,
		CPPAMissing:     9,
		CPPAUnmatched:   []MatchResult{{Name: "unmatched1"}},
		OurUnmatched:    []MatchResult{{Name: "our1"}},
		EmailMismatches: []MatchResult{{Name: "mismatch"}},
		GmailBrokers:    []string{"gmail1"},
		MissingOptOut:   []string{"noopt"},
		MissingWebsite:  []string{"nows"},
		DuplicateNames:  []string{"dup"},
		DuplicateEmails: []string{"dup@e.com"},
		MXNoRecords:     []string{"nomx"},
		Suggestions:     []string{"fix it"},
	}
	printReport(rpt)
}
