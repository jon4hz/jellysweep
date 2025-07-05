package engine

import (
	"fmt"
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
				if libraryConfig != nil {
					if slices.Contains(libraryConfig.ExcludeTags, jellysweepIgnoreTag) {
						log.Debugf("Ignoring item %s due to jellysweep-ignore tag", item.Title)
						hasExcludedTag = true
						break
					}
					if slices.Contains(libraryConfig.ExcludeTags, tagName) {
						hasExcludedTag = true
						log.Debugf("Excluding item %s due to tag: %s", item.Title, tagName)
						break
					}
				}
				// Check for jellysweep-must-keep- tags
				if strings.HasPrefix(tagName, jellysweepKeepPrefix) {
					// Parse the date and requester from the keep tag
					keepDate, _, err := e.parseKeepTagWithRequester(tagName)
					if err != nil {
						log.Warnf("Failed to parse keep tag %s: %v", tagName, err)
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

// parseKeepTagWithRequester extracts the date and requester from a jellysweep-must-keep tag.
// Format: jellysweep-must-keep-YYYY-MM-DD-requester
func (e *Engine) parseKeepTagWithRequester(tagName string) (date time.Time, requester string, err error) {
	if !strings.HasPrefix(tagName, jellysweepKeepPrefix) {
		return time.Time{}, "", fmt.Errorf("not a keep tag")
	}

	// Remove the prefix
	tagContent := strings.TrimPrefix(tagName, jellysweepKeepPrefix)

	// Split by dash to separate date and requester
	parts := strings.Split(tagContent, "-")

	// We need at least 3 parts for YYYY-MM-DD, and optionally a requester part
	if len(parts) < 3 {
		return time.Time{}, "", fmt.Errorf("invalid tag format")
	}

	// Parse date from first 3 parts
	dateStr := strings.Join(parts[:3], "-")
	date, err = time.Parse("2006-01-02", dateStr)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("failed to parse date: %w", err)
	}

	// If there's a 4th part, it's the requester
	if len(parts) > 3 {
		requester = parts[3]
	}

	return date, requester, nil
}

// createKeepTagWithRequester creates a jellysweep-must-keep tag with requester information.
// Format: jellysweep-must-keep-YYYY-MM-DD-requester
func (e *Engine) createKeepTagWithRequester(date time.Time, requester string) string {
	dateStr := date.Format("2006-01-02")
	if requester != "" {
		// Sanitize requester to avoid issues with special characters
		sanitizedRequester := strings.ReplaceAll(requester, "-", "_")
		sanitizedRequester = strings.ReplaceAll(sanitizedRequester, " ", "_")
		return fmt.Sprintf("%s%s-%s", jellysweepKeepPrefix, dateStr, sanitizedRequester)
	}
	return fmt.Sprintf("%s%s", jellysweepKeepPrefix, dateStr)
}

// parseKeepRequestTagWithRequester extracts the date and requester from a jellysweep-keep-request tag.
// Format: jellysweep-keep-request-YYYY-MM-DD-requester
func (e *Engine) parseKeepRequestTagWithRequester(tagName string) (date time.Time, requester string, err error) {
	if !strings.HasPrefix(tagName, jellysweepKeepRequestPrefix) {
		return time.Time{}, "", fmt.Errorf("not a keep request tag")
	}

	// Remove the prefix
	tagContent := strings.TrimPrefix(tagName, jellysweepKeepRequestPrefix)

	// Split by dash to separate date and requester
	parts := strings.Split(tagContent, "-")

	// We need at least 3 parts for YYYY-MM-DD, and optionally a requester part
	if len(parts) < 3 {
		return time.Time{}, "", fmt.Errorf("invalid tag format")
	}

	// Parse date from first 3 parts
	dateStr := strings.Join(parts[:3], "-")
	date, err = time.Parse("2006-01-02", dateStr)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("failed to parse date: %w", err)
	}

	// If there's a 4th part, it's the requester
	if len(parts) > 3 {
		requester = parts[3]
	}

	return date, requester, nil
}

// createKeepRequestTagWithRequester creates a jellysweep-keep-request tag with requester information.
// Format: jellysweep-keep-request-YYYY-MM-DD-requester
func (e *Engine) createKeepRequestTagWithRequester(date time.Time, requester string) string {
	dateStr := date.Format("2006-01-02")
	if requester != "" {
		// Sanitize requester to avoid issues with special characters
		sanitizedRequester := strings.ReplaceAll(requester, "-", "_")
		sanitizedRequester = strings.ReplaceAll(sanitizedRequester, " ", "_")
		return fmt.Sprintf("%s%s-%s", jellysweepKeepRequestPrefix, dateStr, sanitizedRequester)
	}
	return fmt.Sprintf("%s%s", jellysweepKeepRequestPrefix, dateStr)
}
