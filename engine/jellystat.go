package engine

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
)

func (e *Engine) getJellystatMediaItemLastStreamed(ctx context.Context, jellyfinID string) (time.Time, error) {
	lastPlayed, err := e.jellystat.GetLastPlayed(ctx, jellyfinID)
	if err != nil {
		return time.Time{}, err
	}
	if lastPlayed == nil || lastPlayed.LastPlayed == nil {
		return time.Time{}, nil // No playback history found
	}
	return *lastPlayed.LastPlayed, nil
}

// filterJellystatLastStreamThreshold filters out media items that have been streamed within the configured threshold.
func (e *Engine) filterJellystatLastStreamThreshold(ctx context.Context, mediaItems map[string][]MediaItem) (map[string][]MediaItem, error) {
	filteredItems := make(map[string][]MediaItem, 0)
	for lib, items := range mediaItems {
		for _, item := range items {
			lastStreamed, err := e.getJellystatMediaItemLastStreamed(ctx, item.JellyfinID)
			if err != nil {
				return nil, err
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
	return filteredItems, nil
}
