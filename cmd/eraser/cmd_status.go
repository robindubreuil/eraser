package main

import (
	"fmt"

	"github.com/robindubreuil/eraser/internal/history"
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show removal request history and statistics",
		Long:  "Display recent removal requests and overall statistics.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runStatus(limit)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Number of recent requests to show")

	return cmd
}

func runStatus(limit int) error {
	store, err := history.NewStore(history.DefaultDBPath())
	if err != nil {
		return fmt.Errorf("failed to open history: %w", err)
	}
	defer store.Close()

	// Get overall stats
	total, sent, failed, err := store.GetStats()
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	// Get monthly stats
	monthlySent, monthlyFailed, err := store.GetMonthlyStats()
	if err != nil {
		return fmt.Errorf("failed to get monthly stats: %w", err)
	}

	fmt.Println("📊 Eraser Statistics")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("All Time:")
	fmt.Printf("  Total requests: %d\n", total)
	fmt.Printf("  Sent: %d\n", sent)
	fmt.Printf("  Failed: %d\n", failed)
	fmt.Println()
	fmt.Println("This Month:")
	fmt.Printf("  Sent: %d\n", monthlySent)
	fmt.Printf("  Failed: %d\n", monthlyFailed)

	// Get recent requests
	records, err := store.GetRecentRequests(limit)
	if err != nil {
		return fmt.Errorf("failed to get recent requests: %w", err)
	}

	if len(records) > 0 {
		fmt.Println()
		fmt.Printf("📜 Recent Requests (last %d)\n", limit)
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

		for _, r := range records {
			status := "✅"
			if r.Status == history.StatusFailed {
				status = "❌"
			}
			fmt.Printf("%s %s - %s (%s)\n",
				status,
				r.SentAt.Format("2006-01-02 15:04"),
				r.BrokerName,
				r.Template,
			)
			if r.Error != "" {
				fmt.Printf("   Error: %s\n", r.Error)
			}
		}
	}

	return nil
}
