package engine

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/pkg/jellyseerr"
)

// populateRequesterInfo populates the RequestedBy field for media items using Jellyseerr data.
func (e *Engine) populateRequesterInfo(ctx context.Context, mediaItems []arr.MediaItem) []arr.MediaItem {
	if e.jellyseerr == nil {
		log.Debug("Jellyseerr client not available, skipping requester info population")
		return mediaItems
	}

	userSettingsCache := make(map[int]*jellyseerr.RequesterNotificationSettings)

	for i, item := range mediaItems {
		requestInfo, err := e.jellyseerr.GetRequestInfo(ctx, item.TmdbId, string(item.MediaType))
		if err != nil {
			log.Errorf("Failed to get request info for item %s: %v", item.Title, err)
			continue
		}
		if requestInfo == nil || requestInfo.RequestTime == nil {
			log.Debugf("No request info found for item %s", item.Title)
			continue
		}

		if _, ok := userSettingsCache[requestInfo.UserID]; !ok {
			settings, err := e.jellyseerr.GetUserNotificationSettings(ctx, requestInfo.UserID)
			if err != nil {
				log.Debugf("Failed to get user notification settings for user %d: %v", requestInfo.UserID, err)
			} else {
				userSettingsCache[requestInfo.UserID] = settings
			}
		}
		if settings := userSettingsCache[requestInfo.UserID]; settings != nil {
			requestInfo.NotificationSettings = *settings
		}

		item.RequestedBy = requestInfo
		log.Debugf("Populated requester info for %s: requested by %s on %s",
			item.Title, requestInfo.UserEmail, requestInfo.RequestTime.Format("2006-01-02"))

		// Update the items in the map
		mediaItems[i] = item
	}

	return mediaItems
}
