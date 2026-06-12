package sweepuntilfilter

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
	sonarrAPI "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/filter"
	"github.com/shirou/gopsutil/v3/disk"
)

// maxWithheldTitlesLogged caps how many withheld item titles are listed per group in
// the summary log to avoid flooding logs on large libraries.
const maxWithheldTitlesLogged = 15

// bytesPerGB converts bytes to SI gigabytes (1 GB = 1,000,000,000 bytes), matching the
// unit used by the gb_free config option.
const bytesPerGB = 1e9

// zfsPoolKeyPrefix marks mount keys that identify a ZFS pool rather than a single
// filesystem; see resolveMountKey and quotaGroupDiskStats for the special accounting.
const zfsPoolKeyPrefix = "dev:zfs:"

// lookupFold returns the value for key in m, falling back to a case-insensitive match.
// Viper lowercases config map keys while Jellyfin reports display-cased names, so the
// two sides of a lookup frequently disagree only in case.
func lookupFold[V any](m map[string]V, key string) (V, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	lower := strings.ToLower(key)
	for k, v := range m {
		if strings.ToLower(k) == lower {
			return v, true
		}
	}
	var zero V
	return zero, false
}

// LibraryFoldersMap is a shared, concurrency-safe reference to library folder paths.
// The engine updates it each run via Set; the filter reads it during Apply.
type LibraryFoldersMap struct {
	mu sync.RWMutex
	m  map[string][]string
}

// NewLibraryFoldersMap creates an empty LibraryFoldersMap.
func NewLibraryFoldersMap() *LibraryFoldersMap {
	return &LibraryFoldersMap{}
}

// Set replaces the current map with m.
func (l *LibraryFoldersMap) Set(m map[string][]string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m = m
}

// get returns the folder paths for name with case-insensitive fallback.
func (l *LibraryFoldersMap) get(name string) []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	folders, _ := lookupFold(l.m, name)
	return folders
}

// All returns a snapshot copy of the full library→folders map, safe to range without
// holding the lock.
func (l *LibraryFoldersMap) All() map[string][]string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[string][]string, len(l.m))
	for k, v := range l.m {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// pendingMediaStore is the single database capability the filter needs: listing the
// media items currently pending deletion. Narrowing the dependency keeps the filter
// testable with a trivial fake instead of a full database.
type pendingMediaStore interface {
	GetMediaItems(ctx context.Context, includeProtected bool) ([]database.Media, error)
}

// diskStatser abstracts the gopsutil disk calls so tests can simulate arbitrary
// storage layouts (mergerfs pools, ZFS datasets, partial failures) without real mounts.
type diskStatser interface {
	Partitions(ctx context.Context, all bool) ([]disk.PartitionStat, error)
	Usage(ctx context.Context, path string) (*disk.UsageStat, error)
}

// gopsutilDiskStatser is the production diskStatser backed by gopsutil.
type gopsutilDiskStatser struct{}

func (gopsutilDiskStatser) Partitions(ctx context.Context, all bool) ([]disk.PartitionStat, error) {
	return disk.PartitionsWithContext(ctx, all)
}

func (gopsutilDiskStatser) Usage(ctx context.Context, path string) (*disk.UsageStat, error) {
	return disk.UsageWithContext(ctx, path)
}

// Filter implements the filter.Filterer interface for sweep_until quota group limits.
// It restricts which items get marked for deletion based on disk-space targets configured
// per quota group, stopping once the estimated freed space meets the target.
type Filter struct {
	cfg            *config.Config
	db             pendingMediaStore
	libraryFolders *LibraryFoldersMap
	disk           diskStatser
}

var _ filter.Filterer = (*Filter)(nil)

// New creates a new sweep until Filter instance.
func New(cfg *config.Config, db pendingMediaStore, libraryFolders *LibraryFoldersMap) *Filter {
	return &Filter{
		cfg:            cfg,
		db:             db,
		libraryFolders: libraryFolders,
		disk:           gopsutilDiskStatser{},
	}
}

// String returns the name of the filter.
func (f *Filter) String() string { return "Sweep Until Filter" }

// itemFreed pairs a media item with its memoized quota group and estimated freeable
// size so both are computed exactly once per item instead of being recomputed during
// sorting and accumulation.
type itemFreed struct {
	item  arr.MediaItem
	group string
	freed int64
}

// Apply filters media items based on sweep_until quota group disk space limits.
// Libraries without a quota group are passed through unchanged. Once a group's target
// is estimated to be met, further items from that group are excluded.
func (f *Filter) Apply(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	if len(f.cfg.SweepUntilQuotaGroups) == 0 {
		return mediaItems, nil
	}

	// Group names are normalised to lowercase at config validation; build a lowercased
	// view anyway so directly-constructed configs behave identically.
	groupConfigs := make(map[string]*config.QuotaGroupConfig, len(f.cfg.SweepUntilQuotaGroups))
	for name, gc := range f.cfg.SweepUntilQuotaGroups {
		groupConfigs[strings.ToLower(name)] = gc
	}

	// Build library → group and group → []library mappings from current config.
	// Disabled libraries are excluded: their items are never gathered, so counting them
	// would only activate groups (and log warnings) for libraries that cannot sweep.
	libraryGroup := make(map[string]string)
	groupLibraries := make(map[string][]string)
	for name, libCfg := range f.cfg.Libraries {
		if libCfg == nil || !libCfg.Enabled || libCfg.Filter.SweepUntilQuotaGroup == "" {
			continue
		}
		group := strings.ToLower(libCfg.Filter.SweepUntilQuotaGroup)
		if _, ok := groupConfigs[group]; !ok {
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
	partitions, err := f.disk.Partitions(ctx, true)
	if err != nil {
		log.Warn("failed to enumerate system partitions; filesystem deduplication may be imprecise",
			"error", err)
	}

	// Warn when libraries outside any quota group share a filesystem with a group's
	// folders: their growth is invisible to the group's budget and can defeat the target.
	f.warnCoResidentUnmanagedLibraries(libraryGroup, partitions)
	// Warn when groups share a filesystem or when the partition table looks like it
	// belongs to a different mount namespace.
	f.warnGroupMountIssues(groupLibraries, partitions)

	// Pre-compute the pending-deletion seed for each quota group. If the database is
	// unavailable we cannot know how much space is already earmarked, so the only safe
	// choice is to withhold every grouped item this run rather than risk over-queuing.
	groupAccumulated, pendingIDs, seedErr := f.pendingGroupBytes(ctx, libraryGroup, groupConfigs)
	if seedErr != nil {
		log.Warn("failed to query pending media items for sweep_until budget seed; "+
			"withholding all grouped items this run to avoid over-queuing", "error", seedErr)
	}

	groups := make(map[string]*groupQuotaState, len(groupConfigs))
	for groupName, groupCfg := range groupConfigs {
		if groupCfg == nil {
			continue
		}
		// Skip groups that no library references. Config validation already warns about
		// unreferenced groups once at startup; processing them here would mark them
		// "broken" and emit a warning on every cleanup run.
		if len(groupLibraries[groupName]) == 0 {
			continue
		}
		gs := &groupQuotaState{
			cfg:         groupCfg,
			accumulated: groupAccumulated[groupName],
		}
		groups[groupName] = gs

		if seedErr != nil {
			gs.broken = true
			continue
		}

		var allFolders []string
		for _, libName := range groupLibraries[groupName] {
			allFolders = append(allFolders, f.libraryFolders.get(libName)...)
		}
		if len(allFolders) == 0 {
			log.Warn("no library folders found for quota group, will skip all items in group to avoid over-queuing",
				"group", groupName)
			gs.broken = true
			continue
		}

		stats, err := f.quotaGroupDiskStats(ctx, allFolders, partitions)
		if err != nil || stats == nil {
			log.Warn("failed to get complete disk usage for quota group, will skip all items in group to avoid over-queuing",
				"group", groupName, "error", err)
			gs.broken = true
			continue
		}

		gs.stats = stats
		// Warn when the configured target can never be reached: deleting everything can
		// at most free the currently used bytes, so the achievable free space is
		// used+free (which excludes e.g. root-reserved blocks, unlike raw capacity).
		achievableFreeGB := float64(stats.usedBytes+stats.freeBytes) / bytesPerGB
		if groupCfg.GBFree > 0 && groupCfg.GBFree > achievableFreeGB {
			log.Warn("sweep_until quota group gb_free target exceeds the achievable free space; "+
				"the target can never be met and the group will sweep every eligible item each run",
				"group", groupName, "gb_free", groupCfg.GBFree,
				"achievable_free_gb", achievableFreeGB)
		}
		log.Info("sweep_until quota group ready",
			"group", groupName,
			"order", groupCfg.GetOrder(),
			"target_percent_used", groupCfg.PercentUsed,
			"target_gb_free", groupCfg.GBFree,
			"current_used_pct", stats.usedPercent(),
			"current_free_gb", stats.freeGB(),
			"already_freed_gb", float64(gs.accumulated)/bytesPerGB,
		)
	}

	// Memoize each item's quota group (once per distinct library name, not per item)
	// and freeable size. Ungrouped items pass through unconditionally, so their size
	// is never needed.
	cleanupMode := f.cfg.GetCleanupMode()
	keepCount := f.cfg.GetKeepCount()
	groupByLibrary := make(map[string]string)
	entries := make([]itemFreed, len(mediaItems))
	for i, item := range mediaItems {
		group, cached := groupByLibrary[item.LibraryName]
		if !cached {
			group = resolveLibraryGroup(item.LibraryName, libraryGroup)
			groupByLibrary[item.LibraryName] = group
		}
		e := itemFreed{item: item, group: group}
		if e.group != "" {
			e.freed = FreeableSize(item, cleanupMode, keepCount)
		}
		entries[i] = e
	}

	// Reorder each group's items according to its configured order, keeping every item
	// within its group's original slot positions so interleaving with other groups is
	// unchanged. The default order preserves the order items were reported eligible.
	applyGroupOrdering(entries, groups)

	// Single pass in (possibly reordered) order. Each group's quota is tracked independently.
	result := make([]arr.MediaItem, 0, len(mediaItems))
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		item := entry.item
		if entry.group == "" {
			result = append(result, item) // no quota group — include unconditionally
			continue
		}

		gs, ok := groups[entry.group]
		if !ok || gs.broken {
			if ok {
				gs.excluded++
			}
			continue
		}

		// An item whose Jellyfin ID is already pending in the database was re-added in
		// Sonarr/Radarr under a new arr ID (the database filter matches by arr ID only).
		// Its bytes are already counted by the seed; marking it again would double-charge
		// the budget and create a duplicate row.
		if item.JellyfinID != "" && pendingIDs[item.JellyfinID] {
			gs.excluded++
			log.Debug("excluding item - already pending in database under a previous arr ID",
				"title", item.Title, "group", entry.group)
			continue
		}

		if gs.stats.isTargetMet(gs.cfg, gs.accumulated) {
			gs.excluded++
			if len(gs.withheld) < maxWithheldTitlesLogged {
				gs.withheld = append(gs.withheld, item.Title)
			}
			log.Debug("excluding item - quota group target met", "title", item.Title, "group", entry.group)
			continue
		}

		gs.accumulated += entry.freed
		result = append(result, item)
		gs.included++
	}

	for groupName, gs := range groups {
		if gs.broken {
			log.Warn("sweep_until quota group was broken this run; all its items were withheld",
				"group", groupName, "items_withheld", gs.excluded)
			continue
		}
		log.Info("sweep_until quota group limit applied",
			"group", groupName,
			"order", gs.cfg.GetOrder(),
			"items_included", gs.included,
			"items_excluded", gs.excluded,
			"estimated_freed_gb", float64(gs.accumulated-groupAccumulated[groupName])/bytesPerGB,
			"dry_run", f.cfg.DryRun,
		)
		if len(gs.withheld) > 0 {
			log.Info("sweep_until withheld items (kept this run because the quota target was met)",
				"group", groupName, "withheld_count", gs.excluded,
				"first_titles", strings.Join(gs.withheld, ", "))
		}
		// arr-reported sizes can diverge from real filesystem usage (hardlinks counted
		// multiple times, non-media data sharing the pool). Surface it so operators can
		// reconcile a budget that overshoots.
		if gs.stats != nil && float64(gs.accumulated) > float64(gs.stats.usedBytes) {
			log.Warn("sweep_until estimated freed bytes exceed measured filesystem usage; "+
				"arr-reported sizes may over-count or non-media data shares the pool",
				"group", groupName,
				"estimated_freed_gb", float64(gs.accumulated)/bytesPerGB,
				"measured_used_gb", float64(gs.stats.usedBytes)/bytesPerGB,
			)
		}
	}

	return result, nil
}

// pendingGroupBytes returns the estimated freeable bytes of all currently-pending
// (unprotected) media items in the database keyed by quota group name, plus the set of
// pending Jellyfin IDs (used to avoid double-charging re-added items).
// This seeds the sweep_until accumulator so that space already earmarked for deletion by
// previous runs is counted against the disk-space budget before new items are selected.
//
// Attribution prefers the quota group stored on the row at mark time (Media.QuotaGroup),
// which survives library renames and config regrouping; rows without one (older versions)
// fall back to resolving the library name against the current config. Rows that resolve
// to a group no longer defined fall back the same way.
//
// Sizing prefers Media.FreeableSize (the keep-mode-aware estimate stored at mark time;
// nil means unknown) and falls back to Media.FileSize for older rows. Rows referencing
// the same Jellyfin item (e.g. after an arr re-add changed the ArrID) are counted once.
func (f *Filter) pendingGroupBytes(
	ctx context.Context,
	libraryGroup map[string]string,
	groupConfigs map[string]*config.QuotaGroupConfig,
) (map[string]int64, map[string]bool, error) {
	pendingItems, err := f.db.GetMediaItems(ctx, false)
	if err != nil {
		return nil, nil, err
	}

	result := make(map[string]int64)
	pendingIDs := make(map[string]bool, len(pendingItems))
	unattributed := 0
	for _, item := range pendingItems {
		if item.JellyfinID != "" {
			if pendingIDs[item.JellyfinID] {
				continue
			}
			pendingIDs[item.JellyfinID] = true
		}

		size := item.FileSize
		if item.FreeableSize != nil {
			size = *item.FreeableSize
		}
		if size <= 0 {
			continue
		}

		group := strings.ToLower(item.QuotaGroup)
		if group == "" || groupConfigs[group] == nil {
			group = resolveLibraryGroup(item.LibraryName, libraryGroup)
		}
		if group == "" {
			if item.QuotaGroup != "" {
				// The item was budgeted against a group that no longer exists and its
				// library no longer resolves either: its bytes leave the budget, which
				// can make the remaining groups sweep more than expected.
				unattributed++
			}
			continue
		}
		result[group] += size
	}

	if unattributed > 0 {
		log.Warn("pending media items reference quota groups that no longer exist; "+
			"their earmarked bytes no longer count against any budget",
			"items", unattributed)
	}

	for group, bytes := range result {
		log.Info("sweep_until budget seeded with pending DB items",
			"quota_group", group,
			"pending_gb", float64(bytes)/bytesPerGB,
			"dry_run", f.cfg.DryRun,
		)
	}

	return result, pendingIDs, nil
}

// warnCoResidentUnmanagedLibraries logs a warning for each library that is NOT part of
// any quota group but whose folders live on a filesystem that a quota group also uses.
// Such a library's growth is not counted against the group's budget and can prevent the
// group from ever meeting its target. Only libraries known to this jellysweep instance
// (i.e. present in the Jellyfin folder map) can be detected.
func (f *Filter) warnCoResidentUnmanagedLibraries(libraryGroup map[string]string, partitions []disk.PartitionStat) {
	all := f.libraryFolders.All()
	if len(all) == 0 || len(libraryGroup) == 0 {
		return
	}

	// Mount keys covered by quota-managed libraries.
	managed := make(map[string]string) // mountKey -> group name
	for libName, folders := range all {
		group := resolveLibraryGroup(libName, libraryGroup)
		if group == "" {
			continue
		}
		for _, folder := range folders {
			managed[resolveMountKey(folder, partitions)] = group
		}
	}
	if len(managed) == 0 {
		return
	}

	warned := make(map[string]bool)
	for libName, folders := range all {
		if resolveLibraryGroup(libName, libraryGroup) != "" {
			continue // managed library
		}
		for _, folder := range folders {
			key := resolveMountKey(folder, partitions)
			if group, ok := managed[key]; ok && !warned[libName] {
				warned[libName] = true
				log.Warn("library shares a filesystem with a sweep_until quota group but is not in any group; "+
					"its growth is invisible to the group's budget and can defeat the target",
					"library", libName, "shared_with_group", group, "mount_key", key)
			}
		}
	}
}

// warnGroupMountIssues warns when two quota groups share a filesystem (each would
// independently sweep its own full budget against the same pool) and when the partition
// table matches none of the groups' folders (likely a different mount namespace, e.g.
// HOST_PROC set in a container).
func (f *Filter) warnGroupMountIssues(groupLibraries map[string][]string, partitions []disk.PartitionStat) {
	mountClaims := make(map[string]string) // mountKey -> first group claiming it
	totalFolders, unmatchedFolders := 0, 0

	for groupName, libraries := range groupLibraries {
		for _, libName := range libraries {
			for _, folder := range f.libraryFolders.get(libName) {
				key := resolveMountKey(folder, partitions)
				totalFolders++
				if strings.HasPrefix(key, "unmatched:") {
					unmatchedFolders++
				}
				if prev, ok := mountClaims[key]; ok && prev != groupName {
					log.Warn("two sweep_until quota groups share a filesystem; each will independently "+
						"sweep its own full budget against the same pool — put libraries that share "+
						"storage into one group",
						"groups", prev+","+groupName, "mount_key", key)
				} else {
					mountClaims[key] = groupName
				}
			}
		}
	}

	if len(partitions) > 0 && totalFolders > 0 && unmatchedFolders == totalFolders {
		log.Warn("no quota group folder matches any partition table entry; the partition table " +
			"may describe a different mount namespace (e.g. HOST_PROC set in a container) — " +
			"falling back to per-path filesystem identity")
	}
}

// sweepDiskStats holds aggregated disk usage stats across one or more filesystems.
type sweepDiskStats struct {
	usedBytes  uint64
	freeBytes  uint64
	totalBytes uint64 // sum of partition capacities across all unique filesystems
}

// freeGB returns the available space in gigabytes (SI: 1 GB = 1,000,000,000 bytes).
func (s *sweepDiskStats) freeGB() float64 {
	return float64(s.freeBytes) / bytesPerGB
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
//
// accumulatedBytes is clamped to the measured used bytes: deleting media can never free
// more space than the filesystem currently reports as used, so arr-reported sizes (which
// can over-count via hardlinks or include data on a shared pool) cannot overshoot the
// projection.
func (s *sweepDiskStats) isTargetMet(cfg *config.QuotaGroupConfig, accumulatedBytes int64) bool {
	acc := float64(accumulatedBytes)
	if acc < 0 {
		acc = 0
	}
	used := float64(s.usedBytes)
	if acc > used {
		acc = used
	}

	if cfg.GBFree > 0 {
		estimatedFreeGB := (float64(s.freeBytes) + acc) / bytesPerGB
		if estimatedFreeGB >= cfg.GBFree {
			return true
		}
	}
	if cfg.PercentUsed > 0 {
		denominator := float64(s.usedBytes + s.freeBytes)
		if denominator > 0 {
			newUsed := used - acc // >= 0 because acc is clamped to used
			if (newUsed / denominator * 100.0) <= cfg.PercentUsed {
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
	broken      bool  // disk stats or budget seed unavailable — skip all items in this group
	included    int
	excluded    int
	withheld    []string // first few withheld titles, for the summary log
}

// resolveLibraryGroup looks up the quota group name for a given library name,
// with case-insensitive fallback to handle viper key normalisation.
func resolveLibraryGroup(libraryName string, libraryGroup map[string]string) string {
	group, _ := lookupFold(libraryGroup, libraryName)
	return group
}

// resolveMountKey returns a string that uniquely identifies the underlying filesystem
// containing path by walking the partition table and finding the longest matching
// mount point (most specific mount wins). Symlinks are resolved first so two paths to
// the same target are keyed identically; when the path's mount point appears multiple
// times in the table (overmounts, e.g. autofs placeholder plus the real mount), the
// LAST matching entry wins, matching kernel shadowing semantics.
//
// The block-device path is used as the key where available. ZFS datasets are keyed by
// their pool name (the device prefix before the first "/"): each dataset is a distinct
// filesystem, but they all share the pool's free space — quotaGroupDiskStats gives pool
// keys special accounting (used summed per dataset, free counted once). For virtual or
// network filesystems (NFS, overlay, tmpfs, etc.) where the device field is not a real
// block path, the mount point itself is used.
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
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	// Normalise to forward slashes for consistent prefix matching on all platforms.
	cleaned := filepath.ToSlash(filepath.Clean(path))

	bestLen := -1
	var bestDevice, bestMount, bestFstype string

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
		// the mount point directory itself). ">=" implements last-wins for
		// overmounted mount points.
		if strings.HasPrefix(cleaned+"/", prefix) && len(mount) >= bestLen {
			bestLen = len(mount)
			bestDevice = p.Device
			bestMount = p.Mountpoint
			bestFstype = p.Fstype
		}
	}

	if bestLen == -1 {
		log.Warn("could not resolve mount point for path, treating as unique filesystem",
			"path", path)
		return "unmatched:" + cleaned
	}

	// ZFS: key datasets by pool so a group spanning datasets shares one key;
	// quotaGroupDiskStats then sums used across the datasets while counting the
	// pool's free space once.
	if strings.EqualFold(bestFstype, "zfs") && bestDevice != "" {
		pool := bestDevice
		if p, _, found := strings.Cut(bestDevice, "/"); found && p != "" {
			pool = p
		}
		return zfsPoolKeyPrefix + pool
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

// countedFS records the st_dev (when available) of the first path counted under a
// mount key, so spurious key collisions (e.g. a partition table from a different mount
// namespace) can be detected and split.
type countedFS struct {
	dev    uint64
	hasDev bool
}

// quotaGroupDiskStats returns aggregated disk usage stats across all unique
// filesystems referenced by the given folder paths.
//
// Every path is statted; deduplication happens on the resolved mount key, verified by
// st_dev where available so two genuinely different filesystems that resolve to the
// same key (foreign partition table) are still counted separately. ZFS pool keys get
// special accounting: each dataset's used bytes are summed (a dataset's statfs only
// reports its own usage) while the pool-wide free space is counted once.
//
// It returns an error when usage could not be determined for ANY folder: incomplete
// stats look like a smaller pool and would make gb_free targets sweep far more
// aggressively than intended, so the caller must treat the group as broken rather
// than proceed with partial numbers.
func (f *Filter) quotaGroupDiskStats(ctx context.Context, folders []string, partitions []disk.PartitionStat) (*sweepDiskStats, error) {
	seen := make(map[string]countedFS)
	var combined sweepDiskStats
	var failed []string
	var lastErr error
	var foundAny bool

	for _, path := range folders {
		var key string
		if len(partitions) > 0 {
			key = resolveMountKey(path, partitions)
		}

		usage, err := f.disk.Usage(ctx, path)
		if err != nil {
			log.Error("failed to get disk usage for path", "path", path, "error", err)
			failed = append(failed, path)
			lastErr = err
			continue
		}

		dev, hasDev := deviceID(path)

		if key == "" || strings.HasPrefix(key, "unmatched:") {
			// Partition table unavailable (e.g. Docker container without /proc mount) or
			// the path matched no partition. Prefer the filesystem device id (st_dev on
			// unix), which uniquely identifies the mounted filesystem; fall back to a
			// stats-based key using Total+Free, which can miss dedup on an actively
			// written filesystem (under-sweeping, never over-sweeping).
			if hasDev {
				key = fmt.Sprintf("statdev:%d", dev)
			} else {
				key = fmt.Sprintf("stats:%d:%d", usage.Total, usage.Free)
			}
			log.Debug("partition table unavailable for path, using fallback filesystem identity",
				"path", path, "mount_key", key)
		}

		if prev, counted := seen[key]; counted {
			if strings.HasPrefix(key, zfsPoolKeyPrefix) {
				// Sibling dataset of an already-counted pool: its statfs free space is
				// the same pool-wide figure (already counted), but its used bytes are
				// dataset-local and must be added.
				combined.usedBytes += usage.Used
				log.Debug("zfs dataset shares an already-counted pool; adding its used bytes",
					"path", path, "mount_key", key, "used_gb", float64(usage.Used)/bytesPerGB)
				continue
			}
			if hasDev && prev.hasDev && dev != prev.dev {
				// Same mount key but a different underlying filesystem: the partition
				// table likely describes another mount namespace. Count it separately.
				key = fmt.Sprintf("%s+statdev:%d", key, dev)
				if _, again := seen[key]; again {
					continue
				}
			} else {
				log.Debug("path shares filesystem with an already-counted path, skipping",
					"path", path, "mount_key", key)
				continue
			}
		}

		seen[key] = countedFS{dev: dev, hasDev: hasDev}
		log.Debug("counting filesystem for quota group",
			"path", path, "mount_key", key,
			"used_gb", float64(usage.Used)/bytesPerGB,
			"free_gb", float64(usage.Free)/bytesPerGB,
		)
		combined.usedBytes += usage.Used
		combined.freeBytes += usage.Free
		combined.totalBytes += usage.Total
		foundAny = true
	}

	if len(failed) > 0 {
		return nil, fmt.Errorf("disk usage unavailable for %d of %d folder(s) (%s): %w",
			len(failed), len(folders), strings.Join(failed, ", "), lastErr)
	}
	if !foundAny {
		return nil, lastErr
	}
	return &combined, nil
}

// applyGroupOrdering reorders, in place, the items belonging to each quota group within
// their original slot positions, according to that group's configured order. Items
// belonging to other groups (and ungrouped items) keep their positions. The default
// order performs no reordering at all, preserving the order items were reported
// eligible by the preceding filters. A stable sort is used for the other orders, with a
// deterministic tie-break (title, then Jellyfin ID) so equal-sized items are fully
// ordered and the withheld set does not oscillate between runs.
func applyGroupOrdering(entries []itemFreed, groups map[string]*groupQuotaState) {
	for groupName, gs := range groups {
		if gs.broken {
			continue
		}
		order := gs.cfg.GetOrder()
		if order == config.CleanupOrderDefault {
			continue // preserve eligibility order
		}

		// Collect the original positions (in order of appearance) of this group's items.
		var positions []int
		for i := range entries {
			if entries[i].group == groupName {
				positions = append(positions, i)
			}
		}
		if len(positions) <= 1 {
			continue
		}

		// Sort a copy of this group's entries, then write them back into the same slots.
		sorted := make([]itemFreed, len(positions))
		for i, pos := range positions {
			sorted[i] = entries[pos]
		}
		slices.SortStableFunc(sorted, func(a, b itemFreed) int {
			return compareByOrder(order, a, b)
		})
		for i, pos := range positions {
			entries[pos] = sorted[i]
		}
		log.Debug("sweep_until group ordering applied", "group", groupName, "order", order, "item_count", len(positions))
	}
}

// compareByOrder compares two entries for the given order, returning a negative value
// when a sorts before b. All orders fall back to a deterministic tie-break (title, then
// Jellyfin ID); "title" uses only that tie-break.
func compareByOrder(order config.CleanupOrder, a, b itemFreed) int {
	switch order { //nolint: exhaustive // default never reaches here (no reordering)
	case config.CleanupOrderLargestFirst:
		if a.freed != b.freed {
			if a.freed > b.freed {
				return -1
			}
			return 1
		}
	case config.CleanupOrderSmallestFirst:
		if a.freed != b.freed {
			if a.freed < b.freed {
				return -1
			}
			return 1
		}
	case config.CleanupOrderTitle:
		// fall through to deterministic tie-break
	}
	if c := strings.Compare(a.item.Title, b.item.Title); c != 0 {
		return c
	}
	return strings.Compare(a.item.JellyfinID, b.item.JellyfinID)
}

// itemSizeOnDisk returns the item's total size on disk, regardless of cleanup mode.
func itemSizeOnDisk(item arr.MediaItem) int64 {
	switch item.MediaType {
	case models.MediaTypeMovie:
		return item.MovieResource.Statistics.GetSizeOnDisk()
	case models.MediaTypeTV:
		return item.SeriesResource.Statistics.GetSizeOnDisk()
	}
	return 0
}

// FreeableSize returns the estimated number of bytes that deleting item will actually
// free under the given cleanup mode. For movies and cleanup_mode "all" this is the full
// size on disk. For keep_episodes/keep_seasons it subtracts the bytes the deletion
// keeps — the first keepCount regular episodes/seasons plus all Season-0 specials —
// mirroring the Sonarr deletion logic using the per-season statistics Sonarr already
// provides. keep_episodes prorates within a season by episode-file count. The result
// is never negative.
//
// The engine stores this value on the database row at mark time (Media.FreeableSize) so
// the sweep_until budget counts the same figure for pending and newly-considered items.
func FreeableSize(item arr.MediaItem, mode config.CleanupMode, keepCount int) int64 {
	total := itemSizeOnDisk(item)
	if total < 0 {
		return 0
	}
	if item.MediaType != models.MediaTypeTV ||
		(mode != config.CleanupModeKeepEpisodes && mode != config.CleanupModeKeepSeasons) {
		return total
	}

	freeable := total - keptSeriesBytes(item.SeriesResource.GetSeasons(), mode, keepCount)
	if freeable < 0 {
		return 0
	}
	return freeable
}

// keptSeriesBytes estimates how many bytes the Sonarr deletion logic will KEEP for a
// series under keep_episodes/keep_seasons: all of Season 0 (specials), plus the first
// keepCount regular seasons (keep_seasons) or the first keepCount regular episodes
// prorated by per-season size (keep_episodes). It intentionally mirrors
// internal/engine/arr/sonarr/delete.go (getEpisodeFilesToKeep); keep the two in sync.
func keptSeriesBytes(seasons []sonarrAPI.SeasonResource, mode config.CleanupMode, keepCount int) int64 {
	type seasonInfo struct {
		number int32
		size   int64
		files  int32
	}

	var kept int64
	var regular []seasonInfo
	for i := range seasons {
		stats := seasons[i].GetStatistics()
		size := stats.GetSizeOnDisk()
		if size < 0 {
			size = 0
		}
		if seasons[i].GetSeasonNumber() == 0 {
			kept += size // specials are always kept
			continue
		}
		regular = append(regular, seasonInfo{
			number: seasons[i].GetSeasonNumber(),
			size:   size,
			files:  stats.GetEpisodeFileCount(),
		})
	}

	slices.SortFunc(regular, func(a, b seasonInfo) int { return int(a.number - b.number) })

	switch mode { //nolint: exhaustive // only called for the keep modes
	case config.CleanupModeKeepSeasons:
		for i, s := range regular {
			if i >= keepCount {
				break
			}
			kept += s.size
		}
	case config.CleanupModeKeepEpisodes:
		remaining := keepCount
		for _, s := range regular {
			if remaining <= 0 {
				break
			}
			if s.files <= 0 {
				continue
			}
			if int(s.files) <= remaining {
				kept += s.size
				remaining -= int(s.files)
			} else {
				kept += s.size * int64(remaining) / int64(s.files)
				remaining = 0
			}
		}
	}

	return kept
}
