package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/notify/discord"
	"github.com/jon4hz/jellysweep/internal/notify/email"
	"github.com/jon4hz/jellysweep/internal/notify/ntfy"
	"github.com/spf13/cobra"
)

// notifyCmd is the parent for all notification test subcommands.
var notifyCmd = &cobra.Command{
	Use:   "notify",
	Short: "Send test notifications",
	Long:  `Send test notifications to verify your notification configuration.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(notifyCmd)
	initNotifyDiscordCmd()
	initNotifyEmailCmd()
	initNotifyNtfyCmd()
}

// ---------------------------------------------------------------------------
// notify discord
// ---------------------------------------------------------------------------

var notifyDiscordFlags struct {
	WebhookURL string
	DryRun     bool
}

func initNotifyDiscordCmd() {
	cmd := &cobra.Command{
		Use:   "discord",
		Short: "Send a test Discord notification",
		Long: `Send a sample Discord cleanup notification to verify your webhook configuration.

Fires a realistic embed with dummy media items so you can see how it looks in
your Discord server and iterate on the design.

Settings are read from your config file. Use --webhook-url to override the URL
without touching the config.`,
		Example: `jellysweep notify discord
jellysweep notify discord -c config.yml --webhook-url https://discord.com/api/webhooks/...
jellysweep notify discord -c config.yml --dry-run`,
		RunE: runNotifyDiscord,
	}
	cmd.Flags().StringVar(&notifyDiscordFlags.WebhookURL, "webhook-url", "", "Override the Discord webhook URL from config")
	cmd.Flags().BoolVar(&notifyDiscordFlags.DryRun, "dry-run", false, "Build the embed and print its fields without sending it")
	notifyCmd.AddCommand(cmd)
}

func runNotifyDiscord(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(rootCmdPersistentFlags.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	discordCfg := &config.DiscordConfig{
		Enabled:  true,
		Username: "Jellysweep",
	}
	if cfg.Discord != nil {
		discordCfg.WebhookURL = cfg.Discord.WebhookURL
		discordCfg.Username = cfg.Discord.Username
		discordCfg.AvatarURL = cfg.Discord.AvatarURL
	}
	if notifyDiscordFlags.WebhookURL != "" {
		discordCfg.WebhookURL = notifyDiscordFlags.WebhookURL
	}

	if !notifyDiscordFlags.DryRun && discordCfg.WebhookURL == "" {
		return fmt.Errorf("no Discord webhook URL configured — set discord.webhook_url in your config or pass --webhook-url")
	}

	client := discord.New(discordCfg)

	now := time.Now()
	cleanupDate := now.AddDate(0, 0, 7)
	notification := discord.UserNotification{
		MediaItems: []discord.MediaItem{
			{Title: "Breaking Bad", MediaType: "show", RequestedBy: "testuser", CleanupDate: cleanupDate},
			{Title: "Inception", MediaType: "movie", RequestedBy: "testuser2", CleanupDate: cleanupDate},
			{Title: "The Wire", MediaType: "show", RequestedBy: "testuser", CleanupDate: cleanupDate},
		},
		JellysweepURL: cfg.ServerURL,
		DryRun:        notifyDiscordFlags.DryRun,
	}

	if notifyDiscordFlags.DryRun {
		data, err := client.MarshalNotification(notification)
		if err != nil {
			return fmt.Errorf("failed to build Discord payload: %w", err)
		}
		fmt.Println(string(data))
		log.Info("DRY RUN: payload built, not sent")
		return nil
	}

	if err := client.SendCleanupNotification(notification); err != nil {
		return fmt.Errorf("failed to send test Discord notification: %w", err)
	}

	fmt.Println("Test Discord notification sent successfully!")
	return nil
}

// ---------------------------------------------------------------------------
// notify email
// ---------------------------------------------------------------------------

var notifyEmailFlags struct {
	To     string
	DryRun bool
}

func initNotifyEmailCmd() {
	cmd := &cobra.Command{
		Use:   "email",
		Short: "Send a test email notification",
		Long: `Send a sample email cleanup notification to verify your SMTP configuration.

Fires a realistic HTML email with dummy media items so you can preview exactly
how it will look in your mail client and iterate on the template.

SMTP settings are read from your config file. --to is required.`,
		Example: `jellysweep notify email -c config.yml --to you@example.com
jellysweep notify email -c config.yml --to you@example.com --dry-run`,
		RunE: runNotifyEmail,
	}
	cmd.Flags().StringVar(&notifyEmailFlags.To, "to", "", "Recipient email address (required)")
	cmd.Flags().BoolVar(&notifyEmailFlags.DryRun, "dry-run", false, "Log what would be sent without connecting to the SMTP server")
	if err := cmd.MarkFlagRequired("to"); err != nil {
		log.Fatalf("failed to mark --to as required: %v", err)
	}
	notifyCmd.AddCommand(cmd)
}

func runNotifyEmail(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(rootCmdPersistentFlags.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.Email == nil {
		return fmt.Errorf("no email configuration found in config file")
	}

	svc := email.New(cfg.Email)

	now := time.Now()
	notification := email.UserNotification{
		UserEmail: notifyEmailFlags.To,
		UserName:  "testuser",
		MediaItems: []email.MediaItem{
			{Title: "Breaking Bad", MediaType: "show", RequestedBy: "testuser"},
			{Title: "Inception", MediaType: "movie", RequestedBy: "testuser"},
			{Title: "The Wire", MediaType: "show", RequestedBy: ""},
		},
		CleanupDate:   now.AddDate(0, 0, 7),
		JellysweepURL: cfg.ServerURL,
		DryRun:        notifyEmailFlags.DryRun,
	}

	if err := svc.SendCleanupNotification(notification); err != nil {
		return fmt.Errorf("failed to send test email notification: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// notify ntfy
// ---------------------------------------------------------------------------

func initNotifyNtfyCmd() {
	cmd := &cobra.Command{
		Use:   "ntfy",
		Short: "Send a test ntfy deletion summary",
		Long: `Send a sample ntfy deletion summary to verify your ntfy configuration.

Fires a realistic summary message with dummy media items grouped by library
so you can see how it will appear in your ntfy client.

ntfy settings are read from your config file.`,
		Example: `jellysweep notify ntfy -c config.yml`,
		RunE:    runNotifyNtfy,
	}
	notifyCmd.AddCommand(cmd)
}

func runNotifyNtfy(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(rootCmdPersistentFlags.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.Ntfy == nil || !cfg.Ntfy.Enabled {
		return fmt.Errorf("ntfy is not enabled in config")
	}

	client := ntfy.NewClient(&ntfy.Config{
		Enabled:   cfg.Ntfy.Enabled,
		ServerURL: cfg.Ntfy.ServerURL,
		Topic:     cfg.Ntfy.Topic,
		Username:  cfg.Ntfy.Username,
		Password:  cfg.Ntfy.Password,
		Token:     cfg.Ntfy.Token,
	})

	libraries := map[string][]ntfy.MediaItem{
		"TV Shows": {
			{Title: "Breaking Bad", Type: "tv", Year: 2008},
			{Title: "The Wire", Type: "tv", Year: 2002},
		},
		"Movies": {
			{Title: "Inception", Type: "movie", Year: 2010},
		},
	}

	if err := client.SendDeletionSummary(context.Background(), 3, libraries); err != nil {
		return fmt.Errorf("failed to send test ntfy notification: %w", err)
	}

	fmt.Println("Test ntfy notification sent successfully!")
	return nil
}
