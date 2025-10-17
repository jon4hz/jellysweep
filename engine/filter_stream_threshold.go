package engine

import (
	"context"
	"errors"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/streamystats"
)

// filterLastStreamThreshold filters out media items that have been streamed within the configured threshold.
func (e *Engine) filterLastStreamThreshold(ctx context.Context, mediaItems mediaItemsMap) (mediaItemsMap, error) {
	filteredItems := make(mediaItemsMap, 0)
	for lib, items := range mediaItems {
		for _, item := range items {
			lastStreamed, err := e.stats.GetItemLastPlayed(ctx, item.JellyfinID)
			if err != nil {
				if errors.Is(err, streamystats.ErrItemNotFound) {
					log.Warn("Item not found in StreamyStats", "jellyfinID", item.JellyfinID)
					log.Debug("Excluding item without streaming history", "jellyfinID", item.JellyfinID)
					continue
				}
				log.Error("Failed to get last streamed time for item", "jellyfinID", item.JellyfinID, "error", err)
				return nil, err
			}
			if lastStreamed.IsZero() {
				filteredItems[lib] = append(filteredItems[lib], item) // No last streamed time, mark for deletion
				continue
			}
			// Check if the last streamed time is older than the configured threshold
			libraryConfig := e.cfg.GetLibraryConfig(lib)
			if libraryConfig != nil && time.Since(lastStreamed) > time.Duration(libraryConfig.LastStreamThreshold)*24*time.Hour {
				log.Debugf("Including item %s - last streamed on %s", item.Title, lastStreamed.Format(time.RFC3339))
				filteredItems[lib] = append(filteredItems[lib], item)
				continue
			}
			log.Debugf("Excluding item %s due to recent stream: %s", item.Title, lastStreamed.Format(time.RFC3339))
		}
	}
	return filteredItems, nil
}
