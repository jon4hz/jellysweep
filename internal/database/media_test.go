package database_test

import (
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/database/databasetest"
	"github.com/stretchr/testify/require"
)

func createMediaItem(t *testing.T, db *database.Client, media database.Media) database.Media {
	t.Helper()
	if media.JellyfinID == "" {
		media.JellyfinID = "jf-" + media.Title
	}
	if media.MediaType == "" {
		media.MediaType = database.MediaTypeMovie
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

func TestCreateMediaItemsEmptySliceIsNoop(t *testing.T) {
	db, _ := databasetest.New(t)

	// Regression: an empty batch used to return an error, failing cleanup runs
	// where every item was filtered out by policies.
	require.NoError(t, db.CreateMediaItems(t.Context(), nil))
	require.NoError(t, db.CreateMediaItems(t.Context(), []database.Media{}))
}

func TestSetMediaProtectedUntilClearsUnkeepable(t *testing.T) {
	db, _ := databasetest.New(t)

	media := createMediaItem(t, db, database.Media{
		Title:      "Unkeepable Movie",
		ArrID:      1,
		Unkeepable: true,
	})

	// Regression: gorm skips zero-valued struct fields in Updates, so
	// Unkeepable=false was silently dropped and protecting an unkeepable item
	// left it unkeepable.
	protectedUntil := time.Now().Add(90 * 24 * time.Hour)
	require.NoError(t, db.SetMediaProtectedUntil(t.Context(), media.ID, &protectedUntil))

	got, err := db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ProtectedUntil)
	require.WithinDuration(t, protectedUntil, *got.ProtectedUntil, time.Second)
	require.False(t, got.Unkeepable, "protecting a media item must clear the unkeepable flag")

	// Clearing the protection must write the NULL as well.
	require.NoError(t, db.SetMediaProtectedUntil(t.Context(), media.ID, nil))
	got, err = db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.Nil(t, got.ProtectedUntil)
}

func TestMarkMediaAsUnkeepableClearsProtection(t *testing.T) {
	db, _ := databasetest.New(t)

	protectedUntil := time.Now().Add(90 * 24 * time.Hour)
	media := createMediaItem(t, db, database.Media{
		Title:          "Protected Movie",
		ArrID:          2,
		ProtectedUntil: &protectedUntil,
	})

	// Regression: gorm skips nil pointer fields in struct Updates, so denying
	// a keep request on a protected item never removed its protection.
	require.NoError(t, db.MarkMediaAsUnkeepable(t.Context(), media.ID))

	got, err := db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.True(t, got.Unkeepable)
	require.Nil(t, got.ProtectedUntil, "marking media unkeepable must remove its protection")
}
