package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/engine/arr"
	"github.com/jon4hz/jellysweep/notify/email"
	"github.com/jon4hz/jellysweep/notify/ntfy"
)

// sendEmailNotifications sends email notifications to users about their media being marked for deletion.
func (e *Engine) sendEmailNotifications(mediaItems []arr.MediaItem) {
	if e.email == nil || !e.cfg.Email.Enabled {
		log.Debug("Email service not configured or disabled, skipping notifications")
		return
	}

	if len(e.data.userNotifications) == 0 {
		log.Debug("No user notifications to send")
		return
	}

	for userEmail, emailMediaItems := range e.data.userNotifications {
		if len(emailMediaItems) == 0 {
			continue
		}

		// Convert engine MediaItems to email MediaItems
		emailMediaItems := make([]email.MediaItem, 0, len(emailMediaItems))
		for _, item := range emailMediaItems {
			emailMediaItems = append(emailMediaItems, email.MediaItem{
				Title:       item.Title,
				MediaType:   item.MediaType,
				RequestedBy: item.RequestedBy,
				RequestDate: item.RequestDate,
			})
		}

		// Calculate cleanup date (current time + cleanup delay)
		cleanupDate := time.Now()
		if len(mediaItems) > 0 {
			// Use the cleanup delay from the first item's library
			for _, item := range mediaItems {
				if item.RequestedBy == userEmail {
					libraryConfig := e.cfg.GetLibraryConfig(item.LibraryName)
					if libraryConfig != nil {
						cleanupDate = cleanupDate.Add(time.Duration(libraryConfig.CleanupDelay) * 24 * time.Hour)
					}
					break
				}
			}
		}

		notification := email.UserNotification{
			UserEmail:     userEmail,
			UserName:      userEmail, // Use email as name for now, could be enhanced
			MediaItems:    emailMediaItems,
			CleanupDate:   cleanupDate,
			DryRun:        e.cfg.DryRun,
			JellysweepURL: e.cfg.ServerURL,
		}

		if err := e.email.SendCleanupNotification(notification); err != nil {
			log.Errorf("Failed to send email notification to %s: %v", userEmail, err)
		} else {
			log.Infof("Sent cleanup notification to %s for %d media items", userEmail, len(emailMediaItems))
		}
	}
}

// sendNtfyDeletionSummary sends a summary notification about media marked for deletion.
func (e *Engine) sendNtfyDeletionSummary(ctx context.Context, mediaItems []arr.MediaItem) error {
	if e.ntfy == nil {
		log.Debug("Ntfy service not configured, skipping deletion summary notification")
		return nil
	}

	if len(mediaItems) == 0 {
		log.Debug("No media items marked for deletion")
		return nil
	}

	// Calculate totals and prepare media items for notification
	totalItems := len(mediaItems)
	if totalItems == 0 {
		log.Debug("No media items to notify about")
		return nil
	}

	libraries := make(map[string][]ntfy.MediaItem)
	for _, item := range mediaItems {
		mediaType := "tv"
		if item.MediaType == models.MediaTypeMovie {
			mediaType = "movie"
		}

		if _, exists := libraries[item.LibraryName]; !exists {
			libraries[item.LibraryName] = make([]ntfy.MediaItem, 0)
		}

		libraries[item.LibraryName] = append(libraries[item.LibraryName], ntfy.MediaItem{
			Title: item.Title,
			Type:  mediaType,
			Year:  item.Year,
		})
	}

	// Send the notification
	if err := e.ntfy.SendDeletionSummary(ctx, totalItems, libraries); err != nil {
		return fmt.Errorf("failed to send deletion summary notification: %w", err)
	}

	log.Infof("Sent deletion summary notification: %d items across %d libraries", totalItems, len(libraries))
	return nil
}

// sendNtfyDeletionCompletedNotification sends a notification summary of media that was actually deleted.
func (e *Engine) sendNtfyDeletionCompletedNotification(ctx context.Context, deletedItems map[string][]arr.MediaItem) error {
	if e.ntfy == nil {
		log.Debug("Ntfy service not configured, skipping deletion completed notification")
		return nil
	}

	if len(deletedItems) == 0 {
		log.Debug("No media items were deleted")
		return nil
	}

	// Calculate totals and prepare media items for notification
	totalItems := 0
	libraries := make(map[string][]ntfy.MediaItem)

	for library, items := range deletedItems {
		if len(items) > 0 {
			totalItems += len(items)

			// Convert engine MediaItems to ntfy MediaItems
			ntfyItems := make([]ntfy.MediaItem, 0, len(items))
			for _, item := range items {
				mediaType := "tv"
				if item.MediaType == models.MediaTypeMovie {
					mediaType = "movie"
				}

				ntfyItems = append(ntfyItems, ntfy.MediaItem{
					Title: item.Title,
					Type:  mediaType,
					Year:  item.Year,
				})
			}
			libraries[library] = ntfyItems
		}
	}

	if totalItems == 0 {
		log.Debug("No media items to notify about")
		return nil
	}

	// Send the notification
	if err := e.ntfy.SendDeletionCompletedSummary(ctx, totalItems, libraries); err != nil {
		return fmt.Errorf("failed to send deletion completed notification: %w", err)
	}

	log.Infof("Sent deletion completed notification: %d items across %d libraries", totalItems, len(libraries))
	return nil
}
