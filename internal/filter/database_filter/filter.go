package databasefilter

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/filter"
)

// Filter implements the filter.Filterer interface.
type Filter struct {
	db database.MediaDB
}

var _ filter.Filterer = (*Filter)(nil)

// New creates a new database Filter instance.
func New(db database.MediaDB) *Filter {
	return &Filter{
		db: db,
	}
}

// String returns the name of the filter.
func (f *Filter) String() string { return "Database Filter" }

// Apply filters out media items that are already marked for deletion in the database.
func (f *Filter) Apply(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	filteredItems := make([]arr.MediaItem, 0)

	dbItems, err := f.db.GetMediaItems(ctx, true)
	if err != nil {
		return nil, err
	}
	for _, item := range mediaItems {
		markedForDeletion := false
		for _, dbItem := range dbItems {
			if arrItemIsEqual(item, dbItem) {
				log.Debugf("Excluding item %s already marked for deletion in database", item.Title)
				markedForDeletion = true
				break
			}
		}
		if !markedForDeletion {
			log.Debugf("Including item %s not marked for deletion in database", item.Title)
			filteredItems = append(filteredItems, item)
		}
	}

	return filteredItems, nil
}

func arrItemIsEqual(a arr.MediaItem, b database.Media) bool {
	switch a.MediaType {
	case models.MediaTypeMovie:
		return a.MovieResource.GetId() == b.ArrID
	case models.MediaTypeTV:
		return a.SeriesResource.GetId() == b.ArrID
	default:
		return false
	}
}
