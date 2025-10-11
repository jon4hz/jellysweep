package sonarr

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	sonarrAPI "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine/arr"
	"github.com/jon4hz/jellysweep/tags"
)

func (s *Sonarr) DeleteMedia(ctx context.Context, libraryFoldersMap map[string][]string) ([]arr.MediaItem, error) {
	deletedItems := make([]arr.MediaItem, 0)

	sonarrItems, err := s.getItems(ctx, false)
	if err != nil {
		return deletedItems, fmt.Errorf("failed to get Sonarr items: %w", err)
	}

	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return deletedItems, fmt.Errorf("failed to get Sonarr tags: %w", err)
	}

	for _, series := range sonarrItems {
		libraryName := "TV Shows" // TODO: dont hardcode library name

		// Get tag names for this series
		var tagNames []string
		for _, tagID := range series.GetTags() {
			if tagName, exists := tagMap[tagID]; exists {
				tagNames = append(tagNames, tagName)
			}
		}

		// Check if the series should be deleted based on current disk usage
		if !tags.ShouldTriggerDeletionBasedOnDiskUsage(ctx, s.cfg, libraryName, tagNames, libraryFoldersMap) {
			continue
		}

		// Get the global cleanup configuration
		cleanupMode := s.cfg.GetCleanupMode()
		keepCount := s.cfg.GetKeepCount()

		if s.cfg.DryRun {
			log.Infof("Dry run: Would delete Sonarr series %s using cleanup mode: %s", series.GetTitle(), cleanupMode)
			continue
		}

		var deletionDescription string

		switch cleanupMode {
		case config.CleanupModeAll:
			// Delete the entire series (original behavior)
			resp, err := s.client.SeriesAPI.DeleteSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), series.GetId()).
				DeleteFiles(true).
				Execute()
			if err != nil {
				return deletedItems, fmt.Errorf("failed to delete Sonarr series %s: %w", series.GetTitle(), err)
			}
			defer resp.Body.Close() //nolint: errcheck
			deletionDescription = "entire series"

		case config.CleanupModeKeepEpisodes, config.CleanupModeKeepSeasons:
			// Get episode files to keep
			filesToKeep, err := s.getEpisodeFilesToKeep(ctx, series, cleanupMode, keepCount)
			if err != nil {
				log.Errorf("Failed to determine episode files to keep for series %s: %v", series.GetTitle(), err)
				continue
			}

			// Get all episode files for the series
			allEpisodeFiles, err := s.getEpisodeFiles(ctx, series.GetId())
			if err != nil {
				log.Errorf("Failed to get episode files for series %s: %v", series.GetTitle(), err)
				continue
			}

			// Determine which files to delete
			var filesToDelete []int32
			for _, file := range allEpisodeFiles {
				if !slices.Contains(filesToKeep, file.GetId()) {
					filesToDelete = append(filesToDelete, file.GetId())
				}
			}

			// Delete the determined episode files
			if len(filesToDelete) > 0 {
				err := s.deleteEpisodeFiles(ctx, filesToDelete)
				if err != nil {
					log.Errorf("Failed to delete episode files for series %s: %v", series.GetTitle(), err)
					continue
				}

				// Unmonitor episodes that had their files deleted to prevent redownload
				err = s.unmonitorDeletedEpisodes(ctx, series, cleanupMode, keepCount)
				if err != nil {
					log.Warnf("Failed to unmonitor deleted episodes for series %s: %v", series.GetTitle(), err)
					// Continue execution - file deletion succeeded, unmonitoring is not critical
				}

				if cleanupMode == config.CleanupModeKeepEpisodes {
					deletionDescription = fmt.Sprintf("all but first %d episodes (and unmonitored deleted episodes)", keepCount)
				} else {
					deletionDescription = fmt.Sprintf("all but first %d seasons (and unmonitored deleted episodes)", keepCount)
				}
			} else {
				log.Infof("No episode files to delete for series %s (all files are marked to keep)", series.GetTitle())
				continue
			}

		default:
			log.Warnf("Unknown cleanup mode %s for series %s, using default 'all' mode", cleanupMode, series.GetTitle())
			// Fallback to deleting entire series
			resp, err := s.client.SeriesAPI.DeleteSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), series.GetId()).
				DeleteFiles(true).
				Execute()
			if err != nil {
				return deletedItems, fmt.Errorf("failed to delete Sonarr series %s: %w", series.GetTitle(), err)
			}
			defer resp.Body.Close() //nolint: errcheck
			deletionDescription = "entire series (fallback)"
		}

		log.Infof("Deleted from Sonarr series %s: %s", series.GetTitle(), deletionDescription)

		// Remove jellysweep-delete tags from the series after successful deletion
		if cleanupMode != config.CleanupModeAll {
			// Only remove tags if the series still exists (not for complete series deletion)
			err := s.cleanupSonarrTagsAfterDelete(ctx, series)
			if err != nil {
				log.Warnf("Failed to cleanup jellysweep tags from series %s: %v", series.GetTitle(), err)
				// Continue execution - deletion succeeded, tag removal is not critical
			}
		}

		// Add to deleted items list
		deletedItems = append(deletedItems, arr.MediaItem{
			Title:       series.GetTitle(),
			LibraryName: libraryName,
			MediaType:   models.MediaTypeTV,
			Year:        series.GetYear(),
		})
	}

	if len(deletedItems) > 0 {
		if err := s.itemsCache.Clear(ctx); err != nil {
			log.Warnf("Failed to clear Sonarr items cache after deletion: %v", err)
		} else {
			log.Debug("Cleared Sonarr items cache after deletion")
		}
	}

	return deletedItems, nil
}

// getEpisodeFilesToKeep determines which episode files to keep based on cleanup mode.
func (s *Sonarr) getEpisodeFilesToKeep(ctx context.Context, series sonarrAPI.SeriesResource, cleanupMode config.CleanupMode, keepCount int) ([]int32, error) {
	if cleanupMode == config.CleanupModeAll {
		// For "all" mode, we delete the entire series (no episode files to keep)
		return []int32{}, nil
	}

	episodes, err := s.getEpisodes(ctx, series.GetId())
	if err != nil {
		return nil, fmt.Errorf("failed to get episodes for series %s: %w", series.GetTitle(), err)
	}

	var filesToKeep []int32

	switch cleanupMode { //nolint: exhaustive
	case config.CleanupModeKeepEpisodes:
		// Keep the first N episodes (by season and episode number), excluding Season 0 (specials)
		// Filter out Season 0 (specials) episodes
		var regularEpisodes []sonarrAPI.EpisodeResource
		var specialEpisodes []sonarrAPI.EpisodeResource
		for _, episode := range episodes {
			if episode.GetSeasonNumber() == 0 {
				specialEpisodes = append(specialEpisodes, episode)
			} else {
				regularEpisodes = append(regularEpisodes, episode)
			}
		}

		// Sort regular episodes by season number ascending, then by episode number ascending
		slices.SortFunc(regularEpisodes, func(a, b sonarrAPI.EpisodeResource) int {
			// Sort by season number ascending (first seasons first)
			if a.GetSeasonNumber() != b.GetSeasonNumber() {
				return int(a.GetSeasonNumber() - b.GetSeasonNumber())
			}
			// If season numbers are equal, sort by episode number ascending (first episodes first)
			return int(a.GetEpisodeNumber() - b.GetEpisodeNumber())
		})

		// Always keep all special episodes (Season 0)
		for _, episode := range specialEpisodes {
			if episode.HasFile != nil && *episode.HasFile && episode.HasEpisodeFileId() {
				filesToKeep = append(filesToKeep, episode.GetEpisodeFileId())
			}
		}

		// Keep files for the first keepCount regular episodes (by episode order)
		keptEpisodes := 0
		for _, episode := range regularEpisodes {
			if keptEpisodes >= keepCount {
				break
			}
			if episode.HasFile != nil && *episode.HasFile && episode.HasEpisodeFileId() {
				filesToKeep = append(filesToKeep, episode.GetEpisodeFileId())
				keptEpisodes++
			}
		}

	case config.CleanupModeKeepSeasons:
		// Keep the first N lowest-numbered seasons (typically the earliest seasons), excluding Season 0 (specials)
		// Group episodes by season, separating specials from regular seasons
		seasonEpisodes := make(map[int32][]sonarrAPI.EpisodeResource)
		var specialEpisodes []sonarrAPI.EpisodeResource

		for _, episode := range episodes {
			seasonNum := episode.GetSeasonNumber()
			if seasonNum == 0 {
				specialEpisodes = append(specialEpisodes, episode)
			} else {
				seasonEpisodes[seasonNum] = append(seasonEpisodes[seasonNum], episode)
			}
		}

		// Always keep all special episodes (Season 0)
		for _, episode := range specialEpisodes {
			if episode.HasFile != nil && *episode.HasFile && episode.HasEpisodeFileId() {
				filesToKeep = append(filesToKeep, episode.GetEpisodeFileId())
			}
		}

		// Get sorted season numbers (lowest to highest - earliest seasons first), excluding Season 0
		var seasons []int32
		for seasonNum := range seasonEpisodes {
			seasons = append(seasons, seasonNum)
		}

		// Sort in ascending order (lowest season numbers first)
		slices.SortFunc(seasons, func(a, b int32) int {
			return int(a - b) // a - b for ascending order
		})

		// Keep files for the first keepCount regular seasons (lowest-numbered)
		log.Debugf("Series %s: Total regular seasons found: %d, seasons to keep: %d (excluding specials)", series.GetTitle(), len(seasons), keepCount)
		log.Debugf("Series %s: Regular season numbers in order: %v", series.GetTitle(), seasons)

		keptSeasons := 0
		for _, seasonNum := range seasons {
			if keptSeasons >= keepCount {
				log.Debugf("Series %s: Season %d will be deleted (already kept %d seasons)", series.GetTitle(), seasonNum, keptSeasons)
				break
			}

			log.Debugf("Series %s: Season %d will be kept (keeping season %d of %d)", series.GetTitle(), seasonNum, keptSeasons+1, keepCount)
			for _, episode := range seasonEpisodes[seasonNum] {
				if episode.HasFile != nil && *episode.HasFile && episode.HasEpisodeFileId() {
					filesToKeep = append(filesToKeep, episode.GetEpisodeFileId())
				}
			}
			keptSeasons++
		}
	}

	return filesToKeep, nil
}

// getEpisodes retrieves all episodes for a specific series.
func (s *Sonarr) getEpisodes(ctx context.Context, seriesID int32) ([]sonarrAPI.EpisodeResource, error) {
	episodes, resp, err := s.client.EpisodeAPI.ListEpisode(sonarrAuthCtx(ctx, s.cfg.Sonarr)).
		SeriesId(seriesID).
		Execute()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint: errcheck
	return episodes, nil
}

// getEpisodeFiles retrieves all episode files for a specific series.
func (s *Sonarr) getEpisodeFiles(ctx context.Context, seriesID int32) ([]sonarrAPI.EpisodeFileResource, error) {
	episodeFiles, resp, err := s.client.EpisodeFileAPI.ListEpisodeFile(sonarrAuthCtx(ctx, s.cfg.Sonarr)).
		SeriesId(seriesID).
		Execute()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint: errcheck
	return episodeFiles, nil
}

// deleteEpisodeFiles deletes specific episode files from Sonarr.
func (s *Sonarr) deleteEpisodeFiles(ctx context.Context, episodeFileIDs []int32) error {
	if s.client == nil {
		return fmt.Errorf("sonarr client not available")
	}

	for _, fileID := range episodeFileIDs {
		resp, err := s.client.EpisodeFileAPI.DeleteEpisodeFile(sonarrAuthCtx(ctx, s.cfg.Sonarr), fileID).Execute()
		if err != nil {
			return fmt.Errorf("failed to delete episode file %d: %w", fileID, err)
		}
		defer resp.Body.Close() //nolint: errcheck
	}

	return nil
}

// unmonitorDeletedEpisodes unmonitors episodes that were deleted to prevent Sonarr from redownloading them.
func (s *Sonarr) unmonitorDeletedEpisodes(ctx context.Context, series sonarrAPI.SeriesResource, cleanupMode config.CleanupMode, keepCount int) error {
	// Get all episodes for the series
	episodes, err := s.getEpisodes(ctx, series.GetId())
	if err != nil {
		return fmt.Errorf("failed to get episodes for series %s: %w", series.GetTitle(), err)
	}

	var episodesToUnmonitor []int32

	switch cleanupMode { //nolint: exhaustive
	case config.CleanupModeKeepEpisodes:
		// Unmonitor episodes that are not in the first N regular episodes (excluding Season 0 specials)
		// Filter out Season 0 (specials) episodes - these should never be unmonitored
		var regularEpisodes []sonarrAPI.EpisodeResource
		for _, episode := range episodes {
			if episode.GetSeasonNumber() != 0 {
				regularEpisodes = append(regularEpisodes, episode)
			}
		}

		// Sort regular episodes by season number ascending, then by episode number ascending
		slices.SortFunc(regularEpisodes, func(a, b sonarrAPI.EpisodeResource) int {
			// Sort by season number ascending (first seasons first)
			if a.GetSeasonNumber() != b.GetSeasonNumber() {
				return int(a.GetSeasonNumber() - b.GetSeasonNumber())
			}
			// If season numbers are equal, sort by episode number ascending (first episodes first)
			return int(a.GetEpisodeNumber() - b.GetEpisodeNumber())
		})

		// Unmonitor regular episodes beyond the first keepCount episodes
		now := time.Now().UTC()
		for i, episode := range regularEpisodes {
			if i >= keepCount && episodeAlreadyAired(episode, now) {
				episodesToUnmonitor = append(episodesToUnmonitor, episode.GetId())
			}
		}

	case config.CleanupModeKeepSeasons:
		// Unmonitor episodes from seasons that are not in the first N lowest-numbered regular seasons (excluding Season 0)
		// Group episodes by season, separating specials from regular seasons
		seasonEpisodes := make(map[int32][]sonarrAPI.EpisodeResource)
		for _, episode := range episodes {
			seasonNum := episode.GetSeasonNumber()
			if seasonNum != 0 { // Exclude Season 0 (specials) from being unmonitored
				seasonEpisodes[seasonNum] = append(seasonEpisodes[seasonNum], episode)
			}
		}

		// Get sorted season numbers (lowest to highest - earliest seasons first), excluding Season 0
		var seasons []int32
		now := time.Now().UTC()
	seasonLoop:
		for seasonNum := range seasonEpisodes {
			// if the season contains unaired episodes, we dont unmonitor it
			for _, episode := range seasonEpisodes[seasonNum] {
				if !episodeAlreadyAired(episode, now) {
					continue seasonLoop
				}
			}
			seasons = append(seasons, seasonNum)
		}
		slices.Sort(seasons)

		// Unmonitor episodes from regular seasons beyond the first keepCount seasons
		log.Debugf("Series %s (unmonitor): Total regular seasons found: %d, seasons to keep: %d (excluding specials)", series.GetTitle(), len(seasons), keepCount)
		log.Debugf("Series %s (unmonitor): Regular season numbers in order: %v", series.GetTitle(), seasons)

		keptSeasons := 0
		for _, seasonNum := range seasons {
			if keptSeasons >= keepCount {
				log.Debugf("Series %s (unmonitor): Season %d episodes will be unmonitored (already kept %d seasons)", series.GetTitle(), seasonNum, keptSeasons)
				for _, episode := range seasonEpisodes[seasonNum] {
					episodesToUnmonitor = append(episodesToUnmonitor, episode.GetId())
				}
				continue
			} else {
				log.Debugf("Series %s (unmonitor): Season %d episodes will remain monitored (keeping season %d of %d)", series.GetTitle(), seasonNum, keptSeasons+1, keepCount)
			}
			keptSeasons++
		}
	}

	// Unmonitor the determined episodes if any
	if len(episodesToUnmonitor) > 0 {
		monitored := false
		resource := sonarrAPI.NewEpisodesMonitoredResource()
		resource.SetEpisodeIds(episodesToUnmonitor)
		resource.SetMonitored(monitored)

		_, err := s.client.EpisodeAPI.PutEpisodeMonitor(sonarrAuthCtx(ctx, s.cfg.Sonarr)).
			EpisodesMonitoredResource(*resource).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to unmonitor %d episodes for series %s: %w", len(episodesToUnmonitor), series.GetTitle(), err)
		}

		log.Infof("Unmonitored %d episodes for series %s to prevent redownload", len(episodesToUnmonitor), series.GetTitle())
	}

	return nil
}

func episodeAlreadyAired(episode sonarrAPI.EpisodeResource, now time.Time) bool {
	// An episode is considered aired if it has a non-zero air date
	return !episode.GetAirDateUtc().IsZero() && episode.GetAirDateUtc().Before(now)
}

// cleanupSonarrTagsAfterDelete removes jellysweep-delete-, jellysweep-must-delete- and jellysweep-keep-request- tags from a Sonarr series.
func (s *Sonarr) cleanupSonarrTagsAfterDelete(ctx context.Context, series sonarrAPI.SeriesResource) error {
	if series.GetTags() == nil || len(series.GetTags()) == 0 {
		return nil // No tags to remove
	}

	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr tags: %w", err)
	}

	// Find tags to keep (all tags except jellysweep-delete-, jellysweep-must-delete- and jellysweep-keep-request-)
	var tagsToKeep []int32
	var removedTagNames []string

	for _, tagID := range series.GetTags() {
		if tagName, exists := tagMap[tagID]; exists {
			if strings.HasPrefix(tagName, tags.JellysweepTagPrefix) || tagName == tags.JellysweepDeleteForSureTag || strings.HasPrefix(tagName, tags.JellysweepKeepRequestPrefix) {
				// This is a jellysweep-delete- or jellysweep-must-delete-for-sure or jellysweep-keep-request- tag, don't keep it
				removedTagNames = append(removedTagNames, tagName)
			} else {
				// Keep all other tags
				tagsToKeep = append(tagsToKeep, tagID)
			}
		} else {
			// Keep tags we don't recognize
			tagsToKeep = append(tagsToKeep, tagID)
		}
	}

	// If no tags were removed, nothing to do
	if len(removedTagNames) == 0 {
		return nil
	}

	// Update the series with the filtered tags
	series.Tags = tagsToKeep
	_, resp, err := s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", series.GetId())).
		SeriesResource(series).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update series tags: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Removed jellysweep-delete tags from series %s: %v", series.GetTitle(), removedTagNames)
	return nil
}
