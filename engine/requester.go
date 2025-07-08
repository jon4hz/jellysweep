package engine

import (
	"context"

	"github.com/charmbracelet/log"
	"golang.org/x/sync/errgroup"
)

// populateRequesterInfo populates the RequestedBy and RequestDate fields for media items using Jellyseerr data.
func (e *Engine) populateRequesterInfo(ctx context.Context) {
	if e.jellyseerr == nil {
		log.Debug("Jellyseerr client not available, skipping requester info population")
		return
	}

	const concurrencyLimit = 10

	for lib, items := range e.data.mediaItems {
		// Create errgroup with concurrency limit
		g, ctx := errgroup.WithContext(ctx)
		g.SetLimit(concurrencyLimit)

		// Process each item concurrently with the errgroup
		for i := range items {
			g.Go(func() error {
				item := &items[i] // Capture the item reference

				requestInfo, err := e.jellyseerr.GetRequestInfo(ctx, item.TmdbId, string(item.MediaType))
				if err != nil {
					log.Errorf("Failed to get request info for item %s: %v", item.Title, err)
					return nil // Don't fail the entire operation for individual item errors
				}

				if requestInfo != nil && requestInfo.RequestTime != nil {
					item.RequestDate = *requestInfo.RequestTime
					item.RequestedBy = requestInfo.UserEmail
					log.Debugf("Populated requester info for %s: requested by %s on %s",
						item.Title, item.RequestedBy, item.RequestDate.Format("2006-01-02"))
				}

				return nil
			})
		}

		// Wait for all goroutines to complete
		if err := g.Wait(); err != nil {
			log.Errorf("Error processing requester info for library %s: %v", lib, err)
		}

		// Update the items in the map
		e.data.mediaItems[lib] = items
	}
}
