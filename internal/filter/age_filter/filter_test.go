package agefilter

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
	"github.com/stretchr/testify/require"
)

// stubArr implements arr.Arrer; only GetItemAddedDate is used by the filter.
type stubArr struct {
	arr.Arrer
	addedDates map[int32]*time.Time
	sinceSeen  map[int32]time.Time
}

func (s *stubArr) GetItemAddedDate(_ context.Context, itemID int32, since time.Time) (*time.Time, error) {
	if s.sinceSeen != nil {
		s.sinceSeen[itemID] = since
	}
	return s.addedDates[itemID], nil
}

func movieItem(title string, arrID, tmdbID int32) arr.MediaItem {
	movie := radarr.MovieResource{}
	movie.SetId(arrID)
	movie.SetTitle(title)
	return arr.MediaItem{
		Title:         title,
		LibraryName:   "Movies",
		MediaType:     models.MediaTypeMovie,
		TmdbId:        tmdbID,
		MovieResource: movie,
	}
}

func TestApplyFiltersRecentlyAddedItems(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {}, // default ContentAgeThreshold: 30 days
		},
	}
	db, _ := databasetest.New(t)

	recent := time.Now().Add(-10 * 24 * time.Hour)
	old := time.Now().Add(-60 * 24 * time.Hour)
	radarrStub := &stubArr{addedDates: map[int32]*time.Time{1: &recent, 2: &old}}

	f := New(cfg, db, nil, radarrStub)
	got, err := f.Apply(t.Context(), []arr.MediaItem{
		movieItem("Recent Movie", 1, 100),
		movieItem("Old Movie", 2, 200),
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Old Movie", got[0].Title, "recently added items must be excluded")
}

func TestApplyUsesLastDeletionAsHistoryCutoff(t *testing.T) {
	// If an item was deleted before and re-downloaded, only imports after the
	// last deletion count as its age.
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{"Movies": {}},
	}
	db, gdb := databasetest.New(t)

	tmdbID := int32(100)
	require.NoError(t, db.CreateMediaItems(t.Context(), []database.Media{{
		JellyfinID:      "jf-Returning Movie",
		Title:           "Returning Movie",
		ArrID:           1,
		TmdbId:          &tmdbID,
		MediaType:       database.MediaTypeMovie,
		DefaultDeleteAt: time.Now().Add(24 * time.Hour),
	}}))
	items, err := db.GetMediaItems(t.Context(), true)
	require.NoError(t, err)
	items[0].DBDeleteReason = database.DBDeleteReasonDefault
	require.NoError(t, db.DeleteMediaItem(t.Context(), &items[0]))

	// Backdate the tombstone so the deletion time is a known value.
	deletedAt := time.Now().Add(-40 * 24 * time.Hour)
	require.NoError(t, gdb.Unscoped().Model(&database.Media{}).
		Where("id = ?", items[0].ID).
		Update("deleted_at", deletedAt).Error)

	radarrStub := &stubArr{
		addedDates: map[int32]*time.Time{},
		sinceSeen:  map[int32]time.Time{},
	}
	f := New(cfg, db, nil, radarrStub)
	_, err = f.Apply(t.Context(), []arr.MediaItem{movieItem("Returning Movie", 1, tmdbID)})
	require.NoError(t, err)
	require.WithinDuration(t, deletedAt, radarrStub.sinceSeen[1], time.Second,
		"the added-date lookup must start at the last deletion")
}

func TestApplyWithNilArrClientsDoesNotPanic(t *testing.T) {
	// Regression: with only one arr configured, the other client is nil and
	// looking up the added date used to panic on the nil interface.
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {},
			"TV":     {},
		},
	}
	db, _ := databasetest.New(t)
	f := New(cfg, db, nil, nil)

	items := []arr.MediaItem{
		{Title: "Some Movie", LibraryName: "Movies", MediaType: models.MediaTypeMovie},
		{Title: "Some Show", LibraryName: "TV", MediaType: models.MediaTypeTV},
	}

	filtered, err := f.Apply(t.Context(), items)
	require.NoError(t, err)
	// Without an added date the filter fails open: items stay candidates.
	require.Len(t, filtered, 2)
}
