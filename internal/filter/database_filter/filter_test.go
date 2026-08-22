package databasefilter

import (
	"testing"
	"time"

	"github.com/devopsarr/radarr-go/radarr"
	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/database/databasetest"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/stretchr/testify/require"
)

func movieItem(title string, arrID int32) arr.MediaItem {
	movie := radarr.MovieResource{}
	movie.SetId(arrID)
	movie.SetTitle(title)
	return arr.MediaItem{
		JellyfinID:    "jf-" + title,
		Title:         title,
		LibraryName:   "Movies",
		MediaType:     models.MediaTypeMovie,
		MovieResource: movie,
	}
}

func seriesItem(title string, arrID int32) arr.MediaItem {
	series := sonarr.SeriesResource{}
	series.SetId(arrID)
	series.SetTitle(title)
	return arr.MediaItem{
		JellyfinID:     "jf-" + title,
		Title:          title,
		LibraryName:    "TV",
		MediaType:      models.MediaTypeTV,
		SeriesResource: series,
	}
}

func TestApplyExcludesItemsAlreadyInDatabase(t *testing.T) {
	db, _ := databasetest.New(t)
	require.NoError(t, db.CreateMediaItems(t.Context(), []database.Media{
		{
			JellyfinID:      "jf-Known Movie",
			Title:           "Known Movie",
			ArrID:           1,
			MediaType:       database.MediaTypeMovie,
			DefaultDeleteAt: time.Now().Add(24 * time.Hour),
		},
		{
			JellyfinID:      "jf-Known Show",
			Title:           "Known Show",
			ArrID:           7,
			MediaType:       database.MediaTypeTV,
			DefaultDeleteAt: time.Now().Add(24 * time.Hour),
		},
	}))

	f := New(db)
	got, err := f.Apply(t.Context(), []arr.MediaItem{
		movieItem("Known Movie", 1),
		movieItem("New Movie", 2),
		seriesItem("Known Show", 7),
		seriesItem("New Show", 8),
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "New Movie", got[0].Title)
	require.Equal(t, "New Show", got[1].Title)
}

func TestApplyExcludesProtectedItems(t *testing.T) {
	// Protected items are still tracked, so they must not be re-marked.
	db, _ := databasetest.New(t)
	protectedUntil := time.Now().Add(90 * 24 * time.Hour)
	require.NoError(t, db.CreateMediaItems(t.Context(), []database.Media{{
		JellyfinID:      "jf-Protected Movie",
		Title:           "Protected Movie",
		ArrID:           1,
		MediaType:       database.MediaTypeMovie,
		DefaultDeleteAt: time.Now().Add(24 * time.Hour),
		ProtectedUntil:  &protectedUntil,
	}}))

	f := New(db)
	got, err := f.Apply(t.Context(), []arr.MediaItem{movieItem("Protected Movie", 1)})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestApplyIgnoresTombstones(t *testing.T) {
	// Soft-deleted rows are history, not state: an item deleted once must be
	// eligible for a fresh pickup.
	db, _ := databasetest.New(t)
	require.NoError(t, db.CreateMediaItems(t.Context(), []database.Media{{
		JellyfinID:      "jf-Deleted Movie",
		Title:           "Deleted Movie",
		ArrID:           1,
		MediaType:       database.MediaTypeMovie,
		DefaultDeleteAt: time.Now().Add(24 * time.Hour),
	}}))

	items, err := db.GetMediaItems(t.Context(), true)
	require.NoError(t, err)
	require.Len(t, items, 1)
	items[0].DBDeleteReason = database.DBDeleteReasonDefault
	require.NoError(t, db.DeleteMediaItem(t.Context(), &items[0]))

	f := New(db)
	got, err := f.Apply(t.Context(), []arr.MediaItem{movieItem("Deleted Movie", 1)})
	require.NoError(t, err)
	require.Len(t, got, 1, "tombstoned items must be re-picked up")
}
