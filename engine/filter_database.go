package engine

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/database"
	"github.com/jon4hz/jellysweep/engine/arr"
)

func (e *Engine) filterAlreadyMarkedForDeletion(mediaItems mediaItemsMap) (mediaItemsMap, error) {
	filteredItems := make(mediaItemsMap, 0)

	dbItems, err := e.db.GetMediaItems(context.Background())
	if err != nil {
		return nil, err
	}
	for lib, items := range mediaItems {
		for _, item := range items {
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
				filteredItems[lib] = append(filteredItems[lib], item)
			}
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
