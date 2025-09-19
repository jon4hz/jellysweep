package engine

import (
	"context"
	"errors"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/engine/arr"
	"github.com/jon4hz/jellysweep/streamystats"
)

// filterLastStreamThreshold filters out media items that have been streamed within the configured threshold.
func (e *Engine) filterLastStreamThreshold(ctx context.Context) error {
	filteredItems := make(map[string][]arr.MediaItem, 0)
	for lib, items := range e.data.mediaItems {
		for _, item := range items {
			lastStreamed, err := e.stats.GetItemLastPlayed(ctx, item.JellyfinID)
			if err != nil {
				if errors.Is(err, streamystats.ErrItemNotFound) {
					log.Warn("Item not found in StreamyStats", "item", item.JellyfinID)
					// filteredItems[lib] = append(filteredItems[lib], item) // Item not found, mark for deletion
					continue
				}
				log.Error("Failed to get last streamed time for item", "item", item.JellyfinID, "error", err)
				return err
			}
			if lastStreamed.IsZero() {
				filteredItems[lib] = append(filteredItems[lib], item) // No last streamed time, mark for deletion
				continue
			}
			// Check if the last streamed time is older than the configured threshold
			libraryConfig := e.cfg.GetLibraryConfig(lib)
			if libraryConfig != nil && time.Since(lastStreamed) > time.Duration(libraryConfig.LastStreamThreshold)*24*time.Hour {
				filteredItems[lib] = append(filteredItems[lib], item)
				continue
			}
			log.Debugf("Excluding item %s due to recent stream: %s", item.Title, lastStreamed.Format(time.RFC3339))
		}
	}
	e.data.mediaItems = filteredItems
	return nil
}
