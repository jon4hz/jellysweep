package engine

import (
	"context"

	"github.com/charmbracelet/log"
)

func (e *Engine) runEstimateDeletionsJob(ctx context.Context) error {
	log.Info("starting estimate deletions job")

	mediaItems, err := e.db.GetMediaItems(ctx, false)
	if err != nil {
		log.Error("failed to get media items for estimation", "error", err)
		return err
	}

	for _, item := range mediaItems {
		estimatedDeleteAt, err := e.policy.GetEstimatedDeleteAt(ctx, item)
		if err != nil {
			log.Error("failed to estimate delete at for media item", "itemID", item.ID, "error", err)
			continue
		}
		err = e.db.SetMediaEstimatedDeleteAt(ctx, item.ID, estimatedDeleteAt)
		if err != nil {
			log.Error("failed to set estimated delete at for media item", "itemID", item.ID, "error", err)
			continue
		}
		log.Debug("estimated delete at set for media item", "itemID", item.ID, "estimatedDeleteAt", estimatedDeleteAt)
	}

	log.Info("estimate deletions job completed successfully")
	return nil
}
