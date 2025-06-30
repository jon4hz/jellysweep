package engine

import (
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/notify/email"
)

// sendEmailNotifications sends email notifications to users about their media being marked for deletion
func (e *Engine) sendEmailNotifications() error {
	if e.emailSvc == nil {
		log.Debug("Email service not configured, skipping notifications")
		return nil
	}

	if len(e.data.userNotifications) == 0 {
		log.Debug("No user notifications to send")
		return nil
	}

	for userEmail, mediaItems := range e.data.userNotifications {
		if len(mediaItems) == 0 {
			continue
		}

		// Convert engine MediaItems to email MediaItems
		emailMediaItems := make([]email.MediaItem, 0, len(mediaItems))
		for _, item := range mediaItems {
			emailMediaItems = append(emailMediaItems, email.MediaItem{
				Title:       item.Title,
				MediaType:   string(item.MediaType),
				RequestedBy: item.RequestedBy,
				RequestDate: item.RequestDate,
			})
		}

		// Calculate cleanup date (current time + cleanup delay)
		cleanupDate := time.Now()
		if len(mediaItems) > 0 {
			// Use the cleanup delay from the first item's library
			for lib, libItems := range e.data.mediaItems {
				for _, libItem := range libItems {
					if libItem.RequestedBy == userEmail {
						if e.cfg.JellySweep.Libraries[lib] != nil {
							cleanupDate = cleanupDate.Add(time.Duration(e.cfg.JellySweep.Libraries[lib].CleanupDelay) * 24 * time.Hour)
						}
						break
					}
				}
				break
			}
		}

		notification := email.UserNotification{
			UserEmail:   userEmail,
			UserName:    userEmail, // Use email as name for now, could be enhanced
			MediaItems:  emailMediaItems,
			CleanupDate: cleanupDate,
			DryRun:      e.cfg.JellySweep.DryRun,
		}

		// TODO: remove
		notification.UserEmail = "me@jon4hz.io"

		if err := e.emailSvc.SendCleanupNotification(notification); err != nil {
			log.Errorf("Failed to send email notification to %s: %v", userEmail, err)
		} else {
			log.Infof("Sent cleanup notification to %s for %d media items", userEmail, len(emailMediaItems))
		}
	}

	return nil
}

// sendNtfyDeletionSummary sends a summary notification about media marked for deletion
func (e *Engine) sendNtfyDeletionSummary() error {
	if e.ntfySvc == nil {
		log.Debug("Ntfy service not configured, skipping deletion summary notification")
		return nil
	}

	if len(e.data.mediaItems) == 0 {
		log.Debug("No media items marked for deletion")
		return nil
	}

	// Calculate totals
	totalItems := 0
	libraries := make(map[string]int)

	for library, items := range e.data.mediaItems {
		count := len(items)
		if count > 0 {
			libraries[library] = count
			totalItems += count
		}
	}

	if totalItems == 0 {
		log.Debug("No media items to notify about")
		return nil
	}

	// Send the notification
	if err := e.ntfySvc.SendDeletionSummary(totalItems, libraries); err != nil {
		return fmt.Errorf("failed to send deletion summary notification: %w", err)
	}

	log.Infof("Sent deletion summary notification: %d items across %d libraries", totalItems, len(libraries))
	return nil
}
