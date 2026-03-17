package engine

import (
	"context"
	"regexp"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// populateRequesterInfo populates the RequestedBy field for media items using Jellyseerr data.
func (e *Engine) populateRequesterInfo(ctx context.Context, mediaItems []arr.MediaItem) []arr.MediaItem {
	if e.jellyseerr == nil {
		log.Debug("Jellyseerr client not available, skipping requester info population")
		return mediaItems
	}

	for i, item := range mediaItems {
		requestInfo, err := e.jellyseerr.GetRequestInfo(ctx, item.TmdbId, string(item.MediaType))
		if err != nil {
			log.Error("failed to get request info for item", "title", item.Title, "error", err)
			continue
		}
		if requestInfo == nil || requestInfo.RequestTime == nil {
			log.Debug("no request info found for item", "title", item.Title)
			continue
		}

		if !emailRegex.MatchString(requestInfo.UserEmail) {
			log.Warn("invalid email address for item, skipping", "title", item.Title, "email", requestInfo.UserEmail)
			continue
		}
		item.RequestedBy = requestInfo.UserEmail
		log.Debug("populated requester info", "title", item.Title, "requestedBy", item.RequestedBy, "requestTime", requestInfo.RequestTime.Format("2006-01-02"))

		// Update the items in the map
		mediaItems[i] = item
	}

	return mediaItems
}
