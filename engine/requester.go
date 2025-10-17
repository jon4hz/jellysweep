package engine

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/engine/arr"
)

// populateRequesterInfo populates the RequestedBy and RequestDate fields for media items using Jellyseerr data.
func (e *Engine) populateRequesterInfo(ctx context.Context, mediaItems []arr.MediaItem) []arr.MediaItem {
	if e.jellyseerr == nil {
		log.Debug("Jellyseerr client not available, skipping requester info population")
		return mediaItems
	}

	for i, item := range mediaItems {
		requestInfo, err := e.jellyseerr.GetRequestInfo(ctx, item.TmdbId, string(item.MediaType))
		if err != nil {
			log.Errorf("Failed to get request info for item %s: %v", item.Title, err)
			return nil // Don't fail the entire operation for individual item errors
		}
		if requestInfo == nil || requestInfo.RequestTime == nil {
			log.Debugf("No request info found for item %s", item.Title)
			continue
		}

		item.RequestDate = *requestInfo.RequestTime
		item.RequestedBy = requestInfo.UserEmail
		log.Debugf("Populated requester info for %s: requested by %s on %s",
			item.Title, item.RequestedBy, item.RequestDate.Format("2006-01-02"))

		// Update the items in the map
		mediaItems[i] = item
	}

	return mediaItems
}
