package engine

import (
	"context"

	"github.com/charmbracelet/log"
)

// estimateDeletions recalculates the estimated deletion date of every unprotected
// media item by asking the policy engine for the earliest date across all policies.
// It is called at the end of the cleanup job so the UI reflects disk usage thresholds
// that were crossed during the run. Per-item failures are logged and skipped.
func (e *Engine) estimateDeletions(ctx context.Context) error {
	log.Info("Estimating media deletion dates")

	mediaItems, err := e.db.GetMediaItems(ctx, false)
	if err != nil {
		return err
	}

	for _, item := range mediaItems {
		estimatedDeleteAt, err := e.policy.GetEstimatedDeleteAt(ctx, item)
		if err != nil {
			log.Error("failed to estimate deletion date for media item", "title", item.Title, "error", err)
			continue
		}
		if err := e.db.SetMediaEstimatedDeleteAt(ctx, item.ID, estimatedDeleteAt); err != nil {
			log.Error("failed to store estimated deletion date for media item", "title", item.Title, "error", err)
			continue
		}
		log.Debug("estimated deletion date set", "title", item.Title, "estimatedDeleteAt", estimatedDeleteAt)
	}

	log.Info("Media deletion dates estimated successfully")
	return nil
}
