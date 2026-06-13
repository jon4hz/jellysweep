package sweepuntilfilter

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/charmbracelet/log"
	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// The filter logs per-group info/warn lines on every Apply; keep test output readable.
	log.SetLevel(log.FatalLevel)
	os.Exit(m.Run())
}

// --- fakes ---

// fakeDisk implements diskStatser with a stubbed partition table and per-path usage,
// letting tests simulate mergerfs pools, ZFS datasets, partial failures, and missing
// partition tables without any real mounts.
type fakeDisk struct {
	mu         sync.Mutex
	partitions []disk.PartitionStat
	partErr    error
	usage      map[string]disk.UsageStat
	usageErr   map[string]error
	usageCalls []string
}

func (d *fakeDisk) Partitions(_ context.Context, _ bool) ([]disk.PartitionStat, error) {
	return d.partitions, d.partErr
}

func (d *fakeDisk) Usage(_ context.Context, path string) (*disk.UsageStat, error) {
	d.mu.Lock()
	d.usageCalls = append(d.usageCalls, path)
	d.mu.Unlock()
	if err, ok := d.usageErr[path]; ok {
		return nil, err
	}
	u, ok := d.usage[path]
	if !ok {
		return nil, fmt.Errorf("no usage stubbed for %s", path)
	}
	return &u, nil
}

// fakeStore implements pendingMediaStore with a fixed pending-items list.
type fakeStore struct {
	items []database.Media
	err   error
	calls []bool // includeProtected argument of each call
}

func (s *fakeStore) GetMediaItems(_ context.Context, includeProtected bool) ([]database.Media, error) {
	s.calls = append(s.calls, includeProtected)
	if s.err != nil {
		return nil, s.err
	}
	return s.items, nil
}

// --- scenario helpers ---

func usageGB(total, used, free int64) disk.UsageStat {
	return disk.UsageStat{
		Total: uint64(total * gb),
		Used:  uint64(used * gb),
		Free:  uint64(free * gb),
	}
}

// newTestFilter builds a Filter whose libraries each reference a quota group ("" = no
// group) and whose disk/DB calls hit the given fakes.
func newTestFilter(
	groups map[string]*config.QuotaGroupConfig,
	libGroups map[string]string,
	folders map[string][]string,
	fd *fakeDisk,
	fs *fakeStore,
) *Filter {
	libs := make(map[string]*config.CleanupConfig, len(libGroups))
	for name, group := range libGroups {
		libs[name] = &config.CleanupConfig{
			Enabled: true,
			Filter:  config.FilterConfig{SweepUntilQuotaGroup: group},
		}
	}
	cfg := &config.Config{
		Libraries:             libs,
		SweepUntilQuotaGroups: groups,
	}
	lf := NewLibraryFoldersMap()
	lf.Set(folders)
	f := New(cfg, fs, lf)
	f.disk = fd
	return f
}

func titles(items []arr.MediaItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func pending(library string, sizeGB int64) database.Media {
	return database.Media{LibraryName: library, FileSize: sizeGB * gb}
}

// singlePool is the common one-filesystem fixture: /data on /dev/sda1.
func singlePool(total, used, free int64) *fakeDisk {
	return &fakeDisk{
		partitions: []disk.PartitionStat{{Device: "/dev/sda1", Mountpoint: "/data"}},
		usage:      map[string]disk.UsageStat{"/data/movies": usageGB(total, used, free)},
	}
}

var moviesOnData = map[string][]string{"Movies": {"/data/movies"}}

// --- Apply: passthrough & basics ---

func TestApplyNoQuotaGroupsPassesThrough(t *testing.T) {
	f := newTestFilter(nil, map[string]string{"Movies": ""}, moviesOnData, &fakeDisk{}, &fakeStore{})
	in := []arr.MediaItem{makeMovie("Movies", "A", "1", 10*gb)}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"A"}, titles(out))
	assert.Empty(t, (&fakeDisk{}).usageCalls)
}

func TestApplyUngroupedLibraryUnaffected(t *testing.T) {
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 100}}, // target already met (200 free)
		map[string]string{"Movies": "media", "Recordings": ""},
		moviesOnData, fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "A", "1", 10*gb),
		makeMovie("Recordings", "R", "2", 10*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	// Grouped item withheld (target met), ungrouped passes unconditionally.
	assert.Equal(t, []string{"R"}, titles(out))
}

func TestApplyLibraryReferencingUndefinedGroupPassesThrough(t *testing.T) {
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 100}},
		map[string]string{"Movies": "ghost"}, // group not defined
		moviesOnData, fd, &fakeStore{},
	)
	in := []arr.MediaItem{makeMovie("Movies", "A", "1", 10*gb)}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	// The undefined reference is dropped from the mapping, so items are NOT budgeted.
	assert.Equal(t, []string{"A"}, titles(out))
}

// --- Apply: accumulation & cutoff ---

func TestApplyAccumulatesUntilGBFreeTarget(t *testing.T) {
	// 200 GB free, target 250 -> need 50 GB. Three 30 GB movies; the default order
	// preserves arrival order: C (acc 30), A (acc 60), B withheld (200+60 >= 250).
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "C", "3", 30*gb),
		makeMovie("Movies", "A", "1", 30*gb),
		makeMovie("Movies", "B", "2", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"C", "A"}, titles(out))
}

func TestApplyAccumulatesUntilPercentUsedTarget(t *testing.T) {
	// 800/1000 = 80% used, target 75% -> need 50 GB freed.
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {PercentUsed: 75}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "A", "1", 30*gb),
		makeMovie("Movies", "B", "2", 30*gb),
		makeMovie("Movies", "C", "3", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B"}, titles(out))
}

func TestApplyBothTargetsEitherSatisfiedStops(t *testing.T) {
	// gb_free 210 is reached after one 30 GB item even though percent_used 10 never is.
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 210, PercentUsed: 10}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "A", "1", 30*gb),
		makeMovie("Movies", "B", "2", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"A"}, titles(out))
}

func TestApplyTargetAlreadyMetWithholdsEverything(t *testing.T) {
	fd := singlePool(1000, 500, 500)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 400}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, &fakeStore{},
	)
	in := []arr.MediaItem{makeMovie("Movies", "A", "1", 30*gb)}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Empty(t, titles(out))
}

// --- Apply: pending-DB seed ---

func TestApplySeedFromPendingDBReducesNewMarks(t *testing.T) {
	// Need 50 GB; 40 GB already pending -> only one more 30 GB item fits.
	fd := singlePool(1000, 800, 200)
	store := &fakeStore{items: []database.Media{pending("Movies", 40)}}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, store,
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "A", "1", 30*gb),
		makeMovie("Movies", "B", "2", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"A"}, titles(out))
	// The seed must only count unprotected (pending) items.
	require.Len(t, store.calls, 1)
	assert.False(t, store.calls[0], "seed must query GetMediaItems(includeProtected=false)")
}

func TestApplySeedIgnoresPendingItemsOutsideAnyGroup(t *testing.T) {
	fd := singlePool(1000, 800, 200)
	store := &fakeStore{items: []database.Media{pending("Recordings", 9999)}}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"Movies": "media", "Recordings": ""},
		moviesOnData, fd, store,
	)
	in := []arr.MediaItem{makeMovie("Movies", "A", "1", 30*gb)}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"A"}, titles(out))
}

func TestApplySeedDBErrorWithholdsGroupedItems(t *testing.T) {
	// When the pending-items query fails, the filter cannot know how much space is
	// already earmarked, so it must withhold all grouped items rather than risk
	// re-marking up to the full budget on top of existing pending items.
	fd := singlePool(1000, 800, 200)
	store := &fakeStore{err: fmt.Errorf("db down")}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"Movies": "media", "Recordings": ""},
		moviesOnData, fd, store,
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "A", "1", 30*gb),
		makeMovie("Movies", "B", "2", 30*gb),
		makeMovie("Recordings", "R", "3", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"R"}, titles(out), "grouped items withheld, ungrouped unaffected")
}

// --- Apply: broken groups (fail-safe withholding) ---

func TestApplyBrokenGroupNoFoldersWithholdsAll(t *testing.T) {
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 9999}},
		map[string]string{"Movies": "media"},
		map[string][]string{}, // Jellyfin reported no folders for the library
		fd, &fakeStore{},
	)
	in := []arr.MediaItem{makeMovie("Movies", "A", "1", 30*gb)}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Empty(t, titles(out), "broken group must withhold all items to avoid over-queuing")
}

func TestApplyBrokenGroupAllUsageFailWithholdsAll(t *testing.T) {
	fd := &fakeDisk{
		partitions: []disk.PartitionStat{{Device: "/dev/sda1", Mountpoint: "/data"}},
		usageErr:   map[string]error{"/data/movies": fmt.Errorf("path does not exist")},
	}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 9999}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, &fakeStore{},
	)
	in := []arr.MediaItem{makeMovie("Movies", "A", "1", 30*gb)}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Empty(t, titles(out))
}

func TestApplyPartialUsageFailureWithholdsGroup(t *testing.T) {
	// When one of a group's filesystems fails to stat, the partial stats would look
	// like a much smaller pool and sweep far too aggressively. The group must be
	// treated as broken (all items withheld) until stats are complete again.
	fd := &fakeDisk{
		partitions: []disk.PartitionStat{
			{Device: "/dev/sda1", Mountpoint: "/data/a"},
			{Device: "/dev/sdb1", Mountpoint: "/data/b"},
		},
		usage:    map[string]disk.UsageStat{"/data/a": usageGB(100, 90, 10)},
		usageErr: map[string]error{"/data/b": fmt.Errorf("stale NFS handle")},
	}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 50}},
		map[string]string{"Movies": "media"},
		map[string][]string{"Movies": {"/data/a", "/data/b"}},
		fd, &fakeStore{},
	)
	in := []arr.MediaItem{makeMovie("Movies", "A", "1", 30*gb)}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Empty(t, titles(out), "incomplete disk stats must withhold the whole group")
}

// --- Apply: filesystem dedup across storage setups ---

func TestApplyMergerfsPoolCountedOnce(t *testing.T) {
	// Regression test mirroring the verified live deployment: four bind mounts of one
	// mergerfs pool share the same device string, so the pool must be counted exactly
	// once and only one Usage syscall made.
	const pool = "16tbsegate1:1TBWDC1:4tbred:8TB:8tbsegate1:8tbsegate2"
	fd := &fakeDisk{
		partitions: []disk.PartitionStat{
			{Device: pool, Mountpoint: "/data/tvshows", Fstype: "fuse.mergerfs"},
			{Device: pool, Mountpoint: "/data/anime", Fstype: "fuse.mergerfs"},
			{Device: pool, Mountpoint: "/data/movies", Fstype: "fuse.mergerfs"},
			{Device: pool, Mountpoint: "/data/dvr", Fstype: "fuse.mergerfs"},
		},
		usage: map[string]disk.UsageStat{
			"/data/movies":  usageGB(41000, 30000, 9000),
			"/data/tvshows": usageGB(41000, 30000, 9000),
			"/data/anime":   usageGB(41000, 30000, 9000),
		},
	}
	folders := map[string][]string{
		"Movies":   {"/data/movies"},
		"TV Shows": {"/data/tvshows"},
		"Anime":    {"/data/anime"},
	}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 9100}}, // need 100 GB
		map[string]string{"Movies": "media", "TV Shows": "media", "Anime": "media"},
		folders, fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "A", "1", 60*gb),
		makeTV("TV Shows", "B", "2", 60*gb),
		makeTV("Anime", "C", "3", 60*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	// Pool counted once: 9000 free, need 100 -> A (60), B (120 -> next check met), C withheld.
	assert.Equal(t, []string{"A", "B"}, titles(out))
	// Every path is statted (st_dev verification), but the pool is counted once.
	assert.Len(t, fd.usageCalls, 3)
}

func TestApplyZFSDatasetsCountPoolOnce(t *testing.T) {
	// Two ZFS datasets of one pool have distinct device names (tank/movies, tank/tv)
	// but share the pool's free space. Keying them by pool counts that space once:
	// the pool has 100 GB free (< the 150 GB target), so sweeping proceeds.
	fd := &fakeDisk{
		partitions: []disk.PartitionStat{
			{Device: "tank/movies", Mountpoint: "/data/movies", Fstype: "zfs"},
			{Device: "tank/tv", Mountpoint: "/data/tv", Fstype: "zfs"},
		},
		usage: map[string]disk.UsageStat{
			"/data/movies": usageGB(1000, 900, 100),
			"/data/tv":     usageGB(1000, 900, 100),
		},
	}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 150}},
		map[string]string{"Movies": "media", "TV": "media"},
		map[string][]string{"Movies": {"/data/movies"}, "TV": {"/data/tv"}},
		fd, &fakeStore{},
	)
	in := []arr.MediaItem{makeMovie("Movies", "A", "1", 30*gb)}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"A"}, titles(out), "shared pool free space must be counted once")
	assert.Len(t, fd.usageCalls, 2, "both datasets are statted so their used bytes can be summed")
}

func TestQuotaGroupDiskStatsZFSSumsDatasetUsedCountsPoolFreeOnce(t *testing.T) {
	// A dataset's statfs reports its OWN used bytes but the POOL's free space. The
	// aggregate must therefore sum used across datasets while counting free once —
	// otherwise a small dataset statted first would make percent_used wildly understate
	// real pool usage (and the isTargetMet clamp would cap gb_free projections).
	fd := &fakeDisk{
		usage: map[string]disk.UsageStat{
			"/data/movies": usageGB(200, 100, 100),   // small dataset: used 100 GB
			"/data/tv":     usageGB(2000, 1900, 100), // big dataset: used 1900 GB, same pool free
		},
	}
	f := &Filter{disk: fd}
	parts := []disk.PartitionStat{
		{Device: "tank/movies", Mountpoint: "/data/movies", Fstype: "zfs"},
		{Device: "tank/tv", Mountpoint: "/data/tv", Fstype: "zfs"},
	}
	stats, err := f.quotaGroupDiskStats(context.Background(), []string{"/data/movies", "/data/tv"}, parts)
	require.NoError(t, err)
	assert.Equal(t, uint64(2000*gb), stats.usedBytes, "used must sum across datasets")
	assert.Equal(t, uint64(100*gb), stats.freeBytes, "pool free must be counted once")
}

func TestApplyStatsFallbackWithoutPartitionTable(t *testing.T) {
	// No partition table (e.g. minimal container): dedup falls back to a Total+Free
	// stats key. Two paths with identical stats are treated as one filesystem.
	fd := &fakeDisk{
		partErr: fmt.Errorf("no /proc"),
		usage: map[string]disk.UsageStat{
			"/data/movies": usageGB(1000, 800, 200),
			"/data/tv":     usageGB(1000, 800, 200),
		},
	}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"Movies": "media", "TV": "media"},
		map[string][]string{"Movies": {"/data/movies"}, "TV": {"/data/tv"}},
		fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "A", "1", 30*gb),
		makeMovie("Movies", "B", "2", 30*gb),
		makeMovie("Movies", "C", "3", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	// Counted once: 200 free, need 50 -> A, B marked.
	assert.Equal(t, []string{"A", "B"}, titles(out))
	assert.Len(t, fd.usageCalls, 2, "fallback mode must still stat every path")
}

// --- Apply: multiple groups & ordering ---

func TestApplyMultipleGroupsIndependentBudgets(t *testing.T) {
	fd := &fakeDisk{
		partitions: []disk.PartitionStat{
			{Device: "/dev/sda1", Mountpoint: "/data/movies"},
			{Device: "/dev/sdb1", Mountpoint: "/data/rec"},
		},
		usage: map[string]disk.UsageStat{
			"/data/movies": usageGB(1000, 800, 200), // needs 50 for gb_free 250
			"/data/rec":    usageGB(1000, 400, 600), // target 500 already met
		},
	}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{
			"media": {GBFree: 250},
			"rec":   {GBFree: 500},
		},
		map[string]string{"Movies": "media", "Recordings": "rec"},
		map[string][]string{"Movies": {"/data/movies"}, "Recordings": {"/data/rec"}},
		fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeMovie("Recordings", "R1", "10", 30*gb),
		makeMovie("Movies", "A", "1", 30*gb),
		makeMovie("Recordings", "R2", "11", 30*gb),
		makeMovie("Movies", "B", "2", 30*gb),
		makeMovie("Movies", "C", "3", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B"}, titles(out), "media sweeps its budget, rec withholds all")
}

func TestApplyOrderLargestFirstReachesTargetWithFewerItems(t *testing.T) {
	// Need 50 GB. largest_first: the 50 GB item alone meets the target.
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250, Order: config.CleanupOrderLargestFirst}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "Small", "1", 20*gb),
		makeMovie("Movies", "Mid", "2", 30*gb),
		makeMovie("Movies", "Big", "3", 50*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"Big"}, titles(out))
}

func TestApplyOrderSmallestFirstSweepsSmallItems(t *testing.T) {
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250, Order: config.CleanupOrderSmallestFirst}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "Big", "3", 50*gb),
		makeMovie("Movies", "Small", "1", 20*gb),
		makeMovie("Movies", "Mid", "2", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"Small", "Mid"}, titles(out))
}

func TestApplyDefaultOrderPreservesEligibilityOrder(t *testing.T) {
	// The default order sweeps items exactly as they were reported eligible by the
	// preceding filters — no alphabetical re-sorting.
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "Zebra", "1", 30*gb), // arrived first
		makeMovie("Movies", "Apple", "2", 30*gb),
		makeMovie("Movies", "Mango", "3", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"Zebra", "Apple"}, titles(out))
}

func TestApplyOrderTitleSortsAlphabetically(t *testing.T) {
	// order: title provides the stable alphabetical behavior for users who prefer a
	// withheld set that never depends on upstream ordering.
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250, Order: config.CleanupOrderTitle}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "Zebra", "1", 30*gb),
		makeMovie("Movies", "Apple", "2", 30*gb),
		makeMovie("Movies", "Mango", "3", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"Apple", "Mango"}, titles(out))
}

// --- Apply: case-insensitivity (viper lowercases config keys) ---

func TestApplyCaseInsensitiveLibraryNames(t *testing.T) {
	// Config keys arrive lowercased from viper; Jellyfin reports display-cased names.
	fd := &fakeDisk{
		partitions: []disk.PartitionStat{{Device: "/dev/sda1", Mountpoint: "/data"}},
		usage:      map[string]disk.UsageStat{"/data/tv": usageGB(1000, 800, 200)},
	}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"tv shows": "media"},        // viper-lowercased config key
		map[string][]string{"TV Shows": {"/data/tv"}}, // Jellyfin-cased folder map
		fd, &fakeStore{},
	)
	in := []arr.MediaItem{
		makeTV("TV Shows", "A", "1", 30*gb), // Jellyfin-cased item library
		makeTV("TV Shows", "B", "2", 30*gb),
		makeTV("TV Shows", "C", "3", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B"}, titles(out), "case differences must not break grouping or folder lookup")
}

// --- Apply: cancellation ---

func TestApplyContextCancelled(t *testing.T) {
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, &fakeStore{},
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Apply(ctx, []arr.MediaItem{makeMovie("Movies", "A", "1", 30*gb)})
	assert.ErrorIs(t, err, context.Canceled)
}

// --- Multi-run lifecycle: mark -> pending -> delete -> re-measure ---

func TestApplyMultiRunLifecycle(t *testing.T) {
	// Simulates the engine's run cycle: items marked in run 1 become pending DB rows;
	// run 2 must not over-queue; after deletion lands, run 3 re-measures and tops up.
	groups := map[string]*config.QuotaGroupConfig{"media": {GBFree: 200}}
	libGroups := map[string]string{"Movies": "media"}

	pool := func(used, free int64) *fakeDisk { return singlePool(1000, used, free) }
	item := func(title, id string) arr.MediaItem { return makeMovie("Movies", title, id, 60*gb) }

	// Run 1: 100 GB free, need 100 GB. M1 (60), M2 (120 -> target projected met), rest withheld.
	store := &fakeStore{}
	f := newTestFilter(groups, libGroups, moviesOnData, pool(900, 100), store)
	out, err := f.Apply(context.Background(), []arr.MediaItem{
		item("M1", "1"), item("M2", "2"), item("M3", "3"), item("M4", "4"),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"M1", "M2"}, titles(out))

	// Run 2: M1, M2 now pending in the DB (the engine's database_filter keeps them out of
	// the candidate list); disk unchanged because nothing was deleted yet. The seed alone
	// must cover the gap -> no new items marked.
	store = &fakeStore{items: []database.Media{pending("Movies", 60), pending("Movies", 60)}}
	f = newTestFilter(groups, libGroups, moviesOnData, pool(900, 100), store)
	out, err = f.Apply(context.Background(), []arr.MediaItem{item("M3", "3"), item("M4", "4")})
	require.NoError(t, err)
	assert.Empty(t, titles(out), "pending seed must prevent over-queuing while deletions are still scheduled")

	// Run 3: deletion landed but freed only 60 GB of the projected 120 (the keep_seasons
	// phantom-bytes effect — see review finding #1). Pending now empty, disk re-measured
	// at 160 GB free: the filter self-corrects and marks M3 to cover the remaining gap.
	store = &fakeStore{}
	f = newTestFilter(groups, libGroups, moviesOnData, pool(840, 160), store)
	out, err = f.Apply(context.Background(), []arr.MediaItem{item("M3", "3"), item("M4", "4")})
	require.NoError(t, err)
	assert.Equal(t, []string{"M3"}, titles(out), "shortfall from phantom bytes is only recovered after re-measuring")
}

// --- quotaGroupDiskStats unit tests ---

func TestQuotaGroupDiskStatsDedupSameDevice(t *testing.T) {
	fd := &fakeDisk{
		usage: map[string]disk.UsageStat{
			"/data/movies": usageGB(1000, 800, 200),
			"/data/tv":     usageGB(1000, 800, 200),
		},
	}
	f := &Filter{disk: fd}
	parts := []disk.PartitionStat{
		{Device: "/dev/sda1", Mountpoint: "/data/movies"},
		{Device: "/dev/sda1", Mountpoint: "/data/tv"},
	}
	stats, err := f.quotaGroupDiskStats(context.Background(), []string{"/data/movies", "/data/tv"}, parts)
	require.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, uint64(800*gb), stats.usedBytes, "same device counted once")
	assert.Equal(t, uint64(200*gb), stats.freeBytes)
	assert.Len(t, fd.usageCalls, 2, "every path is statted for st_dev verification")
}

func TestQuotaGroupDiskStatsSumsDistinctFilesystems(t *testing.T) {
	fd := &fakeDisk{
		usage: map[string]disk.UsageStat{
			"/data/a": usageGB(1000, 800, 200),
			"/data/b": usageGB(500, 100, 400),
		},
	}
	f := &Filter{disk: fd}
	parts := []disk.PartitionStat{
		{Device: "/dev/sda1", Mountpoint: "/data/a"},
		{Device: "/dev/sdb1", Mountpoint: "/data/b"},
	}
	stats, err := f.quotaGroupDiskStats(context.Background(), []string{"/data/a", "/data/b"}, parts)
	require.NoError(t, err)
	assert.Equal(t, uint64(900*gb), stats.usedBytes)
	assert.Equal(t, uint64(600*gb), stats.freeBytes)
	assert.Equal(t, uint64(1500*gb), stats.totalBytes)
}

func TestQuotaGroupDiskStatsAllPathsFail(t *testing.T) {
	fd := &fakeDisk{
		usageErr: map[string]error{"/data/a": fmt.Errorf("boom")},
	}
	f := &Filter{disk: fd}
	stats, err := f.quotaGroupDiskStats(context.Background(), []string{"/data/a"}, nil)
	assert.Nil(t, stats)
	assert.Error(t, err)
}

func TestQuotaGroupDiskStatsPartialFailureReturnsError(t *testing.T) {
	// One failing path must surface an error so the caller marks the group broken:
	// partial stats look like a smaller pool and would over-sweep.
	fd := &fakeDisk{
		usage:    map[string]disk.UsageStat{"/data/a": usageGB(100, 90, 10)},
		usageErr: map[string]error{"/data/b": fmt.Errorf("boom")},
	}
	f := &Filter{disk: fd}
	parts := []disk.PartitionStat{
		{Device: "/dev/sda1", Mountpoint: "/data/a"},
		{Device: "/dev/sdb1", Mountpoint: "/data/b"},
	}
	stats, err := f.quotaGroupDiskStats(context.Background(), []string{"/data/a", "/data/b"}, parts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/data/b")
	assert.Nil(t, stats)
}

// --- FreeableSize (keep-mode-aware accounting) ---

func makeSeason(number int32, sizeGB int64, episodeFiles int32) sonarr.SeasonResource {
	stats := sonarr.SeasonStatisticsResource{}
	stats.SetSizeOnDisk(sizeGB * gb)
	stats.SetEpisodeFileCount(episodeFiles)
	s := sonarr.SeasonResource{}
	s.SetSeasonNumber(number)
	s.SetStatistics(stats)
	return s
}

func makeTVWithSeasons(lib, title, id string, totalGB int64, seasons ...sonarr.SeasonResource) arr.MediaItem {
	item := makeTV(lib, title, id, totalGB*gb)
	sr := item.SeriesResource
	sr.SetSeasons(seasons)
	item.SeriesResource = sr
	return item
}

func TestFreeableSize(t *testing.T) {
	movie := makeMovie("Movies", "M", "1", 50*gb)
	// 3 regular seasons of 30 GB (10 episodes each) + 10 GB of specials = 100 GB.
	series := makeTVWithSeasons("TV", "S", "2", 100,
		makeSeason(0, 10, 5),
		makeSeason(1, 30, 10),
		makeSeason(2, 30, 10),
		makeSeason(3, 30, 10),
	)

	tests := []struct {
		name      string
		item      arr.MediaItem
		mode      config.CleanupMode
		keepCount int
		want      int64
	}{
		{"movie ignores cleanup mode", movie, config.CleanupModeKeepSeasons, 1, 50 * gb},
		{"tv mode all frees everything", series, config.CleanupModeAll, 0, 100 * gb},
		{"keep_seasons subtracts kept season and specials", series, config.CleanupModeKeepSeasons, 1, 60 * gb},
		{"keep_seasons keeping two seasons", series, config.CleanupModeKeepSeasons, 2, 30 * gb},
		{"keep_seasons keeping more than exists frees nothing", series, config.CleanupModeKeepSeasons, 5, 0},
		// keep_episodes 15: all of season 1 (10 files, 30 GB) + half of season 2
		// (5/10 files, prorated 15 GB) + specials 10 GB kept -> 100-55 = 45 GB freed.
		{"keep_episodes prorates within a season", series, config.CleanupModeKeepEpisodes, 15, 45 * gb},
		{"tv without season stats frees full size", makeTV("TV", "S2", "3", 80*gb), config.CleanupModeKeepSeasons, 1, 80 * gb},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FreeableSize(tt.item, tt.mode, tt.keepCount))
		})
	}
}

func TestApplyUsesFreeableSizeForKeepModes(t *testing.T) {
	// With keep_seasons/keep_count 1, only the freeable part of each series counts
	// toward the budget, so more items are marked than full-size accounting would allow.
	// Need 50 GB; each series is 60 GB total but only 30 GB freeable (S1 of 2 kept).
	fd := singlePool(1000, 800, 200)
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"TV": "media"},
		map[string][]string{"TV": {"/data/movies"}},
		fd, &fakeStore{},
	)
	f.cfg.CleanupMode = config.CleanupModeKeepSeasons
	f.cfg.KeepCount = 1
	in := []arr.MediaItem{
		makeTVWithSeasons("TV", "A", "1", 60, makeSeason(1, 30, 10), makeSeason(2, 30, 10)),
		makeTVWithSeasons("TV", "B", "2", 60, makeSeason(1, 30, 10), makeSeason(2, 30, 10)),
		makeTVWithSeasons("TV", "C", "3", 60, makeSeason(1, 30, 10), makeSeason(2, 30, 10)),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	// 30 GB freeable each: A (acc 30), B (acc 60 -> next check met), C withheld.
	// Full-size accounting would have stopped after A alone.
	assert.Equal(t, []string{"A", "B"}, titles(out))
}

// --- seed attribution & dedup ---

func TestApplySeedUsesStoredQuotaGroupAfterLibraryRename(t *testing.T) {
	// A pending item marked under a library that was since renamed in Jellyfin no
	// longer resolves via library name, but the stored QuotaGroup keeps its bytes in
	// the budget. Need 50 GB; 40 GB pending under the old name -> only one new mark.
	fd := singlePool(1000, 800, 200)
	store := &fakeStore{items: []database.Media{
		{LibraryName: "Old Movies", QuotaGroup: "media", FileSize: 40 * gb, JellyfinID: "old1"},
	}}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, store,
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "A", "1", 30*gb),
		makeMovie("Movies", "B", "2", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"A"}, titles(out))
}

func gbPtr(v int64) *int64 {
	b := v * gb
	return &b
}

func TestApplySeedPrefersFreeableSizeAndDedupsByJellyfinID(t *testing.T) {
	// FreeableSize (30) wins over FileSize (60); the duplicate row for the same
	// Jellyfin item (e.g. after an arr re-add changed the ArrID) is counted once.
	// Seed 30 + need 50 -> one more 30 GB item fits.
	fd := singlePool(1000, 800, 200)
	store := &fakeStore{items: []database.Media{
		{LibraryName: "Movies", FileSize: 60 * gb, FreeableSize: gbPtr(30), JellyfinID: "x"},
		{LibraryName: "Movies", FileSize: 60 * gb, FreeableSize: gbPtr(30), JellyfinID: "x"},
	}}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, store,
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "A", "1", 30*gb),
		makeMovie("Movies", "B", "2", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"A"}, titles(out))
}

func TestApplySeedGenuineZeroFreeableSeedsNothing(t *testing.T) {
	// A stored FreeableSize of 0 is a real estimate ("deletion frees nothing"), not
	// "unknown": it must NOT fall back to FileSize, otherwise unfreeable pending series
	// inflate the budget and withhold genuinely freeable items.
	fd := singlePool(1000, 800, 200)
	store := &fakeStore{items: []database.Media{
		{LibraryName: "Movies", FileSize: 200 * gb, FreeableSize: gbPtr(0), JellyfinID: "z"},
	}}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, store,
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "A", "1", 30*gb),
		makeMovie("Movies", "B", "2", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	// Seed is 0 (not 200), so the full 50 GB gap is open: A and B both marked.
	assert.Equal(t, []string{"A", "B"}, titles(out))
}

func TestApplyExcludesItemsAlreadyPendingUnderNewArrID(t *testing.T) {
	// An item re-added in Sonarr/Radarr gets a new arr ID, so the arr-ID-based database
	// filter lets it through — but its Jellyfin ID is still pending. Marking it again
	// would double-charge the budget and create a duplicate row, so it is excluded.
	fd := singlePool(1000, 800, 200)
	store := &fakeStore{items: []database.Media{
		{LibraryName: "Movies", FileSize: 30 * gb, JellyfinID: "dup"},
	}}
	f := newTestFilter(
		map[string]*config.QuotaGroupConfig{"media": {GBFree: 250}},
		map[string]string{"Movies": "media"},
		moviesOnData, fd, store,
	)
	in := []arr.MediaItem{
		makeMovie("Movies", "ReAdded", "dup", 30*gb), // same JellyfinID as the pending row
		makeMovie("Movies", "Fresh", "new", 30*gb),
	}
	out, err := f.Apply(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"Fresh"}, titles(out))
}

// --- LibraryFoldersMap ---

func TestLibraryFoldersMapGetCaseInsensitive(t *testing.T) {
	lf := NewLibraryFoldersMap()
	lf.Set(map[string][]string{"TV Shows": {"/data/tv"}})
	assert.Equal(t, []string{"/data/tv"}, lf.get("TV Shows"))
	assert.Equal(t, []string{"/data/tv"}, lf.get("tv shows"))
	assert.Nil(t, lf.get("Movies"))
}

func TestLibraryFoldersMapAllReturnsIsolatedCopy(t *testing.T) {
	lf := NewLibraryFoldersMap()
	lf.Set(map[string][]string{"Movies": {"/data/movies"}})
	snapshot := lf.All()
	snapshot["Movies"][0] = "/mutated"
	snapshot["Injected"] = []string{"/x"}
	assert.Equal(t, []string{"/data/movies"}, lf.get("Movies"))
	assert.Nil(t, lf.get("Injected"))
}

func TestLibraryFoldersMapConcurrentAccess(t *testing.T) {
	// Run with -race: Set races against get/All if the lock is ever dropped.
	lf := NewLibraryFoldersMap()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				lf.Set(map[string][]string{fmt.Sprintf("Lib%d", n): {"/data"}})
			}
		}(i)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				lf.get(fmt.Sprintf("lib%d", n))
				lf.All()
			}
		}(i)
	}
	wg.Wait()
}
