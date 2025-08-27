package engine

import (
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/engine/arr"
	"github.com/jon4hz/jellysweep/tags"
)

func (e *Engine) filterMediaTags() {
	filteredItems := make(map[string][]arr.MediaItem, 0)
	for lib, items := range e.data.mediaItems {
		for _, item := range items {
			// Check if the item has any tags that are not in the exclude list
			hasExcludedTag := false
			for _, tagName := range item.Tags {
				if tagName == tags.JellysweepIgnoreTag {
					log.Debugf("Ignoring item %s due to jellysweep-ignore tag", item.Title)
					hasExcludedTag = true
					break
				}
				// Check if the tag is in the exclude list
				libraryConfig := e.cfg.GetLibraryConfig(lib)
				if libraryConfig != nil {
					if slices.Contains(libraryConfig.ExcludeTags, tagName) {
						hasExcludedTag = true
						log.Debugf("Excluding item %s due to tag: %s", item.Title, tagName)
						break
					}
				}
				// Check for jellysweep-must-keep- tags
				if strings.HasPrefix(tagName, tags.JellysweepKeepPrefix) {
					// Parse the date and requester from the keep tag
					keepDate, _, err := tags.ParseKeepTagWithRequester(tagName)
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
				if tagName == tags.JellysweepDeleteForSureTag {
					// This tag indicates the item should be deleted regardless of other tags
					log.Debugf("Item %s has must-delete-for-sure tag: %s", item.Title, tagName)
					hasExcludedTag = true
					break
				}
				// Check for existing jellysweep-delete- tags (including disk usage tags)
				if tags.IsJellysweepDeleteTag(tagName) {
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
