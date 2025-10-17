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
	client     *sonarrAPI.APIClient
	stats      stats.Statser
	cfg        *config.Config
	itemsCache *cache.PrefixedCache[[]sonarrAPI.SeriesResource]
	tagsCache  *cache.PrefixedCache[cache.TagMap]
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

func NewSonarr(client *sonarrAPI.APIClient, cfg *config.Config, stats stats.Statser, itemsCache *cache.PrefixedCache[[]sonarrAPI.SeriesResource], tagsCache *cache.PrefixedCache[cache.TagMap]) *Sonarr {
	return &Sonarr{
		client:     client,
		cfg:        cfg,
		stats:      stats,
		itemsCache: itemsCache,
		tagsCache:  tagsCache,
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
		libraryName := strings.ToLower(jf.ParentLibraryName)
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
