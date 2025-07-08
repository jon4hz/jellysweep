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

func newRadarrClient(cfg *config.RadarrConfig) *radarr.APIClient {
	rcfg := radarr.NewConfiguration()

	// Don't modify the original config URL, work with a copy
	url := cfg.URL

	if strings.HasPrefix(url, "http://") {
		rcfg.Scheme = "http"
		url = strings.TrimPrefix(url, "http://")
	} else if strings.HasPrefix(url, "https://") {
		rcfg.Scheme = "https"
		url = strings.TrimPrefix(url, "https://")
	}

	rcfg.Host = url

	return radarr.NewAPIClient(rcfg)
}

// getRadarrItems retrieves all movies from Radarr.
func (e *Engine) getRadarrItems(ctx context.Context) ([]radarr.MovieResource, error) {
	if e.radarr == nil {
		return nil, fmt.Errorf("radarr client not available")
	}
	movies, resp, err := e.radarr.MovieAPI.ListMovie(radarrAuthCtx(ctx, e.cfg.Radarr)).Execute()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint: errcheck
	return movies, nil
}

// getRadarrTags retrieves all tags from Radarr and returns them as a map with tag IDs as keys and tag names as values.
func (e *Engine) getRadarrTags(ctx context.Context) (map[int32]string, error) {
	if e.radarr == nil {
		return nil, fmt.Errorf("radarr client not available")
	}
	tags, resp, err := e.radarr.TagAPI.ListTag(radarrAuthCtx(ctx, e.cfg.Radarr)).Execute()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint: errcheck
	tagMap := make(map[int32]string)
	for _, tag := range tags {
		tagMap[tag.GetId()] = tag.GetLabel()
	}
	return tagMap, nil
}

func (e *Engine) markRadarrMediaItemsForDeletion(ctx context.Context, dryRun bool) error {
	for lib, items := range e.data.mediaItems {
	movieLoop:
		for _, item := range items {
			if item.MediaType != MediaTypeMovie {
				continue // Only process movies for Radarr
			}

			// check if movie has already a jellysweep delete tag or keep tag
			for _, tagID := range item.MovieResource.GetTags() {
				tagName := e.data.radarrTags[tagID]
				if strings.HasPrefix(tagName, jellysweepTagPrefix) {
					log.Debugf("Radarr movie %s already marked for deletion with tag %s", item.Title, tagName)
					continue movieLoop
				}
				if strings.HasPrefix(tagName, JellysweepKeepPrefix) {
					log.Debugf("Radarr movie %s has expired keep tag %s", item.Title, tagName)
				}
			}

			libraryConfig := e.cfg.GetLibraryConfig(lib)
			cleanupDelay := 1 // default
			if libraryConfig != nil {
				cleanupDelay = libraryConfig.CleanupDelay
				if cleanupDelay <= 0 {
					cleanupDelay = 1
				}
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
			_, resp, err := e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movie.GetId())).
				MovieResource(movie).
				Execute()
			if err != nil {
				return fmt.Errorf("failed to update Radarr movie %s with tag %s: %w", item.Title, deleteTagLabel, err)
			}
			defer resp.Body.Close() //nolint: errcheck
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
	return 0, fmt.Errorf("radarr tag with label %s not found", label)
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
	newTag, resp, err := e.radarr.TagAPI.CreateTag(radarrAuthCtx(ctx, e.cfg.Radarr)).TagResource(tag).Execute()
	if err != nil {
		return fmt.Errorf("failed to create Radarr tag %s: %w", deleteTagLabel, err)
	}
	defer resp.Body.Close() //nolint: errcheck
	log.Infof("Created Radarr tag: %s", deleteTagLabel)
	e.data.radarrTags[newTag.GetId()] = newTag.GetLabel()
	return nil
}

func (e *Engine) cleanupRadarrTags(ctx context.Context) error {
	tags, resp, err := e.radarr.TagDetailsAPI.ListTagDetail(radarrAuthCtx(ctx, e.cfg.Radarr)).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to list Radarr tags: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck
	for _, tag := range tags {
		if len(tag.MovieIds) == 0 && strings.HasPrefix(tag.GetLabel(), jellysweepTagPrefix) {
			// If the tag is a jellysweep delete tag and has no movies associated with it, delete it
			if e.cfg.DryRun {
				log.Infof("Dry run: Would delete Radarr tag %s", tag.GetLabel())
				continue
			}
			resp, err := e.radarr.TagAPI.DeleteTag(radarrAuthCtx(ctx, e.cfg.Radarr), tag.GetId()).Execute()
			if err != nil {
				return fmt.Errorf("failed to delete Radarr tag %s: %w", tag.GetLabel(), err)
			}
			defer resp.Body.Close() //nolint: errcheck
			log.Infof("Deleted Radarr tag: %s", tag.GetLabel())
		}
	}
	return nil
}

func (e *Engine) deleteRadarrMedia(ctx context.Context) ([]MediaItem, error) {
	deletedItems := make([]MediaItem, 0)

	triggerTagIDs := e.triggerTagIDs(e.data.radarrTags)

	if len(triggerTagIDs) == 0 {
		log.Info("No Radarr tags found for deletion")
		return deletedItems, nil
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

		if e.cfg.DryRun {
			log.Infof("Dry run: Would delete Radarr movie %s", movie.GetTitle())
			continue
		}
		// Delete the movie from Radarr
		resp, err := e.radarr.MovieAPI.DeleteMovie(radarrAuthCtx(ctx, e.cfg.Radarr), movie.GetId()).
			DeleteFiles(true).
			Execute()
		if err != nil {
			return deletedItems, fmt.Errorf("failed to delete Radarr movie %s: %w", movie.GetTitle(), err)
		}
		defer resp.Body.Close() //nolint: errcheck
		log.Infof("Deleted Radarr movie %s", movie.GetTitle())

		// Add to deleted items list
		deletedItems = append(deletedItems, MediaItem{
			Title:     movie.GetTitle(),
			MediaType: MediaTypeMovie,
			Year:      movie.GetYear(),
		})
	}

	return deletedItems, nil
}

// removeRecentlyPlayedRadarrDeleteTags removes jellysweep-delete tags from Radarr movies that have been played recently.
func (e *Engine) removeRecentlyPlayedRadarrDeleteTags(ctx context.Context) {
	// Use existing data from engine.data struct
	if e.data.radarrItems == nil || e.data.radarrTags == nil {
		log.Debug("No Radarr data available for removing recently played delete tags")
		return
	}

	for _, movie := range e.data.radarrItems {
		select {
		case <-ctx.Done():
			log.Warn("Context cancelled, stopping removal of recently played Radarr delete tags")
			return
		default:
			// Continue processing if context is not cancelled
		}

		// Check if movie has any jellysweep-delete tags
		var deleteTagIDs []int32
		for _, tagID := range movie.GetTags() {
			if tagName, exists := e.data.radarrTags[tagID]; exists {
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

		// Find the matching jellystat item and library for this movie from original unfiltered data
		var matchingJellystatID string
		var libraryName string

		// Search through all jellystat items to find matching movie
		for _, jellystatItem := range e.data.jellystatItems {
			if jellystatItem.Type == jellystat.ItemTypeMovie &&
				jellystatItem.Name == movie.GetTitle() &&
				jellystatItem.ProductionYear == movie.GetYear() {
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
			libraryConfig := e.cfg.GetLibraryConfig(libraryName)
			if libraryConfig == nil {
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

				if e.cfg.DryRun {
					log.Infof("Dry run: Would remove delete tags from recently played Radarr movie: %s (last played: %s)",
						movie.GetTitle(), lastPlayed.LastPlayed.Format(time.RFC3339))
					continue
				}

				// Update the movie with new tags
				movie.Tags = updatedTags
				_, resp, err := e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movie.GetId())).
					MovieResource(movie).
					Execute()
				if err != nil {
					log.Errorf("Failed to update Radarr movie %s: %v", movie.GetTitle(), err)
					continue
				}
				defer resp.Body.Close() //nolint: errcheck

				log.Infof("Removed delete tags from recently played Radarr movie: %s (last played: %s)",
					movie.GetTitle(), lastPlayed.LastPlayed.Format(time.RFC3339))
			}
		}
	}
}

// removeExpiredRadarrKeepTags removes jellysweep-keep-request and jellysweep-keep tags from Radarr movies that have expired.
func (e *Engine) removeExpiredRadarrKeepTags(ctx context.Context) error {
	if e.data.radarrItems == nil || e.data.radarrTags == nil {
		log.Debug("No Radarr data available for removing expired keep tags")
		return nil
	}

	var expiredKeepTagIDs []int32
	for tagID, tagName := range e.data.radarrTags {
		// Check for both jellysweep-keep-request- and jellysweep-keep- tags
		if strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) || strings.HasPrefix(tagName, JellysweepKeepPrefix) {
			// Parse the date from the tag name using the appropriate parser
			var expirationDate time.Time
			var err error
			if strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) {
				expirationDate, _, err = e.parseKeepRequestTagWithRequester(tagName)
			} else {
				expirationDate, _, err = e.parseKeepTagWithRequester(tagName)
			}
			if err != nil {
				log.Warnf("Failed to parse date from Radarr keep tag %s: %v", tagName, err)
				continue
			}
			if time.Now().After(expirationDate) {
				expiredKeepTagIDs = append(expiredKeepTagIDs, tagID)
			}
		}
	}

	for _, movie := range e.data.radarrItems {
		// get list of tags to keep
		keepTagIDs := make([]int32, 0)
		for _, tagID := range movie.GetTags() {
			if !slices.Contains(expiredKeepTagIDs, tagID) {
				keepTagIDs = append(keepTagIDs, tagID)
			}
		}
		if len(keepTagIDs) == len(movie.GetTags()) {
			// No expired keep tags to remove
			continue
		}
		if e.cfg.DryRun {
			log.Infof("Dry run: Would remove expired keep tags from Radarr movie %s", movie.GetTitle())
			continue
		}

		// Update the movie with the new tags
		movie.Tags = keepTagIDs
		_, resp, err := e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movie.GetId())).
			MovieResource(movie).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to update Radarr movie %s: %w", movie.GetTitle(), err)
		}
		defer resp.Body.Close() //nolint: errcheck
		log.Infof("Removed expired keep tags from Radarr movie %s", movie.GetTitle())
	}

	return nil
}

// getRadarrMediaItemsMarkedForDeletion returns all Radarr movies that are marked for deletion.
func (e *Engine) getRadarrMediaItemsMarkedForDeletion(ctx context.Context) ([]models.MediaItem, error) {
	result := make([]models.MediaItem, 0)

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

				// Check if movie has keep request, keep tags, or delete-for-sure tags
				canRequest := true
				hasRequested := false
				mustDelete := false
				for _, tagID := range movie.GetTags() {
					tagName := radarrTags[tagID]
					if strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) {
						hasRequested = true
						canRequest = false
					} else if strings.HasPrefix(tagName, JellysweepKeepPrefix) {
						// If it has an active keep tag, it shouldn't be requestable
						keepDate, _, err := e.parseKeepTagWithRequester(tagName)
						if err == nil && time.Now().Before(keepDate) {
							canRequest = false // Don't allow requests for items with active keep tags
						}
					} else if tagName == JellysweepDeleteForSureTag {
						canRequest = false // Don't allow requests but still show the media
						mustDelete = true  // This movie is marked for deletion for sure
					}
				}

				mediaItem := models.MediaItem{
					ID:           fmt.Sprintf("radarr-%d", movie.GetId()),
					Title:        movie.GetTitle(),
					Type:         "movie",
					Year:         movie.GetYear(),
					Library:      "Movies",
					DeletionDate: deletionDate,
					PosterURL:    getCachedImageURL(imageURL),
					CanRequest:   canRequest,
					HasRequested: hasRequested,
					MustDelete:   mustDelete,
					FileSize:     movie.GetSizeOnDisk(),
					CleanupMode:  CleanupModeAll, // radarr doesn't have cleanup modes like Sonarr
					KeepCount:    1,
				}

				result = append(result, mediaItem)
				break // Only add once per movie, even if multiple deletion tags
			}
		}
	}

	return result, nil
}

// addRadarrKeepRequestTag adds a keep request tag to a Radarr movie.
func (e *Engine) addRadarrKeepRequestTag(ctx context.Context, movieID int32, username string) error {
	if e.radarr == nil {
		return fmt.Errorf("radarr client not available")
	}

	// Get the movie
	movie, resp, err := e.radarr.MovieAPI.GetMovieById(radarrAuthCtx(ctx, e.cfg.Radarr), movieID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	// Get current tags
	radarrTags, err := e.getRadarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	// Check if movie already has a keep request or delete-for-sure tag
	for _, tagID := range movie.GetTags() {
		tagName := radarrTags[tagID]
		if strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) {
			return fmt.Errorf("keep request already exists for this movie")
		}
		if tagName == JellysweepDeleteForSureTag {
			return fmt.Errorf("keep requests are not allowed for this movie")
		}
	}

	// Create keep request tag with 90 days expiry and username
	expiryDate := time.Now().Add(90 * 24 * time.Hour)
	keepRequestTag := e.createKeepRequestTagWithRequester(expiryDate, username)

	// Ensure the tag exists
	if err := e.ensureRadarrTagExists(ctx, keepRequestTag); err != nil {
		return fmt.Errorf("failed to create keep request tag: %w", err)
	}

	// Get tag ID
	tagID, err := e.getRadarrTagIDByLabel(keepRequestTag)
	if err != nil {
		return fmt.Errorf("failed to get tag ID: %w", err)
	}
	// Add the keep request tag
	movie.Tags = append(movie.Tags, tagID)

	// Update the movie
	_, resp, err = e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movieID)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Added keep request tag %s to Radarr movie %s", keepRequestTag, movie.GetTitle())
	return nil
}

// getRadarrKeepRequests returns all Radarr movies that have keep request tags.
func (e *Engine) getRadarrKeepRequests(ctx context.Context) ([]models.KeepRequest, error) {
	result := make([]models.KeepRequest, 0)

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
			if strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) {
				// skip if the movie has a delete-for-sure tag
				forSureTag, err := e.getRadarrTagIDByLabel(JellysweepDeleteForSureTag)
				if err != nil {
					log.Warnf("failed to get delete-for-sure tag ID: %v", err)
				}
				if slices.Contains(movie.GetTags(), forSureTag) {
					log.Debugf("Skipping Radarr movie %s as it has a delete-for-sure tag", movie.GetTitle())
					continue
				}
				// Parse expiry date and requester from tag
				expiryDate, requester, err := e.parseKeepRequestTagWithRequester(tagName)
				if err != nil {
					log.Warnf("failed to parse keep request tag %s: %v", tagName, err)
					continue
				}

				// Get deletion date from delete tag (if exists)
				var deletionDate time.Time
				for _, deletionTagID := range movie.GetTags() {
					deletionTagName := radarrTags[deletionTagID]
					if strings.HasPrefix(deletionTagName, jellysweepTagPrefix) {
						if parsedDate, err := e.parseDeletionDateFromTag(deletionTagName); err == nil {
							deletionDate = parsedDate
							break
						}
					}
				}

				imageURL := ""
				for _, image := range movie.GetImages() {
					if image.GetCoverType() == radarr.MEDIACOVERTYPES_POSTER {
						imageURL = image.GetRemoteUrl()
						break
					}
				}

				keepRequest := models.KeepRequest{
					ID:           fmt.Sprintf("radarr-%d", movie.GetId()),
					MediaID:      fmt.Sprintf("radarr-%d", movie.GetId()),
					Title:        movie.GetTitle(),
					Type:         "movie",
					Year:         int(movie.GetYear()),
					Library:      "Movies",
					DeletionDate: deletionDate,
					PosterURL:    getCachedImageURL(imageURL),
					RequestedBy:  requester,
					RequestDate:  time.Now(), // Would need to store separately
					ExpiryDate:   expiryDate,
				}

				result = append(result, keepRequest)
				break // Only add once per movie
			}
		}
	}

	return result, nil
}

// Helper methods for accepting/declining Radarr keep requests.
func (e *Engine) acceptRadarrKeepRequest(ctx context.Context, movieID int32) error {
	// Get requester information before removing the tag
	requester, movieTitle, err := e.getRadarrKeepRequestInfo(ctx, movieID)
	if err != nil {
		log.Warnf("Failed to get keep request info for movie %d: %v", movieID, err)
	}

	if err := e.removeRadarrKeepRequestAndDeleteTags(ctx, movieID); err != nil {
		return err
	}

	// Add keep tag with requester information
	if err := e.addRadarrKeepTagWithRequester(ctx, movieID, requester); err != nil {
		return err
	}

	// Send push notification if enabled and we have requester info
	if e.webpush != nil && requester != "" {
		if pushErr := e.webpush.SendKeepRequestNotification(ctx, requester, movieTitle, "Movie", true); pushErr != nil {
			log.Errorf("Failed to send push notification for approved keep request: %v", pushErr)
		}
	}

	return nil
}

func (e *Engine) declineRadarrKeepRequest(ctx context.Context, movieID int32) error {
	// Get requester information before removing the tag
	requester, movieTitle, err := e.getRadarrKeepRequestInfo(ctx, movieID)
	if err != nil {
		log.Warnf("Failed to get keep request info for movie %d: %v", movieID, err)
	}

	if err := e.addRadarrDeleteForSureTag(ctx, movieID); err != nil {
		return err
	}

	// Send push notification if enabled and we have requester info
	if e.webpush != nil && requester != "" {
		if pushErr := e.webpush.SendKeepRequestNotification(ctx, requester, movieTitle, "Movie", false); pushErr != nil {
			log.Errorf("Failed to send push notification for declined keep request: %v", pushErr)
		}
	}

	return nil
}

func (e *Engine) removeRadarrKeepRequestAndDeleteTags(ctx context.Context, movieID int32) error {
	if e.radarr == nil {
		return fmt.Errorf("radarr client not available")
	}

	// Get the movie
	movie, resp, err := e.radarr.MovieAPI.GetMovieById(radarrAuthCtx(ctx, e.cfg.Radarr), movieID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	// Get current tags
	radarrTags, err := e.getRadarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	if e.radarrItemKeepRequestAlreadyProcessed(movie, radarrTags) {
		log.Warn("Radarr movie %s already has a must-keep or must-delete-for-sure tag", movie.GetTitle())
		return ErrRequestAlreadyProcessed
	}

	// Remove keep request and delete tags
	var newTags []int32
	for _, tagID := range movie.GetTags() {
		tagName := radarrTags[tagID]
		if !strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) &&
			!strings.HasPrefix(tagName, jellysweepTagPrefix) {
			newTags = append(newTags, tagID)
		}
	}

	movie.Tags = newTags

	// Update the movie
	_, resp, err = e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movieID)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Removed keep request and delete tags from Radarr movie %s", movie.GetTitle())
	return nil
}

func (e *Engine) radarrItemKeepRequestAlreadyProcessed(movie *radarr.MovieResource, tags map[int32]string) bool {
	if movie == nil {
		return false
	}

	// Check if the series has any keep request tags
	for _, tagID := range movie.GetTags() {
		tagName := tags[tagID]
		if strings.HasPrefix(tagName, JellysweepKeepPrefix) ||
			tagName == JellysweepDeleteForSureTag {
			return true
		}
	}

	return false
}

func (e *Engine) addRadarrDeleteForSureTag(ctx context.Context, movieID int32) error {
	if e.radarr == nil {
		return fmt.Errorf("radarr client not available")
	}

	// Get the movie
	movie, resp, err := e.radarr.MovieAPI.GetMovieById(radarrAuthCtx(ctx, e.cfg.Radarr), movieID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	// Get current tags
	radarrTags, err := e.getRadarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	if e.radarrItemKeepRequestAlreadyProcessed(movie, radarrTags) {
		log.Warn("Radarr movie %s already has a must-keep or must-delete-for-sure tag", movie.GetTitle())
		return ErrRequestAlreadyProcessed
	}

	// Remove keep request and delete tags
	var newTags []int32
	for _, tagID := range movie.GetTags() {
		tagName := radarrTags[tagID]
		if !strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) &&
			!strings.HasPrefix(tagName, jellysweepTagPrefix) {
			newTags = append(newTags, tagID)
		}
	}

	// Ensure the delete-for-sure tag exists
	if err := e.ensureRadarrTagExists(ctx, JellysweepDeleteForSureTag); err != nil {
		return fmt.Errorf("failed to create delete-for-sure tag: %w", err)
	}

	// Get tag ID
	tagID, err := e.getRadarrTagIDByLabel(JellysweepDeleteForSureTag)
	if err != nil {
		return fmt.Errorf("failed to get tag ID: %w", err)
	}

	// Add the tag to the movie
	newTags = append(newTags, tagID)
	movie.Tags = newTags

	// Update the movie
	_, resp, err = e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movieID)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Added delete-for-sure tag to Radarr movie %s", movie.GetTitle())
	return nil
}

func (e *Engine) addRadarrKeepTag(ctx context.Context, movieID int32) error {
	if e.radarr == nil {
		return fmt.Errorf("radarr client not available")
	}

	// Get the movie
	movie, resp, err := e.radarr.MovieAPI.GetMovieById(radarrAuthCtx(ctx, e.cfg.Radarr), movieID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	// Create keep tag with 90 days expiry
	expiryDate := time.Now().Add(90 * 24 * time.Hour)
	keepTag := fmt.Sprintf("%s%s", JellysweepKeepPrefix, expiryDate.Format("2006-01-02"))

	// Ensure the tag exists
	if err := e.ensureRadarrTagExists(ctx, keepTag); err != nil {
		return fmt.Errorf("failed to create keep tag: %w", err)
	}

	// Get tag ID
	tagID, err := e.getRadarrTagIDByLabel(keepTag)
	if err != nil {
		return fmt.Errorf("failed to get tag ID: %w", err)
	}

	// Remove any existing jellysweep-delete tags before adding keep request tag
	var newTags []int32
	for _, existingTagID := range movie.GetTags() {
		tagName := e.data.radarrTags[existingTagID]
		if !strings.HasPrefix(tagName, jellysweepTagPrefix) {
			newTags = append(newTags, existingTagID)
		}
	}

	// Add the keep request tag
	newTags = append(newTags, tagID)
	movie.Tags = newTags

	// Update the movie
	_, resp, err = e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movieID)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Added keep request tag %s to Radarr movie %s", keepTag, movie.GetTitle())
	return nil
}

// resetRadarrTags removes all jellysweep tags from all Radarr movies.
func (e *Engine) resetRadarrTags(ctx context.Context, additionalTags []string) error {
	if e.radarr == nil {
		return nil
	}

	// Get all movies
	movies, resp, err := e.radarr.MovieAPI.ListMovie(radarrAuthCtx(ctx, e.cfg.Radarr)).Execute()
	if err != nil {
		return fmt.Errorf("failed to list Radarr movies: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	// Get all tags to map tag IDs to names
	tags, err := e.getRadarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Radarr tags: %w", err)
	}

	moviesUpdated := 0
	for _, m := range movies {
		// Check if movie has any jellysweep tags
		var hasJellysweepTags bool
		var newTags []int32

		for _, tagID := range m.GetTags() {
			tagName := tags[tagID]
			isJellysweepTag := strings.HasPrefix(tagName, jellysweepTagPrefix) ||
				strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) ||
				strings.HasPrefix(tagName, JellysweepKeepPrefix) ||
				tagName == JellysweepDeleteForSureTag ||
				slices.Contains(additionalTags, tagName)

			if isJellysweepTag {
				hasJellysweepTags = true
				log.Debugf("Removing jellysweep tag '%s' from Radarr movie: %s", tagName, m.GetTitle())
			} else {
				newTags = append(newTags, tagID)
			}
		}

		// Update movie if it had jellysweep tags
		if hasJellysweepTags {
			if e.cfg.DryRun {
				log.Infof("Dry run: Would remove jellysweep tags from Radarr movie: %s", m.GetTitle())
				moviesUpdated++
				continue
			}

			m.Tags = newTags
			_, resp, err = e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", m.GetId())).
				MovieResource(m).
				Execute()
			if err != nil {
				log.Errorf("Failed to update Radarr movie %s: %v", m.GetTitle(), err)
				continue
			}
			defer resp.Body.Close() //nolint: errcheck
			log.Infof("Removed jellysweep tags from Radarr movie: %s", m.GetTitle())
			moviesUpdated++
		}
	}

	log.Infof("Updated %d Radarr movies", moviesUpdated)
	return nil
}

// cleanupAllRadarrTags removes all unused jellysweep tags from Radarr.
func (e *Engine) cleanupAllRadarrTags(ctx context.Context, additionalTags []string) error {
	if e.radarr == nil {
		return nil
	}

	tags, resp, err := e.radarr.TagDetailsAPI.ListTagDetail(radarrAuthCtx(ctx, e.cfg.Radarr)).Execute()
	if err != nil {
		return fmt.Errorf("failed to list Radarr tags: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	tagsDeleted := 0
	for _, tag := range tags {
		tagName := tag.GetLabel()
		isJellysweepTag := strings.HasPrefix(tagName, jellysweepTagPrefix) ||
			strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) ||
			strings.HasPrefix(tagName, JellysweepKeepPrefix) ||
			tagName == JellysweepDeleteForSureTag ||
			slices.Contains(additionalTags, tagName)

		if isJellysweepTag {
			if e.cfg.DryRun {
				log.Infof("Dry run: Would delete Radarr tag: %s", tagName)
				tagsDeleted++
				continue
			}

			resp, err := e.radarr.TagAPI.DeleteTag(radarrAuthCtx(ctx, e.cfg.Radarr), tag.GetId()).Execute()
			if err != nil {
				log.Errorf("Failed to delete Radarr tag %s: %v", tagName, err)
				continue
			}
			defer resp.Body.Close() //nolint: errcheck
			log.Infof("Deleted Radarr tag: %s", tagName)
			tagsDeleted++
		}
	}

	log.Infof("Deleted %d Radarr tags", tagsDeleted)
	return nil
}

// resetSingleRadarrTagsForKeep removes ALL tags (including jellysweep-delete) from a single Radarr movie.
func (e *Engine) resetSingleRadarrTagsForKeep(ctx context.Context, movieID int32) error {
	if e.radarr == nil {
		return fmt.Errorf("radarr client not available")
	}

	// Get the movie
	movie, resp, err := e.radarr.MovieAPI.GetMovieById(radarrAuthCtx(ctx, e.cfg.Radarr), movieID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	// Get all tags to map tag IDs to names
	tags, err := e.getRadarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	// Check if movie has any jellysweep tags and filter them all out (including delete tags)
	var hasJellysweepTags bool
	var newTags []int32

	for _, tagID := range movie.GetTags() {
		tagName := tags[tagID]
		isJellysweepTag := strings.HasPrefix(tagName, jellysweepTagPrefix) ||
			strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) ||
			strings.HasPrefix(tagName, JellysweepKeepPrefix) ||
			tagName == JellysweepDeleteForSureTag

		if isJellysweepTag {
			hasJellysweepTags = true
			log.Debugf("Removing jellysweep tag '%s' from Radarr movie: %s", tagName, movie.GetTitle())
		} else {
			newTags = append(newTags, tagID)
		}
	}

	// Update movie if it had jellysweep tags
	if hasJellysweepTags {
		movie.Tags = newTags
		_, resp, err = e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movieID)).
			MovieResource(*movie).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to update radarr movie: %w", err)
		}
		defer resp.Body.Close() //nolint: errcheck
		log.Infof("Removed all jellysweep tags from Radarr movie for keep action: %s", movie.GetTitle())
	}

	return nil
}

// resetSingleRadarrTagsForMustDelete removes all jellysweep tags EXCEPT jellysweep-delete from a single Radarr movie.
func (e *Engine) resetSingleRadarrTagsForMustDelete(ctx context.Context, movieID int32) error {
	if e.radarr == nil {
		return fmt.Errorf("radarr client not available")
	}

	// Get the movie
	movie, resp, err := e.radarr.MovieAPI.GetMovieById(radarrAuthCtx(ctx, e.cfg.Radarr), movieID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	// Get all tags to map tag IDs to names
	tags, err := e.getRadarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	// Check if movie has any jellysweep tags and filter them out (except jellysweep-delete tags)
	var hasJellysweepTags bool
	var newTags []int32

	for _, tagID := range movie.GetTags() {
		tagName := tags[tagID]
		isJellysweepDeleteTag := strings.HasPrefix(tagName, jellysweepTagPrefix) // jellysweep-delete-*
		isOtherJellysweepTag := strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) ||
			strings.HasPrefix(tagName, JellysweepKeepPrefix) ||
			tagName == JellysweepDeleteForSureTag

		if isOtherJellysweepTag {
			hasJellysweepTags = true
			log.Debugf("Removing jellysweep tag '%s' from Radarr movie: %s", tagName, movie.GetTitle())
		} else if isJellysweepDeleteTag {
			// Keep jellysweep-delete tags
			newTags = append(newTags, tagID)
		} else {
			// Keep non-jellysweep tags
			newTags = append(newTags, tagID)
		}
	}

	// Update movie if it had jellysweep tags
	if hasJellysweepTags {
		movie.Tags = newTags
		_, resp, err = e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movieID)).
			MovieResource(*movie).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to update radarr movie: %w", err)
		}
		defer resp.Body.Close() //nolint: errcheck
		log.Infof("Removed jellysweep tags (except delete tags) from Radarr movie for must-delete action: %s", movie.GetTitle())
	}

	return nil
}

// resetAllRadarrTagsAndAddIgnore removes ALL jellysweep tags from a single Radarr movie and adds the ignore tag.
func (e *Engine) resetAllRadarrTagsAndAddIgnore(ctx context.Context, movieID int32) error {
	if e.radarr == nil {
		return fmt.Errorf("radarr client not available")
	}

	// Get the movie
	movie, _, err := e.radarr.MovieAPI.GetMovieById(radarrAuthCtx(ctx, e.cfg.Radarr), movieID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}

	// Ensure the ignore tag exists
	if err := e.ensureRadarrTagExists(ctx, JellysweepIgnoreTag); err != nil {
		return fmt.Errorf("failed to create ignore tag: %w", err)
	}

	// Get ignore tag ID
	ignoreTagID, err := e.getRadarrTagIDByLabel(JellysweepIgnoreTag)
	if err != nil {
		return fmt.Errorf("failed to get ignore tag ID: %w", err)
	}

	// Get all tags to map tag IDs to names
	tags, err := e.getRadarrTags(ctx)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	// Remove all jellysweep tags and keep non-jellysweep tags
	var newTags []int32

	for _, tagID := range movie.GetTags() {
		tagName := tags[tagID]
		isJellysweepTag := strings.HasPrefix(tagName, jellysweepTagPrefix) ||
			strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) ||
			strings.HasPrefix(tagName, JellysweepKeepPrefix) ||
			tagName == JellysweepDeleteForSureTag ||
			tagName == JellysweepIgnoreTag

		if isJellysweepTag {
			log.Debugf("Removing jellysweep tag '%s' from Radarr movie: %s", tagName, movie.GetTitle())
		} else {
			newTags = append(newTags, tagID)
		}
	}

	// Add the ignore tag if it's not already there
	if !slices.Contains(newTags, ignoreTagID) {
		newTags = append(newTags, ignoreTagID)
	}

	// Update movie tags
	movie.Tags = newTags
	_, resp, err := e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movieID)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Removed all jellysweep tags and added ignore tag to Radarr movie: %s", movie.GetTitle())
	return nil
}

// getRadarrKeepRequestInfo extracts requester information from a Radarr movie's keep request tag.
func (e *Engine) getRadarrKeepRequestInfo(ctx context.Context, movieID int32) (requester, movieTitle string, err error) {
	if e.radarr == nil {
		return "", "", fmt.Errorf("radarr client not available")
	}

	// Get the movie
	movie, resp, err := e.radarr.MovieAPI.GetMovieById(radarrAuthCtx(ctx, e.cfg.Radarr), movieID).Execute()
	if err != nil {
		return "", "", fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	movieTitle = movie.GetTitle()

	// Get current tags
	radarrTags, err := e.getRadarrTags(ctx)
	if err != nil {
		return "", movieTitle, fmt.Errorf("failed to get radarr tags: %w", err)
	}

	// Look for keep request tag and extract requester
	for _, tagID := range movie.GetTags() {
		tagName := radarrTags[tagID]
		if strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) {
			_, requester, parseErr := e.parseKeepRequestTagWithRequester(tagName)
			if parseErr != nil {
				log.Warnf("Failed to parse keep request tag %s: %v", tagName, parseErr)
				continue
			}
			return requester, movieTitle, nil
		}
	}

	return "", movieTitle, fmt.Errorf("no keep request tag found")
}

// addRadarrKeepTagWithRequester adds a keep tag with requester information to a Radarr movie.
func (e *Engine) addRadarrKeepTagWithRequester(ctx context.Context, movieID int32, requester string) error {
	if e.radarr == nil {
		return fmt.Errorf("radarr client not available")
	}

	// Get the movie
	movie, resp, err := e.radarr.MovieAPI.GetMovieById(radarrAuthCtx(ctx, e.cfg.Radarr), movieID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Create keep tag with 90 days expiry and requester information
	expiryDate := time.Now().Add(90 * 24 * time.Hour)
	keepTag := e.createKeepTagWithRequester(expiryDate, requester)

	// Ensure the tag exists
	if err := e.ensureRadarrTagExists(ctx, keepTag); err != nil {
		return fmt.Errorf("failed to create keep tag: %w", err)
	}

	// Get tag ID
	tagID, err := e.getRadarrTagIDByLabel(keepTag)
	if err != nil {
		return fmt.Errorf("failed to get tag ID: %w", err)
	}

	// Remove any existing jellysweep-delete tags before adding keep tag
	var newTags []int32
	for _, existingTagID := range movie.GetTags() {
		tagName, exists := e.data.radarrTags[existingTagID]
		if !exists || !strings.HasPrefix(tagName, jellysweepTagPrefix) {
			newTags = append(newTags, existingTagID)
		}
	}

	// Add the keep tag
	newTags = append(newTags, tagID)
	movie.Tags = newTags

	// Update the movie
	_, resp, err = e.radarr.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, e.cfg.Radarr), fmt.Sprintf("%d", movieID)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	log.Infof("Added keep tag %s to Radarr movie %s", keepTag, movie.GetTitle())
	return nil
}
