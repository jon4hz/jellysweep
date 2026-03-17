package sizefilter

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/dustin/go-humanize"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/filter"
)

// Filter implements the filter.Filterer interface.
type Filter struct {
	cfg *config.Config
}

var _ filter.Filterer = (*Filter)(nil)

// New creates a new size Filter instance.
func New(cfg *config.Config) *Filter {
	return &Filter{
		cfg: cfg,
	}
}

// String returns the name of the filter.
func (f *Filter) String() string { return "Size Filter" }

// Apply filters media items based on size-specific keep criteria.
func (f *Filter) Apply(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	filteredItems := make([]arr.MediaItem, 0)
	for _, item := range mediaItems {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Get the file size for this media item
		var fileSize int64
		switch item.MediaType {
		case models.MediaTypeTV:
			if item.SeriesResource.HasStatistics() {
				stats := item.SeriesResource.GetStatistics()
				if stats.HasSizeOnDisk() {
					fileSize = stats.GetSizeOnDisk()
				}
			}
		case models.MediaTypeMovie:
			fileSize = item.MovieResource.GetSizeOnDisk()
		default:
			log.Warn("unknown media type for item", "mediaType", item.MediaType, "title", item.Title)
			continue
		}

		// Check if the content size meets the configured threshold
		libraryConfig := f.cfg.GetLibraryConfig(item.LibraryName)
		if libraryConfig != nil && libraryConfig.GetContentSizeThreshold() > 0 {
			if fileSize >= libraryConfig.GetContentSizeThreshold() {
				filteredItems = append(filteredItems, item)
				log.Debug("including item for deletion", "title", item.Title, "size", humanize.Bytes(safeUint64(fileSize)), "threshold", humanize.Bytes(safeUint64(libraryConfig.GetContentSizeThreshold())))
			} else {
				log.Debug("excluding item due to small size", "title", item.Title, "size", humanize.Bytes(safeUint64(fileSize)), "threshold", humanize.Bytes(safeUint64(libraryConfig.GetContentSizeThreshold())))
			}
		} else {
			// No size threshold configured or threshold is 0, include the item
			filteredItems = append(filteredItems, item)
			if libraryConfig == nil {
				log.Debug("no library config, including for deletion", "library", item.LibraryName, "title", item.Title)
			} else {
				log.Debug("no size threshold configured, including for deletion", "library", item.LibraryName, "title", item.Title)
			}
		}
	}

	return filteredItems, nil
}

// safeUint64 safely converts int64 to uint64, returning 0 for negative values.
func safeUint64(value int64) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
}
