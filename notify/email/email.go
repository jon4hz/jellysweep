package email

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"net/smtp"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/config"
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

// sendEmail sends an email using SMTP with SSL/TLS support
func (n *NotificationService) sendEmail(to, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", n.config.SMTPHost, n.config.SMTPPort)
	msg := n.formatMessage(n.config.FromEmail, to, subject, body)

	// Create TLS config
	tlsConfig := &tls.Config{
		ServerName:         n.config.SMTPHost,
		InsecureSkipVerify: n.config.InsecureSkipVerify,
	}

	var err error

	if n.config.UseSSL {
		// Use implicit SSL/TLS (connect with TLS from start, typically port 465)
		err = n.sendWithSSL(addr, msg, []string{to}, tlsConfig)
	} else if n.config.UseTLS {
		// Use STARTTLS (start plain then upgrade to TLS, typically port 587)
		err = n.sendWithSTARTTLS(addr, msg, []string{to}, tlsConfig)
	} else {
		// Send without encryption (plain SMTP, typically port 25)
		err = n.sendPlain(addr, msg, []string{to})
	}

	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	log.Info("Email notification sent successfully", "to", to, "subject", subject)
	return nil
}

// formatMessage formats the email message with proper headers
func (n *NotificationService) formatMessage(from, to, subject, body string) string {
	fromName := n.config.FromName
	if fromName == "" {
		fromName = "JellySweep"
	}

	headers := map[string]string{
		"From":         fmt.Sprintf("%s <%s>", fromName, from),
		"To":           to,
		"Subject":      subject,
		"MIME-Version": "1.0",
		"Content-Type": "text/html; charset=UTF-8",
	}

	var msg strings.Builder
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")
	msg.WriteString(body)

	return msg.String()
}

// sendWithSSL sends email using implicit SSL/TLS connection
func (n *NotificationService) sendWithSSL(addr, msg string, to []string, tlsConfig *tls.Config) error {
	// Create TLS connection
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to establish TLS connection: %w", err)
	}
	defer conn.Close()

	// Create SMTP client with TLS connection
	client, err := smtp.NewClient(conn, n.config.SMTPHost)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Quit()

	return n.authenticateAndSend(client, msg, to)
}

// sendWithSTARTTLS sends email using STARTTLS (opportunistic TLS)
func (n *NotificationService) sendWithSTARTTLS(addr, msg string, to []string, tlsConfig *tls.Config) error {
	// Create plain connection
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer client.Quit()

	// Start TLS if supported
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("failed to start TLS: %w", err)
		}
	} else {
		log.Warn("STARTTLS not supported by server, sending without encryption")
	}

	return n.authenticateAndSend(client, msg, to)
}

// sendPlain sends email without encryption
func (n *NotificationService) sendPlain(addr, msg string, to []string) error {
	// Create plain connection
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer client.Quit()

	return n.authenticateAndSend(client, msg, to)
}

// authenticateAndSend handles authentication and message sending
func (n *NotificationService) authenticateAndSend(client *smtp.Client, msg string, to []string) error {
	// Authenticate if credentials are provided
	if n.config.Username != "" && n.config.Password != "" {
		auth := smtp.PlainAuth("", n.config.Username, n.config.Password, n.config.SMTPHost)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}
	}

	// Set sender
	if err := client.Mail(n.config.FromEmail); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	// Set recipients
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("failed to set recipient %s: %w", addr, err)
		}
	}

	// Send message
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to get data writer: %w", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}
