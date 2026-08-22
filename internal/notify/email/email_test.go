package email

import (
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/stretchr/testify/require"
)

func sampleNotification() UserNotification {
	return UserNotification{
		UserEmail:     "alice@example.com",
		UserName:      "alice",
		JellysweepURL: "https://jellysweep.example.com",
		CleanupDate:   time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		MediaItems: []MediaItem{
			{Title: "Old Movie", MediaType: "movie", RequestedBy: "alice@example.com"},
			{Title: "Old Show", MediaType: "tv", RequestedBy: "alice@example.com"},
		},
	}
}

func TestGenerateEmailBodyRendersTemplate(t *testing.T) {
	n := New(&config.EmailConfig{Enabled: true})
	body, err := n.generateEmailBody(sampleNotification())
	require.NoError(t, err)
	require.Contains(t, body, "Old Movie")
	require.Contains(t, body, "Old Show")
	require.Contains(t, body, "https://jellysweep.example.com")
}

func TestSendCleanupNotificationSkipsWithoutSending(t *testing.T) {
	// None of these cases may attempt an SMTP connection: SMTPHost is unset,
	// so an attempt would surface as an error.
	t.Run("disabled", func(t *testing.T) {
		n := New(&config.EmailConfig{Enabled: false})
		require.NoError(t, n.SendCleanupNotification(sampleNotification()))
	})
	t.Run("empty recipient", func(t *testing.T) {
		n := New(&config.EmailConfig{Enabled: true})
		notification := sampleNotification()
		notification.UserEmail = ""
		require.NoError(t, n.SendCleanupNotification(notification))
	})
	t.Run("dry run", func(t *testing.T) {
		n := New(&config.EmailConfig{Enabled: true})
		notification := sampleNotification()
		notification.DryRun = true
		require.NoError(t, n.SendCleanupNotification(notification))
	})
}

func TestSendCleanupNotificationFailsWithoutSMTPServer(t *testing.T) {
	n := New(&config.EmailConfig{Enabled: true, SMTPHost: "127.0.0.1", SMTPPort: 1})
	require.Error(t, n.SendCleanupNotification(sampleNotification()))
}
