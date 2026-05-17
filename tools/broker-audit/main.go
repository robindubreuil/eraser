package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type Broker struct {
	ID        string   `yaml:"id"`
	Name      string   `yaml:"name"`
	Email     string   `yaml:"email"`
	Website   string   `yaml:"website,omitempty"`
	OptOutURL string   `yaml:"opt_out_url,omitempty"`
	Region    string   `yaml:"region"`
	Category  string   `yaml:"category,omitempty"`
	Notes     string   `yaml:"notes,omitempty"`
	Tags      []string `yaml:"tags,omitempty"`
}

type BrokerDatabase struct {
	Brokers []Broker `yaml:"brokers"`
}

type CPPAEntry struct {
	Name    string
	DBA     string
	Website string
	Email   string
	OptOut  string
}

type MatchResult struct {
	Name         string `json:"name"`
	Matched      bool   `json:"matched"`
	MatchType    string `json:"match_type,omitempty"`
	MatchedTo    string `json:"matched_to,omitempty"`
	EmailMatch   bool   `json:"email_match"`
	WebsiteMatch bool   `json:"website_match"`
	CPPAEmail    string `json:"cppa_email,omitempty"`
	CPPAWebsite  string `json:"cppa_website,omitempty"`
	CPPAOptOut   string `json:"cppa_optout,omitempty"`
	OurEmail     string `json:"our_email,omitempty"`
	OurWebsite   string `json:"our_website,omitempty"`
}

type MXDomainResult struct {
	Domain string `json:"domain"`
	HasMX  bool   `json:"has_mx"`
}

type AuditReport struct {
	Timestamp         string           `json:"timestamp"`
	OurBrokerCount    int              `json:"our_broker_count"`
	EUBrokerCount     int              `json:"eu_broker_count"`
	CPPABrokerCount   int              `json:"cppa_broker_count"`
	CPPAMatched       int              `json:"cppa_matched"`
	CPPAMissing       int              `json:"cppa_missing"`
	CPPAUnmatched     []MatchResult    `json:"cppa_unmatched"`
	OurUnmatched      []MatchResult    `json:"our_unmatched"`
	EmailMismatches   []MatchResult    `json:"email_mismatches"`
	WebsiteMismatches []MatchResult    `json:"website_mismatches"`
	GmailBrokers      []string         `json:"gmail_brokers"`
	MissingOptOut     []string         `json:"missing_optout"`
	MissingWebsite    []string         `json:"missing_website"`
	DuplicateNames    []string         `json:"duplicate_names"`
	DuplicateEmails   []string         `json:"duplicate_emails"`
	MXNoRecords       []string         `json:"mx_no_records"`
	MXDomainsChecked  []MXDomainResult `json:"mx_domains_checked"`
	MXFailThreshold   bool             `json:"mx_fail_threshold"`
	Suggestions       []string         `json:"suggestions"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <brokers.yaml> [--cppa <file.csv>] [--fetch-cppa] [--validate] [--json <output.json>]\n", os.Args[0])
		os.Exit(1)
	}

	brokersFile := os.Args[1]
	var cppaFile string
	var fetchCPPA bool
	var doValidate bool
	var jsonOutput string

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--cppa":
			i++
			cppaFile = os.Args[i]
		case "--fetch-cppa":
			fetchCPPA = true
		case "--validate":
			doValidate = true
		case "--json":
			i++
			jsonOutput = os.Args[i]
		}
	}

	db, err := loadBrokers(brokersFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading brokers: %v\n", err)
		os.Exit(1)
	}

	report := &AuditReport{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		OurBrokerCount: len(db.Brokers),
	}

	euCount := 0
	for _, b := range db.Brokers {
		if strings.EqualFold(b.Region, "eu") || strings.EqualFold(b.Region, "uk") {
			euCount++
		}
	}
	report.EUBrokerCount = euCount

	fmt.Printf("Loaded %d brokers from %s\n", len(db.Brokers), brokersFile)

	// Basic quality checks
	runQualityChecks(db, report)

	// MX record validation
	validateMXRecords(db, report)

	// CPPA cross-reference
	if fetchCPPA {
		tmpFile := filepath.Join(os.TempDir(), "cppa-current.csv")
		fmt.Println("Fetching CPPA registry...")
		if err := fetchCPPARegistry(tmpFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching CPPA: %v\n", err)
		} else {
			cppaFile = tmpFile
		}
	}

	if cppaFile != "" {
		cppaEntries, err := parseCPPA(cppaFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing CPPA: %v\n", err)
		} else {
			report.CPPABrokerCount = len(cppaEntries)
			fmt.Printf("Loaded %d CPPA registry entries\n", len(cppaEntries))
			crossReferenceCPPA(db, cppaEntries, report)
		}
	}

	// HTTP validation
	if doValidate {
		runHTTPValidation(db, report)
	}

	// Print report
	printReport(report)

	// JSON output
	if jsonOutput != "" {
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(jsonOutput, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing JSON: %v\n", err)
		} else {
			fmt.Printf("Full report written to %s\n", jsonOutput)
		}
	}
}

func loadBrokers(path string) (*BrokerDatabase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var db BrokerDatabase
	if err := yaml.Unmarshal(data, &db); err != nil {
		return nil, err
	}
	return &db, nil
}

func fetchCPPARegistry(dest string) error {
	resp, err := http.Get("https://cppa.ca.gov/data_broker_registry/registry.csv")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func parseCPPA(path string) ([]CPPAEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Skip first 2 lines (header metadata + column names)
	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.FieldsPerRecord = -1

	// Line 1: metadata
	if _, err := r.Read(); err != nil {
		return nil, fmt.Errorf("reading metadata line: %w", err)
	}
	// Line 2: column names
	if _, err := r.Read(); err != nil {
		return nil, fmt.Errorf("reading column headers: %w", err)
	}

	var entries []CPPAEntry
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if len(record) < 15 {
			continue
		}
		name := strings.TrimSpace(record[0])
		if name == "" {
			continue
		}
		entry := CPPAEntry{
			Name:    normalizeName(name),
			DBA:     normalizeName(strings.TrimSpace(record[1])),
			Website: normalizeURL(strings.TrimSpace(record[2])),
			Email:   strings.ToLower(strings.TrimSpace(record[3])),
			OptOut:  normalizeURL(strings.TrimSpace(record[14])),
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func normalizeName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, ", Inc.", "")
	s = strings.ReplaceAll(s, " Inc.", "")
	s = strings.ReplaceAll(s, " Inc", "")
	s = strings.ReplaceAll(s, ", LLC", "")
	s = strings.ReplaceAll(s, " LLC", "")
	s = strings.ReplaceAll(s, " Corp.", "")
	s = strings.ReplaceAll(s, " Corporation", "")
	s = strings.ReplaceAll(s, " Ltd.", "")
	s = strings.ReplaceAll(s, " Co.", "")
	// Remove punctuation for matching
	reg := regexp.MustCompile(`[.,\-_]`)
	s = reg.ReplaceAllString(s, " ")
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func normalizeURL(u string) string {
	if u == "" {
		return ""
	}
	if !strings.HasPrefix(u, "http") {
		u = "https://" + u
	}
	parsed, err := url.Parse(strings.ToLower(strings.TrimSuffix(u, "/")))
	if err != nil {
		return strings.ToLower(strings.TrimSuffix(u, "/"))
	}
	parsed.Host = strings.TrimPrefix(parsed.Host, "www.")
	return parsed.String()
}

func extractDomain(u string) string {
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	return strings.TrimPrefix(host, "www.")
}

func emailDomain(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}

func crossReferenceCPPA(db *BrokerDatabase, cppa []CPPAEntry, report *AuditReport) {
	ourByName := make(map[string]*Broker)
	ourByDomain := make(map[string]*Broker)
	ourByEmailDomain := make(map[string]*Broker)

	for i := range db.Brokers {
		b := &db.Brokers[i]
		ourByName[normalizeName(b.Name)] = b

		d := extractDomain(b.Website)
		if d != "" {
			ourByDomain[d] = b
		}
		ed := emailDomain(b.Email)
		if ed != "" {
			ourByEmailDomain[ed] = b
		}
	}

	cppaMatched := make(map[int]bool)

	for i, entry := range cppa {
		matched := false

		if b, ok := ourByName[entry.Name]; ok {
			matched = true
			checkEntryMatch(b, &entry, report)
		}

		if !matched && entry.DBA != "" {
			if b, ok := ourByName[entry.DBA]; ok {
				matched = true
				checkEntryMatch(b, &entry, report)
			}
		}

		if !matched && entry.Website != "" {
			d := extractDomain(entry.Website)
			if b, ok := ourByDomain[d]; ok {
				matched = true
				checkEntryMatch(b, &entry, report)
			}
		}

		if !matched && entry.Email != "" {
			ed := emailDomain(entry.Email)
			if b, ok := ourByEmailDomain[ed]; ok {
				matched = true
				checkEntryMatch(b, &entry, report)
			}
		}

		if matched {
			report.CPPAMatched++
			cppaMatched[i] = true
		} else {
			report.CPPAUnmatched = append(report.CPPAUnmatched, MatchResult{
				Name:        entry.Name,
				Matched:     false,
				CPPAEmail:   entry.Email,
				CPPAWebsite: entry.Website,
				CPPAOptOut:  entry.OptOut,
			})
		}
	}

	report.CPPAMissing = len(cppa) - report.CPPAMatched

	// Check which of our brokers didn't match any CPPA entry
	cppaNames := make(map[string]bool)
	cppaDomains := make(map[string]bool)
	for _, entry := range cppa {
		cppaNames[entry.Name] = true
		if entry.DBA != "" {
			cppaNames[entry.DBA] = true
		}
		if d := extractDomain(entry.Website); d != "" {
			cppaDomains[d] = true
		}
		if ed := emailDomain(entry.Email); ed != "" {
			cppaDomains[ed] = true
		}
	}

	for _, b := range db.Brokers {
		n := normalizeName(b.Name)
		d := extractDomain(b.Website)
		ed := emailDomain(b.Email)
		if !cppaNames[n] && !cppaDomains[d] && !cppaDomains[ed] {
			report.OurUnmatched = append(report.OurUnmatched, MatchResult{
				Name:       b.Name,
				Matched:    false,
				OurEmail:   b.Email,
				OurWebsite: b.Website,
			})
		}
	}
}

func checkEntryMatch(b *Broker, entry *CPPAEntry, report *AuditReport) {
	ourEmail := strings.ToLower(b.Email)
	cppaEmail := strings.ToLower(entry.Email)
	ourDomain := emailDomain(b.Email)
	cppaDomain := emailDomain(entry.Email)

	if ourEmail != cppaEmail && ourDomain != cppaDomain && ourDomain != "" && cppaDomain != "" {
		report.EmailMismatches = append(report.EmailMismatches, MatchResult{
			Name:      b.Name,
			Matched:   true,
			OurEmail:  b.Email,
			CPPAEmail: entry.Email,
		})
	}

	ourWS := normalizeURL(b.Website)
	cppaWS := normalizeURL(entry.Website)
	if ourWS != "" && cppaWS != "" && ourWS != cppaWS && extractDomain(b.Website) != extractDomain(entry.Website) {
		report.WebsiteMismatches = append(report.WebsiteMismatches, MatchResult{
			Name:        b.Name,
			Matched:     true,
			OurWebsite:  b.Website,
			CPPAWebsite: entry.Website,
		})
	}

	if b.OptOutURL == "" && entry.OptOut != "" {
		report.Suggestions = append(report.Suggestions, fmt.Sprintf("ADD opt_out_url to %q: %s (from CPPA)", b.Name, entry.OptOut))
	}
}

func runQualityChecks(db *BrokerDatabase, report *AuditReport) {
	emailCount := make(map[string]int)
	nameCount := make(map[string]int)

	for _, b := range db.Brokers {
		emailCount[b.Email]++
		nameCount[strings.ToLower(b.Name)]++

		if strings.HasSuffix(b.Email, "@gmail.com") ||
			strings.HasSuffix(b.Email, "@yahoo.com") ||
			strings.HasSuffix(b.Email, "@hotmail.com") ||
			strings.HasSuffix(b.Email, "@outlook.com") {
			report.GmailBrokers = append(report.GmailBrokers, b.Name)
		}
		if b.OptOutURL == "" {
			report.MissingOptOut = append(report.MissingOptOut, b.Name)
		}
		if b.Website == "" {
			report.MissingWebsite = append(report.MissingWebsite, b.Name)
		}
	}

	for email, count := range emailCount {
		if count > 1 {
			report.DuplicateEmails = append(report.DuplicateEmails, email)
		}
	}
	for name, count := range nameCount {
		if count > 1 {
			report.DuplicateNames = append(report.DuplicateNames, name)
		}
	}
}

func runHTTPValidation(db *BrokerDatabase, report *AuditReport) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	type result struct {
		idx    int
		name   string
		url    string
		status string
		alive  bool
	}

	var websites []int
	for i, b := range db.Brokers {
		if b.Website != "" {
			websites = append(websites, i)
		}
	}
	fmt.Printf("  Validating %d websites (20 concurrent)...\n", len(websites))

	ch := make(chan result, len(websites))
	sem := make(chan struct{}, 20)
	total := len(websites)

	for _, idx := range websites {
		b := db.Brokers[idx]
		sem <- struct{}{}
		go func(idx int, name, website string) {
			defer func() { <-sem }()
			resp, err := client.Get(website)
			if err != nil {
				ch <- result{idx, name, website, err.Error(), false}
				return
			}
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				ch <- result{idx, name, website, fmt.Sprintf("HTTP %d", resp.StatusCode), false}
				return
			}
			ch <- result{idx, name, website, "OK", true}
		}(idx, b.Name, b.Website)
	}

	deadCount := 0
	var deadResults []result
	for i := 0; i < total; i++ {
		r := <-ch
		if !r.alive {
			deadCount++
			deadResults = append(deadResults, r)
		}
		if (i+1)%100 == 0 {
			fmt.Printf("  Progress: %d/%d (%d dead)\n", i+1, total, deadCount)
		}
	}

	if deadCount > 0 {
		fmt.Printf("\n  DEAD URLS (%d):\n", deadCount)
		for _, r := range deadResults {
			fmt.Printf("    %-50s %s  (%s)\n", r.name, r.url, r.status)
		}
		report.Suggestions = append(report.Suggestions, fmt.Sprintf("VALIDATION: %d brokers have dead/unreachable websites", deadCount))
	} else {
		fmt.Println("  All websites reachable!")
	}
}

func validateMXRecords(db *BrokerDatabase, report *AuditReport) {
	domainSet := make(map[string]bool)
	domainToBrokers := make(map[string][]string)
	for _, b := range db.Brokers {
		d := emailDomain(b.Email)
		if d == "" {
			continue
		}
		domainSet[d] = true
		domainToBrokers[d] = append(domainToBrokers[d], b.Name)
	}

	domains := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domains = append(domains, d)
	}

	fmt.Printf("  Checking MX records for %d unique domains (20 concurrent)...\n", len(domains))

	type mxResult struct {
		domain string
		hasMX  bool
	}

	ch := make(chan mxResult, len(domains))
	sem := make(chan struct{}, 20)
	var wg sync.WaitGroup

	for _, domain := range domains {
		wg.Add(1)
		sem <- struct{}{}
		go func(d string) {
			defer func() { <-sem }()
			defer wg.Done()
			mx, _ := net.LookupMX(d)
			ch <- mxResult{d, len(mx) > 0}
		}(domain)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	mxStatus := make(map[string]bool)
	for r := range ch {
		mxStatus[r.domain] = r.hasMX
	}

	var noMXDomains []MXDomainResult
	domainHasMX := make(map[string]bool)
	for _, d := range domains {
		has := mxStatus[d]
		domainHasMX[d] = has
		report.MXDomainsChecked = append(report.MXDomainsChecked, MXDomainResult{d, has})
		if !has {
			noMXDomains = append(noMXDomains, MXDomainResult{d, false})
		}
	}

	var noMXBrokers []string
	for _, b := range db.Brokers {
		d := emailDomain(b.Email)
		if d != "" && !domainHasMX[d] {
			noMXBrokers = append(noMXBrokers, b.Name)
		}
	}
	report.MXNoRecords = noMXBrokers

	fmt.Printf("  MX check: %d/%d domains have no MX records (%d brokers affected)\n",
		len(noMXDomains), len(domains), len(noMXBrokers))
	for _, nr := range noMXDomains {
		fmt.Printf("    NO MX: %-40s (brokers: %s)\n", nr.Domain, strings.Join(domainToBrokers[nr.Domain], ", "))
	}

	badPct := pct(len(noMXBrokers), len(db.Brokers))
	if badPct > 5.0 {
		report.MXFailThreshold = true
		report.Suggestions = append(report.Suggestions,
			fmt.Sprintf("MX GATE: %.1f%% of brokers have domains with no MX records (threshold: 5%%)", badPct))
	}
}

func printReport(report *AuditReport) {
	fmt.Println("\n========== BROKER AUDIT REPORT ==========")
	fmt.Printf("Timestamp: %s\n", report.Timestamp)
	fmt.Printf("Our brokers: %d\n", report.OurBrokerCount)
	fmt.Printf("CPPA registry: %d\n", report.CPPABrokerCount)
	fmt.Println()

	fmt.Printf("--- CPPA CROSS-REFERENCE ---\n")
	fmt.Printf("Matched: %d/%d (%.1f%%)\n", report.CPPAMatched, report.CPPABrokerCount, pct(report.CPPAMatched, report.CPPABrokerCount))
	fmt.Printf("Missing from our DB: %d brokers in CPPA not in our list\n", report.CPPAMissing)
	fmt.Printf("Our brokers not in CPPA: %d\n", len(report.OurUnmatched))

	if len(report.CPPAUnmatched) > 0 {
		fmt.Printf("\n--- TOP 50 MISSING BROKERS (in CPPA, not in our DB) ---\n")
		limit := len(report.CPPAUnmatched)
		if limit > 50 {
			limit = 50
		}
		for i := 0; i < limit; i++ {
			e := report.CPPAUnmatched[i]
			fmt.Printf("  %-50s  %s\n", e.Name, e.CPPAEmail)
		}
		if len(report.CPPAUnmatched) > 50 {
			fmt.Printf("  ... and %d more\n", len(report.CPPAUnmatched)-50)
		}
	}

	if len(report.OurUnmatched) > 0 {
		fmt.Printf("\n--- OUR BROKERS NOT IN CPPA (%d) ---\n", len(report.OurUnmatched))
		limit := len(report.OurUnmatched)
		if limit > 30 {
			limit = 30
		}
		for i := 0; i < limit; i++ {
			fmt.Printf("  %-50s  %s\n", report.OurUnmatched[i].Name, report.OurUnmatched[i].OurEmail)
		}
		if len(report.OurUnmatched) > 30 {
			fmt.Printf("  ... and %d more\n", len(report.OurUnmatched)-30)
		}
	}

	if len(report.EmailMismatches) > 0 {
		fmt.Printf("\n--- EMAIL MISMATCHES (%d) ---\n", len(report.EmailMismatches))
		for _, m := range report.EmailMismatches {
			fmt.Printf("  %-40s  ours: %-40s  cppa: %s\n", m.Name, m.OurEmail, m.CPPAEmail)
		}
	}

	fmt.Printf("\n--- QUALITY ISSUES ---\n")
	fmt.Printf("Gmail/webmail emails: %d\n", len(report.GmailBrokers))
	for _, n := range report.GmailBrokers {
		fmt.Printf("  - %s\n", n)
	}
	fmt.Printf("Missing opt_out_url: %d/%d\n", len(report.MissingOptOut), report.OurBrokerCount)
	fmt.Printf("Missing website: %d/%d\n", len(report.MissingWebsite), report.OurBrokerCount)
	fmt.Printf("Duplicate emails: %d\n", len(report.DuplicateEmails))
	fmt.Printf("Duplicate names: %d\n", len(report.DuplicateNames))

	fmt.Printf("MX no records: %d brokers\n", len(report.MXNoRecords))
	for _, n := range report.MXNoRecords {
		fmt.Printf("  - %s\n", n)
	}
	if report.MXFailThreshold {
		fmt.Println("  ** QUALITY GATE FAILED: >5% brokers have no MX records **")
	}

	if len(report.Suggestions) > 0 {
		fmt.Printf("\n--- SUGGESTIONS (%d) ---\n", len(report.Suggestions))
		limit := len(report.Suggestions)
		if limit > 30 {
			limit = 30
		}
		for i := 0; i < limit; i++ {
			fmt.Printf("  %s\n", report.Suggestions[i])
		}
		if len(report.Suggestions) > 30 {
			fmt.Printf("  ... and %d more\n", len(report.Suggestions)-30)
		}
	}
}

func pct(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den) * 100
}
