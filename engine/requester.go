package engine

import (
	"context"
	"sync"

	"github.com/charmbracelet/log"
)

// populateRequesterInfo populates the RequestedBy and RequestDate fields for media items using Jellyseerr data.
func (e *Engine) populateRequesterInfo(ctx context.Context) {
	if e.jellyseerr == nil {
		log.Debug("Jellyseerr client not available, skipping requester info population")
		return
	}

	const batchSize = 10
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, batchSize)

	for lib, items := range e.data.mediaItems {
		// Process items in batches of 10 concurrently
		for i := 0; i < len(items); i += batchSize {
			end := i + batchSize
			if end > len(items) {
				end = len(items)
			}

			batch := items[i:end]
			wg.Add(1)

			go func(batch []MediaItem) {
				defer wg.Done()

				var batchWg sync.WaitGroup

				// Process each item in the batch concurrently
				for j := range batch {
					batchWg.Add(1)
					semaphore <- struct{}{} // Acquire semaphore slot

					go func(item *MediaItem) {
						defer batchWg.Done()
						defer func() { <-semaphore }() // Release semaphore slot

						requestInfo, err := e.jellyseerr.GetRequestInfo(ctx, item.TmdbId, string(item.MediaType))
						if err != nil {
							log.Debugf("Failed to get request info for item %s: %v", item.Title, err)
							return
						}

						if requestInfo != nil && requestInfo.RequestTime != nil {
							item.RequestDate = *requestInfo.RequestTime
							item.RequestedBy = requestInfo.UserEmail
							log.Debugf("Populated requester info for %s: requested by %s on %s",
								item.Title, item.RequestedBy, item.RequestDate.Format("2006-01-02"))
						}
					}(&batch[j])
				}

				batchWg.Wait()
			}(batch)
		}

		wg.Wait()

		// Update the items in the map
		e.data.mediaItems[lib] = items
	}
}
