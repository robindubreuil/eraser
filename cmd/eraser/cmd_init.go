package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/robindubreuil/eraser/internal/config"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration interactively",
		Long:  "Create a new configuration file with your personal information and email settings.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runInit()
		},
	}
}

func runInit() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("🔐 Eraser Configuration Setup")
	fmt.Println("==============================")
	fmt.Println()

	cfg := &config.Config{}

	// Profile
	fmt.Println("📋 Personal Information (used in removal requests)")
	fmt.Println()

	cfg.Profile.FirstName = prompt(reader, "First name: ")
	cfg.Profile.LastName = prompt(reader, "Last name: ")
	cfg.Profile.Email = prompt(reader, "Email address: ")
	cfg.Profile.Address = prompt(reader, "Street address (optional): ")
	cfg.Profile.City = prompt(reader, "City (optional): ")
	cfg.Profile.State = prompt(reader, "State/Province (optional): ")
	cfg.Profile.ZipCode = prompt(reader, "ZIP/Postal code (optional): ")
	cfg.Profile.Country = prompt(reader, "Country (optional): ")
	cfg.Profile.Phone = prompt(reader, "Phone number (optional): ")

	fmt.Println()
	fmt.Println("📧 Email Settings")
	fmt.Println()

	cfg.Email.Provider = "smtp"
	cfg.Email.From = cfg.Profile.Email

	fmt.Println()
	fmt.Println("Gmail SMTP Configuration:")
	fmt.Println("  (See https://support.google.com/accounts/answer/185833 for app password setup)")
	fmt.Println()
	cfg.Email.SMTP.Host = "smtp.gmail.com"
	cfg.Email.SMTP.Port = 465
	cfg.Email.SMTP.UseTLS = true
	cfg.Email.SMTP.Username = prompt(reader, "  Gmail address: ")
	cfg.Email.SMTP.Password = prompt(reader, "  App password (16-character code): ")

	fmt.Println()
	fmt.Println("Options")
	fmt.Println()

	templateChoice := prompt(reader, "Template mode (auto/gdpr/gdpr-fr/ccpa/generic) [auto]: ")
	if templateChoice == "" {
		templateChoice = "auto"
	}
	cfg.Options.Template = templateChoice

	localeChoice := prompt(reader, "Locale (fr/en/de/es/it/nl/...) [auto-detect from country]: ")
	if localeChoice != "" {
		cfg.Options.Locale = localeChoice
	}
	cfg.Options.RateLimitMs = 2000

	configPath := resolveConfigPath()
	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ Configuration saved to: %s\n", configPath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Review and edit the config file if needed")
	fmt.Println("  2. Run 'eraser list-brokers' to see available brokers")
	fmt.Println("  3. Run 'eraser send --dry-run' to preview emails")
	fmt.Println("  4. Run 'eraser send' to send removal requests")

	return nil
}
