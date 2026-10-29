package favoritesfilter

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/filter"
)

// FavoritesSource provides the Jellyfin IDs of all favorited movies and series.
type FavoritesSource interface {
	GetFavoriteItemIDs(ctx context.Context) (map[string]bool, error)
}

// Filter implements the filter.Filterer interface for Jellyfin favorites.
type Filter struct {
	cfg    *config.Config
	source FavoritesSource
}

var _ filter.Filterer = (*Filter)(nil)

// New creates a new favorites Filter instance.
func New(cfg *config.Config, source FavoritesSource) *Filter {
	return &Filter{cfg: cfg, source: source}
}

// String returns the name of the filter.
func (f *Filter) String() string { return "Favorites Filter" }

// Apply removes media items that any Jellyfin user has marked as a favorite.
// Respects the per-library favorites_enabled setting in the filter configuration.
func (f *Filter) Apply(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	if !f.anyLibraryEnabled(mediaItems) {
		return mediaItems, nil
	}

	favorites, err := f.source.GetFavoriteItemIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch jellyfin favorites: %w", err)
	}

	filteredItems := make([]arr.MediaItem, 0, len(mediaItems))
	for _, item := range mediaItems {
		if f.enabledFor(item.LibraryName) && item.JellyfinID != "" && favorites[item.JellyfinID] {
			log.Debug("Excluding item marked as favorite", "item", item.Title, "library", item.LibraryName, "jellyfinID", item.JellyfinID)
			continue
		}
		filteredItems = append(filteredItems, item)
	}

	return filteredItems, nil
}

func (f *Filter) anyLibraryEnabled(mediaItems []arr.MediaItem) bool {
	for _, item := range mediaItems {
		if f.enabledFor(item.LibraryName) {
			return true
		}
	}
	return false
}

func (f *Filter) enabledFor(libraryName string) bool {
	libraryConfig := f.cfg.GetLibraryConfig(libraryName)
	return libraryConfig != nil && libraryConfig.Filter.FavoritesEnabled
}
