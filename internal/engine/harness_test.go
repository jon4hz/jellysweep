package engine

// The lifecycle test harness wires a real Engine (via newEngine) with a real
// in-memory sqlite database and hand-written fakes for all external services.
// Elapsed time is simulated by backdating timestamps in the database, so the
// production time.Now() comparisons and SQL filters run unmodified.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/devopsarr/radarr-go/radarr"
	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/cache"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/database/databasetest"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/filter"
	agefilter "github.com/jon4hz/jellysweep/internal/filter/age_filter"
	databasefilter "github.com/jon4hz/jellysweep/internal/filter/database_filter"
	moviereleasefilter "github.com/jon4hz/jellysweep/internal/filter/movie_release_filter"
	seriesfilter "github.com/jon4hz/jellysweep/internal/filter/series_filter"
	sizefilter "github.com/jon4hz/jellysweep/internal/filter/size_filter"
	streamfilter "github.com/jon4hz/jellysweep/internal/filter/stream_filter"
	tagsfilter "github.com/jon4hz/jellysweep/internal/filter/tags_filter"
	"github.com/jon4hz/jellysweep/internal/policy"
	"github.com/samber/lo"
	jellyfinAPI "github.com/sj14/jellyfin-go/api"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type harness struct {
	t      *testing.T
	cfg    *config.Config
	db     *database.Client
	gdb    *gorm.DB
	sonarr *fakeArr
	radarr *fakeArr
	stats  *fakeStats
	jf     *fakeJellyfin
	e      *Engine
	user   *database.User
	admin  *database.User

	// usageFn is consulted by the disk usage policy; swappable per test.
	usageFn policy.UsageFunc

	initialDBMigration bool
	nextArrID          int32
}

type mediaRef struct {
	jellyfinID string
	arrID      int32
	mediaType  database.MediaType
	title      string
}

type harnessOpt func(*harness)

func withDryRun() harnessOpt {
	return func(h *harness) { h.cfg.DryRun = true }
}

func withDiskThresholds(library string, usagePercent float64, maxDelayDays int) harnessOpt {
	return func(h *harness) {
		h.cfg.Libraries[library].DiskUsageThresholds = []config.DiskUsageThreshold{
			{UsagePercent: usagePercent, MaxCleanupDelay: maxDelayDays},
		}
	}
}

func withNilStats() harnessOpt {
	return func(h *harness) { h.stats = nil }
}

func withTagMigration() harnessOpt {
	return func(h *harness) { h.initialDBMigration = true }
}

func newHarness(t *testing.T, opts ...harnessOpt) *harness {
	t.Helper()

	db, gdb := databasetest.New(t)
	h := &harness{
		t: t,
		cfg: &config.Config{
			// DryRun defaults to true in production; lifecycle tests exercise
			// real deletions, so it is explicitly disabled here.
			DryRun:          false,
			CleanupSchedule: "0 */12 * * *",
			Sonarr:          &config.SonarrConfig{},
			Radarr:          &config.RadarrConfig{},
			Libraries: map[string]*config.CleanupConfig{
				"Movies": {Enabled: true},
				"TV":     {Enabled: true},
			},
		},
		db:     db,
		gdb:    gdb,
		sonarr: newFakeArr(),
		radarr: newFakeArr(),
		stats:  newFakeStats(),
		jf:     newFakeJellyfin(),
		usageFn: func(context.Context, string) (float64, error) {
			return 0, nil
		},
	}

	for _, opt := range opts {
		opt(h)
	}

	// Mirror the production filter chain from New, minus the tunarr filter.
	filterList := []filter.Filterer{
		databasefilter.New(db),
		seriesfilter.New(h.cfg),
		tagsfilter.New(h.cfg),
		sizefilter.New(h.cfg),
		moviereleasefilter.New(h.cfg),
		agefilter.New(h.cfg, db, h.sonarr, h.radarr),
	}
	if h.stats != nil {
		filterList = append(filterList, streamfilter.New(h.cfg, h.stats))
	}

	engineCache, err := cache.NewEngineCache(&config.CacheConfig{Type: config.CacheTypeMemory})
	require.NoError(t, err)

	deps := engineDeps{
		jellyfin: h.jf,
		sonarr:   h.sonarr,
		radarr:   h.radarr,
		filters:  filter.New(filterList...),
		policyFactory: func(libraryFoldersMap map[string][]string) []policy.Policy {
			return []policy.Policy{
				policy.NewDefaultDelete(h.cfg),
				policy.NewDiskUsageDelete(h.cfg, libraryFoldersMap, policy.WithUsageFunc(
					func(ctx context.Context, path string) (float64, error) {
						return h.usageFn(ctx, path)
					},
				)),
			}
		},
		engineCache:   engineCache,
		imageCacheDir: t.TempDir(),
	}
	if h.stats != nil {
		deps.stats = h.stats
	}

	h.e, err = newEngine(h.cfg, db, h.initialDBMigration, deps)
	require.NoError(t, err)

	h.user, err = db.CreateUser(t.Context(), "alice")
	require.NoError(t, err)
	h.admin, err = db.CreateUser(t.Context(), "admin")
	require.NoError(t, err)

	return h
}

type mediaOpt func(*arr.MediaItem)

func withArrTags(tags ...string) mediaOpt {
	return func(item *arr.MediaItem) { item.Tags = tags }
}

func (h *harness) addMovie(title string, opts ...mediaOpt) *mediaRef {
	h.t.Helper()
	h.nextArrID++
	arrID := h.nextArrID
	jellyfinID := fmt.Sprintf("jf-movie-%d", arrID)

	movie := radarr.MovieResource{}
	movie.SetId(arrID)
	movie.SetTitle(title)
	movie.SetTmdbId(10000 + arrID)
	movie.SetYear(2020)
	movie.SetSizeOnDisk(5 << 30)

	item := arr.MediaItem{
		JellyfinID:    jellyfinID,
		LibraryName:   "Movies",
		Title:         title,
		TmdbId:        10000 + arrID,
		MediaType:     models.MediaTypeMovie,
		MovieResource: movie,
	}
	for _, opt := range opts {
		opt(&item)
	}

	h.radarr.items[arrID] = item
	h.addJellyfinItem(jellyfinID, title, "Movies")

	return &mediaRef{jellyfinID: jellyfinID, arrID: arrID, mediaType: database.MediaTypeMovie, title: title}
}

func (h *harness) addSeries(title string, opts ...mediaOpt) *mediaRef {
	h.t.Helper()
	h.nextArrID++
	arrID := h.nextArrID
	jellyfinID := fmt.Sprintf("jf-series-%d", arrID)

	series := sonarr.SeriesResource{}
	series.SetId(arrID)
	series.SetTitle(title)
	series.SetTvdbId(20000 + arrID)
	series.SetYear(2020)
	seriesStats := sonarr.SeriesStatisticsResource{}
	seriesStats.SetSizeOnDisk(20 << 30)
	series.SetStatistics(seriesStats)

	item := arr.MediaItem{
		JellyfinID:     jellyfinID,
		LibraryName:    "TV",
		Title:          title,
		TvdbId:         20000 + arrID,
		MediaType:      models.MediaTypeTV,
		SeriesResource: series,
	}
	for _, opt := range opts {
		opt(&item)
	}

	h.sonarr.items[arrID] = item
	h.addJellyfinItem(jellyfinID, title, "TV")

	return &mediaRef{jellyfinID: jellyfinID, arrID: arrID, mediaType: database.MediaTypeTV, title: title}
}

func (h *harness) addJellyfinItem(jellyfinID, title, library string) {
	h.jf.mu.Lock()
	defer h.jf.mu.Unlock()
	h.jf.items = append(h.jf.items, arr.JellyfinItem{
		BaseItemDto:       jellyfinAPI.BaseItemDto{Id: lo.ToPtr(jellyfinID), Name: *jellyfinAPI.NewNullableString(lo.ToPtr(title))},
		ParentLibraryName: library,
	})
}

// --- actions ---

func (h *harness) runCleanup() error {
	return h.e.runCleanupJob(h.t.Context())
}

func (h *harness) mustRunCleanup() {
	h.t.Helper()
	require.NoError(h.t, h.runCleanup())
}

// elapseDeleteDelay backdates the live row's default delete date, simulating
// the cleanup delay having passed.
func (h *harness) elapseDeleteDelay(m *mediaRef) {
	h.t.Helper()
	res := h.gdb.Model(&database.Media{}).
		Where("jellyfin_id = ?", m.jellyfinID).
		Update("default_delete_at", time.Now().Add(-time.Hour))
	require.NoError(h.t, res.Error)
	require.NotZero(h.t, res.RowsAffected, "no live row to backdate for %s", m.title)
}

// elapseProtection backdates the live row's protection expiry.
func (h *harness) elapseProtection(m *mediaRef) {
	h.t.Helper()
	res := h.gdb.Model(&database.Media{}).
		Where("jellyfin_id = ? AND protected_until IS NOT NULL", m.jellyfinID).
		Update("protected_until", time.Now().Add(-time.Hour))
	require.NoError(h.t, res.Error)
	require.NotZero(h.t, res.RowsAffected, "no protected row to backdate for %s", m.title)
}

// elapseDiskUsageDate backdates the disk usage policy delete dates.
func (h *harness) elapseDiskUsageDate(m *mediaRef) {
	h.t.Helper()
	row := h.assertMarked(m)
	res := h.gdb.Model(&database.DiskUsageDeletePolicy{}).
		Where("media_id = ?", row.ID).
		Update("delete_date", time.Now().Add(-time.Hour))
	require.NoError(h.t, res.Error)
	require.NotZero(h.t, res.RowsAffected, "no disk usage policy to backdate for %s", m.title)
}

func (h *harness) requestKeep(m *mediaRef) (bool, error) {
	h.t.Helper()
	row := h.assertMarked(m)
	return h.e.RequestKeepMedia(h.t.Context(), row.ID, h.user.ID, h.user.Username)
}

func (h *harness) decideKeep(m *mediaRef, accept bool) error {
	h.t.Helper()
	row := h.assertMarked(m)
	return h.e.HandleKeepRequest(h.t.Context(), h.admin.ID, row.ID, accept)
}

func (h *harness) stream(m *mediaRef, when time.Time) {
	h.stats.setLastPlayed(m.jellyfinID, when)
}

func (h *harness) vanishFromJellyfin(m *mediaRef) {
	h.jf.mu.Lock()
	defer h.jf.mu.Unlock()
	for i, item := range h.jf.items {
		if item.GetId() == m.jellyfinID {
			h.jf.items = append(h.jf.items[:i], h.jf.items[i+1:]...)
			return
		}
	}
}

// --- assertions ---

// liveRow returns the current live database row for the item, or nil.
func (h *harness) liveRow(m *mediaRef) *database.Media {
	h.t.Helper()
	var media database.Media
	err := h.gdb.Preload("DiskUsageDeletePolicies").Preload("Request").
		Where("jellyfin_id = ?", m.jellyfinID).First(&media).Error
	if err != nil {
		require.ErrorIs(h.t, err, gorm.ErrRecordNotFound)
		return nil
	}
	return &media
}

func (h *harness) assertMarked(m *mediaRef) *database.Media {
	h.t.Helper()
	row := h.liveRow(m)
	require.NotNil(h.t, row, "%s must be marked for deletion (live row expected)", m.title)
	return row
}

func (h *harness) assertNotMarked(m *mediaRef) {
	h.t.Helper()
	require.Nil(h.t, h.liveRow(m), "%s must not have a live database row", m.title)
}

// assertTombstone checks the most recent soft-deleted row for the item.
func (h *harness) assertTombstone(m *mediaRef, reason database.DBDeleteReason) {
	h.t.Helper()
	var tombstone database.Media
	err := h.gdb.Unscoped().
		Where("jellyfin_id = ? AND deleted_at IS NOT NULL", m.jellyfinID).
		Order("id DESC").First(&tombstone).Error
	require.NoError(h.t, err, "expected a tombstone for %s", m.title)
	require.Equal(h.t, reason, tombstone.DBDeleteReason)
}

func (h *harness) arrFor(m *mediaRef) *fakeArr {
	if m.mediaType == database.MediaTypeTV {
		return h.sonarr
	}
	return h.radarr
}

func (h *harness) assertDeletedEverywhere(m *mediaRef) {
	h.t.Helper()
	require.True(h.t, h.arrFor(m).hasDeleted(m.arrID), "%s must be deleted in the arr", m.title)
	require.True(h.t, h.jf.hasRemoved(m.jellyfinID), "%s must be removed from jellyfin", m.title)
	h.assertTombstone(m, database.DBDeleteReasonDefault)
	requireHistory(h.t, h.db, m.jellyfinID, database.HistoryEventDeleted)
}

func (h *harness) assertNotDeletedAnywhere(m *mediaRef) {
	h.t.Helper()
	require.False(h.t, h.arrFor(m).hasDeleted(m.arrID), "%s must not be deleted in the arr", m.title)
	require.False(h.t, h.jf.hasRemoved(m.jellyfinID), "%s must not be removed from jellyfin", m.title)
}

func (h *harness) assertHistory(m *mediaRef, events ...database.HistoryEventType) {
	h.t.Helper()
	requireHistory(h.t, h.db, m.jellyfinID, events...)
}
