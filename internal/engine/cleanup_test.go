package engine

import (
	"errors"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/database/databasetest"
	"github.com/jon4hz/jellysweep/internal/policy"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type cleanupFixture struct {
	e      *Engine
	db     *database.Client
	gdb    *gorm.DB
	sonarr *fakeArr
	radarr *fakeArr
	jf     *fakeJellyfin
}

func newCleanupFixture(t *testing.T) *cleanupFixture {
	t.Helper()
	cfg := testConfig()
	cfg.Sonarr = &config.SonarrConfig{}
	cfg.Radarr = &config.RadarrConfig{}

	db, gdb := databasetest.New(t)
	policyEngine := policy.NewEngine()
	policyEngine.SetPolicies(policy.NewDefaultDelete(cfg))

	f := &cleanupFixture{
		db:     db,
		gdb:    gdb,
		sonarr: newFakeArr(),
		radarr: newFakeArr(),
		jf:     newFakeJellyfin(),
	}
	f.e = &Engine{
		cfg:      cfg,
		db:       db,
		policy:   policyEngine,
		sonarr:   f.sonarr,
		radarr:   f.radarr,
		jellyfin: f.jf,
	}
	return f
}

func requireTombstone(t *testing.T, gdb *gorm.DB, id uint, reason database.DBDeleteReason) {
	t.Helper()
	var tombstone database.Media
	require.NoError(t, gdb.Unscoped().First(&tombstone, id).Error)
	require.True(t, tombstone.DeletedAt.Valid, "expected a soft-deleted row")
	require.Equal(t, reason, tombstone.DBDeleteReason)
}

func (f *cleanupFixture) requireLive(t *testing.T, id uint) {
	t.Helper()
	var media database.Media
	require.NoError(t, f.gdb.First(&media, id).Error, "expected the row to still be live")
}

func requireHistory(t *testing.T, db *database.Client, jellyfinID string, want ...database.HistoryEventType) {
	t.Helper()
	events, err := db.GetHistoryEventsByJellyfinID(t.Context(), jellyfinID)
	require.NoError(t, err)
	got := make([]database.HistoryEventType, 0, len(events))
	for _, e := range events {
		got = append(got, e.EventType)
	}
	for _, w := range want {
		require.Contains(t, got, w)
	}
}

func TestCleanupMediaDeletesDueMovie(t *testing.T) {
	f := newCleanupFixture(t)
	due := createMedia(t, f.db, database.Media{
		Title:           "Due Movie",
		ArrID:           1,
		DefaultDeleteAt: time.Now().Add(-time.Hour),
	})
	notDue := createMedia(t, f.db, database.Media{
		Title:           "Fresh Movie",
		ArrID:           2,
		DefaultDeleteAt: time.Now().Add(time.Hour),
	})

	require.NoError(t, f.e.cleanupMedia(t.Context()))

	require.True(t, f.radarr.hasDeleted(1), "due movie must be deleted in radarr")
	require.True(t, f.jf.hasRemoved(due.JellyfinID), "due movie must be removed from jellyfin")
	requireTombstone(t, f.gdb, due.ID, database.DBDeleteReasonDefault)
	requireHistory(t, f.db, due.JellyfinID, database.HistoryEventDeleted)

	require.False(t, f.radarr.hasDeleted(2))
	f.requireLive(t, notDue.ID)
}

func TestCleanupMediaDeletesDueSeries(t *testing.T) {
	f := newCleanupFixture(t)
	due := createMedia(t, f.db, database.Media{
		Title:           "Due Show",
		ArrID:           7,
		MediaType:       database.MediaTypeTV,
		LibraryName:     "TV",
		DefaultDeleteAt: time.Now().Add(-time.Hour),
	})

	require.NoError(t, f.e.cleanupMedia(t.Context()))

	require.True(t, f.sonarr.hasDeleted(7))
	require.True(t, f.jf.hasRemoved(due.JellyfinID))
	requireTombstone(t, f.gdb, due.ID, database.DBDeleteReasonDefault)
}

func TestCleanupMediaDryRunDeletesNothing(t *testing.T) {
	f := newCleanupFixture(t)
	f.e.cfg.DryRun = true
	due := createMedia(t, f.db, database.Media{
		Title:           "Due Movie",
		ArrID:           1,
		DefaultDeleteAt: time.Now().Add(-time.Hour),
	})

	require.NoError(t, f.e.cleanupMedia(t.Context()))

	require.Empty(t, f.radarr.deleted, "dry run must not delete in radarr")
	require.Empty(t, f.jf.removed, "dry run must not remove from jellyfin")
	f.requireLive(t, due.ID)
}

func TestCleanupMediaUnmonitorFlag(t *testing.T) {
	f := newCleanupFixture(t)
	f.e.cfg.Radarr.Unmonitor = true
	createMedia(t, f.db, database.Media{
		Title:           "Due Movie",
		ArrID:           1,
		DefaultDeleteAt: time.Now().Add(-time.Hour),
	})

	require.NoError(t, f.e.cleanupMedia(t.Context()))
	require.Equal(t, []int32{1}, f.radarr.unmonitored)
	require.Equal(t, []int32{1}, f.radarr.deleted)
}

func TestCleanupMediaNilArrClientSkipsItem(t *testing.T) {
	f := newCleanupFixture(t)
	f.e.radarr = nil
	due := createMedia(t, f.db, database.Media{
		Title:           "Due Movie",
		ArrID:           1,
		DefaultDeleteAt: time.Now().Add(-time.Hour),
	})

	require.NoError(t, f.e.cleanupMedia(t.Context()))
	f.requireLive(t, due.ID)
	require.Empty(t, f.jf.removed, "jellyfin must not be touched when the arr delete is impossible")
}

func TestCleanupMediaArrErrorKeepsRowForRetry(t *testing.T) {
	f := newCleanupFixture(t)
	f.radarr.deleteErr[1] = errors.New("radarr down")
	due := createMedia(t, f.db, database.Media{
		Title:           "Due Movie",
		ArrID:           1,
		DefaultDeleteAt: time.Now().Add(-time.Hour),
	})

	require.NoError(t, f.e.cleanupMedia(t.Context()))
	f.requireLive(t, due.ID)
	require.Empty(t, f.jf.removed)

	// Next run, radarr is back: the item is retried and deleted.
	delete(f.radarr.deleteErr, 1)
	require.NoError(t, f.e.cleanupMedia(t.Context()))
	require.True(t, f.radarr.hasDeleted(1))
	requireTombstone(t, f.gdb, due.ID, database.DBDeleteReasonDefault)
}

func TestCleanupMediaJellyfinErrorStillDeletes(t *testing.T) {
	// KNOWN-BEHAVIOR: when the arr deletion succeeded but the Jellyfin removal
	// fails, the row is deleted anyway. The files are gone (arr is the source
	// of truth); the stale Jellyfin entry disappears with the next library scan.
	f := newCleanupFixture(t)
	due := createMedia(t, f.db, database.Media{
		Title:           "Due Movie",
		ArrID:           1,
		DefaultDeleteAt: time.Now().Add(-time.Hour),
	})
	f.jf.removeErr[due.JellyfinID] = errors.New("jellyfin down")

	require.NoError(t, f.e.cleanupMedia(t.Context()))
	require.True(t, f.radarr.hasDeleted(1))
	requireTombstone(t, f.gdb, due.ID, database.DBDeleteReasonDefault)
}

func TestCleanupMediaSkipsProtectedEvenIfDue(t *testing.T) {
	f := newCleanupFixture(t)
	protectedUntil := time.Now().Add(time.Hour)
	due := createMedia(t, f.db, database.Media{
		Title:           "Protected Due Movie",
		ArrID:           1,
		DefaultDeleteAt: time.Now().Add(-time.Hour),
		ProtectedUntil:  &protectedUntil,
	})

	require.NoError(t, f.e.cleanupMedia(t.Context()))
	require.Empty(t, f.radarr.deleted)
	f.requireLive(t, due.ID)
}
