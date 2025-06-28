package engine

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
)

// filterRequestAgeThreshold filters out media items that have been requested within the configured threshold.
func (e *Engine) filterRequestAgeThreshold(ctx context.Context) error {
	filteredItems := make(map[string][]MediaItem, 0)
	for lib, items := range e.data.mediaItems {
		for _, item := range items {
			requestTime, err := e.jellyseerr.GetRequestTime(ctx, item.TmdbId, string(item.MediaType))
			if err != nil {
				log.Errorf("Failed to get request time for item %s: %v", item.Title, err)
				continue
			}
			if requestTime != nil {
				// check if the request time is longer ago than the configured threshold in days
				if time.Since(*requestTime) > time.Duration(e.cfg.Jellysweep.Libraries[lib].RequestAgeThreshold)*24*time.Hour {
					filteredItems[lib] = append(filteredItems[lib], item)
				} else {
					log.Debugf("Excluding item %s due to recent request: %s", item.Title, requestTime.Format(time.RFC3339))
				}
			} else {
				// No request time, mark for deletion
				filteredItems[lib] = append(filteredItems[lib], item)
				log.Debugf("No request time for item %s, marking for deletion", item.Title)
			}
		}
	}
	e.data.mediaItems = filteredItems
	return nil
}
