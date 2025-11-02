package agefilter

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/filter"
)

// Filter implements the filter.Filterer interface.
type Filter struct {
	cfg    *config.Config
	db     database.MediaDB
	sonarr arr.Arrer
	radarr arr.Arrer
}

var _ filter.Filterer = (*Filter)(nil)

// New creates a new history Filter instance.
func New(cfg *config.Config, db database.MediaDB, sonarr arr.Arrer, radarr arr.Arrer) *Filter {
	return &Filter{
		cfg:    cfg,
		db:     db,
		sonarr: sonarr,
		radarr: radarr,
	}
}

// String returns the name of the filter.
func (f *Filter) String() string { return "Age Filter" }

// Apply filters out media items based on their deletion history.
func (f *Filter) Apply(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	filteredItems := make([]arr.MediaItem, 0)

	for _, item := range mediaItems {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// check if item was already deleted once
		var deletedMedia []database.Media
		var err error
		if item.TmdbId != 0 {
			deletedMedia, err = f.db.GetDeletedMediaByTMDBID(ctx, item.TmdbId)
		} else if item.TvdbId != 0 {
			deletedMedia, err = f.db.GetDeletedMediaByTVDBID(ctx, item.TvdbId)
		}
		if err != nil {
			log.Warn("Failed to check deleted media history", "title", item.Title, "error", err)
		}

		var lastDeleted time.Time
		if len(deletedMedia) > 0 {
			for _, dm := range deletedMedia {
				if !dm.DeletedAt.Time.IsZero() && dm.DeletedAt.Time.After(lastDeleted) {
					lastDeleted = dm.DeletedAt.Time
				}
			}
		}

		if !lastDeleted.IsZero() {
			log.Debug("Item was previously deleted", "title", item.Title, "last_deleted", lastDeleted)
		}

		addedDate, err := f.getMediaItemAddedDate(ctx, item, lastDeleted)
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
		libraryConfig := f.cfg.GetLibraryConfig(item.LibraryName)
		if libraryConfig != nil {
			contentAgeThreshold := time.Duration(libraryConfig.GetContentAgeThreshold()) * 24 * time.Hour
			timeSinceAdded := time.Since(*addedDate)

			if timeSinceAdded > contentAgeThreshold {
				filteredItems = append(filteredItems, item)
				log.Debugf("Including item %s for deletion, added %d days ago (threshold: %d days)",
					item.Title, int(timeSinceAdded.Hours()/24), libraryConfig.GetContentAgeThreshold())
			} else {
				log.Debugf("Excluding item %s due to recent addition: %s (%d days ago, threshold: %d days)",
					item.Title, addedDate.Format(time.RFC3339), int(timeSinceAdded.Hours()/24), libraryConfig.GetContentAgeThreshold())
			}
		} else {
			// No library config, include for deletion
			filteredItems = append(filteredItems, item)
			log.Debugf("No library config for %s, marking %s for deletion", item.LibraryName, item.Title)
		}
	}

	return filteredItems, nil
}

// getMediaItemAddedDate returns the first date when media content was added/imported for a given media item.
func (f *Filter) getMediaItemAddedDate(ctx context.Context, item arr.MediaItem, since time.Time) (*time.Time, error) {
	switch item.MediaType {
	case models.MediaTypeMovie:
		return f.radarr.GetItemAddedDate(ctx, item.MovieResource.GetId(), since)
	case models.MediaTypeTV:
		return f.sonarr.GetItemAddedDate(ctx, item.SeriesResource.GetId(), since)
	default:
		return nil, nil
	}
}
