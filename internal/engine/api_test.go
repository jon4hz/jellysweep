package engine

import (
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/database/databasetest"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type keepFixture struct {
	e      *Engine
	db     *database.Client
	gdb    *gorm.DB
	radarr *fakeArr
	sonarr *fakeArr
	user   *database.User
	admin  *database.User
}

func newKeepFixture(t *testing.T) *keepFixture {
	t.Helper()
	db, gdb := databasetest.New(t)

	user, err := db.CreateUser(t.Context(), "alice")
	require.NoError(t, err)
	admin, err := db.CreateUser(t.Context(), "admin")
	require.NoError(t, err)

	f := &keepFixture{
		db:     db,
		gdb:    gdb,
		radarr: newFakeArr(),
		sonarr: newFakeArr(),
		user:   user,
		admin:  admin,
	}
	f.e = &Engine{cfg: testConfig(), db: db, radarr: f.radarr, sonarr: f.sonarr}
	return f
}

func TestRequestKeepMediaCreatesPendingRequest(t *testing.T) {
	f := newKeepFixture(t)
	media := createMedia(t, f.db, database.Media{Title: "Wanted Movie", ArrID: 1})

	autoApproved, err := f.e.RequestKeepMedia(t.Context(), media.ID, f.user.ID, f.user.Username)
	require.NoError(t, err)
	require.False(t, autoApproved)

	pending, err := f.db.GetMediaWithPendingRequest(t.Context())
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, media.ID, pending[0].ID)
	requireHistory(t, f.db, media.JellyfinID, database.HistoryEventRequestCreated)
}

func TestRequestKeepMediaAutoApproval(t *testing.T) {
	f := newKeepFixture(t)
	require.NoError(t, f.db.UpdateUserAutoApproval(t.Context(), f.user.ID, true))
	media := createMedia(t, f.db, database.Media{Title: "Wanted Movie", ArrID: 1})

	autoApproved, err := f.e.RequestKeepMedia(t.Context(), media.ID, f.user.ID, f.user.Username)
	require.NoError(t, err)
	require.True(t, autoApproved)

	got, err := f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ProtectedUntil)
	// Default protection period is 90 days.
	require.WithinDuration(t, time.Now().Add(90*24*time.Hour), *got.ProtectedUntil, time.Minute)
	require.Equal(t, database.RequestStatusApproved, got.Request.Status)
	requireHistory(t, f.db, media.JellyfinID,
		database.HistoryEventRequestCreated,
		database.HistoryEventRequestApproved,
		database.HistoryEventProtected,
	)
}

func TestRequestKeepMediaUnkeepable(t *testing.T) {
	f := newKeepFixture(t)
	media := createMedia(t, f.db, database.Media{Title: "Doomed Movie", ArrID: 1, Unkeepable: true})

	_, err := f.e.RequestKeepMedia(t.Context(), media.ID, f.user.ID, f.user.Username)
	require.ErrorIs(t, err, ErrUnkeepableMedia)
}

func TestRequestKeepMediaOnlyOneRequestPerMedia(t *testing.T) {
	f := newKeepFixture(t)
	media := createMedia(t, f.db, database.Media{Title: "Wanted Movie", ArrID: 1})

	_, err := f.e.RequestKeepMedia(t.Context(), media.ID, f.user.ID, f.user.Username)
	require.NoError(t, err)

	_, err = f.e.RequestKeepMedia(t.Context(), media.ID, f.user.ID, f.user.Username)
	require.ErrorIs(t, err, ErrRequestAlreadyProcessed)
}

func TestHandleKeepRequestApprove(t *testing.T) {
	f := newKeepFixture(t)
	media := createMedia(t, f.db, database.Media{Title: "Wanted Movie", ArrID: 1})
	_, err := f.e.RequestKeepMedia(t.Context(), media.ID, f.user.ID, f.user.Username)
	require.NoError(t, err)

	require.NoError(t, f.e.HandleKeepRequest(t.Context(), f.admin.ID, media.ID, true))

	got, err := f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.Equal(t, database.RequestStatusApproved, got.Request.Status)
	require.NotNil(t, got.ProtectedUntil)
	require.False(t, got.Unkeepable)
}

func TestHandleKeepRequestDenyOnProtectedMediaClearsProtection(t *testing.T) {
	// Regression companion for the gorm zero-value fix: denying a request on a
	// currently protected item must remove the protection.
	f := newKeepFixture(t)
	protectedUntil := time.Now().Add(30 * 24 * time.Hour)
	media := createMedia(t, f.db, database.Media{
		Title:          "Previously Protected",
		ArrID:          1,
		ProtectedUntil: &protectedUntil,
	})
	_, err := f.e.RequestKeepMedia(t.Context(), media.ID, f.user.ID, f.user.Username)
	require.NoError(t, err)

	require.NoError(t, f.e.HandleKeepRequest(t.Context(), f.admin.ID, media.ID, false))

	got, err := f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.Equal(t, database.RequestStatusDenied, got.Request.Status)
	require.True(t, got.Unkeepable)
	require.Nil(t, got.ProtectedUntil, "denying a request must remove any protection")
	requireHistory(t, f.db, media.JellyfinID, database.HistoryEventRequestDenied)
}

func TestHandleKeepRequestWithoutRequestIsNoop(t *testing.T) {
	f := newKeepFixture(t)
	media := createMedia(t, f.db, database.Media{Title: "Quiet Movie", ArrID: 1})

	require.NoError(t, f.e.HandleKeepRequest(t.Context(), f.admin.ID, media.ID, true))

	got, err := f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.Nil(t, got.ProtectedUntil)
}

func TestMarkMediaAsProtectedAdmin(t *testing.T) {
	f := newKeepFixture(t)
	media := createMedia(t, f.db, database.Media{Title: "Kept Movie", ArrID: 1, Unkeepable: true})

	require.NoError(t, f.e.MarkMediaAsProtected(t.Context(), media.ID, f.admin.ID))

	got, err := f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ProtectedUntil)
	require.False(t, got.Unkeepable, "admin protection must clear the unkeepable flag")
	requireHistory(t, f.db, media.JellyfinID, database.HistoryEventAdminKeep)
}

func TestMarkMediaAsUnkeepableAdmin(t *testing.T) {
	f := newKeepFixture(t)
	media := createMedia(t, f.db, database.Media{Title: "Doomed Movie", ArrID: 1})

	require.NoError(t, f.e.MarkMediaAsUnkeepable(t.Context(), media.ID, f.admin.ID))

	got, err := f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.True(t, got.Unkeepable)
	requireHistory(t, f.db, media.JellyfinID, database.HistoryEventAdminUnkeep)
}

func TestMarkMediaAsKeepForever(t *testing.T) {
	f := newKeepFixture(t)
	media := createMedia(t, f.db, database.Media{Title: "Forever Movie", ArrID: 42})

	require.NoError(t, f.e.MarkMediaAsKeepForever(t.Context(), media.ID, f.admin.ID))

	require.Equal(t, []int32{42}, f.radarr.ignored, "ignore tag must be set in the arr")

	requireTombstone(t, f.gdb, media.ID, database.DBDeleteReasonKeepForever)
	requireHistory(t, f.db, media.JellyfinID, database.HistoryEventKeepForever)
}

func TestMarkMediaAsKeepForeverArrFailureKeepsRow(t *testing.T) {
	f := newKeepFixture(t)
	f.e.radarr = nil // arr unavailable -> ignore tag cannot be written
	media := createMedia(t, f.db, database.Media{Title: "Forever Movie", ArrID: 42})

	require.Error(t, f.e.MarkMediaAsKeepForever(t.Context(), media.ID, f.admin.ID))

	var live database.Media
	require.NoError(t, f.gdb.First(&live, media.ID).Error,
		"row must survive when the ignore tag could not be written")
}
