package engine

import (
	"context"
	"errors"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/pkg/streamystats"
)

// filterLastStreamThreshold filters out media items that have been streamed within the configured threshold.
func (e *Engine) filterLastStreamThreshold(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	filteredItems := make([]arr.MediaItem, 0)
	for _, item := range mediaItems {
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
			filteredItems = append(filteredItems, item) // No last streamed time, mark for deletion
			continue
		}
		// Check if the last streamed time is older than the configured threshold
		libraryConfig := e.cfg.GetLibraryConfig(item.LibraryName)
		if libraryConfig != nil && time.Since(lastStreamed) > time.Duration(libraryConfig.GetLastStreamThreshold())*24*time.Hour {
			log.Debugf("Including item %s - last streamed on %s", item.Title, lastStreamed.Format(time.RFC3339))
			filteredItems = append(filteredItems, item)
			continue
		}
		log.Debugf("Excluding item %s due to recent stream: %s", item.Title, lastStreamed.Format(time.RFC3339))
	}

	return filteredItems, nil
}
