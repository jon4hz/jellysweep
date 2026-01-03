package sonarr

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/charmbracelet/log"
	sonarrAPI "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/config"
)

func (s *Sonarr) DeleteMedia(ctx context.Context, seriesID int32, title string) error {
	// Get the global cleanup configuration
	cleanupMode := s.cfg.GetCleanupMode()
	keepCount := s.cfg.GetKeepCount()

	if s.cfg.DryRun {
		log.Infof("Dry run: Would delete Sonarr series %s using cleanup mode: %s", title, cleanupMode)
		return nil
	}

	var deletionDescription string

	switch cleanupMode {
	case config.CleanupModeAll:
		// Delete the entire series (original behavior)
		resp, err := s.client.SeriesAPI.DeleteSeries(s.sonarrAuthCtx(ctx), seriesID).
			DeleteFiles(true).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to delete Sonarr series %s: %w", title, err)
		}
		defer resp.Body.Close() //nolint: errcheck
		deletionDescription = "entire series"

	case config.CleanupModeKeepEpisodes, config.CleanupModeKeepSeasons:
		// Get episode files to keep
		filesToKeep, err := s.getEpisodeFilesToKeep(ctx, seriesID, title, cleanupMode, keepCount)
		if err != nil {
			log.Errorf("Failed to determine episode files to keep for series %s: %v", title, err)
			return err
		}

		// Get all episode files for the series
		allEpisodeFiles, err := s.getEpisodeFiles(ctx, seriesID)
		if err != nil {
			log.Errorf("Failed to get episode files for series %s: %v", title, err)
			return err
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
				log.Errorf("Failed to delete episode files for series %s: %v", title, err)
				return err
			}

			// Unmonitor episodes that had their files deleted to prevent redownload
			err = s.unmonitorDeletedEpisodes(ctx, seriesID, title, cleanupMode, keepCount)
			if err != nil {
				log.Warnf("Failed to unmonitor deleted episodes for series %s: %v", title, err)
				// continue with execution even when unmonitoring fails
			}

			if cleanupMode == config.CleanupModeKeepEpisodes {
				deletionDescription = fmt.Sprintf("all but first %d episodes (and unmonitored deleted episodes)", keepCount)
			} else {
				deletionDescription = fmt.Sprintf("all but first %d seasons (and unmonitored deleted episodes)", keepCount)
			}
		} else {
			log.Infof("No episode files to delete for series %s (all files are marked to keep)", title)
			return nil
		}

	default:
		log.Warnf("Unknown cleanup mode %s for series %s, using default 'all' mode", cleanupMode, title)
		// Fallback to deleting entire series
		resp, err := s.client.SeriesAPI.DeleteSeries(s.sonarrAuthCtx(ctx), seriesID).
			DeleteFiles(true).
			Execute()
		if err != nil {
			log.Errorf("Failed to delete Sonarr series %s: %v", title, err)
			return err
		}
		defer resp.Body.Close() //nolint: errcheck
		deletionDescription = "entire series (fallback)"
	}

	log.Infof("Deleted from Sonarr series %s: %s", title, deletionDescription)
	return nil
}

// getEpisodeFilesToKeep determines which episode files to keep based on cleanup mode.
func (s *Sonarr) getEpisodeFilesToKeep(ctx context.Context, seriesID int32, title string, cleanupMode config.CleanupMode, keepCount int) ([]int32, error) {
	if cleanupMode == config.CleanupModeAll {
		// For "all" mode, we delete the entire series (no episode files to keep)
		return []int32{}, nil
	}

	episodes, err := s.getEpisodes(ctx, seriesID)
	if err != nil {
		return nil, fmt.Errorf("failed to get episodes for series %s: %w", title, err)
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
		log.Debugf("Series %s: Total regular seasons found: %d, seasons to keep: %d (excluding specials)", title, len(seasons), keepCount)
		log.Debugf("Series %s: Regular season numbers in order: %v", title, seasons)

		keptSeasons := 0
		for _, seasonNum := range seasons {
			if keptSeasons >= keepCount {
				log.Debugf("Series %s: Season %d will be deleted (already kept %d seasons)", title, seasonNum, keptSeasons)
				break
			}

			log.Debugf("Series %s: Season %d will be kept (keeping season %d of %d)", title, seasonNum, keptSeasons+1, keepCount)
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
	episodes, resp, err := s.client.EpisodeAPI.ListEpisode(s.sonarrAuthCtx(ctx)).
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
	episodeFiles, resp, err := s.client.EpisodeFileAPI.ListEpisodeFile(s.sonarrAuthCtx(ctx)).
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
		resp, err := s.client.EpisodeFileAPI.DeleteEpisodeFile(s.sonarrAuthCtx(ctx), fileID).Execute()
		if err != nil {
			return fmt.Errorf("failed to delete episode file %d: %w", fileID, err)
		}
		defer resp.Body.Close() //nolint: errcheck
	}

	return nil
}

// unmonitorDeletedEpisodes unmonitors episodes that were deleted to prevent Sonarr from redownloading them.
func (s *Sonarr) unmonitorDeletedEpisodes(ctx context.Context, seriesID int32, title string, cleanupMode config.CleanupMode, keepCount int) error {
	// Get all episodes for the series
	episodes, err := s.getEpisodes(ctx, seriesID)
	if err != nil {
		return fmt.Errorf("failed to get episodes for series %s: %w", title, err)
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
		log.Debugf("Series %s (unmonitor): Total regular seasons found: %d, seasons to keep: %d (excluding specials)", title, len(seasons), keepCount)
		log.Debugf("Series %s (unmonitor): Regular season numbers in order: %v", title, seasons)

		keptSeasons := 0
		for _, seasonNum := range seasons {
			if keptSeasons >= keepCount {
				log.Debugf("Series %s (unmonitor): Season %d episodes will be unmonitored (already kept %d seasons)", title, seasonNum, keptSeasons)
				for _, episode := range seasonEpisodes[seasonNum] {
					episodesToUnmonitor = append(episodesToUnmonitor, episode.GetId())
				}
				continue
			} else {
				log.Debugf("Series %s (unmonitor): Season %d episodes will remain monitored (keeping season %d of %d)", title, seasonNum, keptSeasons+1, keepCount)
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

		_, err := s.client.EpisodeAPI.PutEpisodeMonitor(s.sonarrAuthCtx(ctx)).
			EpisodesMonitoredResource(*resource).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to unmonitor %d episodes for series %s: %w", len(episodesToUnmonitor), title, err)
		}

		log.Infof("Unmonitored %d episodes for series %s to prevent redownload", len(episodesToUnmonitor), title)
	}

	return nil
}

func episodeAlreadyAired(episode sonarrAPI.EpisodeResource, now time.Time) bool {
	// An episode is considered aired if it has a non-zero air date
	return !episode.GetAirDateUtc().IsZero() && episode.GetAirDateUtc().Before(now)
}
