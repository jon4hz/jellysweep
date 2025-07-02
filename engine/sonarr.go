package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/api/models"
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

func (e *Engine) markSonarrMediaItemsForDeletion(ctx context.Context, dryRun bool) error {
	for lib, items := range e.data.mediaItems {
		for _, item := range items {
			if item.MediaType != MediaTypeTV {
				continue // Only process TV series for Sonarr
			}

			// check if series has already a jellysweep delete tag, or keep tag
			for _, tagID := range item.SeriesResource.GetTags() {
				tagName := e.data.sonarrTags[tagID]
				if strings.HasPrefix(tagName, jellysweepKeepPrefix) {
					log.Debugf("Sonarr series %s has expired keep tag %s", item.Title, tagName)
				}
			}

			cleanupDelay := e.cfg.JellySweep.Libraries[lib].CleanupDelay
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
	return 0, fmt.Errorf("sonarr tag with label %s not found", label)
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
		if len(tag.SeriesIds) == 0 && (strings.HasPrefix(tag.GetLabel(), jellysweepTagPrefix) || strings.HasPrefix(tag.GetLabel(), jellysweepKeepRequestPrefix)) {
			// If the tag is a jellysweep delete tag and has no series associated with it, delete it
			if e.cfg.JellySweep.DryRun {
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

func (e *Engine) deleteSonarrMedia(ctx context.Context) ([]MediaItem, error) {
	var deletedItems []MediaItem

	triggerTagIDs, err := e.triggerTagIDs(e.data.sonarrTags)
	if err != nil {
		return deletedItems, err
	}
	if len(triggerTagIDs) == 0 {
		log.Info("No Sonarr tags found for deletion")
		return deletedItems, nil
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

		if e.cfg.JellySweep.DryRun {
			log.Infof("Dry run: Would delete Sonarr series %s", series.GetTitle())
			continue
		}
		// Delete the series from Sonarr
		_, err := e.sonarr.SeriesAPI.DeleteSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), series.GetId()).
			DeleteFiles(true).
			Execute()
		if err != nil {
			return deletedItems, fmt.Errorf("failed to delete Sonarr series %s: %w", series.GetTitle(), err)
		}
		log.Infof("Deleted Sonarr series %s", series.GetTitle())

		// Add to deleted items list
		deletedItems = append(deletedItems, MediaItem{
			Title:     series.GetTitle(),
			MediaType: MediaTypeTV,
			Year:      series.GetYear(),
		})
	}

	return deletedItems, nil
}

// removeExpiredSonarrKeepTags removes jellysweep-keep-request and jellysweep-keep tags from Sonarr series that have expired
func (e *Engine) removeExpiredSonarrKeepTags(ctx context.Context) error {
	if e.data.sonarrItems == nil || e.data.sonarrTags == nil {
		log.Debug("No Sonarr data available for removing expired keep tags")
		return nil
	}

	var expiredKeepTagIDs []int32
	for tagID, tagName := range e.data.sonarrTags {
		// Check for both jellysweep-keep-request- and jellysweep-keep- tags
		if strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) || strings.HasPrefix(tagName, jellysweepKeepPrefix) {
			// Parse the date from the tag name
			var dateStr string
			if strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) {
				dateStr = strings.TrimPrefix(tagName, jellysweepKeepRequestPrefix)
			} else {
				dateStr = strings.TrimPrefix(tagName, jellysweepKeepPrefix)
			}
			expirationDate, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				log.Warnf("Failed to parse date from Sonarr keep tag %s: %v", tagName, err)
				continue
			}
			if time.Now().After(expirationDate) {
				expiredKeepTagIDs = append(expiredKeepTagIDs, tagID)
			}
		}
	}

	for _, series := range e.data.sonarrItems {
		// get list of tags to keep
		keepTagIDs := make([]int32, 0)
		for _, tagID := range series.GetTags() {
			if !slices.Contains(expiredKeepTagIDs, tagID) {
				keepTagIDs = append(keepTagIDs, tagID)
			}
		}
		if len(keepTagIDs) == len(series.GetTags()) {
			// No expired keep tags to remove
			continue
		}
		if e.cfg.JellySweep.DryRun {
			log.Infof("Dry run: Would remove expired keep tags from Sonarr series %s", series.GetTitle())
			continue
		}

		// Update the series with the new tags
		series.Tags = keepTagIDs
		_, _, err := e.sonarr.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), fmt.Sprintf("%d", series.GetId())).
			SeriesResource(series).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to update Sonarr series %s: %w", series.GetTitle(), err)
		}
		log.Infof("Removed expired keep tags from Sonarr series %s", series.GetTitle())
	}

	return nil
}

// removeRecentlyPlayedSonarrDeleteTags removes jellysweep-delete tags from Sonarr series that have been played recently
func (e *Engine) removeRecentlyPlayedSonarrDeleteTags(ctx context.Context) error {
	if e.data.sonarrItems == nil || e.data.sonarrTags == nil {
		log.Debug("No Sonarr data available for removing recently played delete tags")
		return nil
	}

	for _, series := range e.data.sonarrItems {
		// Check if series has any jellysweep-delete tags
		var deleteTagIDs []int32
		for _, tagID := range series.GetTags() {
			if tagName, exists := e.data.sonarrTags[tagID]; exists {
				if strings.HasPrefix(tagName, jellysweepTagPrefix) ||
					strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) {
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
			libraryConfig, exists := e.cfg.JellySweep.Libraries[libraryName]
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

				if e.cfg.JellySweep.DryRun {
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

// getSonarrMediaItemsMarkedForDeletion returns all Sonarr series that are marked for deletion
func (e *Engine) getSonarrMediaItemsMarkedForDeletion(ctx context.Context) ([]models.MediaItem, error) {
	var result []models.MediaItem

	if e.sonarr == nil {
		return result, nil
	}

	// Get all series from Sonarr
	sonarrItems, err := e.getSonarrItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get sonarr items: %w", err)
	}

	// Get Sonarr tags
	sonarrTags, err := e.getSonarrTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	// Process each series
	for _, series := range sonarrItems {
		for _, tagID := range series.GetTags() {
			tagName := sonarrTags[tagID]
			if strings.HasPrefix(tagName, jellysweepTagPrefix) {
				deletionDate, err := e.parseDeletionDateFromTag(tagName)
				if err != nil {
					log.Warnf("failed to parse deletion date from tag %s: %v", tagName, err)
					continue
				}

				imageURL := ""
				for _, image := range series.GetImages() {
					if image.GetCoverType() == sonarr.MEDIACOVERTYPES_POSTER {
						imageURL = image.GetRemoteUrl()
						break // Use the first poster image found
					}
				}

				// Check if series has keep request, keep tags, or delete-for-sure tags
				canRequest := true
				hasRequested := false
				mustDelete := false
				for _, tagID := range series.GetTags() {
					tagName := sonarrTags[tagID]
					if strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) {
						hasRequested = true
						canRequest = false
					} else if strings.HasPrefix(tagName, jellysweepKeepPrefix) {
						// If it has an active keep tag, it shouldn't be requestable
						dateStr := strings.TrimPrefix(tagName, jellysweepKeepPrefix)
						keepDate, err := time.Parse("2006-01-02", dateStr)
						if err == nil && time.Now().Before(keepDate) {
							canRequest = false // Don't allow requests for items with active keep tags
						}
					} else if tagName == jellysweepDeleteForSureTag {
						canRequest = false // Don't allow requests but still show the media
						mustDelete = true  // This series is marked for deletion for sure
					}
				}

				mediaItem := models.MediaItem{
					ID:           fmt.Sprintf("sonarr-%d", series.GetId()),
					Title:        series.GetTitle(),
					Type:         "tv",
					Year:         series.GetYear(),
					Library:      "TV Shows",
					DeletionDate: deletionDate,
					PosterURL:    getCachedImageURL(imageURL),
					CanRequest:   canRequest,
					HasRequested: hasRequested,
					MustDelete:   mustDelete,
					FileSize:     getSeriesFileSize(series),
				}

				result = append(result, mediaItem)
				break // Only add once per series, even if multiple deletion tags
			}
		}
	}

	return result, nil
}

// addSonarrKeepRequestTag adds a keep request tag to a Sonarr series
func (e *Engine) addSonarrKeepRequestTag(ctx context.Context, seriesID int32) error {
	if e.sonarr == nil {
		return fmt.Errorf("sonarr client not available")
	}

	// Get the series
	series, _, err := e.sonarr.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, e.cfg.Sonarr), int32(seriesID)).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}

	// Get current tags
	sonarrTags, err := e.getSonarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	// Check if series already has a keep request or delete-for-sure tag
	for _, tagID := range series.GetTags() {
		tagName := sonarrTags[tagID]
		if strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) {
			return fmt.Errorf("keep request already exists for this series")
		}
		if tagName == jellysweepDeleteForSureTag {
			return fmt.Errorf("keep requests are not allowed for this series")
		}
	}

	// Create keep request tag with 90 days expiry
	expiryDate := time.Now().Add(90 * 24 * time.Hour)
	keepRequestTag := fmt.Sprintf("%s%s", jellysweepKeepRequestPrefix, expiryDate.Format("2006-01-02"))

	// Ensure the tag exists
	if err := e.ensureSonarrTagExists(ctx, keepRequestTag); err != nil {
		return fmt.Errorf("failed to create keep request tag: %w", err)
	}

	// Get tag ID
	tagID, err := e.getSonarrTagIDByLabel(keepRequestTag)
	if err != nil {
		return fmt.Errorf("failed to get tag ID: %w", err)
	}

	// Add the keep request tag
	series.Tags = append(series.Tags, tagID)

	// Update the series
	_, _, err = e.sonarr.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), fmt.Sprintf("%d", seriesID)).
		SeriesResource(*series).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update sonarr series: %w", err)
	}

	log.Infof("Added keep request tag %s to Sonarr series %s", keepRequestTag, series.GetTitle())
	return nil
}

// getSonarrKeepRequests returns all Sonarr series that have keep request tags
func (e *Engine) getSonarrKeepRequests(ctx context.Context) ([]models.KeepRequest, error) {
	var result []models.KeepRequest

	if e.sonarr == nil {
		return result, nil
	}

	// Get all series from Sonarr
	sonarrItems, err := e.getSonarrItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get sonarr items: %w", err)
	}

	// Get Sonarr tags
	sonarrTags, err := e.getSonarrTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	// Process each series
	for _, series := range sonarrItems {
		for _, tagID := range series.GetTags() {
			tagName := sonarrTags[tagID]
			if strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) {
				// skip if the movie has a delete-for-sure tag
				forSureTag, err := e.getSonarrTagIDByLabel(jellysweepDeleteForSureTag)
				if err != nil {
					log.Warnf("failed to get delete-for-sure tag ID: %v", err)
				}
				if slices.Contains(series.GetTags(), forSureTag) {
					log.Debugf("Skipping Sonarr series %s as it has a delete-for-sure tag", series.GetTitle())
					continue
				}
				// Parse expiry date from tag
				expiryDateStr := strings.TrimPrefix(tagName, jellysweepKeepRequestPrefix)
				expiryDate, err := time.Parse("2006-01-02", expiryDateStr)
				if err != nil {
					log.Warnf("failed to parse expiry date from tag %s: %v", tagName, err)
					continue
				}

				// Get deletion date from delete tag (if exists)
				var deletionDate time.Time
				for _, deletionTagID := range series.GetTags() {
					deletionTagName := sonarrTags[deletionTagID]
					if strings.HasPrefix(deletionTagName, jellysweepTagPrefix) {
						if parsedDate, err := e.parseDeletionDateFromTag(deletionTagName); err == nil {
							deletionDate = parsedDate
							break
						}
					}
				}

				imageURL := ""
				for _, image := range series.GetImages() {
					if image.GetCoverType() == sonarr.MEDIACOVERTYPES_POSTER {
						imageURL = image.GetRemoteUrl()
						break
					}
				}

				keepRequest := models.KeepRequest{
					ID:           fmt.Sprintf("sonarr-%d", series.GetId()),
					MediaID:      fmt.Sprintf("sonarr-%d", series.GetId()),
					Title:        series.GetTitle(),
					Type:         "tv",
					Year:         int(series.GetYear()),
					Library:      "TV Shows",
					DeletionDate: deletionDate,
					PosterURL:    getCachedImageURL(imageURL),
					RequestedBy:  "user",     // Would need to extract from tag or store separately
					RequestDate:  time.Now(), // Would need to store separately
					ExpiryDate:   expiryDate,
				}

				result = append(result, keepRequest)
				break // Only add once per series
			}
		}
	}

	return result, nil
}

// Helper methods for accepting/declining Sonarr keep requests
func (e *Engine) acceptSonarrKeepRequest(ctx context.Context, seriesID int32) error {
	if err := e.removeSonarrKeepRequestAndDeleteTags(ctx, seriesID); err != nil {
		return err
	}
	return e.addSonarrKeepTag(ctx, seriesID)
}

func (e *Engine) declineSonarrKeepRequest(ctx context.Context, seriesID int32) error {
	return e.addSonarrDeleteForSureTag(ctx, seriesID)
}

func (e *Engine) removeSonarrKeepRequestAndDeleteTags(ctx context.Context, seriesID int32) error {
	if e.sonarr == nil {
		return fmt.Errorf("sonarr client not available")
	}

	// Get the series
	series, _, err := e.sonarr.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, e.cfg.Sonarr), seriesID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}

	// Get current tags
	sonarrTags, err := e.getSonarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	// Remove keep request and delete tags
	var newTags []int32
	for _, tagID := range series.GetTags() {
		tagName := sonarrTags[tagID]
		if !strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) &&
			!strings.HasPrefix(tagName, jellysweepTagPrefix) {
			newTags = append(newTags, tagID)
		}
	}

	series.Tags = newTags

	// Update the series
	_, _, err = e.sonarr.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), fmt.Sprintf("%d", seriesID)).
		SeriesResource(*series).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update sonarr series: %w", err)
	}

	log.Infof("Removed keep request and delete tags from Sonarr series %s", series.GetTitle())
	return nil
}

func (e *Engine) addSonarrDeleteForSureTag(ctx context.Context, seriesID int32) error {
	if e.sonarr == nil {
		return fmt.Errorf("sonarr client not available")
	}

	// Get the series
	series, _, err := e.sonarr.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, e.cfg.Sonarr), seriesID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}

	// Ensure the delete-for-sure tag exists
	if err := e.ensureSonarrTagExists(ctx, jellysweepDeleteForSureTag); err != nil {
		return fmt.Errorf("failed to create delete-for-sure tag: %w", err)
	}

	// Get tag ID
	tagID, err := e.getSonarrTagIDByLabel(jellysweepDeleteForSureTag)
	if err != nil {
		return fmt.Errorf("failed to get tag ID: %w", err)
	}

	// Add the tag to the series
	series.Tags = append(series.Tags, tagID)

	// Update the series
	_, _, err = e.sonarr.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), fmt.Sprintf("%d", seriesID)).
		SeriesResource(*series).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update sonarr series: %w", err)
	}

	log.Infof("Added delete-for-sure tag to Sonarr series %s", series.GetTitle())
	return nil
}

func (e *Engine) addSonarrKeepTag(ctx context.Context, seriesID int32) error {
	if e.sonarr == nil {
		return fmt.Errorf("sonarr client not available")
	}

	// Get the series
	series, _, err := e.sonarr.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, e.cfg.Sonarr), seriesID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}

	// Create keep tag with 90 days expiry
	expiryDate := time.Now().Add(90 * 24 * time.Hour)
	keepTag := fmt.Sprintf("%s%s", jellysweepKeepPrefix, expiryDate.Format("2006-01-02"))

	// Ensure the tag exists
	if err := e.ensureSonarrTagExists(ctx, keepTag); err != nil {
		return fmt.Errorf("failed to create keep tag: %w", err)
	}

	// Get tag ID
	tagID, err := e.getSonarrTagIDByLabel(keepTag)
	if err != nil {
		return fmt.Errorf("failed to get tag ID: %w", err)
	}

	// Add the tag to the series
	series.Tags = append(series.Tags, tagID)

	// Update the series
	_, _, err = e.sonarr.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), fmt.Sprintf("%d", seriesID)).
		SeriesResource(*series).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update sonarr series: %w", err)
	}

	log.Infof("Added keep tag %s to Sonarr series %s", keepTag, series.GetTitle())
	return nil
}

// resetSonarrTags removes all jellysweep tags from all Sonarr series
func (e *Engine) resetSonarrTags(ctx context.Context) error {
	if e.sonarr == nil {
		return nil
	}

	// Get all series
	series, _, err := e.sonarr.SeriesAPI.ListSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr)).Execute()
	if err != nil {
		return fmt.Errorf("failed to list Sonarr series: %w", err)
	}

	// Get all tags to map tag IDs to names
	tags, err := e.getSonarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr tags: %w", err)
	}

	seriesUpdated := 0
	for _, s := range series {
		// Check if series has any jellysweep tags
		var hasJellysweepTags bool
		var newTags []int32

		for _, tagID := range s.GetTags() {
			tagName := tags[tagID]
			isJellysweepTag := strings.HasPrefix(tagName, jellysweepTagPrefix) ||
				strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) ||
				strings.HasPrefix(tagName, jellysweepKeepPrefix) ||
				tagName == jellysweepDeleteForSureTag

			if isJellysweepTag {
				hasJellysweepTags = true
				log.Debugf("Removing jellysweep tag '%s' from Sonarr series: %s", tagName, s.GetTitle())
			} else {
				newTags = append(newTags, tagID)
			}
		}

		// Update series if it had jellysweep tags
		if hasJellysweepTags {
			if e.cfg.JellySweep.DryRun {
				log.Infof("Dry run: Would remove jellysweep tags from Sonarr series: %s", s.GetTitle())
				seriesUpdated++
				continue
			}

			s.Tags = newTags
			_, _, err = e.sonarr.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), fmt.Sprintf("%d", s.GetId())).
				SeriesResource(s).
				Execute()
			if err != nil {
				log.Errorf("Failed to update Sonarr series %s: %v", s.GetTitle(), err)
				continue
			}
			log.Infof("Removed jellysweep tags from Sonarr series: %s", s.GetTitle())
			seriesUpdated++
		}
	}

	log.Infof("Updated %d Sonarr series", seriesUpdated)
	return nil
}

// cleanupAllSonarrTags removes all unused jellysweep tags from Sonarr
func (e *Engine) cleanupAllSonarrTags(ctx context.Context) error {
	if e.sonarr == nil {
		return nil
	}

	tags, _, err := e.sonarr.TagDetailsAPI.ListTagDetail(sonarrAuthCtx(ctx, e.cfg.Sonarr)).Execute()
	if err != nil {
		return fmt.Errorf("failed to list Sonarr tags: %w", err)
	}

	tagsDeleted := 0
	for _, tag := range tags {
		tagName := tag.GetLabel()
		isJellysweepTag := strings.HasPrefix(tagName, jellysweepTagPrefix) ||
			strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) ||
			strings.HasPrefix(tagName, jellysweepKeepPrefix) ||
			tagName == jellysweepDeleteForSureTag

		if isJellysweepTag {
			if e.cfg.JellySweep.DryRun {
				log.Infof("Dry run: Would delete Sonarr tag: %s", tagName)
				tagsDeleted++
				continue
			}

			_, err := e.sonarr.TagAPI.DeleteTag(sonarrAuthCtx(ctx, e.cfg.Sonarr), tag.GetId()).Execute()
			if err != nil {
				log.Errorf("Failed to delete Sonarr tag %s: %v", tagName, err)
				continue
			}
			log.Infof("Deleted Sonarr tag: %s", tagName)
			tagsDeleted++
		}
	}

	log.Infof("Deleted %d Sonarr tags", tagsDeleted)
	return nil
}

// getSeriesFileSize extracts the file size from a Sonarr series statistics
func getSeriesFileSize(series sonarr.SeriesResource) int64 {
	if series.HasStatistics() {
		stats := series.GetStatistics()
		if stats.HasSizeOnDisk() {
			return stats.GetSizeOnDisk()
		}
	}
	return 0
}

// resetSingleSonarrTagsForKeep removes ALL tags (including jellysweep-delete) from a single Sonarr series
func (e *Engine) resetSingleSonarrTagsForKeep(ctx context.Context, seriesID int32) error {
	if e.sonarr == nil {
		return fmt.Errorf("sonarr client not available")
	}

	// Get the series
	series, _, err := e.sonarr.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, e.cfg.Sonarr), seriesID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}

	// Get all tags to map tag IDs to names
	tags, err := e.getSonarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	// Check if series has any jellysweep tags and filter them all out (including delete tags)
	var hasJellysweepTags bool
	var newTags []int32

	for _, tagID := range series.GetTags() {
		tagName := tags[tagID]
		isJellysweepTag := strings.HasPrefix(tagName, jellysweepTagPrefix) ||
			strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) ||
			strings.HasPrefix(tagName, jellysweepKeepPrefix) ||
			tagName == jellysweepDeleteForSureTag

		if isJellysweepTag {
			hasJellysweepTags = true
			log.Debugf("Removing jellysweep tag '%s' from Sonarr series: %s", tagName, series.GetTitle())
		} else {
			newTags = append(newTags, tagID)
		}
	}

	// Update series if it had jellysweep tags
	if hasJellysweepTags {
		series.Tags = newTags
		_, _, err = e.sonarr.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), fmt.Sprintf("%d", seriesID)).
			SeriesResource(*series).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to update sonarr series: %w", err)
		}
		log.Infof("Removed all jellysweep tags from Sonarr series for keep action: %s", series.GetTitle())
	}

	return nil
}

// resetSingleSonarrTagsForMustDelete removes all jellysweep tags EXCEPT jellysweep-delete from a single Sonarr series
func (e *Engine) resetSingleSonarrTagsForMustDelete(ctx context.Context, seriesID int32) error {
	if e.sonarr == nil {
		return fmt.Errorf("sonarr client not available")
	}

	// Get the series
	series, _, err := e.sonarr.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, e.cfg.Sonarr), seriesID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}

	// Get all tags to map tag IDs to names
	tags, err := e.getSonarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	// Check if series has any jellysweep tags and filter them out (except jellysweep-delete tags)
	var hasJellysweepTags bool
	var newTags []int32

	for _, tagID := range series.GetTags() {
		tagName := tags[tagID]
		isJellysweepDeleteTag := strings.HasPrefix(tagName, jellysweepTagPrefix) // jellysweep-delete-*
		isOtherJellysweepTag := strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) ||
			strings.HasPrefix(tagName, jellysweepKeepPrefix) ||
			tagName == jellysweepDeleteForSureTag

		if isOtherJellysweepTag {
			hasJellysweepTags = true
			log.Debugf("Removing jellysweep tag '%s' from Sonarr series: %s", tagName, series.GetTitle())
		} else if isJellysweepDeleteTag {
			// Keep jellysweep-delete tags
			newTags = append(newTags, tagID)
		} else {
			// Keep non-jellysweep tags
			newTags = append(newTags, tagID)
		}
	}

	// Update series if it had jellysweep tags
	if hasJellysweepTags {
		series.Tags = newTags
		_, _, err = e.sonarr.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, e.cfg.Sonarr), fmt.Sprintf("%d", seriesID)).
			SeriesResource(*series).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to update sonarr series: %w", err)
		}
		log.Infof("Removed jellysweep tags (except delete tags) from Sonarr series for must-delete action: %s", series.GetTitle())
	}

	return nil
}
