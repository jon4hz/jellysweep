package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/shirou/gopsutil/v3/disk"
)

// sweepDiskStats holds the disk usage stats needed for sweep_until calculations.
type sweepDiskStats struct {
	usedBytes   uint64
	freeBytes   uint64
	totalBytes  uint64 // f_blocks * f_bsize — filesystem capacity, stable between resizes
	totalInodes uint64 // f_files — inode capacity, set at mkfs time
}

// mountKey returns a string that identifies the underlying filesystem for
// shared-budget accounting.  It is derived from two values that are fixed at
// filesystem-creation time and do not change during normal operation:
//
//   - totalBytes  (f_blocks × f_bsize): the partition capacity in bytes
//   - totalInodes (f_files):            the total inode count
//
// Any path on the same filesystem — including separate bind-mounts of the same
// volume, which is common in Docker media-server setups — returns the same key,
// so all such libraries share a single deletion budget.  Two unrelated
// filesystems would need to match on both capacity and inode count to collide,
// which is effectively impossible in practice.
func (s *sweepDiskStats) mountKey() string {
	return fmt.Sprintf("fs:%d:%d", s.totalBytes, s.totalInodes)
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
		candidate := &sweepDiskStats{
			usedBytes:   usage.Used,
			freeBytes:   usage.Free,
			totalBytes:  usage.Total,
			totalInodes: usage.InodesTotal,
		}
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

// libraryFoldersByName looks up folder paths for a library with case-insensitive fallback,
// matching the same normalisation that viper applies to map keys.
func (e *Engine) libraryFoldersByName(name string) []string {
	if folders, ok := e.libraryFoldersMap[name]; ok {
		return folders
	}
	nameLower := strings.ToLower(name)
	for k, v := range e.libraryFoldersMap {
		if strings.ToLower(k) == nameLower {
			return v
		}
	}
	return nil
}

// pendingMountBytes returns the total FileSize of all currently-pending (unprotected)
// media items in the database, keyed by resolved mount point.
// This seeds the sweep_until accumulator so that space already earmarked for deletion
// by previous runs is counted against the disk-space budget before new items are selected.
// FileSize is the full on-disk size stored at insertion time; for keep_episodes/keep_seasons
// modes this is an overestimate of what will actually be freed, making the seed conservative
// (i.e. we may queue fewer new items than strictly necessary, but never too many).
// Mount resolution is performed once per unique library to avoid redundant syscalls.
func (e *Engine) pendingMountBytes(ctx context.Context) map[string]int64 {
	pendingItems, err := e.db.GetMediaItems(ctx, false)
	if err != nil {
		log.Warn("failed to query pending media items for sweep_until budget seed, treating pending as 0", "error", err)
		return make(map[string]int64)
	}

	mountByLibrary := make(map[string]string) // library name → resolved mount (or "" on failure)
	result := make(map[string]int64)

	for _, item := range pendingItems {
		if item.FileSize <= 0 {
			continue
		}

		mp, cached := mountByLibrary[item.LibraryName]
		if !cached {
			folders := e.libraryFoldersByName(item.LibraryName)
			if len(folders) == 0 {
				log.Debug("no library folders for pending item, skipping from sweep_until seed",
					"library", item.LibraryName)
				mountByLibrary[item.LibraryName] = ""
				continue
			}
			usage, err := disk.UsageWithContext(ctx, folders[0])
			if err != nil {
				log.Warn("failed to stat pending item library for sweep_until seed, skipping",
					"library", item.LibraryName, "error", err)
				mountByLibrary[item.LibraryName] = ""
				continue
			}
			mp = fmt.Sprintf("fs:%d:%d", usage.Total, usage.InodesTotal)
			mountByLibrary[item.LibraryName] = mp
		}

		if mp != "" {
			result[mp] += item.FileSize
		}
	}

	if len(result) > 0 {
		for mp, bytes := range result {
			log.Info("sweep_until budget seeded with pending DB items",
				"mount", mp,
				"pending_gb", float64(bytes)/1e9,
			)
		}
	}

	return result
}

// applySweepUntilLimits restricts which items get marked for deletion based on the
// sweep_until_gb_free or sweep_until_percent_used settings in each library's config.
// Items are sorted largest-first (by estimated freed bytes) so that the disk target
// is reached with as few items as possible.
// If no sweep_until setting is configured for a library, all items for that library
// are returned unchanged.
func (e *Engine) applySweepUntilLimits(ctx context.Context, mediaItems []arr.MediaItem) []arr.MediaItem {
	// Build a per-library index. Items within each library are later sorted largest-first,
	// so the original intra-library ordering is not preserved.
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
	// It is seeded with the FileSize of items already pending deletion in the database
	// so that previous-run queued items count against the budget before new ones are selected.
	// Libraries on the same mount share this counter so their combined contribution
	// is counted against the target rather than each library calculating independently.
	mountAccumulated := e.pendingMountBytes(ctx)

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
			log.Error("no library folders found for sweep_until limit, skipping all items to avoid over-queuing",
				"library", entry.name)
			continue
		}

		stats, err := getSweepDiskStats(ctx, folders)
		if err != nil {
			log.Error("failed to get disk usage for sweep_until limit, skipping all items to avoid over-queuing",
				"library", entry.name, "error", err)
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
