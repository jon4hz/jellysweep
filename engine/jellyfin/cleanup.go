package jellyfin

import (
	"context"
	"fmt"
	"slices"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/config"
	jellyfin "github.com/sj14/jellyfin-go/api"
)

// RemoveItemWithCleanupMode removes an item from Jellyfin according to the cleanup mode.
// For movies or "all" mode, it removes the entire item.
// For TV series with "keep_episodes" or "keep_seasons" mode, it removes specific episodes/seasons.
func (c *Client) RemoveItemWithCleanupMode(ctx context.Context, itemID, title string, itemType jellyfin.BaseItemKind, cleanupMode config.CleanupMode, keepCount int) error {
	// For movies or "all" mode, remove the entire item
	if itemType == jellyfin.BASEITEMKIND_MOVIE {
		if err := c.RemoveItem(ctx, itemID); err != nil {
			log.Error("failed to remove jellyfin item", "jellyfinID", itemID, "error", err)
			return err
		}
		log.Infof("Removed entire item from Jellyfin: %s", title)
		return nil
	}

	// For TV series, handle keep_episodes and keep_seasons modes
	if itemType != jellyfin.BASEITEMKIND_SERIES {
		log.Warnf("Unsupported item type for cleanup mode %s: %s", cleanupMode, itemType)
		return fmt.Errorf("unsupported item type for cleanup mode %s: %s", cleanupMode, itemType)
	}

	var deletionDescription string

	switch cleanupMode {
	case config.CleanupModeAll:
		// For "all" mode, this shouldn't be reached (handled earlier), but handle it anyway
		if err := c.RemoveItem(ctx, itemID); err != nil {
			log.Error("failed to remove jellyfin item", "jellyfinID", itemID, "error", err)
			return err
		}
		deletionDescription = "entire series"

	case config.CleanupModeKeepEpisodes, config.CleanupModeKeepSeasons:
		// Get all episodes for the series
		allEpisodes, seasonsWithoutEpisodes, err := c.GetEpisodes(ctx, itemID)
		if err != nil {
			log.Errorf("Failed to get episodes for series %s: %v", title, err)
			return err
		}

		// Get episodes to determine what to delete
		episodesToKeep := c.filterEpisodesToKeep(allEpisodes, title, cleanupMode, keepCount)

		// Group episodes by season ID to track which seasons will be empty after deletion
		episodesBySeason := make(map[string][]jellyfin.BaseItemDto)
		for _, episode := range allEpisodes {
			if episode.ParentId.IsSet() {
				seasonID := episode.GetParentId()
				episodesBySeason[seasonID] = append(episodesBySeason[seasonID], episode)
			}
		}

		// Determine which episodes to delete
		var episodesToDelete []string
		for _, episode := range allEpisodes {
			if episode.Id != nil && !slices.Contains(episodesToKeep, episode.GetId()) {
				episodesToDelete = append(episodesToDelete, episode.GetId())
			}
		}

		// Delete the determined episodes
		if len(episodesToDelete) > 0 {
			err := c.deleteEpisodes(ctx, episodesToDelete)
			if err != nil {
				log.Errorf("Failed to delete episodes for series %s: %v", title, err)
				return err
			}

			// After deleting episodes, check which seasons are now empty and delete them
			c.deleteEmptySeasons(ctx, title, episodesBySeason, episodesToDelete)

			if cleanupMode == config.CleanupModeKeepEpisodes {
				deletionDescription = fmt.Sprintf("all but first %d episodes from Jellyfin", keepCount)
			} else {
				deletionDescription = fmt.Sprintf("all but first %d seasons from Jellyfin", keepCount)
			}
		} else {
			log.Infof("No episodes to delete for series %s (all episodes are marked to keep)", title)
			return nil
		}

		if len(seasonsWithoutEpisodes) > 0 {
			log.Infof("Deleting %d seasons without episodes for series %s", len(seasonsWithoutEpisodes), title)
			for _, seasonID := range seasonsWithoutEpisodes {
				_, err := c.jellyfin.LibraryAPI.DeleteItem(ctx, seasonID).Execute()
				if err != nil {
					log.Warnf("Failed to delete season %s without episodes for series %s: %v", seasonID, title, err)
					// Continue deleting other seasons even if one fails
					continue
				}
				log.Debugf("Deleted season %s without episodes for series %s", seasonID, title)
			}
		}

	default:
		log.Warnf("Unknown cleanup mode %s for series %s, using default 'all' mode", cleanupMode, title)
		// Fallback to removing entire series
		if err := c.RemoveItem(ctx, itemID); err != nil {
			log.Error("failed to remove jellyfin item", "jellyfinID", itemID, "error", err)
			return err
		}
		deletionDescription = "entire series (fallback)"
	}

	log.Infof("Deleted from Jellyfin series %s: %s", title, deletionDescription)
	return nil
}

// filterEpisodesToKeep determines which episodes to keep based on cleanup mode.
func (c *Client) filterEpisodesToKeep(episodes []jellyfin.BaseItemDto, title string, cleanupMode config.CleanupMode, keepCount int) []string {
	if cleanupMode == config.CleanupModeAll {
		// For "all" mode, we delete the entire series (no episodes to keep)
		return []string{}
	}

	var episodesToKeep []string

	switch cleanupMode { //nolint: exhaustive
	case config.CleanupModeKeepEpisodes:
		// Keep the first N episodes (by season and episode number)
		// Note: Specials are already excluded by GetEpisodes()

		// Sort episodes by season number ascending, then by episode number ascending
		slices.SortFunc(episodes, func(a, b jellyfin.BaseItemDto) int {
			// Sort by parent index number (season) ascending (first seasons first)
			if a.GetParentIndexNumber() != b.GetParentIndexNumber() {
				return int(a.GetParentIndexNumber() - b.GetParentIndexNumber())
			}
			// If season numbers are equal, sort by index number (episode) ascending (first episodes first)
			return int(a.GetIndexNumber() - b.GetIndexNumber())
		})

		// Keep the first keepCount episodes (by episode order)
		keptEpisodes := 0
		for _, episode := range episodes {
			if keptEpisodes >= keepCount {
				break
			}
			if episode.Id != nil {
				episodesToKeep = append(episodesToKeep, episode.GetId())
				keptEpisodes++
			}
		}

	case config.CleanupModeKeepSeasons:
		// Keep the first N lowest-numbered seasons (typically the earliest seasons)
		// Note: Specials are already excluded by GetEpisodes()

		// Group episodes by season
		seasonEpisodes := make(map[int32][]jellyfin.BaseItemDto)

		for _, episode := range episodes {
			parentIndexNumber := episode.GetParentIndexNumber()
			seasonEpisodes[parentIndexNumber] = append(seasonEpisodes[parentIndexNumber], episode)
		}

		// Get sorted season numbers (lowest to highest - earliest seasons first)
		var seasons []int32
		for seasonNum := range seasonEpisodes {
			seasons = append(seasons, seasonNum)
		}

		// Sort in ascending order (lowest season numbers first)
		slices.SortFunc(seasons, func(a, b int32) int {
			return int(a - b) // a - b for ascending order
		})

		// Keep episodes for the first keepCount seasons (lowest-numbered)
		log.Debugf("Series %s: Total seasons found: %d, seasons to keep: %d", title, len(seasons), keepCount)
		log.Debugf("Series %s: Season numbers in order: %v", title, seasons)

		keptSeasons := 0
		for _, seasonNum := range seasons {
			if keptSeasons >= keepCount {
				log.Debugf("Series %s: Season %d will be deleted (already kept %d seasons)", title, seasonNum, keptSeasons)
				break
			}

			log.Debugf("Series %s: Season %d will be kept (keeping season %d of %d)", title, seasonNum, keptSeasons+1, keepCount)
			for _, episode := range seasonEpisodes[seasonNum] {
				if episode.Id != nil {
					episodesToKeep = append(episodesToKeep, episode.GetId())
				}
			}
			keptSeasons++
		}
	}

	return episodesToKeep
}

// deleteEpisodes deletes specific episodes from Jellyfin.
func (c *Client) deleteEpisodes(ctx context.Context, episodeIDs []string) error {
	if c.jellyfin == nil {
		return fmt.Errorf("jellyfin client not available")
	}

	for _, episodeID := range episodeIDs {
		_, err := c.jellyfin.LibraryAPI.DeleteItem(ctx, episodeID).Execute()
		if err != nil {
			return fmt.Errorf("failed to delete episode %s: %w", episodeID, err)
		}
	}

	return nil
}

// deleteEmptySeasons checks which seasons have all their episodes deleted and removes those season items.
func (c *Client) deleteEmptySeasons(ctx context.Context, title string, episodesBySeason map[string][]jellyfin.BaseItemDto, deletedEpisodeIDs []string) {
	// Track which seasons to delete
	var seasonsToDelete []string

	// For each season, check if all its episodes were deleted
	for seasonID, episodes := range episodesBySeason {
		allEpisodesDeleted := true
		for _, episode := range episodes {
			if episode.Id == nil {
				continue
			}
			episodeID := episode.GetId()
			// If this episode was not deleted, the season should not be deleted
			if !slices.Contains(deletedEpisodeIDs, episodeID) {
				allEpisodesDeleted = false
				break
			}
		}

		// If all episodes from this season were deleted, mark the season for deletion
		if allEpisodesDeleted && len(episodes) > 0 {
			seasonsToDelete = append(seasonsToDelete, seasonID)
		}
	}

	// Delete the empty seasons
	if len(seasonsToDelete) > 0 {
		log.Infof("Deleting %d empty season(s) for series %s", len(seasonsToDelete), title)
		for _, seasonID := range seasonsToDelete {
			_, err := c.jellyfin.LibraryAPI.DeleteItem(ctx, seasonID).Execute()
			if err != nil {
				log.Warnf("Failed to delete empty season %s for series %s: %v", seasonID, title, err)
				// Continue deleting other seasons even if one fails
				continue
			}
			log.Debugf("Deleted empty season %s for series %s", seasonID, title)
		}
	}
}
