package engine

import (
	"context"

	"github.com/charmbracelet/log"
)

// populateRequesterInfo populates the RequestedBy and RequestDate fields for media items using Jellyseerr data.
func (e *Engine) populateRequesterInfo(ctx context.Context) {
	if e.jellyseerr == nil {
		log.Debug("Jellyseerr client not available, skipping requester info population")
		return
	}

	for lib, items := range e.data.mediaItems {
		for i := range items {
			item := &items[i]

			requestInfo, err := e.jellyseerr.GetRequestInfo(ctx, item.TmdbId, string(item.MediaType))
			if err != nil {
				log.Debugf("Failed to get request info for item %s: %v", item.Title, err)
				// Continue processing other items
				continue
			}

			if requestInfo != nil && requestInfo.RequestTime != nil {
				item.RequestDate = *requestInfo.RequestTime
				item.RequestedBy = requestInfo.UserEmail
				log.Debugf("Populated requester info for %s: requested by %s on %s",
					item.Title, item.RequestedBy, item.RequestDate.Format("2006-01-02"))
			}
		}

		// Update the items in the map
		e.data.mediaItems[lib] = items
	}
}
