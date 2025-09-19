package engine

import (
	"github.com/charmbracelet/log"
	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine/arr"
)

// filterSeriesAlreadyMeetingKeepCriteria filters out series that already meet the keep criteria.
func (e *Engine) filterSeriesAlreadyMeetingKeepCriteria() {
	cleanupMode := e.cfg.GetCleanupMode()
	keepCount := e.cfg.GetKeepCount()

	// If cleanup mode is "all", no filtering needed
	if cleanupMode == config.CleanupModeAll {
		return
	}

	totalSkippedCount := 0

	for lib, items := range e.data.mediaItems {
		var filteredItems []arr.MediaItem
		skippedCount := 0

		for _, item := range items {
			if item.MediaType != models.MediaTypeTV {
				// Keep non-TV items as-is
				filteredItems = append(filteredItems, item)
				continue
			}

			if e.shouldSkipSeriesForDeletion(item.SeriesResource, cleanupMode, keepCount) {
				log.Infof("Filtered out series %s - already meets keep criteria (%s: %d)", item.Title, cleanupMode, keepCount)
				skippedCount++
				totalSkippedCount++
			} else {
				filteredItems = append(filteredItems, item)
			}
		}

		// Update the media items for this library
		e.data.mediaItems[lib] = filteredItems

		if skippedCount > 0 {
			log.Infof("Filtered out %d series from library %s that already meet keep criteria", skippedCount, lib)
		}
	}

	if totalSkippedCount > 0 {
		log.Infof("Total filtered out: %d series that already meet keep criteria", totalSkippedCount)
	}
}

// shouldSkipSeriesForDeletion checks if a series already meets the keep criteria and should not be marked for deletion.
func (e *Engine) shouldSkipSeriesForDeletion(series sonarr.SeriesResource, cleanupMode config.CleanupMode, keepCount int) bool {
	if cleanupMode == config.CleanupModeAll {
		// For "all" mode, we always want to delete the entire series, so never skip
		return false
	}

	// Early return for obvious cases
	if keepCount <= 0 {
		// If keepCount is 0 or negative, we don't want to keep anything, so don't skip
		return false
	}

	// Use the seasons data directly from SeriesResource instead of making API calls
	seasons := series.GetSeasons()
	if len(seasons) == 0 {
		// If no seasons data, we can't determine criteria, so don't skip
		return false
	}

	switch cleanupMode { //nolint: exhaustive
	case config.CleanupModeKeepEpisodes:
		// Count regular episodes (excluding Season 0 specials) that have files
		var regularEpisodesWithFiles int
		for _, season := range seasons {
			// Skip Season 0 (specials)
			if season.GetSeasonNumber() == 0 {
				continue
			}

			// Count episodes with files in this season
			if season.HasStatistics() {
				stats := season.GetStatistics()
				if stats.HasEpisodeFileCount() {
					regularEpisodesWithFiles += int(stats.GetEpisodeFileCount())
					// Early exit if we already exceed the keep count
					if regularEpisodesWithFiles > keepCount {
						return false
					}
				}
			}
		}

		// If the series has exactly the desired number of episodes (or fewer), skip marking for deletion
		if regularEpisodesWithFiles <= keepCount {
			log.Debugf("Series %s has %d regular episodes with files, which is <= keep count %d - skipping deletion",
				series.GetTitle(), regularEpisodesWithFiles, keepCount)
			return true
		}

	case config.CleanupModeKeepSeasons:
		// Count regular seasons (excluding Season 0 specials) that have files
		var regularSeasonsWithFiles int
		for _, season := range seasons {
			// Skip Season 0 (specials)
			if season.GetSeasonNumber() == 0 {
				continue
			}

			// Check if this season has any episode files
			if season.HasStatistics() {
				stats := season.GetStatistics()
				if stats.HasEpisodeFileCount() && stats.GetEpisodeFileCount() > 0 {
					regularSeasonsWithFiles++
					// Early exit if we already exceed the keep count
					if regularSeasonsWithFiles > keepCount {
						return false
					}
				}
			}
		}

		// If the series has exactly the desired number of seasons (or fewer), skip marking for deletion
		if regularSeasonsWithFiles <= keepCount {
			log.Debugf("Series %s has %d regular seasons with files, which is <= keep count %d - skipping deletion",
				series.GetTitle(), regularSeasonsWithFiles, keepCount)
			return true
		}
	}

	// Series exceeds the keep criteria, should be marked for deletion
	return false
}
