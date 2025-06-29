package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	radarr "github.com/devopsarr/radarr-go/radarr"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/jellystat"
)

func radarrAuthCtx(ctx context.Context, cfg *config.RadarrConfig) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(
		ctx,
		radarr.ContextAPIKeys,
		map[string]radarr.APIKey{
			"X-Api-Key": {Key: cfg.APIKey},
		},
	)
}

func newRadarrClient(cfg *config.RadarrConfig) (*radarr.APIClient, error) {
	rcfg := radarr.NewConfiguration()

	if strings.HasPrefix(cfg.URL, "http://") {
		rcfg.Scheme = "http"
		cfg.URL = strings.TrimPrefix(cfg.URL, "http://")
	} else if strings.HasPrefix(cfg.URL, "https://") {
		rcfg.Scheme = "https"
		cfg.URL = strings.TrimPrefix(cfg.URL, "https://")
	}

	rcfg.Host = cfg.URL

	return radarr.NewAPIClient(rcfg), nil
}

// getRadarrItems retrieves all movies from Radarr.
func (e *Engine) getRadarrItems(ctx context.Context) ([]radarr.MovieResource, error) {
	movies, _, err := e.radarr.MovieAPI.ListMovie(radarrAuthCtx(ctx, e.cfg.Radarr)).Execute()
	if err != nil {
		return nil, err
	}
	return movies, nil
}

// getRadarrTags retrieves all tags from Radarr and returns them as a map with tag IDs as keys and tag names as values.
func (e *Engine) getRadarrTags(ctx context.Context) (map[int32]string, error) {
	tags, _, err := e.radarr.TagAPI.ListTag(radarrAuthCtx(ctx, e.cfg.Radarr)).Execute()
	if err != nil {
		return nil, err
	}
	tagMap := make(map[int32]string)
	for _, tag := range tags {
		tagMap[tag.GetId()] = tag.GetLabel()
	}
	return tagMap, nil
}

func (e *Engine) filterRadarrTags() error {
	if e.data.radarrItems == nil || e.data.radarrTags == nil {
		log.Warn("No Radarr items or tags available for filtering")
		return nil // No items or tags to filter
	}
	filteredItems := make(map[string][]MediaItem, 0)
	for lib, items := range e.data.mediaItems {
		for _, item := range items {
			// Check if the item has any tags that are not in the exclude list
			hasExcludedTag := false
			for _, tagName := range item.Tags {
				for _, tag := range e.data.radarrTags {
					if tag == tagName && slices.Contains(e.cfg.Jellysweep.Libraries[lib].ExcludeTags, tagName) {
						hasExcludedTag = true
						log.Debugf("Excluding item %s due to tag: %s", item.Title, tagName)
						break
					}
				}
			}
			if !hasExcludedTag {
				filteredItems[lib] = append(filteredItems[lib], item)
			}
		}
	}
	e.data.mediaItems = filteredItems
	return nil
}

func (e *Engine) markRadarrMediaItemsForDeletion(ctx context.Context, dryRun bool) error {
	for lib, items := range e.data.mediaItems {
	movieLoop:
		for _, item := range items {
			if item.MediaType != MediaTypeMovie {
				continue // Only process movies for Radarr
			}

			// check if movie has already a jellysweep delete tag
			for _, tagID := range item.MovieResource.GetTags() {
				if strings.HasPrefix(e.data.radarrTags[tagID], jellysweepTagPrefix) {
					log.Debugf("Radarr movie %s already marked for deletion with tag %s", item.Title, e.data.radarrTags[tagID])
					continue movieLoop
				}
			}

			cleanupDelay := e.cfg.Jellysweep.Libraries[lib].CleanupDelay
			if cleanupDelay <= 0 {
				cleanupDelay = 1
			}
			deleteTagLabel := fmt.Sprintf("%s%s", jellysweepTagPrefix, time.Now().Add(time.Duration(cleanupDelay)*24*time.Hour).Format("2006-01-02"))

			if dryRun {
				log.Infof("Dry run: Would mark Radarr movie %s for deletion with tag %s", item.Title, deleteTagLabel)
				continue
			}

			if err := e.ensureRadarrTagExists(ctx, deleteTagLabel); err != nil {
				return err
			}
			// Add the delete tag to the movie
			movie := item.MovieResource
			tagID, err := e.getRadarrTagIDByLabel(deleteTagLabel)
			if err != nil {
				return err
			}
			movie.Tags = append(movie.Tags, tagID)
			// Update the movie in Radarr
			_, _, err = e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movie.GetId())).
				MovieResource(movie).
				Execute()
			if err != nil {
				return fmt.Errorf("failed to update Radarr movie %s with tag %s: %w", item.Title, deleteTagLabel, err)
			}
			log.Infof("Marked Radarr movie %s for deletion with tag %s", item.Title, deleteTagLabel)
		}
	}
	return nil
}

func (e *Engine) getRadarrTagIDByLabel(label string) (int32, error) {
	for id, tag := range e.data.radarrTags {
		if tag == label {
			return id, nil
		}
	}
	return 0, fmt.Errorf("Radarr tag with label %s not found", label)
}

func (e *Engine) ensureRadarrTagExists(ctx context.Context, deleteTagLabel string) error {
	for _, tag := range e.data.radarrTags {
		if tag == deleteTagLabel {
			return nil
		}
	}
	tag := radarr.TagResource{
		Label: *radarr.NewNullableString(&deleteTagLabel),
	}
	newTag, _, err := e.radarr.TagAPI.CreateTag(radarrAuthCtx(ctx, e.cfg.Radarr)).TagResource(tag).Execute()
	if err != nil {
		return fmt.Errorf("failed to create Radarr tag %s: %w", deleteTagLabel, err)
	}
	log.Infof("Created Radarr tag: %s", deleteTagLabel)
	e.data.radarrTags[newTag.GetId()] = newTag.GetLabel()
	return nil
}

func (e *Engine) cleanupRadarrTags(ctx context.Context) error {
	tags, _, err := e.radarr.TagDetailsAPI.ListTagDetail(radarrAuthCtx(ctx, e.cfg.Radarr)).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to list Radarr tags: %w", err)
	}
	for _, tag := range tags {
		if len(tag.MovieIds) == 0 && strings.HasPrefix(tag.GetLabel(), jellysweepTagPrefix) {
			// If the tag is a jellysweep delete tag and has no movies associated with it, delete it
			if e.cfg.Jellysweep.DryRun {
				log.Infof("Dry run: Would delete Radarr tag %s", tag.GetLabel())
				continue
			}
			_, err := e.radarr.TagAPI.DeleteTag(radarrAuthCtx(ctx, e.cfg.Radarr), tag.GetId()).Execute()
			if err != nil {
				return fmt.Errorf("failed to delete Radarr tag %s: %w", tag.GetLabel(), err)
			}
			log.Infof("Deleted Radarr tag: %s", tag.GetLabel())
		}
	}
	return nil
}

func (e *Engine) deleteRadarrMedia(ctx context.Context) error {
	triggerTagIDs, err := e.triggerTagIDs(e.data.radarrTags)
	if err != nil {
		return err
	}
	if len(triggerTagIDs) == 0 {
		log.Info("No Radarr tags found for deletion")
		return nil
	}

	for _, movie := range e.data.radarrItems {
		// Check if the movie has any of the trigger tags
		// check if slices have matching tag IDs
		var shouldDelete bool
		for _, tagID := range movie.GetTags() {
			if slices.Contains(triggerTagIDs, tagID) {
				shouldDelete = true
				break
			}
		}
		if !shouldDelete {
			continue
		}

		if e.cfg.Jellysweep.DryRun {
			log.Infof("Dry run: Would delete Radarr movie %s", movie.GetTitle())
			continue
		}
		// Delete the movie from Radarr
		_, err := e.radarr.MovieAPI.DeleteMovie(radarrAuthCtx(ctx, e.cfg.Radarr), movie.GetId()).
			DeleteFiles(true).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to delete Radarr movie %s: %w", movie.GetTitle(), err)
		}
		log.Infof("Deleted Radarr movie %s", movie.GetTitle())
	}

	return nil
}

// removeRecentlyPlayedRadarrDeleteTags removes jellysweep-delete tags from Radarr movies that have been played recently
func (e *Engine) removeRecentlyPlayedRadarrDeleteTags(ctx context.Context) error {
	// Use existing data from engine.data struct
	if e.data.radarrItems == nil || e.data.radarrTags == nil {
		log.Debug("No Radarr data available for removing recently played delete tags")
		return nil
	}

	for _, movie := range e.data.radarrItems {
		// Check if movie has any jellysweep-delete tags
		var deleteTagIDs []int32
		for _, tagID := range movie.GetTags() {
			if tagName, exists := e.data.radarrTags[tagID]; exists {
				if strings.HasPrefix(tagName, jellysweepTagPrefix) {
					deleteTagIDs = append(deleteTagIDs, tagID)
				}
			}
		}

		// Skip if no delete tags found
		if len(deleteTagIDs) == 0 {
			continue
		}

		// Find the matching jellystat item and library for this movie from original unfiltered data
		var matchingJellystatID string
		var libraryName string

		// Search through all jellystat items to find matching movie
		for _, jellystatItem := range e.data.jellystatItems {
			if jellystatItem.Type == jellystat.ItemTypeMovie && jellystatItem.Name == movie.GetTitle() {
				matchingJellystatID = jellystatItem.ID
				// Get library name from the library ID map
				if libName := e.getLibraryNameByID(jellystatItem.ParentID); libName != "" {
					libraryName = libName
				}
				break
			}
		}

		if matchingJellystatID == "" || libraryName == "" {
			log.Debugf("No matching Jellystat item or library found for Radarr movie: %s", movie.GetTitle())
			continue
		}

		// Check when the movie was last played
		lastPlayed, err := e.jellystat.GetLastPlayed(ctx, matchingJellystatID)
		if err != nil {
			log.Warnf("Failed to get last played time for movie %s: %v", movie.GetTitle(), err)
			continue
		}

		// If the movie has been played recently, remove the delete tags
		if lastPlayed != nil && lastPlayed.LastPlayed != nil {
			// Get the library config to get the threshold
			libraryConfig, exists := e.cfg.Jellysweep.Libraries[libraryName]
			if !exists {
				log.Warnf("Library config not found for library %s, skipping", libraryName)
				continue
			}

			timeSinceLastPlayed := time.Since(*lastPlayed.LastPlayed)
			thresholdDuration := time.Duration(libraryConfig.LastStreamThreshold) * 24 * time.Hour

			if timeSinceLastPlayed < thresholdDuration {
				// Remove delete tags
				updatedTags := make([]int32, 0)
				for _, tagID := range movie.GetTags() {
					if !slices.Contains(deleteTagIDs, tagID) {
						updatedTags = append(updatedTags, tagID)
					}
				}

				if e.cfg.Jellysweep.DryRun {
					log.Infof("Dry run: Would remove delete tags from recently played Radarr movie: %s (last played: %s)",
						movie.GetTitle(), lastPlayed.LastPlayed.Format(time.RFC3339))
					continue
				}

				// Update the movie with new tags
				movie.Tags = updatedTags
				_, _, err = e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movie.GetId())).
					MovieResource(movie).
					Execute()
				if err != nil {
					log.Errorf("Failed to update Radarr movie %s: %v", movie.GetTitle(), err)
					continue
				}

				log.Infof("Removed delete tags from recently played Radarr movie: %s (last played: %s)",
					movie.GetTitle(), lastPlayed.LastPlayed.Format(time.RFC3339))
			}
		}
	}

	return nil
}

// getRadarrMediaItemsMarkedForDeletion returns all Radarr movies that are marked for deletion
func (e *Engine) getRadarrMediaItemsMarkedForDeletion(ctx context.Context) ([]models.MediaItem, error) {
	var result []models.MediaItem

	if e.radarr == nil {
		return result, nil
	}

	// Get all movies from Radarr
	radarrItems, err := e.getRadarrItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get radarr items: %w", err)
	}

	// Get Radarr tags
	radarrTags, err := e.getRadarrTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get radarr tags: %w", err)
	}

	// Process each movie
	for _, movie := range radarrItems {
		for _, tagID := range movie.GetTags() {
			tagName := radarrTags[tagID]
			if strings.HasPrefix(tagName, jellysweepTagPrefix) {
				deletionDate, err := e.parseDeletionDateFromTag(tagName)
				if err != nil {
					log.Warnf("failed to parse deletion date from tag %s: %v", tagName, err)
					continue
				}

				imageURL := ""
				for _, image := range movie.GetImages() {
					if image.GetCoverType() == radarr.MEDIACOVERTYPES_POSTER {
						imageURL = image.GetRemoteUrl()
						break // Use the first poster image found
					}
				}

				mediaItem := models.MediaItem{
					ID:           fmt.Sprintf("radarr-%d", movie.GetId()),
					Title:        movie.GetTitle(),
					Type:         "movie",
					Year:         int(movie.GetYear()),
					Library:      "Movies",
					DeletionDate: deletionDate,
					PosterURL:    getCachedImageURL(imageURL),
					CanRequest:   true,
					HasRequested: false, // Would need to check against a database
				}

				result = append(result, mediaItem)
				break // Only add once per movie, even if multiple deletion tags
			}
		}
	}

	return result, nil
}
