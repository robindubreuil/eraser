package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robindubreuil/eraser/internal/config"
	"github.com/spf13/cobra"
)

var (
	cfgFile    string
	brokerFile string
	dryRun     bool
)

func resolveBrokerPath() string {
	if brokerFile != "" {
		return brokerFile
	}
	if _, err := os.Stat("data/brokers.yaml"); err == nil {
		return "data/brokers.yaml"
	}
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "data", "brokers.yaml")
}

func resolveConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	return config.DefaultConfigPath()
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "eraser",
		Short: "Eraser - Automated data broker removal requests",
		Long: `Eraser is an open-source tool that automates sending data removal
requests to data brokers, helping you protect your privacy.

It supports GDPR, CCPA, and generic removal request templates, and can
send via Gmail SMTP.`,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.eraser/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&brokerFile, "brokers", "", "broker database file (default is ./data/brokers.yaml)")

	// Add commands
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(sendCmd())
	rootCmd.AddCommand(serveCmd())
	rootCmd.AddCommand(listBrokersCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(addBrokerCmd())
	rootCmd.AddCommand(monitorCmd())
	rootCmd.AddCommand(pipelineCmd())
	rootCmd.AddCommand(fillCmd())
	rootCmd.AddCommand(confirmCmd())
	rootCmd.AddCommand(cleanupBouncesCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func prompt(reader *bufio.Reader, message string) string {
	fmt.Print(message)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(input)
}

// truncateURL truncates a URL to the specified length for display
func truncateURL(url string, maxLen int) string {
	if len(url) <= maxLen {
		return url
	}
	return url[:maxLen-3] + "..."
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
