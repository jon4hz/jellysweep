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

// NotificationService handles email notifications for cleanup actions.
type NotificationService struct {
	config *config.EmailConfig
}

// MediaItem represents a media item that was marked for deletion.
type MediaItem struct {
	Title       string
	MediaType   string
	RequestedBy string
	RequestDate time.Time
}

// UserNotification contains the data for a user's notification email.
type UserNotification struct {
	UserEmail     string
	UserName      string
	MediaItems    []MediaItem
	CleanupDate   time.Time
	JellySweepURL string
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

	subject := fmt.Sprintf("[JellySweep] Media Cleanup Notification - %d items affected", len(notification.MediaItems))

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

// generateEmailBody creates the HTML email body.
func (n *NotificationService) generateEmailBody(notification UserNotification) (string, error) {
	tmpl := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>JellySweep Media Cleanup Notification</title>
    <style>
        @import url('https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&display=swap');
        
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: 'Inter', system-ui, sans-serif;
            background-color: #0d1117;
            color: #f3f4f6;
            line-height: 1.6;
            padding: 20px;
            min-height: 100vh;
        }
        
        .container {
            max-width: 600px;
            margin: 0 auto;
            background-color: #111827;
            border: 1px solid #1f2937;
            border-radius: 8px;
            box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.5);
            overflow: hidden;
        }
        
        .header {
            background-color: #1f2937;
            border-bottom: 1px solid #374151;
            padding: 24px;
        }
        
        .header-brand {
            display: flex;
            align-items: center;
            margin-bottom: 16px;
        }
        
        .brand-icon {
            width: 32px;
            height: 32px;
            background-color: #4f46e5;
            border-radius: 8px;
            display: flex;
            align-items: center;
            justify-content: center;
            margin-right: 12px;
        }
        
        .brand-name {
            font-size: 20px;
            font-weight: 600;
            color: #f3f4f6;
        }
        
        .header h2 {
            font-size: 24px;
            font-weight: 700;
            color: #f3f4f6;
            margin-bottom: 8px;
        }
        
        .header p {
            color: #d1d5db;
            font-size: 16px;
        }
        
        .content {
            padding: 24px;
        }
        
        .dry-run-notice {
            background-color: #1e40af;
            border: 1px solid #3b82f6;
            color: #dbeafe;
            padding: 16px;
            border-radius: 8px;
            margin-bottom: 24px;
            display: flex;
            align-items: center;
        }
        
        .dry-run-notice::before {
            content: "ℹ";
            font-weight: bold;
            margin-right: 8px;
            font-size: 18px;
        }
        
        .description {
            color: #d1d5db;
            font-size: 16px;
            margin-bottom: 24px;
        }
        
        .media-section {
            background-color: #1f2937;
            border: 1px solid #374151;
            border-radius: 8px;
            padding: 20px;
            margin-bottom: 24px;
        }
        
        .media-section h3 {
            font-size: 18px;
            font-weight: 600;
            color: #f3f4f6;
            margin-bottom: 16px;
            display: flex;
            align-items: center;
        }
        
        .media-section h3::before {
            content: "📁";
            margin-right: 8px;
        }
        
        .media-item {
            background-color: #111827;
            border: 1px solid #374151;
            border-radius: 6px;
            padding: 16px;
            margin-bottom: 12px;
        }
        
        .media-item:last-child {
            margin-bottom: 0;
        }
        
        .media-title {
            font-weight: 600;
            font-size: 16px;
            color: #f3f4f6;
            margin-bottom: 8px;
        }
        
        .media-details {
            font-size: 14px;
            color: #9ca3af;
            display: flex;
            flex-wrap: wrap;
            gap: 16px;
        }
        
        .media-detail-item {
            display: flex;
            align-items: center;
        }
        
        .media-detail-item::before {
            content: "•";
            margin-right: 8px;
            color: #6b7280;
        }
        
        .media-detail-item:first-child::before {
            content: none;
        }
        
        .warning-notice {
            background-color: #dc2626;
            border: 1px solid #ef4444;
            color: #fecaca;
            padding: 16px;
            border-radius: 8px;
            margin-bottom: 24px;
            display: flex;
            align-items: flex-start;
        }
        
        .warning-notice::before {
            content: "⚠";
            font-weight: bold;
            margin-right: 8px;
            font-size: 18px;
            flex-shrink: 0;
        }
        
        .warning-content {
            flex: 1;
        }
        
        .warning-content strong {
            display: block;
            margin-bottom: 4px;
            font-weight: 600;
        }
        
        .footer {
            background-color: #1f2937;
            border-top: 1px solid #374151;
            padding: 20px 24px;
            text-align: center;
        }
        
        .footer p {
            color: #9ca3af;
            font-size: 14px;
            margin-bottom: 8px;
        }
        
        .footer p:last-child {
            margin-bottom: 0;
        }
        
        .footer-logo {
            color: #6b7280;
            font-size: 12px;
            margin-top: 16px;
        }
        
        .jellysweep-link {
            display: inline-flex;
            align-items: center;
            background-color: #4f46e5;
            color: #ffffff !important;
            text-decoration: none;
            padding: 8px 16px;
            border-radius: 6px;
            font-weight: 500;
            font-size: 14px;
            transition: background-color 0.2s ease;
        }
        
        .jellysweep-link:hover {
            background-color: #4338ca;
            text-decoration: none;
        }
        
        .jellysweep-link-icon {
            width: 16px;
            height: 16px;
            margin-right: 6px;
            border-radius: 4px;
        }
        
        .brand-icon-img {
            width: 24px;
            height: 24px;
            border-radius: 6px;
        }
        
        /* Responsive design */
        @media (max-width: 640px) {
            body {
                padding: 12px;
            }
            
            .header, .content, .footer {
                padding: 16px;
            }
            
            .media-details {
                flex-direction: column;
                gap: 8px;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div class="header-brand">
                <div class="brand-icon">
                    {{if .JellySweepURL}}
                    <img src="{{.JellySweepURL}}/static/jellysweep.png" alt="🧹" class="brand-icon-img" />
                    {{else}}
                    🧹
                    {{end}}
                </div>
                <div class="brand-name">JellySweep</div>
            </div>
            <h2>Media Cleanup Notification</h2>
            <p>Hello {{.UserName}},</p>
        </div>

        <div class="content">
            <div class="description">
                The following media items you requested have been marked for deletion:
            </div>

            <div class="media-section">
                <h3>Media Items ({{len .MediaItems}} total)</h3>
                {{range .MediaItems}}
                <div class="media-item">
                    <div class="media-title">{{.Title}}</div>
                    <div class="media-details">
                        <div class="media-detail-item">{{.MediaType}}</div>
                        <div class="media-detail-item">Requested {{.RequestDate.Format "January 2, 2006"}}</div>
                    </div>
                </div>
                {{end}}
            </div>
            <div class="warning-notice">
                <div class="warning-content">
                    <strong>Action Required</strong>
                    These items will be permanently deleted on {{.CleanupDate.Format "January 2, 2006"}}. 
                    If you wish to keep any of these items, please submit a request using the link below:
                    <br><br>
                    {{if .JellySweepURL}}
                    <a href="{{.JellySweepURL}}" target="_blank" class="jellysweep-link">
                        <img src="{{.JellySweepURL}}/static/jellysweep.png" alt="🧹" class="jellysweep-link-icon" />
                        Open JellySweep
                    </a>
                    {{else}}
                    Please contact your administrator.
                    {{end}}
                </div>
            </div>
        </div>

        <div class="footer">
            <p>This notification was sent by JellySweep automated cleanup system.</p>
            <p>If you have any questions, please contact your administrator.</p>
            <div class="footer-logo">
                Powered by JellySweep
            </div>
        </div>
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
