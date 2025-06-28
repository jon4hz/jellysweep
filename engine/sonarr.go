package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/jellystat"
)

func sonarrAuthCtx(ctx context.Context, cfg *config.SonarrConfig) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(
		ctx,
		sonarr.ContextAPIKeys,
		map[string]sonarr.APIKey{
			"X-Api-Key": {Key: cfg.APIKey},
		},
	)
}

func newSonarrClient(cfg *config.SonarrConfig) (*sonarr.APIClient, error) {
	scfg := sonarr.NewConfiguration()

	if strings.HasPrefix(cfg.URL, "http://") {
		scfg.Scheme = "http"
		cfg.URL = strings.TrimPrefix(cfg.URL, "http://")
	} else if strings.HasPrefix(cfg.URL, "https://") {
		scfg.Scheme = "https"
		cfg.URL = strings.TrimPrefix(cfg.URL, "https://")
	}

	scfg.Host = cfg.URL

	return sonarr.NewAPIClient(scfg), nil
}

// getSonarrItems retrieves all series from Sonarr.
func (e *Engine) getSonarrItems(ctx context.Context) ([]sonarr.SeriesResource, error) {
	series, _, err := e.sonarr.SeriesAPI.ListSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr)).IncludeSeasonImages(false).Execute()
	if err != nil {
		return nil, err
	}
	return series, nil
}

// getSonarrTags retrieves all tags from Sonarr and returns them as a map with tag IDs as keys and tag names as values.
func (e *Engine) getSonarrTags(ctx context.Context) (map[int32]string, error) {
	tags, _, err := e.sonarr.TagAPI.ListTag(sonarrAuthCtx(ctx, e.cfg.Sonarr)).Execute()
	if err != nil {
		return nil, err
	}
	tagMap := make(map[int32]string)
	for _, tag := range tags {
		tagMap[tag.GetId()] = tag.GetLabel()
	}
	return tagMap, nil
}

func (e *Engine) filterSonarrTags() error {
	if e.data.sonarrItems == nil || e.data.sonarrTags == nil {
		log.Warn("No Sonarr items or tags available for filtering")
		return nil // No items or tags to filter
	}
	filteredItems := make(map[string][]MediaItem, 0)
	for lib, items := range e.data.mediaItems {
		for _, item := range items {
			// Check if the item has any tags that are not in the exclude list
			hasExcludedTag := false
			for _, tagName := range item.Tags {
				for _, tag := range e.data.sonarrTags {
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

func (e *Engine) markSonarrMediaItemsForDeletion(ctx context.Context, dryRun bool) error {
	for lib, items := range e.data.mediaItems {
	seriesLoop:
		for _, item := range items {
			if item.MediaType != MediaTypeTV {
				continue // Only process TV series for Sonarr
			}

			// check if series has already a jellysweep delete tag
			for _, tagID := range item.SeriesResource.GetTags() {
				if strings.HasPrefix(e.data.sonarrTags[tagID], jellysweepTagPrefix) {
					log.Debugf("Sonarr series %s already marked for deletion with tag %s", item.Title, e.data.sonarrTags[tagID])
					continue seriesLoop
				}
			}

			cleanupDelay := e.cfg.Jellysweep.Libraries[lib].CleanupDelay
			if cleanupDelay <= 0 {
				cleanupDelay = 1
			}
			deleteTagLabel := fmt.Sprintf("%s%s", jellysweepTagPrefix, time.Now().Add(time.Duration(cleanupDelay)*24*time.Hour).Format("2006-01-02"))

			if dryRun {
				log.Infof("Dry run: Would mark Sonarr series %s for deletion with tag %s", item.Title, deleteTagLabel)
				continue
			}

			if err := e.ensureSonarrTagExists(ctx, deleteTagLabel); err != nil {
				return err
			}
			// Add the delete tag to the series
			series := item.SeriesResource
			tagID, err := e.getSonarrTagIDByLabel(deleteTagLabel)
			if err != nil {
				return err
			}
			fmt.Println(tagID)
			series.Tags = append(series.Tags, tagID)
			// Update the series in Sonarr
			_, _, err = e.sonarr.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), fmt.Sprintf("%d", series.GetId())).
				SeriesResource(series).
				Execute()
			if err != nil {
				return fmt.Errorf("failed to update Sonarr series %s with tag %s: %w", item.Title, deleteTagLabel, err)
			}
			log.Infof("Marked Sonarr series %s for deletion with tag %s", item.Title, deleteTagLabel)
		}
	}
	return nil
}

func (e *Engine) getSonarrTagIDByLabel(label string) (int32, error) {
	for id, tag := range e.data.sonarrTags {
		if tag == label {
			return id, nil
		}
	}
	return 0, fmt.Errorf("Sonarr tag with label %s not found", label)
}

func (e *Engine) ensureSonarrTagExists(ctx context.Context, deleteTagLabel string) error {
	for _, tag := range e.data.sonarrTags {
		if tag == deleteTagLabel {
			return nil
		}
	}
	tag := sonarr.TagResource{
		Label: *sonarr.NewNullableString(&deleteTagLabel),
	}
	newTag, _, err := e.sonarr.TagAPI.CreateTag(sonarrAuthCtx(ctx, e.cfg.Sonarr)).TagResource(tag).Execute()
	if err != nil {
		return fmt.Errorf("failed to create Sonarr tag %s: %w", deleteTagLabel, err)
	}
	log.Infof("Created Sonarr tag: %s", deleteTagLabel)
	e.data.sonarrTags[newTag.GetId()] = newTag.GetLabel()
	return nil
}

func (e *Engine) cleanupSonarrTags(ctx context.Context) error {
	tags, _, err := e.sonarr.TagDetailsAPI.ListTagDetail(sonarrAuthCtx(ctx, e.cfg.Sonarr)).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to list Sonarr tags: %w", err)
	}
	for _, tag := range tags {
		if len(tag.SeriesIds) == 0 && strings.HasPrefix(tag.GetLabel(), jellysweepTagPrefix) {
			// If the tag is a jellysweep delete tag and has no series associated with it, delete it
			if e.cfg.Jellysweep.DryRun {
				log.Infof("Dry run: Would delete Sonarr tag %s", tag.GetLabel())
				continue
			}
			_, err := e.sonarr.TagAPI.DeleteTag(sonarrAuthCtx(ctx, e.cfg.Sonarr), tag.GetId()).Execute()
			if err != nil {
				return fmt.Errorf("failed to delete Sonarr tag %s: %w", tag.GetLabel(), err)
			}
			log.Infof("Deleted Sonarr tag: %s", tag.GetLabel())
		}
	}
	return nil
}

func (e *Engine) deleteSonarrMedia(ctx context.Context) error {
	triggerTagIDs, err := e.triggerTagIDs(e.data.sonarrTags)
	if err != nil {
		return err
	}
	if len(triggerTagIDs) == 0 {
		log.Info("No Sonarr tags found for deletion")
		return nil
	}

	for _, series := range e.data.sonarrItems {
		// Check if the series has any of the trigger tags
		// chec if slices have matching tag IDs
		var shouldDelete bool
		for _, tagID := range series.GetTags() {
			if slices.Contains(triggerTagIDs, tagID) {
				shouldDelete = true
				break
			}
		}
		if !shouldDelete {
			continue
		}

		if e.cfg.Jellysweep.DryRun {
			log.Infof("Dry run: Would delete Sonarr series %s", series.GetTitle())
			continue
		}
		// Delete the series from Sonarr
		_, err := e.sonarr.SeriesAPI.DeleteSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), series.GetId()).
			DeleteFiles(true).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to delete Sonarr series %s: %w", series.GetTitle(), err)
		}
		log.Infof("Deleted Sonarr series %s", series.GetTitle())
		return nil // TODO: remove
	}

	return nil
}

// removeRecentlyPlayedSonarrDeleteTags removes jellysweep-delete tags from Sonarr series that have been played recently
func (e *Engine) removeRecentlyPlayedSonarrDeleteTags(ctx context.Context) error {
	// Use existing data from engine.data struct
	if e.data.sonarrItems == nil || e.data.sonarrTags == nil {
		log.Debug("No Sonarr data available for removing recently played delete tags")
		return nil
	}

	for _, series := range e.data.sonarrItems {
		// Check if series has any jellysweep-delete tags
		var deleteTagIDs []int32
		for _, tagID := range series.GetTags() {
			if tagName, exists := e.data.sonarrTags[tagID]; exists {
				if strings.HasPrefix(tagName, jellysweepTagPrefix) {
					deleteTagIDs = append(deleteTagIDs, tagID)
				}
			}
		}

		// Skip if no delete tags found
		if len(deleteTagIDs) == 0 {
			continue
		}

		// Find the matching jellystat item and library for this series from original unfiltered data
		var matchingJellystatID string
		var libraryName string

		// Search through all jellystat items to find matching series
		for _, jellystatItem := range e.data.jellystatItems {
			if jellystatItem.Type == jellystat.ItemTypeSeries && jellystatItem.Name == series.GetTitle() && jellystatItem.ProductionYear == series.GetYear() {
				matchingJellystatID = jellystatItem.ID
				// Get library name from the library ID map
				if libName := e.getLibraryNameByID(jellystatItem.ParentID); libName != "" {
					libraryName = libName
				}
				break
			}
		}

		if matchingJellystatID == "" || libraryName == "" {
			log.Debugf("No matching Jellystat item or library found for Sonarr series: %s", series.GetTitle())
			continue
		}

		// Check when the series was last played
		lastPlayed, err := e.jellystat.GetLastPlayed(ctx, matchingJellystatID)
		if err != nil {
			log.Warnf("Failed to get last played time for series %s: %v", series.GetTitle(), err)
			continue
		}

		// If the series has been played recently, remove the delete tags
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
				for _, tagID := range series.GetTags() {
					if !slices.Contains(deleteTagIDs, tagID) {
						updatedTags = append(updatedTags, tagID)
					}
				}

				if e.cfg.Jellysweep.DryRun {
					log.Infof("Dry run: Would remove delete tags from recently played Sonarr series: %s (last played: %s)",
						series.GetTitle(), lastPlayed.LastPlayed.Format(time.RFC3339))
					continue
				}

				// Update the series with new tags
				series.Tags = updatedTags
				_, _, err = e.sonarr.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), fmt.Sprintf("%d", series.GetId())).
					SeriesResource(series).
					Execute()
				if err != nil {
					log.Errorf("Failed to update Sonarr series %s: %v", series.GetTitle(), err)
					continue
				}

				log.Infof("Removed delete tags from recently played Sonarr series: %s (last played: %s)",
					series.GetTitle(), lastPlayed.LastPlayed.Format(time.RFC3339))
			}
		}
	}

	return nil
}
