package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robindubreuil/eraser/internal/broker"
	"github.com/spf13/cobra"
)

func TestResolveBrokerPathExplicit(t *testing.T) {
	brokerFile = "/custom/brokers.yaml"
	got := resolveBrokerPath()
	if got != "/custom/brokers.yaml" {
		t.Errorf("resolveBrokerPath() = %q, want /custom/brokers.yaml", got)
	}
	brokerFile = ""
}

func TestResolveBrokerPathDataDir(t *testing.T) {
	brokerFile = ""
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "brokers.yaml"), []byte("brokers: []"), 0644); err != nil {
		t.Fatal(err)
	}
	origWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	got := resolveBrokerPath()
	want := "data/brokers.yaml"
	if got != want {
		t.Errorf("resolveBrokerPath() = %q, want %q", got, want)
	}
}

func TestResolveBrokerPathFallback(t *testing.T) {
	brokerFile = ""
	origWd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	got := resolveBrokerPath()
	if !strings.HasSuffix(got, filepath.Join("data", "brokers.yaml")) {
		t.Errorf("resolveBrokerPath() = %q, want .../data/brokers.yaml", got)
	}
}

func TestSendStateSaveLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	state := &sendState{
		BrokerIDs: []string{"a", "b", "c"},
		Sent:      5,
		Failed:    1,
		StartedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}
	if err := saveSendState(state); err != nil {
		t.Fatalf("saveSendState: %v", err)
	}

	loaded, err := loadSendState()
	if err != nil {
		t.Fatalf("loadSendState: %v", err)
	}
	if loaded.Sent != 5 {
		t.Errorf("Sent = %d, want 5", loaded.Sent)
	}
	if loaded.Failed != 1 {
		t.Errorf("Failed = %d, want 1", loaded.Failed)
	}
	if len(loaded.BrokerIDs) != 3 {
		t.Errorf("BrokerIDs len = %d, want 3", len(loaded.BrokerIDs))
	}
	if !loaded.StartedAt.Equal(state.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", loaded.StartedAt, state.StartedAt)
	}
}

func TestSendStateLoadMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	loaded, err := loadSendState()
	if err != nil {
		t.Fatalf("loadSendState: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for missing state, got %+v", loaded)
	}
}

func TestSendStateClear(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	state := &sendState{BrokerIDs: []string{"x"}, Sent: 1, StartedAt: time.Now()}
	if err := saveSendState(state); err != nil {
		t.Fatal(err)
	}
	if err := clearSendState(); err != nil {
		t.Fatalf("clearSendState: %v", err)
	}
	loaded, _ := loadSendState()
	if loaded != nil {
		t.Error("expected nil after clear")
	}
}

func TestSendStateClearIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := clearSendState(); err != nil {
		t.Fatalf("clearSendState on missing file: %v", err)
	}
}

func TestSendStateJSONRoundtrip(t *testing.T) {
	state := &sendState{
		BrokerIDs: []string{"broker-1", "broker-2"},
		Sent:      42,
		Failed:    3,
		StartedAt: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var back sendState
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Sent != 42 || back.Failed != 3 {
		t.Errorf("roundtrip: Sent=%d Failed=%d", back.Sent, back.Failed)
	}
	if len(back.BrokerIDs) != 2 {
		t.Errorf("roundtrip: BrokerIDs len = %d", len(back.BrokerIDs))
	}
}

func TestRemoveID(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	result := removeID(ids, "b")
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	if result[0] != "a" || result[1] != "c" || result[2] != "d" {
		t.Errorf("result = %v, want [a c d]", result)
	}
}

func TestRemoveIDNotFound(t *testing.T) {
	ids := []string{"a", "b"}
	result := removeID(ids, "z")
	if len(result) != 2 {
		t.Errorf("len = %d, want 2 (unchanged)", len(result))
	}
}

func TestRemoveIDEmpty(t *testing.T) {
	result := removeID([]string{}, "x")
	if len(result) != 0 {
		t.Errorf("len = %d, want 0", len(result))
	}
}

func TestToIDSet(t *testing.T) {
	s := toIDSet([]string{"a", "b", "c"})
	if len(s) != 3 {
		t.Fatalf("len = %d, want 3", len(s))
	}
	if !s["a"] || !s["b"] || !s["c"] {
		t.Error("missing expected keys")
	}
	if s["z"] {
		t.Error("unexpected key z")
	}
}

func TestBrokerIDs(t *testing.T) {
	brokers := []broker.Broker{
		{ID: "id1", Name: "A"},
		{ID: "id2", Name: "B"},
	}
	ids := brokerIDs(brokers)
	if len(ids) != 2 || ids[0] != "id1" || ids[1] != "id2" {
		t.Errorf("brokerIDs = %v, want [id1 id2]", ids)
	}
}

func TestTruncateURL(t *testing.T) {
	if got := truncateURL("short", 10); got != "short" {
		t.Errorf("truncateURL short = %q", got)
	}
	if got := truncateURL("a-very-long-url-here", 10); got != "a-very-..." {
		t.Errorf("truncateURL long = %q, want a-very-...", got)
	}
}

func TestTruncateString(t *testing.T) {
	if got := truncateString("hi", 5); got != "hi" {
		t.Errorf("truncateString = %q", got)
	}
	if got := truncateString("hello world", 8); got != "hello..." {
		t.Errorf("truncateString = %q, want hello...", got)
	}
}

func TestRootCmdFlags(t *testing.T) {
	brokerFile = ""
	cfgFile = ""

	root := &cobra.Command{Use: "eraser"}
	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file")
	root.PersistentFlags().StringVar(&brokerFile, "brokers", "", "broker database file")

	args := []string{"--config", "/tmp/test.yaml", "--brokers", "/tmp/b.yaml"}
	if err := root.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	if cfgFile != "/tmp/test.yaml" {
		t.Errorf("cfgFile = %q", cfgFile)
	}
	if brokerFile != "/tmp/b.yaml" {
		t.Errorf("brokerFile = %q", brokerFile)
	}
}

func TestResolveConfigPath(t *testing.T) {
	cfgFile = "/my/config.yaml"
	got := resolveConfigPath()
	if got != "/my/config.yaml" {
		t.Errorf("resolveConfigPath() = %q, want /my/config.yaml", got)
	}
	cfgFile = ""
}
