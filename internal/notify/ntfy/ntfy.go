package ntfy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/config"
)

// Client represents a ntfy notification client.
type Client struct {
	serverURL  string
	topic      string
	username   string
	password   string
	token      string
	httpClient *http.Client
}

// Message represents a ntfy message.
type Message struct {
	Topic    string            `json:"topic"`
	Title    string            `json:"title"`
	Message  string            `json:"message"`
	Priority int               `json:"priority,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Actions  []Action          `json:"actions,omitempty"`
	Extras   map[string]string `json:"extras,omitempty"`
}

// Action represents a ntfy action button.
type Action struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	URL    string `json:"url,omitempty"`
	Method string `json:"method,omitempty"`
}

// NewClient creates a new ntfy client.
func NewClient(cfg *config.NtfyConfig) *Client {
	// Validate server URL
	if cfg.ServerURL != "" {
		if _, err := url.Parse(cfg.ServerURL); err != nil {
			log.Error("invalid ntfy server URL", "error", err)
		}
	}

	return &Client{
		serverURL: cfg.ServerURL,
		topic:     cfg.Topic,
		username:  cfg.Username,
		password:  cfg.Password,
		token:     cfg.Token,
		httpClient: &http.Client{
			Timeout: config.TimeoutDuration(cfg.Timeout),
		},
	}
}

// SendMessage sends a message to ntfy.
func (c *Client) SendMessage(ctx context.Context, msg Message) error {
	if c.topic != "" {
		msg.Topic = c.topic
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.serverURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Markdown", "yes")

	// Authentication: Token takes precedence over username/password
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 400 {
		// Try to read response body for better error information
		var errorMsg strings.Builder
		if resp.Body != nil {
			buf := make([]byte, 256)
			if n, _ := resp.Body.Read(buf); n > 0 {
				errorMsg.WriteString(": ")
				errorMsg.Write(buf[:n])
			}
		}
		return fmt.Errorf("ntfy server returned status %d%s", resp.StatusCode, errorMsg.String())
	}

	log.Debug("Sent ntfy notification", "topic", msg.Topic, "title", msg.Title)
	return nil
}

// SendKeepRequest sends a notification about a new keep request.
func (c *Client) SendKeepRequest(ctx context.Context, mediaTitle, mediaType, username string) error {
	// Choose appropriate emoji based on media type
	emoji := "📺" //nolint:goconst
	if mediaType == "Movie" {
		emoji = "🎬" //nolint:goconst
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🛡️ **User:** %s\n", username)
	fmt.Fprintf(&b, "📋 **Type:** %s\n", mediaType)
	fmt.Fprintf(&b, "🎯 **Title:** %s\n\n", mediaTitle)
	b.WriteString("⚠️ Please review this keep request in the admin panel.")

	msg := Message{
		Title:    fmt.Sprintf("%s Keep Request", emoji),
		Message:  b.String(),
		Priority: 4, // High priority
		Tags:     []string{"warning", "jellysweep", "keep-request"},
	}

	return c.SendMessage(ctx, msg)
}

// MediaItem represents a media item for notifications.
type MediaItem struct {
	Title string
	Type  string // "movie" or "tv"
	Year  int32
}

// SendDeletionSummary sends a summary of media marked for deletion.
func (c *Client) SendDeletionSummary(ctx context.Context, totalItems int, libraries map[string][]MediaItem) error {
	if totalItems == 0 {
		log.Debug("No media marked for deletion, skipping ntfy notification")
		return nil
	}

	libraryDetails := make([]string, 0)
	mediaDetails := make([]string, 0)

	for library, items := range libraries {
		emoji := "📚"
		switch library {
		case "Movies":
			emoji = "🎬"
		case "TV Shows":
			emoji = "📺"
		}

		// Add library header to summary
		libraryDetails = append(libraryDetails, fmt.Sprintf("  %s **%s:** %d items", emoji, library, len(items)))

		// Add library section to detailed list
		mediaDetails = append(mediaDetails, fmt.Sprintf("\n%s **%s:**", emoji, library))

		// Add all media titles to detailed list
		for _, item := range items {
			mediaDetails = append(mediaDetails, fmt.Sprintf("  • %s (%d)", item.Title, item.Year))
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🗑️ **Total Items:** %d\n\n", totalItems)
	b.WriteString("📊 **Summary:**\n")
	for _, detail := range libraryDetails {
		fmt.Fprintf(&b, "%s\n", detail)
	}
	b.WriteString("\n📋 **Details:**")
	for _, detail := range mediaDetails {
		fmt.Fprintf(&b, "%s\n", detail)
	}
	b.WriteString("\n⏰ Media will be deleted after the cleanup delay period.")

	msg := Message{
		Title:    "🧹🪼 Cleanup Summary",
		Message:  b.String(),
		Priority: 4,
		Tags:     []string{"information", "jellysweep", "cleanup"},
	}

	return c.SendMessage(ctx, msg)
}

// SendDeletionCompletedSummary sends a summary of media that was actually deleted.
func (c *Client) SendDeletionCompletedSummary(ctx context.Context, totalItems int, libraries map[string][]MediaItem) error {
	if totalItems == 0 {
		log.Debug("No media was deleted, skipping ntfy notification")
		return nil
	}

	libraryDetails := make([]string, 0)
	mediaDetails := make([]string, 0)

	for library, items := range libraries {
		emoji := "📚"
		switch library {
		case "Movies":
			emoji = "🎬"
		case "TV Shows":
			emoji = "📺"
		}

		// Add library header to summary
		libraryDetails = append(libraryDetails, fmt.Sprintf("  %s **%s:** %d items", emoji, library, len(items)))

		// Add library section to detailed list
		mediaDetails = append(mediaDetails, fmt.Sprintf("\n%s **%s:**", emoji, library))

		// Add all media titles to detailed list
		for _, item := range items {
			mediaDetails = append(mediaDetails, fmt.Sprintf("  • %s (%d)", item.Title, item.Year))
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "✅ **Total Items Deleted:** %d\n\n", totalItems)
	b.WriteString("📊 **Summary:**\n")
	for _, detail := range libraryDetails {
		fmt.Fprintf(&b, "%s\n", detail)
	}
	b.WriteString("\n📋 **Deleted Media:**")
	for _, detail := range mediaDetails {
		fmt.Fprintf(&b, "%s\n", detail)
	}
	b.WriteString("\n🎉 Cleanup completed successfully!")

	msg := Message{
		Title:    "✅🪼 Cleanup Completed",
		Message:  b.String(),
		Priority: 4,
		Tags:     []string{"success", "jellysweep", "cleanup-completed"},
	}

	return c.SendMessage(ctx, msg)
}
