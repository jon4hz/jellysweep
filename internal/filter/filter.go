package filter

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
)

// Filterer defines the interface for media item filters.
type Filterer interface {
	fmt.Stringer
	// Apply filters media items based on specific criteria and returns the filtered list.
	Apply(context.Context, []arr.MediaItem) ([]arr.MediaItem, error)
}

// Filter applies all provided filters sequentially to media items.
type Filter struct {
	filters []Filterer
}

// New creates a new Filter instance with the given filters.
func New(filters ...Filterer) *Filter {
	return &Filter{
		filters: filters,
	}
}

// ApplyAll applies all filters sequentially to the provided media items.
func (f *Filter) ApplyAll(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	var err error
	filteredItems := mediaItems

	for _, filter := range f.filters {
		preFilterCount := len(filteredItems)
		log.Info("Applying filter to media items.", "filter", filter.String(), "initial_items", preFilterCount)
		filteredItems, err = filter.Apply(ctx, filteredItems)
		if err != nil {
			log.Error("Failed to apply filter.", "filter", filter.String(), "error", err)
			return nil, err
		}
		log.Info("Filter applied successfully.", "filter", filter.String(), "remaining_items", len(filteredItems), "filtered_out", preFilterCount-len(filteredItems))
	}

	return filteredItems, nil
}
