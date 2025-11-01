package engine

import (
	"slices"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/tags"
)

func (e *Engine) filterMediaTags(mediaItems []arr.MediaItem) []arr.MediaItem {
	filteredItems := make([]arr.MediaItem, 0)
	for _, item := range mediaItems {
		// Check if the item has any tags that are not in the exclude list
		hasExcludedTag := false
		for _, tagName := range item.Tags {
			if tagName == tags.JellysweepIgnoreTag {
				log.Debugf("Ignoring item %s due to jellysweep-ignore tag", item.Title)
				hasExcludedTag = true
				break
			}
			// Check if the tag is in the exclude list
			libraryConfig := e.cfg.GetLibraryConfig(item.LibraryName)
			if libraryConfig != nil {
				if slices.Contains(libraryConfig.ExcludeTags, tagName) {
					hasExcludedTag = true
					log.Debugf("Excluding item %s due to tag: %s", item.Title, tagName)
					break
				}
			}
		}
		if !hasExcludedTag {
			filteredItems = append(filteredItems, item)
		}
	}
	return filteredItems
}
