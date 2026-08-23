package engine

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/policy"
)

// newDiskUsageFunc fetches the root folder disk usage from Sonarr and Radarr
// once and returns a UsageFunc answering from that snapshot. The usage of a
// media type is the highest usage among the root folders of its arr. An arr
// that is not configured, unreachable, or reports no usable root folders yields
// ok=false; this never fails the cleanup run.
func (e *Engine) newDiskUsageFunc(ctx context.Context) policy.UsageFunc {
	snapshot := map[database.MediaType]float64{}
	fetch := func(mediaType database.MediaType, name string, client arr.Arrer) {
		if client == nil {
			return
		}
		usage, err := client.GetRootFolderUsage(ctx)
		if err != nil {
			log.Error("failed to get disk usage from arr", "arr", name, "error", err)
			return
		}
		if len(usage) == 0 {
			log.Warn("arr reported no root folder disk usage", "arr", name)
			return
		}
		var highest float64
		for path, percent := range usage {
			log.Debug("Root folder disk usage", "arr", name, "path", path, "usagePercent", percent)
			highest = max(highest, percent)
		}
		snapshot[mediaType] = highest
	}
	fetch(database.MediaTypeTV, "sonarr", e.sonarr)
	fetch(database.MediaTypeMovie, "radarr", e.radarr)

	return func(_ context.Context, mediaType database.MediaType) (float64, bool, error) {
		usage, ok := snapshot[mediaType]
		return usage, ok, nil
	}
}
