package radarr

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	radarrAPI "github.com/devopsarr/radarr-go/radarr"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/cache"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine/arr"
	"github.com/jon4hz/jellysweep/engine/stats"
	"github.com/jon4hz/jellysweep/tags"
	"github.com/samber/lo"
	jellyfin "github.com/sj14/jellyfin-go/api"
)

var _ arr.Arrer = (*Radarr)(nil)

type Radarr struct {
	client *radarrAPI.APIClient

	cfg             *config.Config
	stats           stats.Statser
	itemsCache      *cache.PrefixedCache[[]radarrAPI.MovieResource]
	tagsCache       *cache.PrefixedCache[cache.TagMap]
	libraryResolver cache.LibraryResolver
}

func radarrAuthCtx(ctx context.Context, cfg *config.RadarrConfig) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return ctx
	}
	return context.WithValue(
		ctx,
		radarrAPI.ContextAPIKeys,
		map[string]radarrAPI.APIKey{
			"X-Api-Key": {Key: cfg.APIKey},
		},
	)
}

func NewRadarr(client *radarrAPI.APIClient, cfg *config.Config, stats stats.Statser, itemsCache *cache.PrefixedCache[[]radarrAPI.MovieResource], tagsCache *cache.PrefixedCache[cache.TagMap], libraryResolver cache.LibraryResolver) *Radarr {
	return &Radarr{
		client:          client,
		cfg:             cfg,
		stats:           stats,
		itemsCache:      itemsCache,
		tagsCache:       tagsCache,
		libraryResolver: libraryResolver,
	}
}

// GetItems merges Jellyfin items with Radarr movies into library-grouped MediaItems.
func (r *Radarr) GetItems(ctx context.Context, jellyfinItems []arr.JellyfinItem, forceRefresh bool) (map[string][]arr.MediaItem, error) {
	tagMap, err := r.GetTags(ctx, forceRefresh)
	if err != nil {
		return nil, fmt.Errorf("failed to get Radarr tags: %w", err)
	}

	movies, err := r.getItems(ctx, forceRefresh)
	if err != nil {
		return nil, fmt.Errorf("failed to get Radarr items: %w", err)
	}

	// Index movies by title+year for quick lookup
	byKey := make(map[string]radarrAPI.MovieResource, len(movies))
	for _, m := range movies {
		key := fmt.Sprintf("%s|%d", strings.ToLower(m.GetTitle()), m.GetYear())
		byKey[key] = m
	}

	mediaItems := make(map[string][]arr.MediaItem)
	for _, jf := range jellyfinItems {
		libraryName := r.libraryResolver.GetLibraryNameByID(jf.ParentLibraryID)
		libraryName = strings.ToLower(libraryName)
		if libraryName == "" {
			log.Error("Library name is empty for Jellyfin item, skipping", "item_id", jf.GetId(), "item_name", jf.GetName())
			continue
		}

		if jf.GetType() != jellyfin.BASEITEMKIND_MOVIE {
			continue
		}
		key := fmt.Sprintf("%s|%d", strings.ToLower(jf.GetName()), jf.GetProductionYear())
		mr, ok := byKey[key]
		if !ok {
			continue
		}

		mediaItems[libraryName] = append(mediaItems[libraryName], arr.MediaItem{
			JellyfinID:    jf.GetId(),
			LibraryName:   libraryName,
			MovieResource: mr,
			Title:         mr.GetTitle(),
			TmdbId:        mr.GetTmdbId(),
			Year:          mr.GetYear(),
			Tags:          lo.Map(mr.GetTags(), func(tag int32, _ int) string { return tagMap[tag] }),
			MediaType:     models.MediaTypeMovie,
		})
	}

	log.Info("Merged jellyfin items with radarr movies", "mediaCount", len(mediaItems), "jellyfinCount", len(jellyfinItems))
	return mediaItems, nil
}

func (r *Radarr) getItems(ctx context.Context, forceRefresh bool) ([]radarrAPI.MovieResource, error) {
	if forceRefresh {
		if err := r.itemsCache.Clear(ctx); err != nil {
			log.Debug("Failed to clear radarr items cache, fetching from API", "error", err)
		}
	}

	cachedItems, err := r.itemsCache.Get(ctx, "all")
	if err != nil {
		log.Debug("Failed to get Radarr items from cache, fetching from API", "error", err)
	}
	if len(cachedItems) != 0 && !forceRefresh {
		return cachedItems, nil
	}

	movies, resp, err := r.client.MovieAPI.ListMovie(radarrAuthCtx(ctx, r.cfg.Radarr)).Execute()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint: errcheck

	if err := r.itemsCache.Set(ctx, "all", movies); err != nil {
		log.Warnf("Failed to cache Radarr items: %v", err)
	}
	return movies, nil
}

func (r *Radarr) GetTags(ctx context.Context, forceRefresh bool) (cache.TagMap, error) {
	if forceRefresh {
		if err := r.tagsCache.Clear(ctx); err != nil {
			log.Debug("Failed to clear Radarr tags cache, fetching from API", "error", err)
		}
	}

	cachedTags, err := r.tagsCache.Get(ctx, "all")
	if err != nil {
		log.Debug("Failed to get Radarr tags from cache, fetching from API", "error", err)
	}
	if len(cachedTags) != 0 && !forceRefresh {
		return cachedTags, nil
	}

	tagList, resp, err := r.client.TagAPI.ListTag(radarrAuthCtx(ctx, r.cfg.Radarr)).Execute()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint: errcheck

	tagMap := make(cache.TagMap)
	for _, t := range tagList {
		tagMap[t.GetId()] = t.GetLabel()
	}
	if err := r.tagsCache.Set(ctx, "all", tagMap); err != nil {
		log.Warnf("Failed to cache Radarr tags: %v", err)
	}

	return tagMap, nil
}

func (r *Radarr) MarkItemForDeletion(ctx context.Context, mediaItems map[string][]arr.MediaItem, libraryFoldersMap map[string][]string) error {
	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Radarr tags: %w", err)
	}

	clearCache := false
	for lib, items := range mediaItems {
	movieLoop:
		for _, item := range items {
			if item.MediaType != models.MediaTypeMovie {
				continue
			}

			// check if movie already has a jellysweep delete or keep tag
			for _, tagID := range item.MovieResource.GetTags() {
				tagName := tagMap[tagID]
				if strings.HasPrefix(tagName, tags.JellysweepTagPrefix) {
					log.Debugf("Radarr movie %s already marked for deletion with tag %s", item.Title, tagName)
					continue movieLoop
				}
				if strings.HasPrefix(tagName, tags.JellysweepKeepPrefix) {
					log.Debugf("Radarr movie %s has keep tag %s", item.Title, tagName)
				}
			}

			// Generate deletion tags based on library config and disk usage thresholds
			deletionTags, err := tags.GenerateDeletionTags(ctx, r.cfg, lib, libraryFoldersMap)
			if err != nil {
				log.Errorf("Failed to generate deletion tags for library %s: %v", lib, err)
				continue
			}

			if r.cfg.DryRun {
				log.Infof("Dry run: Would mark Radarr movie %s for deletion with tags %v", item.Title, deletionTags)
				continue
			}

			// Add all deletion tags to the movie
			movie := item.MovieResource
			for _, deleteTagLabel := range deletionTags {
				if err := r.EnsureTagExists(ctx, deleteTagLabel); err != nil {
					log.Errorf("Failed to ensure Radarr tag exists: %v", err)
					continue
				}
				tagID, err := r.GetTagIDByLabel(ctx, deleteTagLabel)
				if err != nil {
					log.Errorf("Failed to get Radarr tag ID: %v", err)
					continue
				}
				if !slices.Contains(movie.GetTags(), tagID) {
					movie.Tags = append(movie.Tags, tagID)
				}
			}

			// Update the movie in Radarr
			_, resp, err := r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", movie.GetId())).
				MovieResource(movie).
				Execute()
			if err != nil {
				return fmt.Errorf("failed to update Radarr movie %s with tags %v: %w", item.Title, deletionTags, err)
			}
			defer resp.Body.Close() //nolint: errcheck

			log.Infof("Marked Radarr movie %s for deletion with tags %v", item.Title, deletionTags)
			clearCache = true
		}
	}

	if clearCache {
		if err := r.itemsCache.Clear(ctx); err != nil {
			log.Warnf("Failed to clear Radarr items cache after marking for deletion: %v", err)
		} else {
			log.Debug("Cleared Radarr items cache after marking for deletion")
		}
	}
	return nil
}

func (r *Radarr) GetTagIDByLabel(ctx context.Context, label string) (int32, error) {
	tagsMap, err := r.GetTags(ctx, false)
	if err != nil {
		return 0, fmt.Errorf("failed to get radarr tags: %w", err)
	}

	for id, tag := range tagsMap {
		if tag == label {
			return id, nil
		}
	}

	return 0, fmt.Errorf("radarr tag with label %s not found", label)
}

func (r *Radarr) EnsureTagExists(ctx context.Context, label string) error {
	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	for _, tag := range tagMap {
		if tag == label {
			return nil
		}
	}

	tag := radarrAPI.TagResource{
		Label: *radarrAPI.NewNullableString(&label),
	}
	newTag, resp, err := r.client.TagAPI.CreateTag(radarrAuthCtx(ctx, r.cfg.Radarr)).TagResource(tag).Execute()
	if err != nil {
		return fmt.Errorf("failed to create Radarr tag %s: %w", label, err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Created Radarr tag: %s", label)

	tagMap[newTag.GetId()] = newTag.GetLabel()
	if err := r.tagsCache.Set(ctx, "all", tagMap); err != nil {
		log.Warnf("Failed to cache new Radarr tag %s: %v", label, err)
	}
	return nil
}

func (r *Radarr) CleanupTags(ctx context.Context) error {
	tagsList, resp, err := r.client.TagDetailsAPI.ListTagDetail(radarrAuthCtx(ctx, r.cfg.Radarr)).Execute()
	if err != nil {
		return fmt.Errorf("failed to list Radarr tags: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	for _, t := range tagsList {
		if len(t.MovieIds) == 0 && tags.IsJellysweepTag(t.GetLabel()) {
			if r.cfg.DryRun {
				log.Infof("Dry run: Would delete Radarr tag %s", t.GetLabel())
				continue
			}
			resp, err := r.client.TagAPI.DeleteTag(radarrAuthCtx(ctx, r.cfg.Radarr), t.GetId()).Execute()
			if err != nil {
				return fmt.Errorf("failed to delete Radarr tag %s: %w", t.GetLabel(), err)
			}
			defer resp.Body.Close() //nolint: errcheck
			log.Infof("Deleted Radarr tag: %s", t.GetLabel())
		}
	}
	return nil
}

func (r *Radarr) DeleteMedia(ctx context.Context, libraryFoldersMap map[string][]string) ([]arr.MediaItem, error) {
	deleted := make([]arr.MediaItem, 0)

	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return deleted, fmt.Errorf("failed to get radarr tags: %w", err)
	}

	movies, err := r.getItems(ctx, false)
	if err != nil {
		return deleted, fmt.Errorf("failed to get radarr items: %w", err)
	}

	for _, movie := range movies {
		libraryName := "Movies" // TODO: Implement library name retrieval

		var tagNames []string
		for _, tagID := range movie.GetTags() {
			if name, ok := tagMap[tagID]; ok {
				tagNames = append(tagNames, name)
			}
		}

		if !tags.ShouldTriggerDeletionBasedOnDiskUsage(ctx, r.cfg, libraryName, tagNames, libraryFoldersMap) {
			continue
		}

		if r.cfg.DryRun {
			log.Infof("Dry run: Would delete Radarr movie %s", movie.GetTitle())
			continue
		}

		resp, err := r.client.MovieAPI.DeleteMovie(radarrAuthCtx(ctx, r.cfg.Radarr), movie.GetId()).
			DeleteFiles(true).
			Execute()
		if err != nil {
			return deleted, fmt.Errorf("failed to delete Radarr movie %s: %w", movie.GetTitle(), err)
		}
		defer resp.Body.Close() //nolint: errcheck

		log.Infof("Deleted Radarr movie %s", movie.GetTitle())

		deleted = append(deleted, arr.MediaItem{
			Title:       movie.GetTitle(),
			LibraryName: libraryName,
			MediaType:   models.MediaTypeMovie,
			Year:        movie.GetYear(),
		})
	}

	if len(deleted) > 0 {
		if err := r.itemsCache.Clear(ctx); err != nil {
			log.Warnf("Failed to clear Radarr items cache after deletion: %v", err)
		} else {
			log.Debug("Cleared Radarr items cache after deletion")
		}
	}

	return deleted, nil
}

func (r *Radarr) RemoveExpiredKeepTags(ctx context.Context) error {
	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Radarr tags for removing expired keep tags: %w", err)
	}

	var expiredIDs []int32
	for id, name := range tagMap {
		if strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) || strings.HasPrefix(name, tags.JellysweepKeepPrefix) {
			var exp time.Time
			var parseErr error
			if strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) {
				exp, _, parseErr = tags.ParseKeepRequestTagWithRequester(name)
			} else {
				exp, _, parseErr = tags.ParseKeepTagWithRequester(name)
			}
			if parseErr != nil {
				log.Warnf("Failed to parse date from Radarr keep tag %s: %v", name, parseErr)
				continue
			}
			if time.Now().After(exp) {
				expiredIDs = append(expiredIDs, id)
			}
		}
	}

	movies, err := r.getItems(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Radarr items for removing expired keep tags: %w", err)
	}

	clearCache := false
	for _, m := range movies {
		select {
		case <-ctx.Done():
			log.Warn("Context cancelled, stopping removal of recently played Sonarr delete tags")
			return ctx.Err()
		default:
			// Continue processing if context is not cancelled
		}

		keep := make([]int32, 0)
		for _, id := range m.GetTags() {
			if !slices.Contains(expiredIDs, id) {
				keep = append(keep, id)
			}
		}
		if len(keep) == len(m.GetTags()) {
			continue
		}
		if r.cfg.DryRun {
			log.Infof("Dry run: Would remove expired keep tags from Radarr movie %s", m.GetTitle())
			continue
		}

		m.Tags = keep
		_, resp, err := r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", m.GetId())).
			MovieResource(m).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to update Radarr movie %s: %w", m.GetTitle(), err)
		}
		defer resp.Body.Close() //nolint: errcheck

		log.Infof("Removed expired keep tags from Radarr movie %s", m.GetTitle())
		clearCache = true
	}

	if clearCache {
		// Clear the Radarr items cache to ensure we don't have stale data
		if err := r.itemsCache.Clear(ctx); err != nil {
			log.Warnf("Failed to clear Radarr items cache after removing expired keep tags: %v", err)
		} else {
			log.Debug("Cleared Radarr items cache after removing expired keep tags")
		}
	}
	return nil
}

func (r *Radarr) RemoveRecentlyPlayedDeleteTags(ctx context.Context, jellyfinItems []arr.JellyfinItem) error {
	movies, err := r.getItems(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Radarr items: %w", err)
	}

	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Radarr tags: %w", err)
	}

	clearCache := false
	for _, movie := range movies {
		// Check if movie has any jellysweep-delete tags
		var deleteTagIDs []int32
		for _, tagID := range movie.GetTags() {
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
			if jellyfinItem.GetType() == jellyfin.BASEITEMKIND_MOVIE &&
				jellyfinItem.GetName() == movie.GetTitle() &&
				jellyfinItem.GetProductionYear() == movie.GetYear() {
				matchingJellystatID = jellyfinItem.GetId()
				// Get library name from the library resolver
				if libName := r.libraryResolver.GetLibraryNameByID(jellyfinItem.ParentLibraryID); libName != "" {
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
		lastPlayed, err := r.stats.GetItemLastPlayed(ctx, matchingJellystatID)
		if err != nil {
			log.Warnf("Failed to get last played time for movie %s: %v", movie.GetTitle(), err)
			continue
		}

		// If the series has been played recently, remove the delete tags
		if !lastPlayed.IsZero() {
			// Get the library config to get the threshold
			libraryConfig := r.cfg.GetLibraryConfig(libraryName)
			if libraryConfig == nil {
				log.Warnf("Library config not found for library %s, skipping", libraryName)
				continue
			}

			timeSinceLastPlayed := time.Since(lastPlayed)
			thresholdDuration := time.Duration(libraryConfig.LastStreamThreshold) * 24 * time.Hour

			if timeSinceLastPlayed < thresholdDuration {
				// Remove delete tags
				updatedTags := make([]int32, 0)
				for _, tagID := range movie.GetTags() {
					if !slices.Contains(deleteTagIDs, tagID) {
						updatedTags = append(updatedTags, tagID)
					}
				}

				if r.cfg.DryRun {
					log.Infof("Dry run: Would remove delete tags from recently played Radarr movie: %s (last played: %s)",
						movie.GetTitle(), lastPlayed.Format(time.RFC3339))
					continue
				}

				// Update the movie with new tags
				movie.Tags = updatedTags
				_, _, err = r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", movie.GetId())).
					MovieResource(movie).
					Execute()
				if err != nil {
					log.Errorf("Failed to update Radarr movie %s: %v", movie.GetTitle(), err)
					continue
				}
				clearCache = true

				log.Infof("Removed delete tags from recently played Radarr movie: %s (last played: %s)",
					movie.GetTitle(), lastPlayed.Format(time.RFC3339))
			}
		}
	}
	if clearCache {
		// Clear the Radarr items cache to ensure we don't have stale data
		if err := r.itemsCache.Clear(ctx); err != nil {
			log.Warnf("Failed to clear Radarr items cache after removing recently played delete tags:%v", err)
		} else {
			log.Debug("Cleared Radarr items cache after removing recently played delete tags")
		}
	}
	return nil
}

func (r *Radarr) GetMediaItemsMarkedForDeletion(ctx context.Context, forceRefresh bool) ([]models.MediaItem, error) {
	movies, err := r.getItems(ctx, forceRefresh)
	if err != nil {
		return nil, fmt.Errorf("failed to get radarr items: %w", err)
	}

	tagMap, err := r.GetTags(ctx, forceRefresh)
	if err != nil {
		return nil, fmt.Errorf("failed to get radarr tags: %w", err)
	}

	result := make([]models.MediaItem, 0)
	for _, m := range movies {
		for _, tagID := range m.GetTags() {
			name := tagMap[tagID]
			if tags.IsJellysweepDeleteTag(name) {
				info, err := tags.ParseJellysweepTag(name)
				if err != nil {
					log.Warnf("failed to parse jellysweep tag %s: %v", name, err)
					continue
				}

				imageURL := ""
				for _, img := range m.GetImages() {
					if img.GetCoverType() == radarrAPI.MEDIACOVERTYPES_POSTER {
						imageURL = img.GetRemoteUrl()
						break
					}
				}

				canRequest := true
				hasRequested := false
				mustDelete := false
				for _, tid := range m.GetTags() {
					tn := tagMap[tid]
					if strings.HasPrefix(tn, tags.JellysweepKeepRequestPrefix) {
						hasRequested = true
						canRequest = false
					} else if strings.HasPrefix(tn, tags.JellysweepKeepPrefix) {
						if keepDate, _, err := tags.ParseKeepTagWithRequester(tn); err == nil && time.Now().Before(keepDate) {
							canRequest = false
						}
					} else if tn == tags.JellysweepDeleteForSureTag {
						canRequest = false
						mustDelete = true
					}
				}

				result = append(result, models.MediaItem{
					ID:           fmt.Sprintf("radarr-%d", m.GetId()),
					Title:        m.GetTitle(),
					Type:         "movie",
					Year:         m.GetYear(),
					Library:      "Movies",
					DeletionDate: info.DeletionDate,
					PosterURL:    arr.GetCachedImageURL(imageURL),
					CanRequest:   canRequest,
					HasRequested: hasRequested,
					MustDelete:   mustDelete,
					FileSize:     m.GetSizeOnDisk(),
					CleanupMode:  config.CleanupModeAll,
					KeepCount:    1,
				})
				break
			}
		}
	}
	return result, nil
}

func (r *Radarr) AddKeepRequest(ctx context.Context, id int32, username string) (string, string, error) {
	movie, resp, err := r.client.MovieAPI.GetMovieById(radarrAuthCtx(ctx, r.cfg.Radarr), id).Execute()
	if err != nil {
		return "", "", fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return "", "", fmt.Errorf("failed to get radarr tags: %w", err)
	}

	for _, tagID := range movie.GetTags() {
		name := tagMap[tagID]
		if strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) {
			return "", "", fmt.Errorf("keep request already exists for this movie")
		}
		if name == tags.JellysweepDeleteForSureTag {
			return "", "", fmt.Errorf("keep requests are not allowed for this movie")
		}
	}

	expiry := time.Now().Add(90 * 24 * time.Hour)
	label := tags.CreateKeepRequestTagWithRequester(expiry, username)
	if err := r.EnsureTagExists(ctx, label); err != nil {
		return "", "", fmt.Errorf("failed to create keep request tag: %w", err)
	}

	tagID, err := r.GetTagIDByLabel(ctx, label)
	if err != nil {
		return "", "", fmt.Errorf("failed to get tag ID: %w", err)
	}

	movie.Tags = append(movie.Tags, tagID)
	_, resp, err = r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", id)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return "", "", fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	// refresh cache in background
	go func() {
		if err := r.itemsCache.Clear(context.Background()); err != nil {
			log.Warnf("Failed to clear Radarr items cache after adding keep request tag: %v", err)
		} else {
			log.Debug("Cleared Radarr items cache after adding keep request tag")
		}
	}()

	log.Infof("Added keep request tag %s to Radarr movie %s", label, movie.GetTitle())
	return movie.GetTitle(), "Movie", nil
}

func (r *Radarr) GetKeepRequests(ctx context.Context, libraryFoldersMap map[string][]string, forceRefresh bool) ([]models.KeepRequest, error) {
	movies, err := r.getItems(ctx, forceRefresh)
	if err != nil {
		return nil, fmt.Errorf("failed to get radarr items: %w", err)
	}

	tagMap, err := r.GetTags(ctx, forceRefresh)
	if err != nil {
		return nil, fmt.Errorf("failed to get radarr tags: %w", err)
	}

	result := make([]models.KeepRequest, 0)
	for _, m := range movies {
		for _, tagID := range m.GetTags() {
			name := tagMap[tagID]
			if strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) {
				// skip if movie has delete-for-sure
				forSureID, _ := r.GetTagIDByLabel(ctx, tags.JellysweepDeleteForSureTag)
				if forSureID != 0 && slices.Contains(m.GetTags(), forSureID) {
					log.Debugf("Skipping Radarr movie %s as it has a delete-for-sure tag", m.GetTitle())
					continue
				}
				expiry, _, err := tags.ParseKeepRequestTagWithRequester(name)
				if err != nil {
					log.Warnf("failed to parse keep request tag %s: %v", name, err)
					continue
				}

				// Get deletion date from all delete tags
				var allDeleteTags []string
				for _, deletionTagID := range m.GetTags() {
					deletionTagName := tagMap[deletionTagID]
					if tags.IsJellysweepDeleteTag(deletionTagName) {
						allDeleteTags = append(allDeleteTags, deletionTagName)
					}
				}

				var deletionDate time.Time
				if len(allDeleteTags) > 0 {
					if parsedDate, err := tags.ParseDeletionDateFromTag(ctx, r.cfg, allDeleteTags, "Movies", libraryFoldersMap); err == nil { // TODO: dont hardcode library name
						deletionDate = parsedDate
					}
				}

				imageURL := ""
				for _, image := range m.GetImages() {
					if image.GetCoverType() == radarrAPI.MEDIACOVERTYPES_POSTER {
						imageURL = image.GetRemoteUrl()
						break
					}
				}

				result = append(result, models.KeepRequest{
					ID:           fmt.Sprintf("radarr-%d", m.GetId()),
					MediaID:      fmt.Sprintf("radarr-%d", m.GetId()),
					Title:        m.GetTitle(),
					Type:         "movie",
					Year:         int(m.GetYear()),
					Library:      "Movies",
					DeletionDate: deletionDate,
					PosterURL:    arr.GetCachedImageURL(imageURL),
					RequestDate:  time.Now(),
					ExpiryDate:   expiry,
				})
				break
			}
		}
	}
	return result, nil
}

func (r *Radarr) AcceptKeepRequest(ctx context.Context, id int32) (*arr.KeepRequestResponse, error) {
	requester, title, err := r.getKeepRequestInfo(ctx, id)
	if err != nil {
		log.Warnf("Failed to get keep request info for movie %d: %v", id, err)
	}

	if err := r.removeKeepRequestAndDeleteTags(ctx, id); err != nil {
		return nil, err
	}

	if err := r.addKeepTagWithRequester(ctx, id, requester); err != nil {
		return nil, err
	}

	go func() {
		if err := r.itemsCache.Clear(context.Background()); err != nil {
			log.Warnf("Failed to clear Radarr items cache after accepting keep request: %v", err)
		} else {
			log.Debug("Cleared Radarr items cache after accepting keep request")
		}
	}()

	return &arr.KeepRequestResponse{
		Requester: requester,
		Title:     title,
		MediaType: "Movie",
		Approved:  true,
	}, nil
}

func (r *Radarr) DeclineKeepRequest(ctx context.Context, id int32) (*arr.KeepRequestResponse, error) {
	requester, title, err := r.getKeepRequestInfo(ctx, id)
	if err != nil {
		log.Warnf("Failed to get keep request info for movie %d: %v", id, err)
	}

	if err := r.AddDeleteForSureTag(ctx, id); err != nil {
		return nil, err
	}

	go func() {
		if err := r.itemsCache.Clear(context.Background()); err != nil {
			log.Warnf("Failed to clear Radarr items cache after declining keep request: %v", err)
		} else {
			log.Debug("Cleared Radarr items cache after declining keep request")
		}
	}()

	return &arr.KeepRequestResponse{
		Requester: requester,
		Title:     title,
		MediaType: "Movie",
		Approved:  false,
	}, nil
}

func (r *Radarr) AddKeepTag(ctx context.Context, movieID int32) error {
	// Get the movie
	movie, resp, err := r.client.MovieAPI.GetMovieById(radarrAuthCtx(ctx, r.cfg.Radarr), movieID).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	// Create keep tag with 90 days expiry
	expiryDate := time.Now().Add(90 * 24 * time.Hour)
	keepTag := fmt.Sprintf("%s%s", tags.JellysweepKeepPrefix, expiryDate.Format("2006-01-02"))

	// Ensure the tag exists
	if err := r.EnsureTagExists(ctx, keepTag); err != nil {
		return fmt.Errorf("failed to create keep tag: %w", err)
	}

	// Get tag ID
	tagID, err := r.GetTagIDByLabel(ctx, keepTag)
	if err != nil {
		return fmt.Errorf("failed to get tag ID: %w", err)
	}

	// Get current tags for filtering
	radarrTags, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	// Remove any existing jellysweep-delete tags before adding keep request tag
	var newTags []int32
	for _, existingTagID := range movie.GetTags() {
		tagName := radarrTags[existingTagID]
		if !strings.HasPrefix(tagName, tags.JellysweepTagPrefix) {
			newTags = append(newTags, existingTagID)
		}
	}

	// Add the keep request tag
	newTags = append(newTags, tagID)
	movie.Tags = newTags

	// Update the movie
	_, resp, err = r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", movieID)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Added keep request tag %s to Radarr movie %s", keepTag, movie.GetTitle())
	return nil
}

func (r *Radarr) AddDeleteForSureTag(ctx context.Context, id int32) error {
	movie, resp, err := r.client.MovieAPI.GetMovieById(radarrAuthCtx(ctx, r.cfg.Radarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	if r.keepRequestAlreadyProcessed(movie, tagMap) {
		log.Warnf("Radarr movie %s already has a must-keep or must-delete-for-sure tag", movie.GetTitle())
		return arr.ErrRequestAlreadyProcessed
	}

	if err := r.EnsureTagExists(ctx, tags.JellysweepDeleteForSureTag); err != nil {
		return fmt.Errorf("failed to create delete-for-sure tag: %w", err)
	}

	tagID, err := r.GetTagIDByLabel(ctx, tags.JellysweepDeleteForSureTag)
	if err != nil {
		return fmt.Errorf("failed to get tag ID: %w", err)
	}

	newTags := tags.FilterTagsForMustDelete(movie.GetTags(), tagMap)
	if !slices.Contains(newTags, tagID) {
		newTags = append(newTags, tagID)
	}

	movie.Tags = newTags
	_, resp, err = r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", id)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Added delete-for-sure tag to Radarr movie %s", movie.GetTitle())
	return nil
}

func (r *Radarr) ResetTags(ctx context.Context, additionalTags []string) error {
	movies, err := r.getItems(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to list radarr movies: %w", err)
	}

	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get Radarr tags: %w", err)
	}

	updated := 0
	for _, m := range movies {
		hasJellysweepTags := false
		newTags := make([]int32, 0)

		for _, id := range m.GetTags() {
			name := tagMap[id]
			isJellysweepTag := strings.HasPrefix(name, tags.JellysweepTagPrefix) ||
				strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) ||
				strings.HasPrefix(name, tags.JellysweepKeepPrefix) ||
				name == tags.JellysweepDeleteForSureTag ||
				slices.Contains(additionalTags, name)

			if isJellysweepTag {
				hasJellysweepTags = true
				log.Debugf("Removing jellysweep tag '%s' from Radarr movie: %s", name, m.GetTitle())
			} else {
				newTags = append(newTags, id)
			}
		}

		if hasJellysweepTags {
			if r.cfg.DryRun {
				log.Infof("Dry run: Would remove jellysweep tags from Radarr movie: %s", m.GetTitle())
				updated++
				continue
			}

			m.Tags = newTags
			_, resp, err := r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", m.GetId())).
				MovieResource(m).
				Execute()
			if err != nil {
				log.Errorf("Failed to update Radarr movie %s: %v", m.GetTitle(), err)
				continue
			}
			defer resp.Body.Close() //nolint: errcheck
			log.Infof("Removed jellysweep tags from Radarr movie: %s", m.GetTitle())
			updated++
		}
	}

	log.Infof("Updated %d Radarr movies", updated)
	return nil
}

func (r *Radarr) CleanupAllTags(ctx context.Context, additionalTags []string) error {
	tagsList, resp, err := r.client.TagDetailsAPI.ListTagDetail(radarrAuthCtx(ctx, r.cfg.Radarr)).Execute()
	if err != nil {
		return fmt.Errorf("failed to list Radarr tags: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	deleted := 0
	for _, t := range tagsList {
		name := t.GetLabel()
		isJellysweepTag := strings.HasPrefix(name, tags.JellysweepTagPrefix) ||
			strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) ||
			strings.HasPrefix(name, tags.JellysweepKeepPrefix) ||
			name == tags.JellysweepDeleteForSureTag ||
			slices.Contains(additionalTags, name)

		if isJellysweepTag {
			if r.cfg.DryRun {
				log.Infof("Dry run: Would delete Radarr tag: %s", name)
				deleted++
				continue
			}
			resp, err := r.client.TagAPI.DeleteTag(radarrAuthCtx(ctx, r.cfg.Radarr), t.GetId()).Execute()
			if err != nil {
				log.Errorf("Failed to delete Radarr tag %s: %v", name, err)
				continue
			}
			defer resp.Body.Close() //nolint: errcheck
			log.Infof("Deleted Radarr tag: %s", name)
			deleted++
		}
	}

	if deleted > 0 {
		if err := r.tagsCache.Clear(ctx); err != nil {
			log.Warn("Failed to clear radarr tags cache: %v", err)
		}
	}

	log.Infof("Deleted %d Radarr tags", deleted)
	return nil
}

func (r *Radarr) ResetSingleTagsForKeep(ctx context.Context, id int32) error {
	movie, resp, err := r.client.MovieAPI.GetMovieById(radarrAuthCtx(ctx, r.cfg.Radarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	hasJellysweepTags := false
	newTags := make([]int32, 0)
	for _, tid := range movie.GetTags() {
		name := tagMap[tid]
		if tags.IsJellysweepTag(name) {
			hasJellysweepTags = true
			log.Debugf("Removing jellysweep tag '%s' from Radarr movie: %s", name, movie.GetTitle())
			continue
		}
		newTags = append(newTags, tid)
	}

	if hasJellysweepTags {
		movie.Tags = newTags
		_, resp, err = r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", id)).
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

func (r *Radarr) ResetSingleTagsForMustDelete(ctx context.Context, id int32) error {
	movie, resp, err := r.client.MovieAPI.GetMovieById(radarrAuthCtx(ctx, r.cfg.Radarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	hasJellysweepTags := false
	newTags := make([]int32, 0)
	for _, tid := range movie.GetTags() {
		name := tagMap[tid]
		isJellysweepDeleteTag := tags.IsJellysweepDeleteTag(name)
		isOtherJellysweepTag := tags.IsJellysweepTag(name) && !isJellysweepDeleteTag

		if isOtherJellysweepTag {
			hasJellysweepTags = true
			log.Debugf("Removing jellysweep tag '%s' from Radarr movie: %s", name, movie.GetTitle())
		} else if isJellysweepDeleteTag {
			// Keep jellysweep-delete tags
			newTags = append(newTags, tid)
		} else {
			// Keep non-jellysweep tags
			newTags = append(newTags, tid)
		}
	}

	if hasJellysweepTags {
		movie.Tags = newTags
		_, resp, err = r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", id)).
			MovieResource(*movie).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to update radarr movie: %w", err)
		}
		defer resp.Body.Close() //nolint: errcheck
		log.Infof("Removed jellysweep tags (except delete) from Radarr movie for must-delete action: %s", movie.GetTitle())
	}

	return nil
}

func (r *Radarr) ResetAllTagsAndAddIgnore(ctx context.Context, id int32) error {
	movie, _, err := r.client.MovieAPI.GetMovieById(radarrAuthCtx(ctx, r.cfg.Radarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}

	if err := r.EnsureTagExists(ctx, tags.JellysweepIgnoreTag); err != nil {
		return fmt.Errorf("failed to create ignore tag: %w", err)
	}

	ignoreID, err := r.GetTagIDByLabel(ctx, tags.JellysweepIgnoreTag)
	if err != nil {
		return fmt.Errorf("failed to get ignore tag ID: %w", err)
	}

	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	newTags := make([]int32, 0)
	for _, tid := range movie.GetTags() {
		name := tagMap[tid]
		if tags.IsJellysweepTag(name) {
			log.Debugf("Removing jellysweep tag '%s' from Radarr movie: %s", name, movie.GetTitle())
		} else {
			newTags = append(newTags, tid)
		}
	}

	if !slices.Contains(newTags, ignoreID) {
		newTags = append(newTags, ignoreID)
	}

	movie.Tags = newTags
	_, resp, err := r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", id)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Removed all jellysweep tags and added ignore tag to Radarr movie: %s", movie.GetTitle())
	return nil
}

func (r *Radarr) getKeepRequestInfo(ctx context.Context, id int32) (requester, title string, err error) {
	movie, resp, err := r.client.MovieAPI.GetMovieById(radarrAuthCtx(ctx, r.cfg.Radarr), id).Execute()
	if err != nil {
		return "", "", fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	title = movie.GetTitle()
	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return "", title, fmt.Errorf("failed to get radarr tags: %w", err)
	}

	for _, tid := range movie.GetTags() {
		name := tagMap[tid]
		if strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) {
			_, requester, perr := tags.ParseKeepRequestTagWithRequester(name)
			if perr != nil {
				log.Warnf("Failed to parse keep request tag %s: %v", name, perr)
				continue
			}
			return requester, title, nil
		}
	}

	return "", title, fmt.Errorf("no keep request tag found")
}

func (r *Radarr) addKeepTagWithRequester(ctx context.Context, id int32, requester string) error {
	movie, resp, err := r.client.MovieAPI.GetMovieById(radarrAuthCtx(ctx, r.cfg.Radarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	expiry := time.Now().Add(90 * 24 * time.Hour)
	keepTag := tags.CreateKeepTagWithRequester(expiry, requester)
	if err := r.EnsureTagExists(ctx, keepTag); err != nil {
		return fmt.Errorf("failed to create keep tag: %w", err)
	}

	tagID, err := r.GetTagIDByLabel(ctx, keepTag)
	if err != nil {
		return fmt.Errorf("failed to get tag ID: %w", err)
	}

	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	newTags := make([]int32, 0)
	for _, existing := range movie.GetTags() {
		name := tagMap[existing]
		if !strings.HasPrefix(name, tags.JellysweepTagPrefix) {
			newTags = append(newTags, existing)
		}
	}
	newTags = append(newTags, tagID)

	movie.Tags = newTags
	_, resp, err = r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", id)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Added keep tag %s to Radarr movie %s", keepTag, movie.GetTitle())
	return nil
}

func (r *Radarr) removeKeepRequestAndDeleteTags(ctx context.Context, id int32) error {
	movie, resp, err := r.client.MovieAPI.GetMovieById(radarrAuthCtx(ctx, r.cfg.Radarr), id).Execute()
	if err != nil {
		return fmt.Errorf("failed to get radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	tagMap, err := r.GetTags(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	if r.keepRequestAlreadyProcessed(movie, tagMap) {
		log.Warnf("Radarr movie %s already has a must-keep or must-delete-for-sure tag", movie.GetTitle())
		return arr.ErrRequestAlreadyProcessed
	}

	newTags := make([]int32, 0)
	for _, tid := range movie.GetTags() {
		name := tagMap[tid]
		if !strings.HasPrefix(name, tags.JellysweepKeepRequestPrefix) &&
			!strings.HasPrefix(name, tags.JellysweepTagPrefix) {
			newTags = append(newTags, tid)
		}
	}

	movie.Tags = newTags
	_, resp, err = r.client.MovieAPI.UpdateMovie(radarrAuthCtx(ctx, r.cfg.Radarr), fmt.Sprintf("%d", id)).
		MovieResource(*movie).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to update radarr movie: %w", err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Removed keep request and delete tags from Radarr movie %s", movie.GetTitle())
	return nil
}

func (r *Radarr) keepRequestAlreadyProcessed(movie *radarrAPI.MovieResource, tagMap cache.TagMap) bool {
	if movie == nil {
		return false
	}
	for _, tid := range movie.GetTags() {
		name := tagMap[tid]
		if strings.HasPrefix(name, tags.JellysweepKeepPrefix) ||
			name == tags.JellysweepDeleteForSureTag {
			return true
		}
	}
	return false
}

// GetItemAddedDate retrieves the first date when a movie was imported.
func (r *Radarr) GetItemAddedDate(ctx context.Context, movieID int32) (*time.Time, error) {
	var allHistory []radarrAPI.HistoryResource
	page := int32(1)
	pageSize := int32(250)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		historyResp, resp, err := r.client.HistoryAPI.GetHistory(radarrAuthCtx(ctx, r.cfg.Radarr)).
			Page(page).
			PageSize(pageSize).
			MovieIds([]int32{movieID}).
			Execute()
		if err != nil {
			log.Warnf("Failed to get Radarr history for movie %d: %v", movieID, err)
			return nil, err
		}
		_ = resp.Body.Close()

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

	// Find the earliest "downloaded" or "importedmovie" event
	var earliestTime *time.Time
	for _, record := range allHistory {
		eventType := record.GetEventType()
		if eventType == radarrAPI.MOVIEHISTORYEVENTTYPE_DOWNLOAD_FOLDER_IMPORTED ||
			eventType == radarrAPI.MOVIEHISTORYEVENTTYPE_MOVIE_FOLDER_IMPORTED {
			recordTime := record.GetDate()
			if earliestTime == nil || recordTime.Before(*earliestTime) {
				earliestTime = &recordTime
			}
		}
	}

	if earliestTime != nil {
		log.Debugf("Radarr movie %d first imported on: %s", movieID, earliestTime.Format(time.RFC3339))
	}

	return earliestTime, nil
}
