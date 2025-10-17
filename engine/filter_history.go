package engine

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/engine/arr"
)

// getMediaItemAddedDate returns the first date when media content was added/imported for a given media item.
func (e *Engine) getMediaItemAddedDate(ctx context.Context, item arr.MediaItem) (*time.Time, error) {
	switch item.MediaType {
	case models.MediaTypeMovie:
		return e.radarr.GetItemAddedDate(ctx, item.MovieResource.GetId())
	case models.MediaTypeTV:
		return e.sonarr.GetItemAddedDate(ctx, item.SeriesResource.GetId())
	default:
		return nil, nil
	}
}

// filterContentAgeThreshold filters out media items that have been added within the configured threshold.
func (e *Engine) filterContentAgeThreshold(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	filteredItems := make([]arr.MediaItem, 0)

	for _, item := range mediaItems {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		addedDate, err := e.getMediaItemAddedDate(ctx, item)
		if err != nil {
			log.Errorf("Failed to get added date for item %s: %v", item.Title, err)
			// If we can't get the added date, continue processing but mark for deletion
			// This maintains the current behavior for items without history
			filteredItems = append(filteredItems, item)
			continue
		}

		if addedDate == nil {
			// No added date found, include for deletion (maintaining current behavior)
			filteredItems = append(filteredItems, item)
			log.Debugf("No added date for item %s, marking for deletion", item.Title)
			continue
		}

		// Check if the content has been added longer ago than the configured threshold
		libraryConfig := e.cfg.GetLibraryConfig(item.LibraryName)
		if libraryConfig != nil {
			contentAgeThreshold := time.Duration(libraryConfig.ContentAgeThreshold) * 24 * time.Hour
			timeSinceAdded := time.Since(*addedDate)

			if timeSinceAdded > contentAgeThreshold {
				filteredItems = append(filteredItems, item)
				log.Debugf("Including item %s for deletion, added %d days ago (threshold: %d days)",
					item.Title, int(timeSinceAdded.Hours()/24), libraryConfig.ContentAgeThreshold)
			} else {
				log.Debugf("Excluding item %s due to recent addition: %s (%d days ago, threshold: %d days)",
					item.Title, addedDate.Format(time.RFC3339), int(timeSinceAdded.Hours()/24), libraryConfig.ContentAgeThreshold)
			}
		} else {
			// No library config, include for deletion
			filteredItems = append(filteredItems, item)
			log.Debugf("No library config for %s, marking %s for deletion", item.LibraryName, item.Title)
		}
	}

	return filteredItems, nil
}
