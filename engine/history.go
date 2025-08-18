package engine

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
	radarr "github.com/devopsarr/radarr-go/radarr"
	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/api/models"
)

// getMediaItemAddedDate returns the first date when media content was added/imported for a given media item.
func (e *Engine) getMediaItemAddedDate(ctx context.Context, item MediaItem) (*time.Time, error) {
	switch item.MediaType {
	case models.MediaTypeMovie:
		return e.getRadarrItemAddedDate(ctx, item.MovieResource.GetId())
	case models.MediaTypeTV:
		return e.getSonarrItemAddedDate(ctx, item.SeriesResource.GetId())
	default:
		return nil, nil
	}
}

// getSonarrItemAddedDate retrieves the first date when any episode of a series was imported.
func (e *Engine) getSonarrItemAddedDate(ctx context.Context, seriesID int32) (*time.Time, error) {
	if e.sonarr == nil {
		return nil, nil
	}

	var allHistory []sonarr.HistoryResource
	page := int32(1)
	pageSize := int32(250)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		historyResp, resp, err := e.sonarr.HistoryAPI.GetHistory(sonarrAuthCtx(ctx, e.cfg.Sonarr)).
			Page(page).
			PageSize(pageSize).
			SeriesIds([]int32{seriesID}).
			Execute()
		if err != nil {
			log.Warnf("Failed to get Sonarr history for series %d: %v", seriesID, err)
			return nil, err
		}
		_ = resp.Body.Close()

		if len(historyResp.Records) == 0 {
			break
		}

		allHistory = append(allHistory, historyResp.Records...)

		// Check if we have more pages
		if historyResp.TotalRecords == nil || len(allHistory) >= int(*historyResp.TotalRecords) {
			break
		}

		page++
	}

	// Find the earliest import date from download/import events
	var earliestDate *time.Time
	for _, record := range allHistory {
		if record.EventType != nil && record.Date != nil {
			eventType := *record.EventType
			if eventType == sonarr.EPISODEHISTORYEVENTTYPE_DOWNLOAD_FOLDER_IMPORTED ||
				eventType == sonarr.EPISODEHISTORYEVENTTYPE_SERIES_FOLDER_IMPORTED {
				if earliestDate == nil || record.Date.Before(*earliestDate) {
					earliestDate = record.Date
				}
			}
		}
	}

	if earliestDate != nil {
		log.Debugf("Sonarr series %d first imported on: %s", seriesID, earliestDate.Format(time.RFC3339))
	}

	return earliestDate, nil
}

// getRadarrItemAddedDate retrieves the first date when a movie was imported.
func (e *Engine) getRadarrItemAddedDate(ctx context.Context, movieID int32) (*time.Time, error) {
	if e.radarr == nil {
		return nil, nil
	}

	var allHistory []radarr.HistoryResource
	page := int32(1)
	pageSize := int32(250)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		historyResp, resp, err := e.radarr.HistoryAPI.GetHistory(radarrAuthCtx(ctx, e.cfg.Radarr)).
			Page(page).
			PageSize(pageSize).
			MovieIds([]int32{movieID}).
			Execute()
		if err != nil {
			log.Warnf("Failed to get Radarr history for movie %d: %v", movieID, err)
			return nil, err
		}
		_ = resp.Body.Close()

		if len(historyResp.Records) == 0 {
			break
		}

		allHistory = append(allHistory, historyResp.Records...)

		// Check if we have more pages
		if historyResp.TotalRecords == nil || len(allHistory) >= int(*historyResp.TotalRecords) {
			break
		}

		page++
	}

	// Find the earliest import date from download/import events
	var earliestDate *time.Time
	for _, record := range allHistory {
		if record.EventType != nil && record.Date != nil {
			eventType := *record.EventType
			if eventType == radarr.MOVIEHISTORYEVENTTYPE_DOWNLOAD_FOLDER_IMPORTED ||
				eventType == radarr.MOVIEHISTORYEVENTTYPE_MOVIE_FOLDER_IMPORTED {
				if earliestDate == nil || record.Date.Before(*earliestDate) {
					earliestDate = record.Date
				}
			}
		}
	}

	if earliestDate != nil {
		log.Debugf("Radarr movie %d first imported on: %s", movieID, earliestDate.Format(time.RFC3339))
	}

	return earliestDate, nil
}

// filterContentAgeThreshold filters out media items that have been added within the configured threshold.
func (e *Engine) filterContentAgeThreshold(ctx context.Context, mediaItems map[string][]MediaItem) (map[string][]MediaItem, error) {
	filteredItems := make(map[string][]MediaItem, 0)

	for lib, items := range mediaItems {
		for _, item := range items {
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
				filteredItems[lib] = append(filteredItems[lib], item)
				continue
			}

			if addedDate == nil {
				// No added date found, include for deletion (maintaining current behavior)
				filteredItems[lib] = append(filteredItems[lib], item)
				log.Debugf("No added date for item %s, marking for deletion", item.Title)
				continue
			}

			// Check if the content has been added longer ago than the configured threshold
			libraryConfig := e.cfg.GetLibraryConfig(lib)
			if libraryConfig != nil {
				contentAgeThreshold := time.Duration(libraryConfig.ContentAgeThreshold) * 24 * time.Hour
				timeSinceAdded := time.Since(*addedDate)

				if timeSinceAdded > contentAgeThreshold {
					filteredItems[lib] = append(filteredItems[lib], item)
					log.Debugf("Including item %s for deletion, added %d days ago (threshold: %d days)",
						item.Title, int(timeSinceAdded.Hours()/24), libraryConfig.ContentAgeThreshold)
				} else {
					log.Debugf("Excluding item %s due to recent addition: %s (%d days ago, threshold: %d days)",
						item.Title, addedDate.Format(time.RFC3339), int(timeSinceAdded.Hours()/24), libraryConfig.ContentAgeThreshold)
				}
			} else {
				// No library config, include for deletion
				filteredItems[lib] = append(filteredItems[lib], item)
				log.Debugf("No library config for %s, marking %s for deletion", lib, item.Title)
			}
		}
	}

	return filteredItems, nil
}
