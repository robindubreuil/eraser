package main

import (
	"fmt"

	"github.com/robindubreuil/eraser/internal/broker"
	"github.com/spf13/cobra"
)

func listBrokersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-brokers",
		Short: "List all data brokers in the database",
		Long:  "Show all data brokers that will receive removal requests.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runListBrokers()
		},
	}
}

func runListBrokers() error {
	brokerDB, err := broker.LoadFromFile(resolveBrokerPath())
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	fmt.Printf("📋 Data Brokers (%d total)\n", len(brokerDB.Brokers))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	for _, b := range brokerDB.Brokers {
		fmt.Printf("\n%s [%s]\n", b.Name, b.ID)
		fmt.Printf("  📧 %s\n", b.Email)
		if b.Website != "" {
			fmt.Printf("  🌐 %s\n", b.Website)
		}
		if b.OptOutURL != "" {
			fmt.Printf("  🔗 Opt-out: %s\n", b.OptOutURL)
		}
		fmt.Printf("  🌍 Region: %s\n", b.Region)
		if b.Category != "" {
			fmt.Printf("  📁 Category: %s\n", b.Category)
		}
	}

	return nil
}
