package email

import (
	"bytes"
	"crypto/tls"
	"embed"
	"fmt"
	"html/template"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/config"
	mail "github.com/xhit/go-simple-mail/v2"
)

// NotificationService handles email notifications for cleanup actions.
type NotificationService struct {
	config *config.EmailConfig
}

// MediaItem represents a media item that was marked for deletion.
type MediaItem struct {
	Title       string
	MediaType   string
	RequestedBy string
}

// UserNotification contains the data for a user's notification email.
type UserNotification struct {
	UserEmail     string
	UserName      string
	MediaItems    []MediaItem
	CleanupDate   time.Time
	JellysweepURL string
	DryRun        bool
}

// New creates a new email notification service.
func New(cfg *config.EmailConfig) *NotificationService {
	return &NotificationService{
		config: cfg,
	}
}

// SendCleanupNotification sends an email notification to users about their media being marked for deletion.
func (n *NotificationService) SendCleanupNotification(notification UserNotification) error {
	if !n.config.Enabled {
		log.Debug("Email notifications are disabled, skipping notification")
		return nil
	}

	if notification.UserEmail == "" {
		log.Warn("User email is empty, skipping notification", "user", notification.UserName)
		return nil
	}

	subject := fmt.Sprintf("[Jellysweep] Media Cleanup Notification - %d items affected", len(notification.MediaItems))

	// In dry run mode, only log what would be sent
	if notification.DryRun {
		log.Debug("DRY RUN: Would send email notification",
			"to", notification.UserEmail,
			"subject", subject,
			"media_count", len(notification.MediaItems))
		return nil
	}

	body, err := n.generateEmailBody(notification)
	if err != nil {
		return fmt.Errorf("failed to generate email body: %w", err)
	}

	return n.sendEmail(notification.UserEmail, subject, body)
}

//go:embed templates/*.html
var templatesFS embed.FS

// generateEmailBody creates the HTML email body.
func (n *NotificationService) generateEmailBody(notification UserNotification) (string, error) {
	t, err := template.New("").ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "email.html", notification); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// sendEmail sends an email using go-simple-mail library.
func (n *NotificationService) sendEmail(to, subject, body string) error {
	// Create SMTP server configuration
	server := mail.NewSMTPClient()
	server.Host = n.config.SMTPHost
	server.Port = n.config.SMTPPort
	server.Username = n.config.Username
	server.Password = n.config.Password

	// Configure encryption
	if n.config.UseSSL {
		server.Encryption = mail.EncryptionSSLTLS
	} else if n.config.UseTLS {
		server.Encryption = mail.EncryptionSTARTTLS
	} else {
		server.Encryption = mail.EncryptionNone
	}

	// Configure TLS settings
	if n.config.InsecureSkipVerify {
		server.TLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}

	// Keep connection alive for sending multiple emails if needed
	server.KeepAlive = false
	server.ConnectTimeout = 10 * time.Second
	server.SendTimeout = 10 * time.Second

	// Create SMTP client
	smtpClient, err := server.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer func() {
		if closeErr := smtpClient.Close(); closeErr != nil {
			log.Warn("Failed to close SMTP client", "error", closeErr)
		}
	}()

	// Create email
	email := mail.NewMSG()

	// Set sender
	fromName := n.config.FromName
	if fromName == "" {
		fromName = "Jellysweep"
	}
	email.SetFrom(fmt.Sprintf("%s <%s>", fromName, n.config.FromEmail))

	email.AddTo(to)

	// Set subject
	email.SetSubject(subject)

	// Set HTML body
	email.SetBody(mail.TextHTML, body)

	// Send email
	if err := email.Send(smtpClient); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	log.Info("Email notification sent successfully", "to", to, "subject", subject)
	return nil
}
