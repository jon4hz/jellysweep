package engine

import (
	"context"
	"fmt"
	"sort"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/shirou/gopsutil/v3/disk"
)

// sweepDiskStats holds the disk usage stats needed for sweep_until calculations.
type sweepDiskStats struct {
	usedBytes uint64
	freeBytes uint64
}

// mountKey returns a string that uniquely identifies the underlying filesystem.
// Two library paths on the same mount will return identical (usedBytes, freeBytes)
// from Statfs, making this a reliable same-mount indicator within a single sweep run.
func (s *sweepDiskStats) mountKey() string {
	return fmt.Sprintf("%d-%d", s.usedBytes, s.freeBytes)
}

// freeGB returns the available space in gigabytes (SI: 1 GB = 1,000,000,000 bytes).
func (s *sweepDiskStats) freeGB() float64 {
	return float64(s.freeBytes) / 1e9
}

// usedPercent returns the df-style used percentage: used/(used+available)*100.
// This matches the percentage shown by the `df` command.
func (s *sweepDiskStats) usedPercent() float64 {
	denominator := s.usedBytes + s.freeBytes
	if denominator == 0 {
		return 0
	}
	return float64(s.usedBytes) / float64(denominator) * 100.0
}

// isTargetMet reports whether the sweep_until target is satisfied given the
// bytes accumulated for deletion so far.
func (s *sweepDiskStats) isTargetMet(cfg *config.CleanupConfig, accumulatedBytes int64) bool {
	if cfg.SweepUntilGBFree > 0 {
		estimatedFreeGB := (float64(s.freeBytes) + float64(accumulatedBytes)) / 1e9
		if estimatedFreeGB >= cfg.SweepUntilGBFree {
			return true
		}
	}
	if cfg.SweepUntilPercentUsed > 0 {
		denominator := float64(s.usedBytes + s.freeBytes)
		if denominator > 0 {
			newUsed := float64(s.usedBytes) - float64(accumulatedBytes)
			if newUsed < 0 {
				newUsed = 0
			}
			estimatedUsedPct := newUsed / denominator * 100.0
			if estimatedUsedPct <= cfg.SweepUntilPercentUsed {
				return true
			}
		}
	}
	return false
}

// getSweepDiskStats returns disk usage stats for a library by checking all its folder paths.
// When paths span multiple mounts, the most-used mount (highest usage percent) is used,
// matching the conservative behaviour of the existing disk threshold policy.
func getSweepDiskStats(ctx context.Context, folders []string) (*sweepDiskStats, error) {
	var result *sweepDiskStats
	var lastErr error

	for _, path := range folders {
		usage, err := disk.UsageWithContext(ctx, path)
		if err != nil {
			log.Error("failed to get disk usage for path", "path", path, "error", err)
			lastErr = err
			continue
		}
		candidate := &sweepDiskStats{usedBytes: usage.Used, freeBytes: usage.Free}
		if result == nil || candidate.usedPercent() > result.usedPercent() {
			result = candidate
		}
	}

	if result == nil {
		return nil, lastErr
	}
	return result, nil
}

// estimateFreedSize estimates the bytes that would be freed by deleting the given item,
// taking cleanup_mode and keep_count into account for TV series.
func estimateFreedSize(item arr.MediaItem, cleanupMode config.CleanupMode, keepCount int) int64 {
	switch item.MediaType {
	case models.MediaTypeMovie:
		return item.MovieResource.Statistics.GetSizeOnDisk()

	case models.MediaTypeTV:
		totalSize := item.SeriesResource.Statistics.GetSizeOnDisk()
		if cleanupMode == config.CleanupModeAll || keepCount <= 0 {
			return totalSize
		}

		seasons := item.SeriesResource.GetSeasons()

		switch cleanupMode { //nolint:exhaustive
		case config.CleanupModeKeepEpisodes:
			var totalEps int
			for _, season := range seasons {
				if season.GetSeasonNumber() == 0 {
					continue // skip specials
				}
				if season.HasStatistics() {
					stats := season.GetStatistics()
					totalEps += int(stats.GetEpisodeFileCount())
				}
			}
			if totalEps <= keepCount {
				return 0 // series already meets keep criteria, nothing to free
			}
			return int64(float64(totalSize) * float64(totalEps-keepCount) / float64(totalEps))

		case config.CleanupModeKeepSeasons:
			var seasonsWithFiles int
			for _, season := range seasons {
				if season.GetSeasonNumber() == 0 {
					continue // skip specials
				}
				if season.HasStatistics() {
					stats := season.GetStatistics()
					if stats.GetEpisodeFileCount() > 0 {
						seasonsWithFiles++
					}
				}
			}
			if seasonsWithFiles <= keepCount {
				return 0 // series already meets keep criteria, nothing to free
			}
			return int64(float64(totalSize) * float64(seasonsWithFiles-keepCount) / float64(seasonsWithFiles))
		}
	}

	return 0
}

// applySweepUntilLimits restricts which items get marked for deletion based on the
// sweep_until_gb_free or sweep_until_percent_used settings in each library's config.
// Items are sorted largest-first (by estimated freed bytes) so that the disk target
// is reached with as few items as possible.
// If no sweep_until setting is configured for a library, all items for that library
// are returned unchanged.
func (e *Engine) applySweepUntilLimits(ctx context.Context, mediaItems []arr.MediaItem) []arr.MediaItem {
	// Build a per-library index while preserving the original ordering within each library.
	type libraryEntry struct {
		name  string
		items []arr.MediaItem
	}
	seen := make(map[string]int) // library name → index in entries
	entries := make([]libraryEntry, 0)
	for _, item := range mediaItems {
		idx, ok := seen[item.LibraryName]
		if !ok {
			idx = len(entries)
			seen[item.LibraryName] = idx
			entries = append(entries, libraryEntry{name: item.LibraryName})
		}
		entries[idx].items = append(entries[idx].items, item)
	}

	cleanupMode := e.cfg.GetCleanupMode()
	keepCount := e.cfg.GetKeepCount()

	// mountAccumulated tracks bytes already earmarked for deletion per mount point.
	// Libraries on the same mount share this counter so their combined contribution
	// is counted against the target rather than each library calculating independently.
	mountAccumulated := make(map[string]int64)

	result := make([]arr.MediaItem, 0, len(mediaItems))

	for _, entry := range entries {
		libraryConfig := e.cfg.GetLibraryConfig(entry.name)
		if libraryConfig == nil ||
			(libraryConfig.SweepUntilGBFree <= 0 && libraryConfig.SweepUntilPercentUsed <= 0) {
			// No sweep_until configured for this library – include everything.
			result = append(result, entry.items...)
			continue
		}

		folders := e.libraryFoldersMap[entry.name]
		if len(folders) == 0 {
			log.Warn("no library folders found for sweep_until limit, including all items", "library", entry.name)
			result = append(result, entry.items...)
			continue
		}

		stats, err := getSweepDiskStats(ctx, folders)
		if err != nil {
			log.Error("failed to get disk usage for sweep_until limit, including all items",
				"library", entry.name, "error", err)
			result = append(result, entry.items...)
			continue
		}

		mountKey := stats.mountKey()
		alreadyAccumulated := mountAccumulated[mountKey]

		log.Info("applying sweep_until limit",
			"library", entry.name,
			"sweep_until_gb_free", libraryConfig.SweepUntilGBFree,
			"sweep_until_percent_used", libraryConfig.SweepUntilPercentUsed,
			"current_free_gb", stats.freeGB(),
			"current_used_pct", stats.usedPercent(),
			"already_freed_gb_on_mount", float64(alreadyAccumulated)/1e9,
		)

		// Sort by estimated freed size descending so we reach the target with fewest items.
		sorted := make([]arr.MediaItem, len(entry.items))
		copy(sorted, entry.items)
		sort.Slice(sorted, func(i, j int) bool {
			return estimateFreedSize(sorted[i], cleanupMode, keepCount) >
				estimateFreedSize(sorted[j], cleanupMode, keepCount)
		})

		var newlyAccumulated int64
		included := 0
		for _, item := range sorted {
			if stats.isTargetMet(libraryConfig, alreadyAccumulated+newlyAccumulated) {
				break
			}
			newlyAccumulated += estimateFreedSize(item, cleanupMode, keepCount)
			result = append(result, item)
			included++
		}

		mountAccumulated[mountKey] += newlyAccumulated

		log.Info("sweep_until limit applied",
			"library", entry.name,
			"items_included", included,
			"items_excluded", len(entry.items)-included,
			"estimated_freed_gb", float64(newlyAccumulated)/1e9,
		)
	}

	return result
}
