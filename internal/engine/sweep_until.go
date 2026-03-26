package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/shirou/gopsutil/v3/disk"
)

// sweepDiskStats holds aggregated disk usage stats across one or more filesystems.
type sweepDiskStats struct {
	usedBytes  uint64
	freeBytes  uint64
	totalBytes uint64 // sum of partition capacities across all unique filesystems
}

// mountKeyFor returns a string that uniquely identifies the underlying filesystem
// for a given usage stat, used for deduplication across paths.
//
// It is derived from two values fixed at filesystem-creation time:
//   - Total bytes (f_blocks × f_bsize): partition capacity
//   - Total inodes (f_files): inode count
//
// Any path on the same filesystem — including bind-mounts or Docker volume mounts
// of the same underlying volume — returns the same key.
func mountKeyFor(usage *disk.UsageStat) string {
	return fmt.Sprintf("fs:%d:%d", usage.Total, usage.InodesTotal)
}

// freeGB returns the available space in gigabytes (SI: 1 GB = 1,000,000,000 bytes).
func (s *sweepDiskStats) freeGB() float64 {
	return float64(s.freeBytes) / 1e9
}

// usedPercent returns the df-style used percentage: used/(used+free)*100.
func (s *sweepDiskStats) usedPercent() float64 {
	denominator := s.usedBytes + s.freeBytes
	if denominator == 0 {
		return 0
	}
	return float64(s.usedBytes) / float64(denominator) * 100.0
}

// isTargetMet reports whether either sweep_until condition in the quota group config
// would be satisfied after accounting for the bytes accumulated for deletion so far.
// If both percent_used and gb_free are set, the target is met as soon as either is satisfied.
func (s *sweepDiskStats) isTargetMet(cfg *config.QuotaGroupConfig, accumulatedBytes int64) bool {
	if cfg.GBFree > 0 {
		estimatedFreeGB := (float64(s.freeBytes) + float64(accumulatedBytes)) / 1e9
		if estimatedFreeGB >= cfg.GBFree {
			return true
		}
	}
	if cfg.PercentUsed > 0 {
		denominator := float64(s.usedBytes + s.freeBytes)
		if denominator > 0 {
			newUsed := float64(s.usedBytes) - float64(accumulatedBytes)
			if newUsed < 0 {
				newUsed = 0
			}
			if (newUsed/denominator*100.0) <= cfg.PercentUsed {
				return true
			}
		}
	}
	return false
}

// getQuotaGroupDiskStats returns aggregated disk usage stats across all unique
// filesystems referenced by the given folder paths. Paths that map to the same
// underlying filesystem (identified by total bytes + inode count) are counted
// only once, so bind-mounts, NFS mounts, and Docker volume mounts are handled
// correctly across Windows, Linux, and macOS.
func getQuotaGroupDiskStats(ctx context.Context, folders []string) (*sweepDiskStats, error) {
	seen := make(map[string]bool)
	var combined sweepDiskStats
	var lastErr error
	var foundAny bool

	for _, path := range folders {
		usage, err := disk.UsageWithContext(ctx, path)
		if err != nil {
			log.Error("failed to get disk usage for path", "path", path, "error", err)
			lastErr = err
			continue
		}
		key := mountKeyFor(usage)
		if seen[key] {
			continue // already counted this filesystem
		}
		seen[key] = true
		combined.usedBytes += usage.Used
		combined.freeBytes += usage.Free
		combined.totalBytes += usage.Total
		foundAny = true
	}

	if !foundAny {
		return nil, lastErr
	}
	return &combined, nil
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
			var hasAnyStats bool
			for _, season := range seasons {
				if season.GetSeasonNumber() == 0 {
					continue // skip specials
				}
				if season.HasStatistics() {
					hasAnyStats = true
					stats := season.GetStatistics()
					totalEps += int(stats.GetEpisodeFileCount())
				}
			}
			if !hasAnyStats {
				// Season statistics were absent from the Sonarr response (e.g. series not
				// yet scanned). Fall back to the full series size so the sweep_until budget
				// is still decremented and subsequent items are not over-queued.
				return totalSize
			}
			if totalEps <= keepCount {
				return 0 // series already meets keep criteria, nothing to free
			}
			return int64(float64(totalSize) * float64(totalEps-keepCount) / float64(totalEps))

		case config.CleanupModeKeepSeasons:
			var seasonsWithFiles int
			var hasAnyStats bool
			for _, season := range seasons {
				if season.GetSeasonNumber() == 0 {
					continue // skip specials
				}
				if season.HasStatistics() {
					hasAnyStats = true
					stats := season.GetStatistics()
					if stats.GetEpisodeFileCount() > 0 {
						seasonsWithFiles++
					}
				}
			}
			if !hasAnyStats {
				// Season statistics were absent from the Sonarr response. Fall back to
				// the full series size so the sweep_until budget is still decremented.
				return totalSize
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

// resolveLibraryGroup looks up the quota group name for a given library name,
// with case-insensitive fallback to handle viper key normalisation.
func resolveLibraryGroup(libraryName string, libraryGroup map[string]string) string {
	if g, ok := libraryGroup[libraryName]; ok {
		return g
	}
	lower := strings.ToLower(libraryName)
	for k, v := range libraryGroup {
		if strings.ToLower(k) == lower {
			return v
		}
	}
	return ""
}

// pendingGroupBytes returns the total FileSize of all currently-pending (unprotected)
// media items in the database, keyed by quota group name.
// This seeds the sweep_until accumulator so that space already earmarked for deletion
// by previous runs is counted against the disk-space budget before new items are selected.
// libraryGroup maps library name → quota group name (built from the current config).
func (e *Engine) pendingGroupBytes(ctx context.Context, libraryGroup map[string]string) map[string]int64 {
	pendingItems, err := e.db.GetMediaItems(ctx, false)
	if err != nil {
		log.Warn("failed to query pending media items for sweep_until budget seed, treating pending as 0", "error", err)
		return make(map[string]int64)
	}

	result := make(map[string]int64)
	for _, item := range pendingItems {
		if item.FileSize <= 0 {
			continue
		}
		group := resolveLibraryGroup(item.LibraryName, libraryGroup)
		if group == "" {
			continue
		}
		result[group] += item.FileSize
	}

	for group, bytes := range result {
		log.Info("sweep_until budget seeded with pending DB items",
			"quota_group", group,
			"pending_gb", float64(bytes)/1e9,
		)
	}

	return result
}

// groupQuotaState holds runtime quota tracking for a single sweep_until_quota_group.
type groupQuotaState struct {
	cfg         *config.QuotaGroupConfig
	stats       *sweepDiskStats
	accumulated int64 // seed (pending DB items) + newly accumulated this run
	broken      bool  // disk stats unavailable — skip all items in this group
}

// applySweepUntilLimits restricts which items get marked for deletion based on
// sweep_until_quota_groups. Libraries that share the same quota group contribute to a
// combined disk-space budget calculated by summing storage across all unique filesystems
// their media resides on. Items are processed in their original filter order with each
// group's quota tracked independently; once a group's target is met, further items from
// that group are skipped while items from other groups continue to be evaluated.
// Libraries without a quota group are passed through unchanged.
func (e *Engine) applySweepUntilLimits(ctx context.Context, mediaItems []arr.MediaItem) []arr.MediaItem {
	if len(e.cfg.SweepUntilQuotaGroups) == 0 {
		return mediaItems
	}

	// Build library → group and group → []library mappings from current config.
	libraryGroup := make(map[string]string)
	groupLibraries := make(map[string][]string)
	for name, libCfg := range e.cfg.Libraries {
		if libCfg == nil || libCfg.Filter.SweepUntilQuotaGroup == "" {
			continue
		}
		group := libCfg.Filter.SweepUntilQuotaGroup
		if _, ok := e.cfg.SweepUntilQuotaGroups[group]; !ok {
			log.Warn("library references undefined sweep_until_quota_group, skipping",
				"library", name, "group", group)
			continue
		}
		libraryGroup[name] = group
		groupLibraries[group] = append(groupLibraries[group], name)
	}

	// Pre-compute disk stats and seed accumulated bytes for each quota group.
	groupAccumulated := e.pendingGroupBytes(ctx, libraryGroup)
	groups := make(map[string]*groupQuotaState, len(e.cfg.SweepUntilQuotaGroups))

	for groupName, groupCfg := range e.cfg.SweepUntilQuotaGroups {
		if groupCfg == nil {
			continue
		}
		gs := &groupQuotaState{
			cfg:         groupCfg,
			accumulated: groupAccumulated[groupName],
		}

		var allFolders []string
		for _, libName := range groupLibraries[groupName] {
			allFolders = append(allFolders, e.libraryFoldersByName(libName)...)
		}
		if len(allFolders) == 0 {
			log.Error("no library folders found for quota group, will skip all items in group to avoid over-queuing",
				"group", groupName)
			gs.broken = true
		} else {
			stats, err := getQuotaGroupDiskStats(ctx, allFolders)
			if err != nil || stats == nil {
				log.Error("failed to get disk usage for quota group, will skip all items in group to avoid over-queuing",
					"group", groupName, "error", err)
				gs.broken = true
			} else {
				gs.stats = stats
				log.Info("sweep_until quota group ready",
					"group", groupName,
					"target_percent_used", groupCfg.PercentUsed,
					"target_gb_free", groupCfg.GBFree,
					"current_used_pct", stats.usedPercent(),
					"current_free_gb", stats.freeGB(),
					"already_freed_gb", float64(gs.accumulated)/1e9,
				)
			}
		}
		groups[groupName] = gs
	}

	cleanupMode := e.cfg.GetCleanupMode()
	keepCount := e.cfg.GetKeepCount()

	// Single pass in original filter order. Each group's quota is tracked independently,
	// so items from all libraries in a group (Movies, TV Shows, etc.) are evaluated as
	// they appear rather than being bucketed by library type first.
	result := make([]arr.MediaItem, 0, len(mediaItems))
	groupIncluded := make(map[string]int)
	groupExcluded := make(map[string]int)

	for _, item := range mediaItems {
		group := resolveLibraryGroup(item.LibraryName, libraryGroup)
		if group == "" {
			result = append(result, item) // no quota group — include unconditionally
			continue
		}

		gs, ok := groups[group]
		if !ok || gs.broken {
			groupExcluded[group]++
			continue
		}

		if gs.stats.isTargetMet(gs.cfg, gs.accumulated) {
			groupExcluded[group]++
			continue
		}

		gs.accumulated += estimateFreedSize(item, cleanupMode, keepCount)
		result = append(result, item)
		groupIncluded[group]++
	}

	for groupName, gs := range groups {
		if gs.broken {
			continue
		}
		log.Info("sweep_until quota group limit applied",
			"group", groupName,
			"items_included", groupIncluded[groupName],
			"items_excluded", groupExcluded[groupName],
			"estimated_freed_gb", float64(gs.accumulated-groupAccumulated[groupName])/1e9,
		)
	}

	return result
}
