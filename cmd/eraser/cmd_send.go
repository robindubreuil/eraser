package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/robindubreuil/eraser/internal/broker"
	"github.com/robindubreuil/eraser/internal/config"
	"github.com/robindubreuil/eraser/internal/email"
	"github.com/robindubreuil/eraser/internal/history"
	"github.com/robindubreuil/eraser/internal/template"
	"github.com/spf13/cobra"
)

type sendState struct {
	BrokerIDs []string  `json:"broker_ids"`
	Sent      int       `json:"sent"`
	Failed    int       `json:"failed"`
	StartedAt time.Time `json:"started_at"`
}

func sendStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".eraser")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "pending_send.json"), nil
}

func saveSendState(state *sendState) error {
	path, err := sendStatePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func loadSendState() (*sendState, error) {
	path, err := sendStatePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state sendState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func clearSendState() error {
	path, err := sendStatePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func sendCmd() *cobra.Command {
	var resume bool

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send removal requests to data brokers",
		Long:  "Send data removal requests to all configured data brokers.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSend(resume)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview emails without sending")
	cmd.Flags().BoolVar(&resume, "resume", false, "Resume from last interrupted send")

	return cmd
}

func runSend(resume bool) error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	if dryRun {
		cfg.Options.DryRun = true
	}

	brokerDB, err := broker.LoadFromFile(resolveBrokerPath())
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	brokers := brokerDB.Filter(cfg.Options.Regions, cfg.Options.ExcludedBrokers)

	if resume {
		brokers, err = applyResume(brokers)
		if err != nil {
			return err
		}
	}

	if len(brokers) == 0 {
		fmt.Println("No brokers to process.")
		return nil
	}

	tmplEngine, err := template.NewEngine()
	if err != nil {
		return fmt.Errorf("failed to initialize templates: %w", err)
	}

	var sender email.Sender
	if !cfg.Options.DryRun {
		sender, err = email.NewSender(cfg.Email)
		if err != nil {
			return fmt.Errorf("failed to initialize email sender: %w", err)
		}
	}

	store, err := history.NewStore(history.DefaultDBPath())
	if err != nil {
		return fmt.Errorf("failed to initialize history: %w", err)
	}
	defer store.Close()

	if cfg.Options.DryRun {
		fmt.Println("DRY RUN MODE - No emails will be sent")
		fmt.Println()
	}

	fmt.Printf("Processing %d brokers...\n", len(brokers))
	fmt.Println()

	state := &sendState{
		BrokerIDs: brokerIDs(brokers),
		StartedAt: time.Now(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println()
		fmt.Println("Interrupted! Saving progress...")
		_ = saveSendState(state)
		fmt.Printf("  Sent so far: %d\n", state.Sent)
		fmt.Printf("  Remaining:   %d\n", len(state.BrokerIDs))
		fmt.Println()
		fmt.Println("Run 'eraser send --resume' to continue.")
		cancel()
	}()

	for i, b := range brokers {
		if ctx.Err() != nil {
			return nil
		}

		fmt.Printf("[%d/%d] %s (%s)\n", i+1, len(brokers), b.Name, b.Email)

		emailMsg, err := tmplEngine.Render(cfg.Options.Template, cfg.Profile, b)
		if err != nil {
			fmt.Printf("  Failed to render template: %v\n", err)
			state.Failed++
			state.BrokerIDs = removeID(state.BrokerIDs, b.ID)
			_ = saveSendState(state)
			continue
		}

		if cfg.Options.DryRun {
			fmt.Printf("  Would send: %s\n", emailMsg.Subject)
			fmt.Printf("  To: %s\n", b.Email)
			state.Sent++
		} else {
			msg := email.Message{
				To:      b.Email,
				From:    cfg.Email.From,
				Subject: emailMsg.Subject,
				Body:    emailMsg.Body,
			}

			ctx := context.Background()
			result := sender.Send(ctx, msg)

			record := &history.Record{
				BrokerID:   b.ID,
				BrokerName: b.Name,
				Email:      b.Email,
				Template:   cfg.Options.Template,
				SentAt:     time.Now(),
			}

			if result.Success {
				record.Status = history.StatusSent
				record.MessageID = result.MessageID
				fmt.Printf("  Sent successfully\n")
				state.Sent++
			} else {
				record.Status = history.StatusFailed
				record.Error = result.Error.Error()
				fmt.Printf("  Failed: %v\n", result.Error)
				state.Failed++
			}

			if err := store.Add(record); err != nil {
				fmt.Printf("  Warning: Failed to record history: %v\n", err)
			}

			if i < len(brokers)-1 {
				time.Sleep(time.Duration(cfg.Options.RateLimitMs) * time.Millisecond)
			}
		}

		state.BrokerIDs = removeID(state.BrokerIDs, b.ID)
		_ = saveSendState(state)
	}

	_ = clearSendState()

	fmt.Println()
	fmt.Println("----------------------------------------")
	if cfg.Options.DryRun {
		fmt.Printf("Dry run complete: %d brokers would receive emails\n", state.Sent)
	} else {
		fmt.Printf("Complete: %d sent, %d failed\n", state.Sent, state.Failed)
	}

	return nil
}

func applyResume(brokers []broker.Broker) ([]broker.Broker, error) {
	state, err := loadSendState()
	if err != nil {
		return nil, fmt.Errorf("failed to load resume state: %w", err)
	}
	if state == nil {
		fmt.Println("No pending send found. Starting fresh.")
		return brokers, nil
	}

	remaining := toIDSet(state.BrokerIDs)
	var filtered []broker.Broker
	for _, b := range brokers {
		if remaining[b.ID] {
			filtered = append(filtered, b)
		}
	}

	fmt.Printf("Resuming: %d sent previously, %d remaining\n", state.Sent, len(filtered))
	return filtered, nil
}

func brokerIDs(brokers []broker.Broker) []string {
	ids := make([]string, len(brokers))
	for i, b := range brokers {
		ids[i] = b.ID
	}
	return ids
}

func removeID(ids []string, id string) []string {
	for i, v := range ids {
		if v == id {
			return append(ids[:i], ids[i+1:]...)
		}
	}
	return ids
}

func toIDSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
