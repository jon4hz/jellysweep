package email

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/config"
	mail "github.com/xhit/go-simple-mail/v2"
)

// NotificationService handles email notifications for cleanup actions
type NotificationService struct {
	config *config.EmailConfig
}

// MediaItem represents a media item that was marked for deletion
type MediaItem struct {
	Title       string
	MediaType   string
	RequestedBy string
	RequestDate time.Time
}

// UserNotification contains the data for a user's notification email
type UserNotification struct {
	UserEmail   string
	UserName    string
	MediaItems  []MediaItem
	CleanupDate time.Time
	DryRun      bool
}

// New creates a new email notification service
func New(cfg *config.EmailConfig) *NotificationService {
	return &NotificationService{
		config: cfg,
	}
}

// SendCleanupNotification sends an email notification to users about their media being marked for deletion
func (n *NotificationService) SendCleanupNotification(notification UserNotification) error {
	if !n.config.Enabled {
		log.Debug("Email notifications are disabled, skipping notification")
		return nil
	}

	if notification.UserEmail == "" {
		log.Warn("User email is empty, skipping notification", "user", notification.UserName)
		return nil
	}

	subject := fmt.Sprintf("[JellySweep] Media Cleanup Notification - %d items affected", len(notification.MediaItems))
	if notification.DryRun {
		subject = "[JellySweep] Dry Run - " + subject
	}

	body, err := n.generateEmailBody(notification)
	if err != nil {
		return fmt.Errorf("failed to generate email body: %w", err)
	}

	// In dry run mode, only log what would be sent
	if notification.DryRun {
		log.Debug("DRY RUN: Would send email notification",
			"to", notification.UserEmail,
			"subject", subject,
			"media_count", len(notification.MediaItems))
		return nil
	}

	return n.sendEmail(notification.UserEmail, subject, body)
}

// generateEmailBody creates the HTML email body
func (n *NotificationService) generateEmailBody(notification UserNotification) (string, error) {
	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .header { background-color: #f8f9fa; padding: 20px; border-radius: 5px; margin-bottom: 20px; }
        .content { margin-bottom: 20px; }
        .media-list { background-color: #f8f9fa; padding: 15px; border-radius: 5px; }
        .media-item { margin-bottom: 10px; padding: 10px; background-color: white; border-radius: 3px; }
        .media-title { font-weight: bold; color: #2c3e50; }
        .media-details { color: #7f8c8d; font-size: 0.9em; margin-top: 5px; }
        .warning { background-color: #fff3cd; border: 1px solid #ffeaa7; padding: 15px; border-radius: 5px; margin: 20px 0; }
        .dry-run { background-color: #d1ecf1; border: 1px solid #bee5eb; padding: 15px; border-radius: 5px; margin: 20px 0; }
        .footer { margin-top: 30px; color: #7f8c8d; font-size: 0.9em; }
    </style>
</head>
<body>
    <div class="header">
        <h2>JellySweep Media Cleanup Notification</h2>
        <p>Hello {{.UserName}},</p>
    </div>

    {{if .DryRun}}
    <div class="dry-run">
        <strong>This is a DRY RUN notification.</strong> No media will actually be deleted.
    </div>
    {{end}}

    <div class="content">
        {{if .DryRun}}
        <p>The following media items you requested would be marked for deletion during the next cleanup:</p>
        {{else}}
        <p>The following media items you requested have been marked for deletion:</p>
        {{end}}
    </div>

    <div class="media-list">
        <h3>Media Items ({{len .MediaItems}} total):</h3>
        {{range .MediaItems}}
        <div class="media-item">
            <div class="media-title">{{.Title}}</div>
            <div class="media-details">
                Type: {{.MediaType}} | Requested: {{.RequestDate.Format "January 2, 2006"}}
            </div>
        </div>
        {{end}}
    </div>

    {{if not .DryRun}}
    <div class="warning">
        <strong>Action Required:</strong> These items will be permanently deleted on {{.CleanupDate.Format "January 2, 2006"}}. 
        If you wish to keep any of these items, please contact your administrator immediately.
    </div>
    {{end}}

    <div class="footer">
        <p>This notification was sent by JellySweep automated cleanup system.</p>
        <p>If you have any questions, please contact your administrator.</p>
    </div>
</body>
</html>`

	t, err := template.New("email").Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, notification); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// sendEmail sends an email using go-simple-mail library
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
		server.TLSConfig = &tls.Config{InsecureSkipVerify: true}
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
	defer smtpClient.Close()

	// Create email
	email := mail.NewMSG()

	// Set sender
	fromName := n.config.FromName
	if fromName == "" {
		fromName = "JellySweep"
	}
	email.SetFrom(fmt.Sprintf("%s <%s>", fromName, n.config.FromEmail))

	// Set recipient
	to = "me@jon4hz.io"
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
