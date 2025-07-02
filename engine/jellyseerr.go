package engine

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
)

// filterRequestAgeThreshold filters out media items that have been requested within the configured threshold.
func (e *Engine) filterRequestAgeThreshold(ctx context.Context) {
	filteredItems := make(map[string][]MediaItem, 0)
	// Initialize user notifications map if not already done
	if e.data.userNotifications == nil {
		e.data.userNotifications = make(map[string][]MediaItem)
	}

	for lib, items := range e.data.mediaItems {
		for _, item := range items {
			requestInfo, err := e.jellyseerr.GetRequestInfo(ctx, item.TmdbId, string(item.MediaType))
			if err != nil {
				log.Errorf("Failed to get request info for item %s: %v", item.Title, err)
				continue
			}

			if requestInfo.RequestTime != nil {
				// check if the request time is longer ago than the configured threshold in days
				if time.Since(*requestInfo.RequestTime) > time.Duration(e.cfg.JellySweep.Libraries[lib].RequestAgeThreshold)*24*time.Hour {
					// Update the item with user information
					item.RequestedBy = requestInfo.UserEmail
					item.RequestDate = *requestInfo.RequestTime

					filteredItems[lib] = append(filteredItems[lib], item)

					// Track for email notifications
					if requestInfo.UserEmail != "" {
						e.data.userNotifications[requestInfo.UserEmail] = append(e.data.userNotifications[requestInfo.UserEmail], item)
						log.Debugf("Marking item %s for deletion, requested by %s on %s", item.Title, requestInfo.UserEmail, requestInfo.RequestTime.Format(time.RFC3339))
					}
				} else {
					log.Debugf("Excluding item %s due to recent request: %s by %s", item.Title, requestInfo.RequestTime.Format(time.RFC3339), requestInfo.UserEmail)
				}
			} else {
				// No request time, mark for deletion but no user to notify
				filteredItems[lib] = append(filteredItems[lib], item)
				log.Debugf("No request time for item %s, marking for deletion", item.Title)
			}
		}
	}
	e.data.mediaItems = filteredItems
}
