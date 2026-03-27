package sweepuntilfilter

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/filter"
	"github.com/shirou/gopsutil/v3/disk"
)

// LibraryFoldersMap is a shared mutable reference to library folder paths.
// The engine updates it each run via Set; the filter reads it during Apply.
type LibraryFoldersMap struct {
	m map[string][]string
}

// NewLibraryFoldersMap creates an empty LibraryFoldersMap.
func NewLibraryFoldersMap() *LibraryFoldersMap {
	return &LibraryFoldersMap{}
}

// Set replaces the current map with m.
func (l *LibraryFoldersMap) Set(m map[string][]string) {
	l.m = m
}

// get returns the folder paths for name with case-insensitive fallback.
func (l *LibraryFoldersMap) get(name string) []string {
	if folders, ok := l.m[name]; ok {
		return folders
	}
	lower := strings.ToLower(name)
	for k, v := range l.m {
		if strings.ToLower(k) == lower {
			return v
		}
	}
	return nil
}

// Filter implements the filter.Filterer interface for sweep_until quota group limits.
// It restricts which items get marked for deletion based on disk-space targets configured
// per quota group, stopping once the estimated freed space meets the target.
type Filter struct {
	cfg          *config.Config
	db           database.DB
	libraryFolders *LibraryFoldersMap
}

var _ filter.Filterer = (*Filter)(nil)

// New creates a new sweep until Filter instance.
func New(cfg *config.Config, db database.DB, libraryFolders *LibraryFoldersMap) *Filter {
	return &Filter{
		cfg:            cfg,
		db:             db,
		libraryFolders: libraryFolders,
	}
}

// String returns the name of the filter.
func (f *Filter) String() string { return "Sweep Until Filter" }

// Apply filters media items based on sweep_until quota group disk space limits.
// Libraries without a quota group are passed through unchanged. Once a group's target
// is estimated to be met, further items from that group are excluded.
func (f *Filter) Apply(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	if len(f.cfg.SweepUntilQuotaGroups) == 0 {
		return mediaItems, nil
	}

	// Build library → group and group → []library mappings from current config.
	libraryGroup := make(map[string]string)
	groupLibraries := make(map[string][]string)
	for name, libCfg := range f.cfg.Libraries {
		if libCfg == nil || libCfg.Filter.SweepUntilQuotaGroup == "" {
			continue
		}
		group := libCfg.Filter.SweepUntilQuotaGroup
		if _, ok := f.cfg.SweepUntilQuotaGroups[group]; !ok {
			log.Warn("library references undefined sweep_until_quota_group, skipping",
				"library", name, "group", group)
			continue
		}
		libraryGroup[name] = group
		groupLibraries[group] = append(groupLibraries[group], name)
	}

	// Fetch the partition table once for the entire filter run.
	// all=true includes virtual/fuse filesystems (mergerfs, ZFS, etc.) that are
	// excluded by default but commonly used for media storage.
	partitions, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		log.Warn("failed to enumerate system partitions; filesystem deduplication may be imprecise",
			"error", err)
	}

	// Pre-compute disk stats and seed accumulated bytes for each quota group.
	groupAccumulated := f.pendingGroupBytes(ctx, libraryGroup)
	groups := make(map[string]*groupQuotaState, len(f.cfg.SweepUntilQuotaGroups))

	for groupName, groupCfg := range f.cfg.SweepUntilQuotaGroups {
		if groupCfg == nil {
			continue
		}
		gs := &groupQuotaState{
			cfg:         groupCfg,
			accumulated: groupAccumulated[groupName],
		}

		var allFolders []string
		for _, libName := range groupLibraries[groupName] {
			allFolders = append(allFolders, f.libraryFolders.get(libName)...)
		}
		if len(allFolders) == 0 {
			log.Error("no library folders found for quota group, will skip all items in group to avoid over-queuing",
				"group", groupName)
			gs.broken = true
		} else {
			stats, err := getQuotaGroupDiskStats(ctx, allFolders, partitions)
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

	cleanupMode := f.cfg.GetCleanupMode()
	keepCount := f.cfg.GetKeepCount()

	// For groups with largest_first enabled, reorder their items by estimated freed size
	// (descending) while keeping all other items at their original positions.
	mediaItems = applyLargestFirstSort(mediaItems, libraryGroup, groups, cleanupMode, keepCount)

	// Single pass in original filter order. Each group's quota is tracked independently.
	result := make([]arr.MediaItem, 0, len(mediaItems))
	groupIncluded := make(map[string]int)
	groupExcluded := make(map[string]int)

	for _, item := range mediaItems {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

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
			log.Debug("excluding item - quota group target met", "title", item.Title, "group", group)
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

	return result, nil
}

// pendingGroupBytes returns the total FileSize of all currently-pending (unprotected)
// media items in the database, keyed by quota group name.
// This seeds the sweep_until accumulator so that space already earmarked for deletion
// by previous runs is counted against the disk-space budget before new items are selected.
func (f *Filter) pendingGroupBytes(ctx context.Context, libraryGroup map[string]string) map[string]int64 {
	pendingItems, err := f.db.GetMediaItems(ctx, false)
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

// sweepDiskStats holds aggregated disk usage stats across one or more filesystems.
type sweepDiskStats struct {
	usedBytes  uint64
	freeBytes  uint64
	totalBytes uint64 // sum of partition capacities across all unique filesystems
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

// groupQuotaState holds runtime quota tracking for a single sweep_until_quota_group.
type groupQuotaState struct {
	cfg         *config.QuotaGroupConfig
	stats       *sweepDiskStats
	accumulated int64 // seed (pending DB items) + newly accumulated this run
	broken      bool  // disk stats unavailable — skip all items in this group
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

// resolveMountKey returns a string that uniquely identifies the underlying filesystem
// containing path by walking the partition table and finding the longest matching
// mount point (most specific mount wins).
//
// The block-device path is used as the key where available. For virtual or
// network filesystems (NFS, overlay, tmpfs, etc.) where the device field is not
// a real block path, the mount point itself is used as the key.
//
// This is safe across all platforms and deployment scenarios:
//   - Windows NTFS: InodesTotal is always 0, making capacity-based keys collide
//     for same-size drives. Device paths (e.g. \\.\C:) are unique per volume.
//   - Linux same-size drives: two 8 TB ext4 drives have identical Total and
//     InodesTotal values. Their block devices (/dev/sda1, /dev/sdb1) are distinct.
//   - Docker bind mounts: the container sees the host block device, so two bind
//     paths from the same source volume map to the same key and are counted once.
//   - NFS: server:/export/path is used as the device, uniquely identifying the share.
//
// If no partition covers path (unusual), the cleaned path itself is returned as a
// fallback so that unresolved paths are never incorrectly merged with each other.
func resolveMountKey(path string, partitions []disk.PartitionStat) string {
	// Normalise to forward slashes for consistent prefix matching on all platforms.
	cleaned := filepath.ToSlash(filepath.Clean(path))

	bestLen := -1
	var bestDevice, bestMount string

	for _, p := range partitions {
		mount := filepath.ToSlash(filepath.Clean(p.Mountpoint))

		// Build a prefix that always ends with "/" so that the root mount "/"
		// matches every absolute path without false-positives (e.g. "/foo" must
		// not match the mount "/foobar").
		prefix := mount
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}

		// cleaned+"/" lets us match when path == mount exactly (e.g. path is
		// the mount point directory itself).
		if strings.HasPrefix(cleaned+"/", prefix) && len(mount) > bestLen {
			bestLen = len(mount)
			bestDevice = p.Device
			bestMount = p.Mountpoint
		}
	}

	if bestLen == -1 {
		log.Warn("could not resolve mount point for path, treating as unique filesystem",
			"path", path)
		return "unmatched:" + cleaned
	}

	// Prefer block device path. Ignore placeholder device names used by virtual
	// filesystems (overlay, none, tmpfs, proc, sysfs, etc.) so that they fall
	// back to the mount point, which is always unique per entry in the table.
	if bestDevice != "" && bestDevice != "none" &&
		!strings.HasPrefix(bestDevice, "tmpfs") &&
		!strings.HasPrefix(bestDevice, "overlay") &&
		!strings.HasPrefix(bestDevice, "proc") &&
		!strings.HasPrefix(bestDevice, "sysfs") {
		return "dev:" + bestDevice
	}
	return "mount:" + bestMount
}

// getQuotaGroupDiskStats returns aggregated disk usage stats across all unique
// filesystems referenced by the given folder paths.
func getQuotaGroupDiskStats(ctx context.Context, folders []string, partitions []disk.PartitionStat) (*sweepDiskStats, error) {
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
		key := resolveMountKey(path, partitions)
		if strings.HasPrefix(key, "unmatched:") {
			// Partition table unavailable (e.g. Docker container without /proc mount).
			// Fall back to a stats-based key using Total+Free to detect paths that share
			// the same underlying filesystem without relying on device paths.
			statsKey := fmt.Sprintf("stats:%d:%d", usage.Total, usage.Free)
			log.Debug("partition table unavailable for path, using stats-based deduplication",
				"path", path, "stats_key", statsKey)
			key = statsKey
		}
		if seen[key] {
			log.Debug("path shares filesystem with an already-counted path, skipping",
				"path", path, "mount_key", key)
			continue
		}
		seen[key] = true
		log.Debug("counting filesystem for quota group",
			"path", path, "mount_key", key,
			"used_gb", float64(usage.Used)/1e9,
			"free_gb", float64(usage.Free)/1e9,
		)
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

// applyLargestFirstSort returns a copy of mediaItems where items belonging to quota
// groups that have LargestFirst=true are re-ordered by estimated freed size (descending)
// within their original slot positions. Items belonging to other groups are unchanged.
// A stable sort is used so that equal-sized items preserve their prior scheduled order.
func applyLargestFirstSort(
	mediaItems []arr.MediaItem,
	libraryGroup map[string]string,
	groups map[string]*groupQuotaState,
	cleanupMode config.CleanupMode,
	keepCount int,
) []arr.MediaItem {
	needsSort := false
	for _, gs := range groups {
		if gs.cfg.LargestFirst {
			needsSort = true
			break
		}
	}
	if !needsSort {
		return mediaItems
	}

	newItems := make([]arr.MediaItem, len(mediaItems))
	copy(newItems, mediaItems)

	for groupName, gs := range groups {
		if !gs.cfg.LargestFirst {
			continue
		}

		// Collect the original positions (in order of appearance) of items in this group.
		var positions []int
		for i, item := range mediaItems {
			if resolveLibraryGroup(item.LibraryName, libraryGroup) == groupName {
				positions = append(positions, i)
			}
		}
		if len(positions) == 0 {
			continue
		}

		// Build a sorted copy of the positions, descending by estimated freed size.
		sortedPositions := make([]int, len(positions))
		copy(sortedPositions, positions)
		sort.SliceStable(sortedPositions, func(a, b int) bool {
			sizeA := estimateFreedSize(mediaItems[sortedPositions[a]], cleanupMode, keepCount)
			sizeB := estimateFreedSize(mediaItems[sortedPositions[b]], cleanupMode, keepCount)
			return sizeA > sizeB
		})

		// Write sorted items back into the group's original slot positions.
		for i, pos := range positions {
			newItems[pos] = mediaItems[sortedPositions[i]]
		}
		log.Debug("largest_first sort applied to quota group",
			"group", groupName, "item_count", len(positions))
	}

	return newItems
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
