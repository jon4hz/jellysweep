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
	client     *radarrAPI.APIClient
	cfg        *config.Config
	stats      stats.Statser
	itemsCache *cache.PrefixedCache[[]radarrAPI.MovieResource]
	tagsCache  *cache.PrefixedCache[cache.TagMap]
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

func NewRadarr(client *radarrAPI.APIClient, cfg *config.Config, stats stats.Statser, itemsCache *cache.PrefixedCache[[]radarrAPI.MovieResource], tagsCache *cache.PrefixedCache[cache.TagMap]) *Radarr {
	return &Radarr{
		client:     client,
		cfg:        cfg,
		stats:      stats,
		itemsCache: itemsCache,
		tagsCache:  tagsCache,
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
		libraryName := strings.ToLower(jf.ParentLibraryName)
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

func (r *Radarr) DeleteMedia(ctx context.Context, movieID int32, title string) error {
	if r.cfg.DryRun {
		log.Infof("Dry run: Would delete Radarr movie %s", title)
		return nil
	}

	resp, err := r.client.MovieAPI.DeleteMovie(radarrAuthCtx(ctx, r.cfg.Radarr), movieID).
		DeleteFiles(true).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to delete Radarr movie %s: %w", title, err)
	}
	defer resp.Body.Close() //nolint: errcheck

	log.Infof("Deleted Radarr movie %s", title)
	return nil
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
				// Get library name directly from jellyfin item
				if libName := jellyfinItem.ParentLibraryName; libName != "" {
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
