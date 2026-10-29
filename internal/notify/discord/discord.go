package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/config"
)

// Client handles Discord notifications for cleanup actions.
type Client struct {
	config *config.DiscordConfig
}

// MediaItem represents a media item that was marked for deletion.
type MediaItem struct {
	Title       string
	MediaType   string
	RequestedBy string    // display name of the requester
	DiscordID   string    // Discord user ID of the requester (from Jellyseerr)
	CleanupDate time.Time // when this item will be deleted
}

// UserNotification contains the data for a user's Discord notification.
type UserNotification struct {
	MediaItems    []MediaItem
	JellysweepURL string
	DryRun        bool
}

// New creates a new Discord notification client.
func New(cfg *config.DiscordConfig) *Client {
	return &Client{
		config: cfg,
	}
}

// MarshalNotification returns the indented JSON webhook payloads that would be sent
// for the given notification. Intended for dry-run previews.
func (c *Client) MarshalNotification(notification UserNotification) ([]byte, error) {
	payloads := BuildWebhookPayloads(
		notification.MediaItems,
		notification.JellysweepURL,
		c.config.Username,
		c.config.AvatarURL,
	)
	return json.MarshalIndent(payloads, "", "  ")
}

// shouldSend reports whether the client is configured to send notifications.
func (c *Client) shouldSend() bool {
	return c.config != nil && c.config.Enabled && c.config.WebhookURL != ""
}

// SendCleanupNotification sends Discord webhook notification(s) about media marked for deletion.
// Large item lists are automatically split across multiple messages to respect Discord's limits.
func (c *Client) SendCleanupNotification(notification UserNotification) error {
	if !c.shouldSend() {
		log.Debug("Discord notifications are disabled or not configured, skipping")
		return nil
	}

	if notification.DryRun {
		log.Debug("DRY RUN: Would send Discord notification",
			"media_count", len(notification.MediaItems))
		return nil
	}

	log.Debug("Sending Discord notification", "media_count", len(notification.MediaItems))

	payloads := BuildWebhookPayloads(
		notification.MediaItems,
		notification.JellysweepURL,
		c.config.Username,
		c.config.AvatarURL,
	)

	for i, payload := range payloads {
		body, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("discord: failed to marshal webhook payload %d: %w", i+1, err)
		}

		resp, err := http.Post(c.config.WebhookURL, "application/json", bytes.NewReader(body)) //nolint:noctx
		if err != nil {
			return fmt.Errorf("discord: webhook POST %d failed: %w", i+1, err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("discord: webhook %d returned non-2xx status: %s", i+1, resp.Status)
		}
	}

	log.Infof("Sent Discord cleanup notification (%d items, %d message(s))", len(notification.MediaItems), len(payloads))
	return nil
}
