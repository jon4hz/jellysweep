package tagsfilter

import (
	"context"
	"slices"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/filter"
	"github.com/jon4hz/jellysweep/internal/tags"
)

// Filter implements the filter.Filterer interface.
type Filter struct {
	cfg *config.Config
}

var _ filter.Filterer = (*Filter)(nil)

// New creates a new tags Filter instance.
func New(cfg *config.Config) *Filter {
	return &Filter{
		cfg: cfg,
	}
}

// String returns the name of the filter.
func (f *Filter) String() string { return "Tags Filter" }

// Apply filters media items based on tags-specific keep criteria.
func (f *Filter) Apply(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
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
			libraryConfig := f.cfg.GetLibraryConfig(item.LibraryName)
			if libraryConfig != nil {
				if slices.Contains(libraryConfig.GetExcludeTags(), tagName) {
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

	return filteredItems, nil
}
