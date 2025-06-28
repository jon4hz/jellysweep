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
				if strings.HasPrefix(e.data.sonarrTags[tagID], "jellysweep-delete-") {
					log.Debugf("Sonarr series %s already marked for deletion with tag %s", item.Title, e.data.sonarrTags[tagID])
					continue seriesLoop
				}
			}

			cleanupDelay := e.cfg.Jellysweep.Libraries[lib].CleanupDelay
			if cleanupDelay <= 0 {
				cleanupDelay = 1
			}
			deleteTagLabel := fmt.Sprintf("jellysweep-delete-%s", time.Now().Add(time.Duration(cleanupDelay)*24*time.Hour).Format("2006-01-02"))

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
		if len(tag.SeriesIds) == 0 && strings.HasPrefix(tag.GetLabel(), "jellysweep-delete-") {
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
	}

	return nil
}
