package engine

import (
	"context"
	"testing"
	"time"

	"github.com/devopsarr/radarr-go/radarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/database/databasetest"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/policy"
	"github.com/stretchr/testify/require"
)

func testConfig() *config.Config {
	return &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {Enabled: true},
			"TV":     {Enabled: true},
		},
	}
}

func createMedia(t *testing.T, db database.DB, media database.Media) database.Media {
	t.Helper()
	if media.JellyfinID == "" {
		media.JellyfinID = "jf-" + media.Title
	}
	if media.MediaType == "" {
		media.MediaType = database.MediaTypeMovie
	}
	if media.LibraryName == "" {
		media.LibraryName = "Movies"
	}
	if media.DefaultDeleteAt.IsZero() {
		media.DefaultDeleteAt = time.Now().Add(30 * 24 * time.Hour)
	}
	require.NoError(t, db.CreateMediaItems(t.Context(), []database.Media{media}))

	items, err := db.GetMediaItems(t.Context(), true)
	require.NoError(t, err)
	for _, item := range items {
		if item.JellyfinID == media.JellyfinID {
			return item
		}
	}
	t.Fatalf("created media item %q not found", media.Title)
	return database.Media{}
}

func TestRemoveRecentlyPlayedItemsNoStatsBackend(t *testing.T) {
	// Regression: without a stats backend e.stats is a nil interface and the
	// cleanup job used to panic here.
	db, _ := databasetest.New(t)
	e := &Engine{cfg: testConfig(), db: db, stats: nil}

	createMedia(t, db, database.Media{Title: "A Movie", ArrID: 1})

	require.NotPanics(t, func() {
		e.removeRecentlyPlayedItems(t.Context())
	})

	items, err := db.GetMediaItems(t.Context(), true)
	require.NoError(t, err)
	require.Len(t, items, 1, "items must be untouched when no stats backend is configured")
}

func TestRemoveRecentlyPlayedItemsSkipsProtected(t *testing.T) {
	// Regression: protected items used to be tombstoned with reason "streamed",
	// silently discarding their protection and keep-request history.
	db, gdb := databasetest.New(t)
	stats := newFakeStats()
	e := &Engine{cfg: testConfig(), db: db, stats: stats}

	protectedUntil := time.Now().Add(90 * 24 * time.Hour)
	protected := createMedia(t, db, database.Media{
		Title:          "Protected Movie",
		ArrID:          1,
		ProtectedUntil: &protectedUntil,
	})
	unprotected := createMedia(t, db, database.Media{
		Title: "Unprotected Movie",
		ArrID: 2,
	})

	// Both items were streamed yesterday, well inside the 30-day threshold.
	streamed := time.Now().Add(-24 * time.Hour)
	stats.setLastPlayed(protected.JellyfinID, streamed)
	stats.setLastPlayed(unprotected.JellyfinID, streamed)

	e.removeRecentlyPlayedItems(t.Context())

	items, err := db.GetMediaItems(t.Context(), true)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, protected.ID, items[0].ID, "protected item must survive being streamed")

	// The unprotected item must be tombstoned with reason "streamed".
	var tombstone database.Media
	require.NoError(t, gdb.Unscoped().First(&tombstone, unprotected.ID).Error)
	require.NotNil(t, tombstone.DeletedAt)
	require.Equal(t, database.DBDeleteReasonStreamed, tombstone.DBDeleteReason)
}

func TestSaveMediaItemsToDatabaseRespectsContext(t *testing.T) {
	// Regression: saveMediaItemsToDatabase used context.Background() instead of
	// the job context, so a canceled job kept writing to the database.
	db, _ := databasetest.New(t)
	e := &Engine{cfg: testConfig(), db: db, policy: policy.NewEngine()}

	movie := radarr.MovieResource{}
	movie.SetId(1)
	movie.SetTitle("A Movie")
	items := []arr.MediaItem{{
		JellyfinID:    "jf-a-movie",
		LibraryName:   "Movies",
		Title:         "A Movie",
		MediaType:     models.MediaTypeMovie,
		MovieResource: movie,
	}}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.Error(t, e.saveMediaItemsToDatabase(ctx, items), "canceled context must abort the save")

	saved, err := db.GetMediaItems(t.Context(), true)
	require.NoError(t, err)
	require.Empty(t, saved, "nothing may be written after the job context is canceled")
}
