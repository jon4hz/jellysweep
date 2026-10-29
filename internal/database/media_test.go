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

func TestGetMediaItemsProtectedFiltering(t *testing.T) {
	db, _ := databasetest.New(t)

	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	createMediaItem(t, db, database.Media{Title: "Unprotected", ArrID: 1})
	createMediaItem(t, db, database.Media{Title: "Protected", ArrID: 2, ProtectedUntil: &future})
	createMediaItem(t, db, database.Media{Title: "Expired Protection", ArrID: 3, ProtectedUntil: &past})

	all, err := db.GetMediaItems(t.Context(), true)
	require.NoError(t, err)
	require.Len(t, all, 3)

	unprotected, err := db.GetMediaItems(t.Context(), false)
	require.NoError(t, err)
	titles := make([]string, 0, len(unprotected))
	for _, m := range unprotected {
		titles = append(titles, m.Title)
	}
	require.ElementsMatch(t, []string{"Unprotected", "Expired Protection"}, titles,
		"actively protected items must be excluded; expired protection must not")
}

func TestGetMediaExpiredProtectionBoundary(t *testing.T) {
	db, _ := databasetest.New(t)

	asOf := time.Now()
	before := asOf.Add(-time.Minute)
	after := asOf.Add(time.Minute)
	createMediaItem(t, db, database.Media{Title: "Expired", ArrID: 1, ProtectedUntil: &before})
	createMediaItem(t, db, database.Media{Title: "Exactly Now", ArrID: 2, ProtectedUntil: &asOf})
	createMediaItem(t, db, database.Media{Title: "Still Protected", ArrID: 3, ProtectedUntil: &after})
	createMediaItem(t, db, database.Media{Title: "Never Protected", ArrID: 4})

	expired, err := db.GetMediaExpiredProtection(t.Context(), asOf)
	require.NoError(t, err)
	titles := make([]string, 0, len(expired))
	for _, m := range expired {
		titles = append(titles, m.Title)
	}
	require.ElementsMatch(t, []string{"Expired", "Exactly Now"}, titles,
		"protected_until <= asOf is expired; NULL never expires")
}

func TestDeleteMediaItemWritesReasonAndTombstone(t *testing.T) {
	db, gdb := databasetest.New(t)

	tmdbID := int32(4242)
	media := createMediaItem(t, db, database.Media{
		Title:  "Doomed Movie",
		ArrID:  1,
		TmdbId: &tmdbID,
		DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{
			{Threshold: 90, DeleteDate: time.Now().Add(48 * time.Hour)},
		},
	})

	media.DBDeleteReason = database.DBDeleteReasonDefault
	require.NoError(t, db.DeleteMediaItem(t.Context(), &media))

	// Gone from the live set.
	live, err := db.GetMediaItems(t.Context(), true)
	require.NoError(t, err)
	require.Empty(t, live)

	// Present as a tombstone with the reason recorded.
	var tombstone database.Media
	require.NoError(t, gdb.Unscoped().First(&tombstone, media.ID).Error)
	require.True(t, tombstone.DeletedAt.Valid)
	require.Equal(t, database.DBDeleteReasonDefault, tombstone.DBDeleteReason)

	// Tombstone queries find it by TMDB ID.
	deleted, err := db.GetDeletedMediaByTMDBID(t.Context(), tmdbID)
	require.NoError(t, err)
	require.Len(t, deleted, 1)
	require.Equal(t, media.ID, deleted[0].ID)
}

func TestGetDeletedMediaOnlyReturnsTombstones(t *testing.T) {
	db, _ := databasetest.New(t)

	tmdbID := int32(7)
	createMediaItem(t, db, database.Media{Title: "Alive", ArrID: 1, TmdbId: &tmdbID})

	deleted, err := db.GetDeletedMediaByTMDBID(t.Context(), tmdbID)
	require.NoError(t, err)
	require.Empty(t, deleted, "live rows must not appear in the deletion history")
}

func TestReMarkAfterDeletionCreatesNewRow(t *testing.T) {
	// The mark -> delete -> re-pickup cycle must work: the unique index
	// includes DefaultDeleteAt, and tombstones don't block new rows.
	db, _ := databasetest.New(t)

	first := createMediaItem(t, db, database.Media{Title: "Cycled Movie", ArrID: 1})
	first.DBDeleteReason = database.DBDeleteReasonStreamed
	require.NoError(t, db.DeleteMediaItem(t.Context(), &first))

	second := createMediaItem(t, db, database.Media{
		Title:           "Cycled Movie",
		ArrID:           1,
		DefaultDeleteAt: time.Now().Add(31 * 24 * time.Hour),
	})
	require.NotEqual(t, first.ID, second.ID, "a re-pickup is a fresh row, not a resurrection")
}

func TestGetMediaWithPendingRequest(t *testing.T) {
	db, _ := databasetest.New(t)

	user, err := db.CreateUser(t.Context(), "alice")
	require.NoError(t, err)

	requested := createMediaItem(t, db, database.Media{Title: "Requested", ArrID: 1})
	createMediaItem(t, db, database.Media{Title: "Unrequested", ArrID: 2})
	future := time.Now().Add(time.Hour)
	protectedRequested := createMediaItem(t, db, database.Media{Title: "Protected Requested", ArrID: 3, ProtectedUntil: &future})

	_, err = db.CreateRequest(t.Context(), requested.ID, user.ID)
	require.NoError(t, err)
	_, err = db.CreateRequest(t.Context(), protectedRequested.ID, user.ID)
	require.NoError(t, err)

	pending, err := db.GetMediaWithPendingRequest(t.Context())
	require.NoError(t, err)
	require.Len(t, pending, 1, "protected media must not show up as pending")
	require.Equal(t, requested.ID, pending[0].ID)
	require.Equal(t, user.ID, pending[0].Request.UserID)

	// Approving removes it from the pending set.
	require.NoError(t, db.UpdateRequestStatus(t.Context(), pending[0].Request.ID, database.RequestStatusApproved))
	pending, err = db.GetMediaWithPendingRequest(t.Context())
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestUpdateRequestStatusUnknownRequest(t *testing.T) {
	db, _ := databasetest.New(t)
	err := db.UpdateRequestStatus(t.Context(), 999, database.RequestStatusApproved)
	require.Error(t, err)
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

func TestSetMediaEstimatedDeleteAtWritesZeroValue(t *testing.T) {
	db, _ := databasetest.New(t)

	media := createMediaItem(t, db, database.Media{Title: "Estimated Movie", ArrID: 1})
	require.True(t, media.EstimatedDeleteAt.IsZero())

	estimate := time.Now().Add(2 * 24 * time.Hour)
	require.NoError(t, db.SetMediaEstimatedDeleteAt(t.Context(), media.ID, estimate))
	got, err := db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.WithinDuration(t, estimate, got.EstimatedDeleteAt, time.Second)

	// Once no policy applies anymore the estimate is reset; the zero time must
	// actually be written, otherwise the UI keeps showing a stale date.
	require.NoError(t, db.SetMediaEstimatedDeleteAt(t.Context(), media.ID, time.Time{}))
	got, err = db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.True(t, got.EstimatedDeleteAt.IsZero(), "zero estimate must clear the stored value")
}
