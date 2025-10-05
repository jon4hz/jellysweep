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
	"github.com/jon4hz/jellysweep/cache"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine/arr"
	"github.com/jon4hz/jellysweep/engine/stats"
	"github.com/jon4hz/jellysweep/tags"
	"github.com/samber/lo"
	jellyfin "github.com/sj14/jellyfin-go/api"
)

var _ arr.Arrer = (*Sonarr)(nil)

type Sonarr struct {
	client          *sonarrAPI.APIClient
	cfg             *config.Config
	stats           stats.Statser
	itemsCache      *cache.PrefixedCache[[]sonarrAPI.SeriesResource]
	tagsCache       *cache.PrefixedCache[cache.TagMap]
	libraryResolver cache.LibraryResolver
}

func sonarrAuthCtx(ctx context.Context, cfg *config.SonarrConfig) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return ctx
	}
	return context.WithValue(
		ctx,
		sonarrAPI.ContextAPIKeys,
		map[string]sonarrAPI.APIKey{
			"X-Api-Key": {Key: cfg.APIKey},
		},
	)
}

func NewSonarr(client *sonarrAPI.APIClient, cfg *config.Config, stats stats.Statser, itemsCache *cache.PrefixedCache[[]sonarrAPI.SeriesResource], tagsCache *cache.PrefixedCache[cache.TagMap], libraryResolver cache.LibraryResolver) *Sonarr {
	return &Sonarr{
		client:          client,
		cfg:             cfg,
		stats:           stats,
		itemsCache:      itemsCache,
		tagsCache:       tagsCache,
		libraryResolver: libraryResolver,
	}
}

func (s *Sonarr) GetItems(ctx context.Context, jellyfinItems []arr.JellyfinItem, forceRefresh bool) (map[string][]arr.MediaItem, error) {
	tagMap, err := s.GetTags(ctx, forceRefresh)
	if err != nil {
		return nil, err
	}

	series, err := s.getItems(ctx, forceRefresh)
	if err != nil {
		return nil, err
	}

	// Index series by title+year for quick lookup
	byKey := make(map[string]sonarrAPI.SeriesResource, len(series))
	for _, s := range series {
		key := fmt.Sprintf("%s|%d", strings.ToLower(s.GetTitle()), s.GetYear())
		byKey[key] = s
	}

	mediaItems := make(map[string][]arr.MediaItem, 0)
	for _, jf := range jellyfinItems {
		libraryName := s.libraryResolver.GetLibraryNameByID(jf.ParentLibraryID)
		libraryName = strings.ToLower(libraryName)
		if libraryName == "" {
			log.Error("Library name is empty for Jellyfin item, skipping", "item_id", jf.GetId(), "item_name", jf.GetName())
			continue
		}
		if jf.GetType() != jellyfin.BASEITEMKIND_SERIES {
			continue
		}

		key := fmt.Sprintf("%s|%d", strings.ToLower(jf.GetName()), jf.GetProductionYear())
		sr, ok := byKey[key]
		if !ok {
			continue
		}

		mediaItems[libraryName] = append(mediaItems[libraryName], arr.MediaItem{
			JellyfinID:     jf.GetId(),
			LibraryName:    libraryName,
			SeriesResource: sr,
			Title:          sr.GetTitle(),
			TmdbId:         sr.GetTmdbId(),
			Year:           sr.GetYear(),
			Tags:           lo.Map(sr.GetTags(), func(tag int32, _ int) string { return tagMap[tag] }),
			MediaType:      models.MediaTypeTV,
		})
	}

	log.Info("Merged jellyfin items with sonarr series", "mediaCount", len(mediaItems), "jellyfinCount", len(jellyfinItems))
	return mediaItems, nil
}

func (s *Sonarr) getItems(ctx context.Context, forceRefresh bool) ([]sonarrAPI.SeriesResource, error) {
	if forceRefresh {
		if err := s.itemsCache.Clear(ctx); err != nil {
			log.Debug("Failed to clear sonarr items cache, fetching from API", "error", err)
		}
	}

	cachedItems, err := s.itemsCache.Get(ctx, "all")
	if err != nil {
		log.Debug("Failed to get Sonarr items from cache, fetching from API", "error", err)
	}
	if len(cachedItems) != 0 && !forceRefresh {
		return cachedItems, nil
	}

	series, resp, err := s.client.SeriesAPI.ListSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr)).IncludeSeasonImages(false).Execute()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint: errcheck

	if err := s.itemsCache.Set(ctx, "all", series); err != nil {
		log.Warnf("Failed to cache Sonarr items: %v", err)
	}

	return series, nil
}

func (s *Sonarr) GetTags(ctx context.Context, forceRefresh bool) (cache.TagMap, error) {
	if forceRefresh {
		if err := s.tagsCache.Clear(ctx); err != nil {
			log.Debug("Failed to clear Sonarr tags cache, fetching from API", "error", err)
		}
	}

	cachedTags, err := s.tagsCache.Get(ctx, "all")
	if err != nil {
		log.Debug("Failed to get Sonarr tags from cache, fetching from API", "error", err)
	}
	if len(cachedTags) != 0 && !forceRefresh {
		return cachedTags, nil
	}

	tagList, resp, err := s.client.TagAPI.ListTag(sonarrAuthCtx(ctx, s.cfg.Sonarr)).Execute()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint: errcheck

	tagMap := make(cache.TagMap)
	for _, tag := range tagList {
		tagMap[tag.GetId()] = tag.GetLabel()
	}
	if err := s.tagsCache.Set(ctx, "all", tagMap); err != nil {
		log.Warnf("Failed to cache Sonarr tags: %v", err)
	}

	return tagMap, nil
}

func (s *Sonarr) MarkItemForDeletion(ctx context.Context, mediaItems map[string][]arr.MediaItem, libraryFoldersMap map[string][]string) error {
	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr tags: %w", err)
	}

	clearCache := false
	for lib, items := range mediaItems {
		for _, item := range items {
			if item.MediaType != models.MediaTypeTV {
				continue // Only process TV series for Sonarr
			}

			// check if series has already a jellysweep delete tag, or keep tag
			for _, tagID := range item.SeriesResource.GetTags() {
				tagName := tagMap[tagID]
				if strings.HasPrefix(tagName, tags.JellysweepKeepPrefix) {
					log.Debugf("Sonarr series %s has expired keep tag %s", item.Title, tagName)
				}
			}

			// Generate deletion tags using the new abstracted function
			deletionTags, err := tags.GenerateDeletionTags(ctx, s.cfg, lib, libraryFoldersMap)
			if err != nil {
				log.Errorf("Failed to generate deletion tags for library %s: %v", lib, err)
				continue
			}

			if s.cfg.DryRun {
				log.Infof("Dry run: Would mark Sonarr series %s for deletion with tags %v", item.Title, deletionTags)
				continue
			}

			// Add all deletion tags to the series
			series := item.SeriesResource
			for _, deleteTagLabel := range deletionTags {
				if err := s.EnsureTagExists(ctx, deleteTagLabel); err != nil {
					log.Errorf("Failed to ensure Sonarr tag exists: %v", err)
					continue
				}

				tagID, err := s.GetTagIDByLabel(ctx, deleteTagLabel)
				if err != nil {
					log.Errorf("Failed to get Sonarr tag ID: %v", err)
					continue
				}

				// Check if tag is already present
				tagExists := slices.Contains(series.GetTags(), tagID)
				if !tagExists {
					series.Tags = append(series.Tags, tagID)
				}
			}
			// Update the series in Sonarr
			_, resp, err := s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", series.GetId())).
				SeriesResource(series).
				Execute()
			if err != nil {
				return fmt.Errorf("failed to update Sonarr series %s with tags %v: %w", item.Title, deletionTags, err)
			}
			defer resp.Body.Close() //nolint: errcheck
			log.Infof("Marked Sonarr series %s for deletion with tags %v", item.Title, deletionTags)
			clearCache = true
		}
	}
	if clearCache {
		if err := s.itemsCache.Clear(ctx); err != nil {
			log.Warnf("Failed to clear Sonarr items cache after marking for deletion: %v", err)
		} else {
			log.Debug("Cleared Sonarr items cache after marking for deletion")
		}
	}
	return nil
}

func (s *Sonarr) GetTagIDByLabel(ctx context.Context, label string) (int32, error) {
	tagsMap, err := s.GetTags(ctx, false)
	if err != nil {
		return 0, fmt.Errorf("failed to get Sonarr tags: %w", err)
	}

	for id, tag := range tagsMap {
		if tag == label {
			return id, nil
		}
	}

	return 0, fmt.Errorf("sonarr tag with label %s not found", label)
}

func (s *Sonarr) EnsureTagExists(ctx context.Context, deleteTagLabel string) error {
	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr tags: %w", err)
	}

	for _, tag := range tagMap {
		if tag == deleteTagLabel {
			return nil
		}
	}

	tag := sonarrAPI.TagResource{
		Label: *sonarrAPI.NewNullableString(&deleteTagLabel),
	}
	newTag, resp, err := s.client.TagAPI.CreateTag(sonarrAuthCtx(ctx, s.cfg.Sonarr)).TagResource(tag).Execute()
	if err != nil {
		return fmt.Errorf("failed to create Sonarr tag %s: %w", deleteTagLabel, err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Created Sonarr tag: %s", deleteTagLabel)

	tagMap[newTag.GetId()] = newTag.GetLabel()
	if err := s.tagsCache.Set(ctx, "all", tagMap); err != nil {
		log.Warnf("Failed to cache new Sonarr tag %s: %v", deleteTagLabel, err)
	}
	return nil
}

func (s *Sonarr) CleanupTags(ctx context.Context) error {
	tagsList, resp, err := s.client.TagDetailsAPI.ListTagDetail(sonarrAuthCtx(ctx, s.cfg.Sonarr)).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to list Sonarr tags: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	for _, tag := range tagsList {
		if len(tag.SeriesIds) == 0 && tags.IsJellysweepTag(tag.GetLabel()) {
			if s.cfg.DryRun {
				log.Infof("Dry run: Would delete Sonarr tag %s", tag.GetLabel())
				continue
			}
			resp, err := s.client.TagAPI.DeleteTag(sonarrAuthCtx(ctx, s.cfg.Sonarr), tag.GetId()).Execute()
			if err != nil {
				return fmt.Errorf("failed to delete Sonarr tag %s: %w", tag.GetLabel(), err)
			}
			defer resp.Body.Close() //nolint: errcheck
			log.Infof("Deleted Sonarr tag: %s", tag.GetLabel())
		}
	}
	return nil
}

func (s *Sonarr) RemoveExpiredKeepTags(ctx context.Context) error {
	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr tags: %w", err)
	}

	var expiredKeepTagIDs []int32
	for tagID, tagName := range tagMap {
		// Check for both jellysweep-keep-request- and jellysweep-keep- tags
		if strings.HasPrefix(tagName, tags.JellysweepKeepRequestPrefix) || strings.HasPrefix(tagName, tags.JellysweepKeepPrefix) {
			// Parse the date from the tag name using the appropriate parser
			var expirationDate time.Time
			var err error
			if strings.HasPrefix(tagName, tags.JellysweepKeepRequestPrefix) {
				expirationDate, _, err = tags.ParseKeepRequestTagWithRequester(tagName)
			} else {
				expirationDate, _, err = tags.ParseKeepTagWithRequester(tagName)
			}
			if err != nil {
				log.Warnf("Failed to parse date from Sonarr keep tag %s: %v", tagName, err)
				continue
			}
			if time.Now().After(expirationDate) {
				expiredKeepTagIDs = append(expiredKeepTagIDs, tagID)
			}
		}
	}

	sonarrItems, err := s.getItems(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr items: %w", err)
	}

	clearCache := false
	for _, series := range sonarrItems {
		select {
		case <-ctx.Done():
			log.Warn("Context cancelled, stopping removal of recently played Sonarr delete tags")
			return ctx.Err()
		default:
			// Continue processing if context is not cancelled
		}

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
		if s.cfg.DryRun {
			log.Infof("Dry run: Would remove expired keep tags from Sonarr series %s", series.GetTitle())
			continue
		}

		// Update the series with the new tags
		series.Tags = keepTagIDs
		_, resp, err := s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", series.GetId())).
			SeriesResource(series).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to update Sonarr series %s: %w", series.GetTitle(), err)
		}
		defer resp.Body.Close() //nolint: errcheck
		log.Infof("Removed expired keep tags from Sonarr series %s", series.GetTitle())
		clearCache = true
	}

	if clearCache {
		// Clear the Sonarr items cache to ensure we don't have stale data
		if err := s.itemsCache.Clear(ctx); err != nil {
			log.Warnf("Failed to clear Sonarr items cache after removing expired keep tags: %v", err)
		} else {
			log.Debug("Cleared Sonarr items cache after removing expired keep tags")
		}
	}
	return nil
}

func (s *Sonarr) RemoveRecentlyPlayedDeleteTags(ctx context.Context, jellyfinItems []arr.JellyfinItem) error {
	sonarrItems, err := s.getItems(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr items: %w", err)
	}

	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr tags: %w", err)
	}

	clearCache := false
	for _, series := range sonarrItems {
		// Check if series has any jellysweep-delete tags
		var deleteTagIDs []int32
		for _, tagID := range series.GetTags() {
			if tagName, exists := tagMap[tagID]; exists {
				if tags.IsJellysweepDeleteTag(tagName) ||
					strings.HasPrefix(tagName, tags.JellysweepKeepRequestPrefix) {
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

		// Search through all jellyfin items to find matching series
		for _, jellyfinItem := range jellyfinItems {
			if jellyfinItem.GetType() == jellyfin.BASEITEMKIND_SERIES &&
				jellyfinItem.GetName() == series.GetTitle() &&
				jellyfinItem.GetProductionYear() == series.GetYear() {
				matchingJellystatID = jellyfinItem.GetId()
				// Get library name from the library resolver
				if libName := s.libraryResolver.GetLibraryNameByID(jellyfinItem.ParentLibraryID); libName != "" {
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
		lastPlayed, err := s.stats.GetItemLastPlayed(ctx, matchingJellystatID)
		if err != nil {
			log.Warnf("Failed to get last played time for series %s: %v", series.GetTitle(), err)
			continue
		}

		// If the series has been played recently, remove the delete tags
		if !lastPlayed.IsZero() {
			// Get the library config to get the threshold
			libraryConfig := s.cfg.GetLibraryConfig(libraryName)
			if libraryConfig == nil {
				log.Warnf("Library config not found for library %s, skipping", libraryName)
				continue
			}

			timeSinceLastPlayed := time.Since(lastPlayed)
			thresholdDuration := time.Duration(libraryConfig.LastStreamThreshold) * 24 * time.Hour

			if timeSinceLastPlayed < thresholdDuration {
				// Remove delete tags
				updatedTags := make([]int32, 0)
				for _, tagID := range series.GetTags() {
					if !slices.Contains(deleteTagIDs, tagID) {
						updatedTags = append(updatedTags, tagID)
					}
				}

				if s.cfg.DryRun {
					log.Infof("Dry run: Would remove delete tags from recently played Sonarr series: %s (last played: %s)",
						series.GetTitle(), lastPlayed.Format(time.RFC3339))
					continue
				}

				// Update the series with new tags
				series.Tags = updatedTags
				_, _, err = s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", series.GetId())).
					SeriesResource(series).
					Execute()
				if err != nil {
					log.Errorf("Failed to update Sonarr series %s: %v", series.GetTitle(), err)
					continue
				}
				clearCache = true

				log.Infof("Removed delete tags from recently played Sonarr series: %s (last played: %s)",
					series.GetTitle(), lastPlayed.Format(time.RFC3339))
			}
		}
	}
	if clearCache {
		// Clear the Sonarr items cache to ensure we don't have stale data
		if err := s.itemsCache.Clear(ctx); err != nil {
			log.Warnf("Failed to clear Sonarr items cache after removing recently played delete tags:%v", err)
		} else {
			log.Debug("Cleared Sonarr items cache after removing recently played delete tags")
		}
	}
	return nil
}

// GetMediaItemsMarkedForDeletion returns Sonarr series that have jellysweep-delete tags.
func (s *Sonarr) GetMediaItemsMarkedForDeletion(ctx context.Context, forceRefresh bool) ([]models.MediaItem, error) {
	items, err := s.getItems(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get sonarr items: %w", err)
	}

	tagMap, err := s.GetTags(ctx, forceRefresh)
	if err != nil {
		return nil, fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	result := make([]models.MediaItem, 0)
	for _, series := range items {
		for _, tagID := range series.GetTags() {
			tagName := tagMap[tagID]
			if tags.IsJellysweepDeleteTag(tagName) {
				// Parse the tag to get deletion date
				tagInfo, err := tags.ParseJellysweepTag(tagName)
				if err != nil {
					log.Warnf("failed to parse jellysweep tag %s: %v", tagName, err)
					continue
				}

				imageURL := ""
				for _, image := range series.GetImages() {
					if image.GetCoverType() == sonarrAPI.MEDIACOVERTYPES_POSTER {
						imageURL = image.GetRemoteUrl()
						break // Use the first poster image found
					}
				}

				// Check if series has keep request, keep tags, or delete-for-sure tags
				canRequest := true
				hasRequested := false
				mustDelete := false
				for _, tagID := range series.GetTags() {
					tagName := tagMap[tagID]
					if strings.HasPrefix(tagName, tags.JellysweepKeepRequestPrefix) {
						hasRequested = true
						canRequest = false
					} else if strings.HasPrefix(tagName, tags.JellysweepKeepPrefix) {
						// If it has an active keep tag, it shouldn't be requestable
						keepDate, _, err := tags.ParseKeepTagWithRequester(tagName)
						if err == nil && time.Now().Before(keepDate) {
							canRequest = false // Don't allow requests for items with active keep tags
						}
					} else if tagName == tags.JellysweepDeleteForSureTag {
						canRequest = false // Don't allow requests but still show the media
						mustDelete = true  // This series is marked for deletion for sure
					}
				}

				// Get the global cleanup configuration
				cleanupMode := s.cfg.GetCleanupMode()
				keepCount := s.cfg.GetKeepCount()

				result = append(result, models.MediaItem{
					ID:           fmt.Sprintf("sonarr-%d", series.GetId()),
					Title:        series.GetTitle(),
					Type:         "tv",
					Year:         series.GetYear(),
					Library:      "TV Shows",
					DeletionDate: tagInfo.DeletionDate,
					PosterURL:    arr.GetCachedImageURL(imageURL),
					CanRequest:   canRequest,
					HasRequested: hasRequested,
					MustDelete:   mustDelete,
					FileSize:     GetSeriesFileSize(series),
					CleanupMode:  cleanupMode,
					KeepCount:    keepCount,
				})
				break // Only add once per series, even if multiple deletion tags
			}
		}
	}

	return result, nil
}

// AddKeepRequest adds a keep-request tag with 90 days expiry and requester.
func (s *Sonarr) AddKeepRequest(ctx context.Context, id int32, username string) (string, string, error) {
	// Get series
	series, _, err := s.client.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, s.cfg.Sonarr), id).Execute()
	if err != nil {
		return "", "", fmt.Errorf("failed to get sonarr series: %w", err)
	}

	// Ensure not already requested or must-delete-for-sure
	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return "", "", fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	for _, tagID := range series.GetTags() {
		name := tagMap[tagID]
		if strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) {
			return "", "", fmt.Errorf("keep request already exists for this series")
		}
		if name == tags.JellysweepDeleteForSureTag {
			return "", "", fmt.Errorf("keep requests are not allowed for this series")
		}
	}

	// Build tag and ensure it exists
	expiry := time.Now().Add(90 * 24 * time.Hour)
	label := tags.CreateKeepRequestTagWithRequester(expiry, username)
	if err := s.EnsureTagExists(ctx, label); err != nil {
		return "", "", fmt.Errorf("failed to create keep request tag: %w", err)
	}

	tagID, err := s.GetTagIDByLabel(ctx, label)
	if err != nil {
		return "", "", fmt.Errorf("failed to get tag id: %w", err)
	}

	series.Tags = append(series.Tags, tagID)
	_, _, err = s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", id)).
		SeriesResource(*series).
		Execute()
	if err != nil {
		return "", "", fmt.Errorf("failed to update sonarr series: %w", err)
	}

	// refresh the cache in background
	go func() {
		if err := s.itemsCache.Clear(context.Background()); err != nil {
			log.Warnf("Failed to clear sonarr items cache after adding keep request tag: %v", err)
		} else {
			log.Debug("Cleared sonarr items cache after adding keep request tag")
		}
	}()

	log.Infof("Added keep request tag %s to sonarr series %s", label, series.GetTitle())
	return series.GetTitle(), "TV Show", nil
}

// GetKeepRequests lists series that have keep-request tags.
func (s *Sonarr) GetKeepRequests(ctx context.Context, libraryFoldersMap map[string][]string, forceRefresh bool) ([]models.KeepRequest, error) {
	items, err := s.getItems(ctx, forceRefresh)
	if err != nil {
		return nil, fmt.Errorf("failed to get sonarr items: %w", err)
	}
	tagMap, err := s.GetTags(ctx, forceRefresh)
	if err != nil {
		return nil, fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	result := make([]models.KeepRequest, 0)
	// Process each series
	for _, series := range items {
		for _, tagID := range series.GetTags() {
			tagName := tagMap[tagID]
			if strings.HasPrefix(tagName, tags.JellysweepKeepRequestPrefix) {
				// skip if the movie has a delete-for-sure tag
				forSureTag, err := s.GetTagIDByLabel(ctx, tags.JellysweepDeleteForSureTag)
				if err != nil {
					log.Warnf("failed to get delete-for-sure tag ID: %v", err)
				}
				if slices.Contains(series.GetTags(), forSureTag) {
					log.Debugf("Skipping Sonarr series %s as it has a delete-for-sure tag", series.GetTitle())
					continue
				}
				// Parse expiry date and requester from tag
				expiryDate, _, err := tags.ParseKeepRequestTagWithRequester(tagName)
				if err != nil {
					log.Warnf("failed to parse keep request tag %s: %v", tagName, err)
					continue
				}

				// Get deletion date from all delete tags
				var allDeleteTags []string
				for _, deletionTagID := range series.GetTags() {
					deletionTagName := tagMap[deletionTagID]
					if tags.IsJellysweepDeleteTag(deletionTagName) {
						allDeleteTags = append(allDeleteTags, deletionTagName)
					}
				}

				var deletionDate time.Time
				if len(allDeleteTags) > 0 {
					if parsedDate, err := tags.ParseDeletionDateFromTag(ctx, s.cfg, allDeleteTags, "TV Shows", libraryFoldersMap); err == nil { // TODO: dont hardcode library name
						deletionDate = parsedDate
					}
				}

				imageURL := ""
				for _, image := range series.GetImages() {
					if image.GetCoverType() == sonarrAPI.MEDIACOVERTYPES_POSTER {
						imageURL = image.GetRemoteUrl()
						break
					}
				}

				result = append(result, models.KeepRequest{
					ID:           fmt.Sprintf("sonarr-%d", series.GetId()),
					MediaID:      fmt.Sprintf("sonarr-%d", series.GetId()),
					Title:        series.GetTitle(),
					Type:         "tv",
					Year:         int(series.GetYear()),
					Library:      "TV Shows",
					DeletionDate: deletionDate,
					PosterURL:    arr.GetCachedImageURL(imageURL),
					RequestDate:  time.Now(), // Would need to store separately
					ExpiryDate:   expiryDate,
				})
				break // Only add once per series
			}
		}
	}

	return result, nil
}

// AcceptKeepRequest approves a keep request: remove request+delete tags and add keep tag with requester.
func (s *Sonarr) AcceptKeepRequest(ctx context.Context, id int32) (*arr.KeepRequestResponse, error) {
	requester, seriesTitle, err := s.getKeepRequestInfo(ctx, id)
	if err != nil {
		log.Warn("failed to get keep request info", "id", id, "error", err)
	}

	if err := s.removeKeepRequestAndDeleteTags(ctx, id); err != nil {
		return nil, err
	}

	if err := s.addKeepTagWithRequester(ctx, id, requester); err != nil {
		return nil, err
	}

	go func() {
		if err := s.itemsCache.Clear(context.Background()); err != nil {
			log.Warnf("Failed to clear sonarr items cache after accepting keep request: %v", err)
		} else {
			log.Debug("Cleared sonarr items cache after accepting keep request")
		}
	}()

	return &arr.KeepRequestResponse{
		Requester: requester,
		Title:     seriesTitle,
		MediaType: "TV Show",
		Approved:  true,
	}, nil
}

// DeclineKeepRequest rejects a keep request: add must-delete-for-sure tag.
func (s *Sonarr) DeclineKeepRequest(ctx context.Context, id int32) (*arr.KeepRequestResponse, error) {
	requester, seriesTitle, err := s.getKeepRequestInfo(ctx, id)
	if err != nil {
		log.Warn("failed to get keep request info", "id", id, "error", err)
	}

	if err := s.AddDeleteForSureTag(ctx, id); err != nil {
		return nil, err
	}

	go func() {
		if err := s.itemsCache.Clear(context.Background()); err != nil {
			log.Warnf("Failed to clear sonarr items cache after declining keep request: %v", err)
		} else {
			log.Debug("Cleared sonarr items cache after declining keep request")
		}
	}()

	return &arr.KeepRequestResponse{
		Requester: requester,
		Title:     seriesTitle,
		MediaType: "TV Show",
		Approved:  false,
	}, nil
}

func (s *Sonarr) AddKeepTag(ctx context.Context, seriesID int32) error {
	// Get the series
	series, _, err := s.client.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, s.cfg.Sonarr), seriesID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}

	// Create keep tag with 90 days expiry
	expiryDate := time.Now().Add(90 * 24 * time.Hour)
	keepTag := fmt.Sprintf("%s%s", tags.JellysweepKeepPrefix, expiryDate.Format("2006-01-02"))

	// Ensure the tag exists
	if err := s.EnsureTagExists(ctx, keepTag); err != nil {
		return fmt.Errorf("failed to create keep tag: %w", err)
	}

	// Get tag ID
	tagID, err := s.GetTagIDByLabel(ctx, keepTag)
	if err != nil {
		return fmt.Errorf("failed to get tag ID: %w", err)
	}

	// Add the tag to the series
	series.Tags = append(series.Tags, tagID)

	// Update the series
	_, _, err = s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", seriesID)).
		SeriesResource(*series).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update sonarr series: %w", err)
	}

	log.Infof("Added keep tag %s to Sonarr series %s", keepTag, series.GetTitle())
	return nil
}

// AddDeleteForSureTag adds jellysweep-must-delete-for-sure while preserving delete tags.
func (s *Sonarr) AddDeleteForSureTag(ctx context.Context, id int32) error {
	series, resp, err := s.client.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, s.cfg.Sonarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	if s.keepRequestAlreadyProcessed(series, tagMap) {
		log.Warn("Series already has a must-keep or must-delete-for-sure tag", "title", series.GetTitle())
		return arr.ErrRequestAlreadyProcessed
	}

	// Ensure tag exists
	if err := s.EnsureTagExists(ctx, tags.JellysweepDeleteForSureTag); err != nil {
		return fmt.Errorf("failed to ensure delete-for-sure tag: %w", err)
	}

	idTag, err := s.GetTagIDByLabel(ctx, tags.JellysweepDeleteForSureTag)
	if err != nil {
		return fmt.Errorf("failed to get delete-for-sure tag id: %w", err)
	}

	newTags := tags.FilterTagsForMustDelete(series.GetTags(), tagMap)
	if !slices.Contains(newTags, idTag) {
		newTags = append(newTags, idTag)
	}

	series.Tags = newTags
	_, resp, err = s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", id)).
		SeriesResource(*series).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update sonarr series: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Info("Added delete-for-sure tag to sonarr series", "title", series.GetTitle())
	return nil
}

// ResetTags removes jellysweep-related tags (and additionalTags) from all series.
func (s *Sonarr) ResetTags(ctx context.Context, additionalTags []string) error {
	series, err := s.getItems(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to list sonarr series: %w", err)
	}

	tagMap, err := s.GetTags(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	seriesUpdated := 0
	for _, serie := range series {
		// Check if series has any jellysweep tags
		var hasJellysweepTags bool
		var newTags []int32

		for _, tagID := range serie.GetTags() {
			tagName := tagMap[tagID]
			isJellysweepTag := strings.HasPrefix(tagName, tags.JellysweepTagPrefix) ||
				strings.HasPrefix(tagName, tags.JellysweepKeepRequestPrefix) ||
				strings.HasPrefix(tagName, tags.JellysweepKeepPrefix) ||
				tagName == tags.JellysweepDeleteForSureTag ||
				slices.Contains(additionalTags, tagName)

			if isJellysweepTag {
				hasJellysweepTags = true
				log.Debugf("Removing jellysweep tag '%s' from Sonarr series: %s", tagName, serie.GetTitle())
			} else {
				newTags = append(newTags, tagID)
			}
		}

		// Update series if it had jellysweep tags
		if hasJellysweepTags {
			if s.cfg.DryRun {
				log.Infof("Dry run: Would remove jellysweep tags from Sonarr series: %s", serie.GetTitle())
				seriesUpdated++
				continue
			}

			serie.Tags = newTags
			_, _, err = s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", serie.GetId())).
				SeriesResource(serie).
				Execute()
			if err != nil {
				log.Errorf("Failed to update Sonarr series %s: %v", serie.GetTitle(), err)
				continue
			}
			log.Infof("Removed jellysweep tags from Sonarr series: %s", serie.GetTitle())
			seriesUpdated++
		}
	}

	log.Infof("Updated %d Sonarr series", seriesUpdated)
	return nil
}

// CleanupAllTags deletes all unused jellysweep tags from Sonarr.
func (s *Sonarr) CleanupAllTags(ctx context.Context, additionalTags []string) error {
	tagsList, resp, err := s.client.TagDetailsAPI.ListTagDetail(sonarrAuthCtx(ctx, s.cfg.Sonarr)).Execute()
	if err != nil {
		return fmt.Errorf("failed to list sonarr tags: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	deleted := 0
	for _, td := range tagsList {
		name := td.GetLabel()
		isJellysweepTag := strings.HasPrefix(name, tags.JellysweepTagPrefix) ||
			strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) ||
			strings.HasPrefix(name, tags.JellysweepKeepPrefix) ||
			name == tags.JellysweepDeleteForSureTag ||
			slices.Contains(additionalTags, name)

		if isJellysweepTag {
			if s.cfg.DryRun {
				log.Infof("Dry run: Would delete sonarr tag: %s", name)
				deleted++
				continue
			}
			resp, err := s.client.TagAPI.DeleteTag(sonarrAuthCtx(ctx, s.cfg.Sonarr), td.GetId()).Execute()
			if err != nil {
				log.Errorf("Failed to delete sonarr tag %s: %v", name, err)
				continue
			}
			defer resp.Body.Close() //nolint: errcheck
			log.Info("Deleted sonarr tag", "name", name)
			deleted++
		}
	}

	if deleted > 0 {
		if err := s.tagsCache.Clear(ctx); err != nil {
			log.Warn("Failed to clear sonarr tags cache: %v", err)
		}
	}

	log.Infof("Deleted %d sonarr tags", deleted)
	return nil
}

// ResetSingleTagsForKeep removes all jellysweep tags (including delete) from a single series.
func (s *Sonarr) ResetSingleTagsForKeep(ctx context.Context, id int32) error {
	series, resp, err := s.client.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, s.cfg.Sonarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	var hasJellysweepTags bool
	newTags := make([]int32, 0)
	for _, tid := range series.GetTags() {
		name := tagMap[tid]
		if tags.IsJellysweepTag(name) {
			hasJellysweepTags = true
			log.Debug("Removing jellysweep tag from sonarr series", "tag", name, "series", series.GetTitle())
			continue
		}
		newTags = append(newTags, tid)
	}

	if hasJellysweepTags {
		series.Tags = newTags
		_, resp, err = s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", id)).
			SeriesResource(*series).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to update sonarr series: %w", err)
		}
		defer resp.Body.Close() //nolint: errcheck
		log.Info("Removed all jellysweep tags from series for keep action", "series", series.GetTitle())
	}

	return nil
}

// ResetSingleTagsForMustDelete removes all jellysweep tags except delete tags from a single series.
func (s *Sonarr) ResetSingleTagsForMustDelete(ctx context.Context, id int32) error {
	series, _, err := s.client.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, s.cfg.Sonarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}

	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	var hasJellysweepTags bool
	newTags := make([]int32, 0)
	for _, tid := range series.GetTags() {
		name := tagMap[tid]
		isJellysweepDeleteTag := tags.IsJellysweepDeleteTag(name)
		isOtherJellysweepTag := tags.IsJellysweepTag(name) && !isJellysweepDeleteTag

		if isOtherJellysweepTag {
			hasJellysweepTags = true
			log.Debugf("Removing jellysweep tag '%s' from Sonarr series: %s", name, series.GetTitle())
		} else if isJellysweepDeleteTag {
			// Keep jellysweep-delete tags
			newTags = append(newTags, tid)
		} else {
			// Keep non-jellysweep tags
			newTags = append(newTags, tid)
		}
	}
	if hasJellysweepTags {
		series.Tags = newTags
		_, _, err = s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", id)).
			SeriesResource(*series).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to update sonarr series: %w", err)
		}
		log.Info("Removed jellysweep tags (except delete tags) from Sonarr series for must-delete action", "series", series.GetTitle())
	}

	return nil
}

// ResetAllTagsAndAddIgnore removes all jellysweep tags and adds ignore tag to a single series.
func (s *Sonarr) ResetAllTagsAndAddIgnore(ctx context.Context, id int32) error {
	series, _, err := s.client.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, s.cfg.Sonarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}

	if err := s.EnsureTagExists(ctx, tags.JellysweepIgnoreTag); err != nil {
		return fmt.Errorf("failed to ensure ignore tag: %w", err)
	}

	ignoreID, err := s.GetTagIDByLabel(ctx, tags.JellysweepIgnoreTag)
	if err != nil {
		return fmt.Errorf("failed to get ignore tag id: %w", err)
	}

	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	newTags := make([]int32, 0)
	for _, tid := range series.GetTags() {
		name := tagMap[tid]
		if tags.IsJellysweepTag(name) {
			log.Debug("Removing jellysweep tag from series: %s", "tag", name, "series", series.GetTitle())
			continue
		}
		newTags = append(newTags, tid)
	}

	if !slices.Contains(newTags, ignoreID) {
		newTags = append(newTags, ignoreID)
	}

	series.Tags = newTags
	_, resp, err := s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", id)).
		SeriesResource(*series).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update sonarr series: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Info("Removed all jellysweep tags and added ignore tag to series", "series", series.GetTitle())
	return nil
}

func (s *Sonarr) getKeepRequestInfo(ctx context.Context, id int32) (requester, seriesTitle string, err error) {
	series, resp, err := s.client.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, s.cfg.Sonarr), id).Execute()
	if err != nil {
		return "", "", fmt.Errorf("failed to get sonarr series: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	seriesTitle = series.GetTitle()
	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return "", seriesTitle, fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	for _, tid := range series.GetTags() {
		name := tagMap[tid]
		if strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) {
			_, requester, perr := tags.ParseKeepRequestTagWithRequester(name)
			if perr != nil {
				log.Warnf("Failed to parse keep request tag %s: %v", name, perr)
				continue
			}
			return requester, seriesTitle, nil
		}
	}

	return "", seriesTitle, fmt.Errorf("no keep request tag found")
}

func (s *Sonarr) addKeepTagWithRequester(ctx context.Context, id int32, requester string) error {
	series, resp, err := s.client.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, s.cfg.Sonarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	expiry := time.Now().Add(90 * 24 * time.Hour)
	label := tags.CreateKeepTagWithRequester(expiry, requester)
	if err := s.EnsureTagExists(ctx, label); err != nil {
		return fmt.Errorf("failed to create keep tag: %w", err)
	}

	tagID, err := s.GetTagIDByLabel(ctx, label)
	if err != nil {
		return fmt.Errorf("failed to get tag id: %w", err)
	}

	// remove any existing jellysweep-delete tags before adding keep tag
	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	newTags := make([]int32, 0)
	for _, existing := range series.GetTags() {
		name := tagMap[existing]
		if !strings.HasPrefix(name, tags.JellysweepTagPrefix) {
			newTags = append(newTags, existing)
		}
	}
	newTags = append(newTags, tagID)

	series.Tags = newTags
	series, resp, err = s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", id)).
		SeriesResource(*series).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update sonarr series: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Added keep tag %s to sonarr series %s", label, series.GetTitle())
	return nil
}

func (s *Sonarr) removeKeepRequestAndDeleteTags(ctx context.Context, id int32) error {
	series, resp, err := s.client.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, s.cfg.Sonarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get sonarr series: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	tagMap, err := s.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	if s.keepRequestAlreadyProcessed(series, tagMap) {
		log.Warnf("Sonarr series %s already has a must-keep or must-delete-for-sure tag", series.GetTitle())
		return arr.ErrRequestAlreadyProcessed
	}

	newTags := make([]int32, 0)
	for _, tid := range series.GetTags() {
		name := tagMap[tid]
		if !strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) &&
			!strings.HasPrefix(name, tags.JellysweepTagPrefix) {
			newTags = append(newTags, tid)
		}
	}

	series.Tags = newTags
	_, resp, err = s.client.SeriesAPI.UpdateSeries(sonarrAuthCtx(ctx, s.cfg.Sonarr), fmt.Sprintf("%d", id)).
		SeriesResource(*series).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update sonarr series: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	log.Infof("Removed keep request and delete tags from sonarr series %s", series.GetTitle())
	return nil
}

func (s *Sonarr) keepRequestAlreadyProcessed(series *sonarrAPI.SeriesResource, tagMap cache.TagMap) bool {
	if series == nil {
		return false
	}
	for _, tid := range series.GetTags() {
		name := tagMap[tid]
		if strings.HasPrefix(name, tags.JellysweepKeepPrefix) ||
			name == tags.JellysweepDeleteForSureTag {
			return true
		}
	}
	return false
}

func GetSeriesFileSize(series sonarrAPI.SeriesResource) int64 {
	if series.HasStatistics() {
		stats := series.GetStatistics()
		if stats.HasSizeOnDisk() {
			return stats.GetSizeOnDisk()
		}
	}
	return 0
}

// GetItemAddedDate retrieves the first date when any episode of a series was imported.
func (s *Sonarr) GetItemAddedDate(ctx context.Context, seriesID int32) (*time.Time, error) {
	var allHistory []sonarrAPI.HistoryResource
	page := int32(1)
	pageSize := int32(250)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		historyResp, resp, err := s.client.HistoryAPI.GetHistory(sonarrAuthCtx(ctx, s.cfg.Sonarr)).
			Page(page).
			PageSize(pageSize).
			SeriesIds([]int32{seriesID}).
			Execute()
		if err != nil {
			log.Warnf("Failed to get Sonarr history for series %d: %v", seriesID, err)
			return nil, err
		}
		defer resp.Body.Close() //nolint: errcheck

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

	// Find the earliest "downloaded" or "importedepisodefile" event
	var earliestTime *time.Time
	for _, record := range allHistory {
		eventType := record.GetEventType()
		if eventType == sonarrAPI.EPISODEHISTORYEVENTTYPE_DOWNLOAD_FOLDER_IMPORTED ||
			eventType == sonarrAPI.EPISODEHISTORYEVENTTYPE_SERIES_FOLDER_IMPORTED {
			recordTime := record.GetDate()
			if earliestTime == nil || recordTime.Before(*earliestTime) {
				earliestTime = &recordTime
			}
		}
	}

	if earliestTime != nil {
		log.Debugf("Sonarr series %d first imported on: %s", seriesID, earliestTime.Format(time.RFC3339))
	}

	return earliestTime, nil
}
