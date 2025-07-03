package engine

import (
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// parseDeletionDateFromTag extracts the deletion date from a jellysweep tag.
func (e *Engine) parseDeletionDateFromTag(tagName string) (time.Time, error) {
	tagLabel := strings.TrimPrefix(tagName, jellysweepTagPrefix)
	dateStr := strings.TrimSuffix(tagLabel, "-")
	return time.Parse("2006-01-02", dateStr)
}

// triggerTagIDs returns tag IDs that should trigger deletion based on their date labels.
func (e *Engine) triggerTagIDs(tags map[int32]string) []int32 {
	triggerTagIDs := make([]int32, 0)
	for id, tag := range tags {
		if strings.HasPrefix(tag, jellysweepTagPrefix) {
			tagLabel := strings.TrimPrefix(tag, jellysweepTagPrefix)

			// Parse the date from the tag label
			dateStr := strings.TrimSuffix(tagLabel, "-")
			date, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				log.Warnf("failed to parse date from tag label %s: %v", tagLabel, err)
				continue
			}
			// Check if the date is in the past
			if date.Before(time.Now()) {
				// If the date is in the past, add the tag ID to the trigger list
				triggerTagIDs = append(triggerTagIDs, id)
			} else {
				log.Debugf("Skipping tag %s as it is not yet due for deletion", tagLabel)
			}
		}
	}
	return triggerTagIDs
}

func (e *Engine) filterMediaTags() {
	filteredItems := make(map[string][]MediaItem, 0)
	for lib, items := range e.data.mediaItems {
		for _, item := range items {
			// Check if the item has any tags that are not in the exclude list
			hasExcludedTag := false
			for _, tagName := range item.Tags {
				// Check if the tag is in the exclude list
				libraryConfig := e.cfg.GetLibraryConfig(lib)
				if libraryConfig != nil && slices.Contains(libraryConfig.ExcludeTags, tagName) {
					hasExcludedTag = true
					log.Debugf("Excluding item %s due to tag: %s", item.Title, tagName)
					break
				}
				// Check for jellysweep-must-keep- tags
				if strings.HasPrefix(tagName, jellysweepKeepPrefix) {
					// Parse the date to check if the keep tag is still valid
					dateStr := strings.TrimPrefix(tagName, jellysweepKeepPrefix)
					keepDate, err := time.Parse("2006-01-02", dateStr)
					if err != nil {
						log.Warnf("Failed to parse date from keep tag %s: %v", tagName, err)
						continue
					}
					if time.Now().Before(keepDate) {
						log.Debugf("Item %s has active keep tag: %s", item.Title, tagName)
						hasExcludedTag = true
						break
					} else {
						log.Debugf("Item %s has expired keep tag: %s", item.Title, tagName)
					}
				}
				// Check for jellysweep-must-delete-for-sure tags
				if strings.HasPrefix(tagName, jellysweepDeleteForSureTag) {
					// This tag indicates the item should be deleted regardless of other tags
					log.Debugf("Item %s has must-delete-for-sure tag: %s", item.Title, tagName)
					hasExcludedTag = true
					break
				}
				// Check for existing jellysweep-delete- tags
				if strings.HasPrefix(tagName, jellysweepTagPrefix) {
					// This tag indicates the item is already marked for deletion
					log.Debugf("Item %s already marked for deletion with tag: %s", item.Title, tagName)
					hasExcludedTag = true
					break
				}
			}
			if !hasExcludedTag {
				filteredItems[lib] = append(filteredItems[lib], item)
			}
		}
	}
	e.data.mediaItems = filteredItems
}
