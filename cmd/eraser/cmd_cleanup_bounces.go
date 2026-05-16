package main

import (
	"context"
	"fmt"
	"time"

	"github.com/robindubreuil/eraser/internal/broker"
	"github.com/robindubreuil/eraser/internal/config"
	"github.com/robindubreuil/eraser/internal/inbox"
	"github.com/spf13/cobra"
)

func cleanupBouncesCmd() *cobra.Command {
	var (
		remove bool
		days   int
	)

	cmd := &cobra.Command{
		Use:   "cleanup-bounces",
		Short: "Find and remove bounced broker email addresses",
		Long: `Scan your inbox for bounced/undeliverable emails and identify
invalid broker email addresses. Optionally remove them from the database.

By default, this command shows what would be removed without making changes.
Use --remove to actually remove the invalid brokers from the database.

Examples:
  eraser cleanup-bounces                 # Show bounced emails (dry run)
  eraser cleanup-bounces --remove        # Remove bounced brokers
  eraser cleanup-bounces --days 30       # Look back 30 days`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runCleanupBounces(remove, days)
		},
	}

	cmd.Flags().BoolVar(&remove, "remove", false, "Actually remove bounced brokers from database")
	cmd.Flags().IntVar(&days, "days", 30, "Number of days to scan for bounced emails")

	return cmd
}

func runCleanupBounces(remove bool, days int) error {
	// Load config
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if inbox is configured
	if !cfg.Inbox.Enabled {
		return fmt.Errorf("inbox monitoring not configured. Run 'eraser init' to set up")
	}

	// Load broker database
	brokerPath := resolveBrokerPath()
	brokerDB, err := broker.LoadFromFile(brokerPath)
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	fmt.Println("🔍 Scanning inbox for bounced emails...")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Create inbox monitor
	monitor := inbox.NewMonitor(cfg.Inbox, brokerDB.Brokers)

	// Connect
	ctx := context.Background()
	if err := monitor.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to inbox: %w", err)
	}
	defer monitor.Disconnect() //nolint:errcheck

	// Fetch bounce emails
	bounceEmails, err := monitor.FetchBounceEmails(ctx, days)
	if err != nil {
		return fmt.Errorf("failed to fetch bounce emails: %w", err)
	}

	if len(bounceEmails) == 0 {
		fmt.Println("✓ No bounced emails found!")
		return nil
	}

	fmt.Printf("Found %d bounced email(s):\n\n", len(bounceEmails))

	// Track brokers to remove
	type bouncedBroker struct {
		email      string
		broker     *broker.Broker
		subject    string
		receivedAt time.Time
	}
	var bouncedBrokers []bouncedBroker

	for _, email := range bounceEmails {
		// Extract the bounced recipient
		bouncedRecipient := inbox.ExtractBouncedRecipient(&email)
		if bouncedRecipient == "" {
			fmt.Printf("⚠️  Could not extract bounced address from: %s\n", email.Subject)
			continue
		}

		// Find the broker
		b := brokerDB.FindByEmail(bouncedRecipient)
		if b == nil {
			fmt.Printf("⚠️  %s - not found in broker database\n", bouncedRecipient)
			continue
		}

		fmt.Printf("❌ %s\n", bouncedRecipient)
		fmt.Printf("   Broker: %s (%s)\n", b.Name, b.ID)
		fmt.Printf("   Subject: %s\n", truncateString(email.Subject, 60))
		fmt.Printf("   Date: %s\n", email.ReceivedAt.Format("2006-01-02"))
		fmt.Println()

		bouncedBrokers = append(bouncedBrokers, bouncedBroker{
			email:      bouncedRecipient,
			broker:     b,
			subject:    email.Subject,
			receivedAt: email.ReceivedAt,
		})
	}

	if len(bouncedBrokers) == 0 {
		fmt.Println("✓ No broker email addresses need to be removed")
		return nil
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if !remove {
		fmt.Printf("\n📊 Found %d broker(s) with invalid email addresses\n", len(bouncedBrokers))
		fmt.Println("Run with --remove to delete these brokers from the database")
		return nil
	}

	// Remove the brokers
	fmt.Printf("\n🗑️  Removing %d broker(s) from database...\n\n", len(bouncedBrokers))

	removed := 0
	for _, bb := range bouncedBrokers {
		if brokerDB.RemoveByEmail(bb.email) != nil {
			fmt.Printf("✓ Removed %s (%s)\n", bb.broker.Name, bb.email)
			removed++
		}
	}

	// Save with backup
	if err := brokerDB.SaveWithBackup(brokerPath); err != nil {
		return fmt.Errorf("failed to save broker database: %w", err)
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("✓ Removed %d broker(s) with invalid email addresses\n", removed)
	fmt.Printf("  Backup saved to: %s.bak\n", brokerPath)

	return nil
}
