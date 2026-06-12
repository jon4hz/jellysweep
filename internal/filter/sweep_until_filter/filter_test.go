package sweepuntilfilter

import (
	"testing"

	radarr "github.com/devopsarr/radarr-go/radarr"
	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gb = int64(1_000_000_000)

// --- fixtures ---

func makeMovie(lib, title, id string, size int64) arr.MediaItem {
	stats := radarr.MovieStatisticsResource{}
	stats.SetSizeOnDisk(size)
	mr := radarr.MovieResource{}
	mr.SetStatistics(stats)
	return arr.MediaItem{
		JellyfinID:    id,
		LibraryName:   lib,
		Title:         title,
		MediaType:     models.MediaTypeMovie,
		MovieResource: mr,
	}
}

func makeTV(lib, title, id string, totalSize int64) arr.MediaItem {
	ss := sonarr.SeriesStatisticsResource{}
	ss.SetSizeOnDisk(totalSize)
	sr := sonarr.SeriesResource{}
	sr.SetStatistics(ss)
	return arr.MediaItem{
		JellyfinID:     id,
		LibraryName:    lib,
		Title:          title,
		MediaType:      models.MediaTypeTV,
		SeriesResource: sr,
	}
}

// --- itemSizeOnDisk ---

func TestItemSizeOnDisk(t *testing.T) {
	tests := []struct {
		name string
		item arr.MediaItem
		want int64
	}{
		{"movie returns size on disk", makeMovie("Movies", "M", "1", 5*gb), 5 * gb},
		{"tv returns series size on disk", makeTV("TV", "S", "2", 12*gb), 12 * gb},
		{"unknown media type returns 0", arr.MediaItem{LibraryName: "x", Title: "y"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, itemSizeOnDisk(tt.item))
		})
	}
}

// --- isTargetMet ---

func TestIsTargetMet(t *testing.T) {
	tests := []struct {
		name        string
		stats       sweepDiskStats
		cfg         config.QuotaGroupConfig
		accumulated int64
		want        bool
	}{
		{
			name:        "gb_free met after accumulation",
			stats:       sweepDiskStats{usedBytes: uint64(600 * gb), freeBytes: uint64(400 * gb)},
			cfg:         config.QuotaGroupConfig{GBFree: 500},
			accumulated: 150 * gb, // free 400 + 150 = 550 >= 500
			want:        true,
		},
		{
			name:        "gb_free not met",
			stats:       sweepDiskStats{usedBytes: uint64(600 * gb), freeBytes: uint64(400 * gb)},
			cfg:         config.QuotaGroupConfig{GBFree: 500},
			accumulated: 50 * gb, // free 450 < 500
			want:        false,
		},
		{
			name:        "percent_used met after accumulation",
			stats:       sweepDiskStats{usedBytes: uint64(800 * gb), freeBytes: uint64(200 * gb)},
			cfg:         config.QuotaGroupConfig{PercentUsed: 70},
			accumulated: 150 * gb, // newUsed 650/1000 = 65% <= 70
			want:        true,
		},
		{
			name:        "percent_used not met",
			stats:       sweepDiskStats{usedBytes: uint64(800 * gb), freeBytes: uint64(200 * gb)},
			cfg:         config.QuotaGroupConfig{PercentUsed: 70},
			accumulated: 50 * gb, // newUsed 750/1000 = 75% > 70
			want:        false,
		},
		{
			name:        "accumulated clamped to used: cannot free more than used",
			stats:       sweepDiskStats{usedBytes: uint64(100 * gb), freeBytes: 0},
			cfg:         config.QuotaGroupConfig{GBFree: 150},
			accumulated: 500 * gb, // clamp to 100; free 0+100 = 100 < 150
			want:        false,
		},
		{
			name:        "accumulated clamp still meets reachable gb_free",
			stats:       sweepDiskStats{usedBytes: uint64(100 * gb), freeBytes: 0},
			cfg:         config.QuotaGroupConfig{GBFree: 90},
			accumulated: 500 * gb, // clamp to 100; free 100 >= 90
			want:        true,
		},
		{
			name:        "both conditions set, gb_free satisfied first",
			stats:       sweepDiskStats{usedBytes: uint64(800 * gb), freeBytes: uint64(200 * gb)},
			cfg:         config.QuotaGroupConfig{PercentUsed: 10, GBFree: 300},
			accumulated: 150 * gb, // free 350 >= 300 -> true even though percent not met
			want:        true,
		},
		{
			name:        "zero capacity never meets percent target",
			stats:       sweepDiskStats{usedBytes: 0, freeBytes: 0},
			cfg:         config.QuotaGroupConfig{PercentUsed: 50},
			accumulated: 0,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stats.isTargetMet(&tt.cfg, tt.accumulated)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSweepDiskStatsHelpers(t *testing.T) {
	s := sweepDiskStats{usedBytes: uint64(750 * gb), freeBytes: uint64(250 * gb)}
	assert.InDelta(t, 75.0, s.usedPercent(), 0.0001)
	assert.InDelta(t, 250.0, s.freeGB(), 0.0001)

	empty := sweepDiskStats{}
	assert.Equal(t, 0.0, empty.usedPercent())
}

// --- resolveMountKey ---

func TestResolveMountKey(t *testing.T) {
	partitions := []disk.PartitionStat{
		{Device: "/dev/sda1", Mountpoint: "/"},
		{Device: "/dev/sdb1", Mountpoint: "/mnt/media"},
		{Device: "/dev/sdc1", Mountpoint: "/foo"},
		{Device: "overlay", Mountpoint: "/var/lib/docker"},
	}

	zfsPartitions := []disk.PartitionStat{
		{Device: "tank/movies", Mountpoint: "/data/movies", Fstype: "zfs"},
		{Device: "tank/tv", Mountpoint: "/data/tv", Fstype: "zfs"},
		{Device: "vault", Mountpoint: "/data/vault", Fstype: "zfs"},
	}
	// Overmount: autofs placeholder and the real NFS mount share a mountpoint; the
	// LAST entry in the table is the effective one and must win.
	overmounted := []disk.PartitionStat{
		{Device: "systemd-1", Mountpoint: "/mnt/nas", Fstype: "autofs"},
		{Device: "server:/export", Mountpoint: "/mnt/nas", Fstype: "nfs4"},
	}

	tests := []struct {
		name string
		path string
		part []disk.PartitionStat
		want string
	}{
		{"longest mount wins", "/mnt/media/movies", partitions, "dev:/dev/sdb1"},
		{"exact mount path", "/mnt/media", partitions, "dev:/dev/sdb1"},
		{"falls back to root", "/movies", partitions, "dev:/dev/sda1"},
		{"prefix boundary: /foobar must not match /foo", "/foobar/x", partitions, "dev:/dev/sda1"},
		{"virtual device falls back to mount", "/var/lib/docker/data", partitions, "mount:/var/lib/docker"},
		{"no partitions yields unmatched", "/data/movies", nil, "unmatched:/data/movies"},
		{"zfs datasets keyed by pool", "/data/movies/film", zfsPartitions, "dev:zfs:tank"},
		{"zfs sibling dataset shares pool key", "/data/tv/show", zfsPartitions, "dev:zfs:tank"},
		{"zfs pool-root dataset", "/data/vault/x", zfsPartitions, "dev:zfs:vault"},
		{"overmounted mountpoint: last entry wins", "/mnt/nas/media", overmounted, "dev:server:/export"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveMountKey(tt.path, tt.part))
		})
	}
}

// --- resolveLibraryGroup ---

func TestResolveLibraryGroup(t *testing.T) {
	libraryGroup := map[string]string{
		"movies":   "media",
		"TV Shows": "media",
	}
	assert.Equal(t, "media", resolveLibraryGroup("movies", libraryGroup))
	assert.Equal(t, "media", resolveLibraryGroup("Movies", libraryGroup)) // case-insensitive fallback
	assert.Equal(t, "media", resolveLibraryGroup("tv shows", libraryGroup))
	assert.Equal(t, "", resolveLibraryGroup("Recordings", libraryGroup))
}

// --- applyGroupOrdering ---

func TestApplyGroupOrdering(t *testing.T) {
	// positions 0 and 3 are ungrouped and must never move.
	build := func() []itemFreed {
		return []itemFreed{
			{item: arr.MediaItem{LibraryName: "other", Title: "Z", JellyfinID: "z"}, freed: 999},
			{item: arr.MediaItem{LibraryName: "movies", Title: "B", JellyfinID: "b"}, group: "media", freed: 10},
			{item: arr.MediaItem{LibraryName: "movies", Title: "A", JellyfinID: "a"}, group: "media", freed: 30},
			{item: arr.MediaItem{LibraryName: "other", Title: "Y", JellyfinID: "y"}, freed: 5},
			{item: arr.MediaItem{LibraryName: "movies", Title: "C", JellyfinID: "c"}, group: "media", freed: 20},
		}
	}

	titlesAt := func(entries []itemFreed, idx ...int) []string {
		out := make([]string, 0, len(idx))
		for _, i := range idx {
			out = append(out, entries[i].item.Title)
		}
		return out
	}

	tests := []struct {
		name      string
		order     config.CleanupOrder
		wantGroup []string // titles at group slots 1,2,4 after ordering
	}{
		{"largest_first descending", config.CleanupOrderLargestFirst, []string{"A", "C", "B"}},  // 30,20,10
		{"smallest_first ascending", config.CleanupOrderSmallestFirst, []string{"B", "C", "A"}}, // 10,20,30
		{"title sorts alphabetically", config.CleanupOrderTitle, []string{"A", "B", "C"}},
		{"default preserves arrival order", config.CleanupOrderDefault, []string{"B", "A", "C"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := build()
			groups := map[string]*groupQuotaState{
				"media": {cfg: &config.QuotaGroupConfig{Order: tt.order}},
			}
			applyGroupOrdering(entries, groups)

			// Ungrouped slots untouched.
			assert.Equal(t, "Z", entries[0].item.Title)
			assert.Equal(t, "Y", entries[3].item.Title)
			// Grouped slots reordered.
			assert.Equal(t, tt.wantGroup, titlesAt(entries, 1, 2, 4))
		})
	}
}

func TestApplyGroupOrderingSkipsBrokenGroups(t *testing.T) {
	entries := []itemFreed{
		{item: arr.MediaItem{LibraryName: "movies", Title: "B"}, group: "media", freed: 10},
		{item: arr.MediaItem{LibraryName: "movies", Title: "A"}, group: "media", freed: 30},
	}
	groups := map[string]*groupQuotaState{
		"media": {cfg: &config.QuotaGroupConfig{Order: config.CleanupOrderLargestFirst}, broken: true},
	}
	applyGroupOrdering(entries, groups)
	// Broken group is left untouched (would otherwise be reordered to A,B).
	assert.Equal(t, "B", entries[0].item.Title)
	assert.Equal(t, "A", entries[1].item.Title)
}

func TestQuotaGroupGetOrder(t *testing.T) {
	require.Equal(t, config.CleanupOrderDefault, (&config.QuotaGroupConfig{}).GetOrder())
	require.Equal(t, config.CleanupOrderTitle, (&config.QuotaGroupConfig{Order: config.CleanupOrderTitle}).GetOrder())
	require.Equal(t, config.CleanupOrderSmallestFirst, (&config.QuotaGroupConfig{Order: config.CleanupOrderSmallestFirst}).GetOrder())
}
