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

// ExcludedTag reports whether any of tagNames excludes an item of the given
// library from deletion, and returns the first such tag. The jellysweep-ignore
// tag always excludes; user-configured exclude tags are scoped per library.
//
// This is the single source of truth for exclude-tag semantics: the filter
// uses it for new candidates and the engine uses it to re-evaluate items that
// are already queued.
func ExcludedTag(cfg *config.Config, libraryName string, tagNames []string) (string, bool) {
	var excludeTags []string
	if libraryConfig := cfg.GetLibraryConfig(libraryName); libraryConfig != nil {
		excludeTags = libraryConfig.GetExcludeTags()
	}
	for _, tagName := range tagNames {
		if tagName == tags.JellysweepIgnoreTag || slices.Contains(excludeTags, tagName) {
			return tagName, true
		}
	}
	return "", false
}

// Apply filters media items based on tags-specific keep criteria.
func (f *Filter) Apply(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	filteredItems := make([]arr.MediaItem, 0)
	for _, item := range mediaItems {
		if tagName, excluded := ExcludedTag(f.cfg, item.LibraryName, item.Tags); excluded {
			log.Debug("excluding item due to tag", "title", item.Title, "tag", tagName)
			continue
		}
		filteredItems = append(filteredItems, item)
	}

	return filteredItems, nil
}
