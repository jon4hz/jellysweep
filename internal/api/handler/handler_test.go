package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/internal/api/handler"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/database/databasetest"
	"github.com/jon4hz/jellysweep/internal/engine"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	router *gin.Engine
	db     *database.Client
	user   *database.User
	admin  *database.User
}

// newFixture builds a real engine (no network calls happen unless the cleanup
// job runs) and mounts the user and admin handlers with a stubbed auth
// middleware that injects the given user into the request context.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Chdir(t.TempDir()) // the engine creates its image cache directory in cwd

	db, _ := databasetest.New(t)
	cfg := &config.Config{
		CleanupSchedule: "0 */12 * * *",
		Jellyfin:        &config.JellyfinConfig{URL: "http://jellyfin.invalid", APIKey: "k"},
		Cache:           &config.CacheConfig{Type: config.CacheTypeMemory},
		Libraries:       map[string]*config.CleanupConfig{"Movies": {Enabled: true}},
	}
	eng, err := engine.New(cfg, db, false)
	require.NoError(t, err)
	t.Cleanup(func() { eng.Close() }) //nolint:errcheck

	f := &fixture{db: db}
	f.user, err = db.CreateUser(t.Context(), "alice")
	require.NoError(t, err)
	f.admin, err = db.CreateUser(t.Context(), "admin")
	require.NoError(t, err)

	h := handler.New(eng, cfg)
	adminH := handler.NewAdmin(eng, cfg)

	f.router = gin.New()
	f.router.GET("/api/media", h.GetMediaItems)
	f.router.POST("/api/media/:id/request-keep", f.as(f.user, false), h.RequestKeepMedia) // same path as production
	f.router.POST("/api/media/:id/keep-anonymous", h.RequestKeepMedia)
	f.router.GET("/api/me", f.as(f.user, false), h.Me)
	adminAPI := f.router.Group("/admin/api", f.as(f.admin, true))
	adminAPI.GET("/keep-requests", adminH.GetKeepRequests)
	adminAPI.POST("/keep-requests/:id/accept", adminH.AcceptKeepRequest)
	adminAPI.POST("/keep-requests/:id/decline", adminH.DeclineKeepRequest)
	adminAPI.POST("/media/:id/keep", adminH.MarkMediaAsProtected)
	adminAPI.POST("/media/:id/delete", adminH.MarkMediaAsUnkeepable)
	return f
}

func (f *fixture) as(user *database.User, isAdmin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user", &models.User{ID: user.ID, Username: user.Username, IsAdmin: isAdmin})
		c.Next()
	}
}

func (f *fixture) do(t *testing.T, method, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	var body map[string]any
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	}
	return w.Code, body
}

func (f *fixture) createMedia(t *testing.T, title string, unkeepable bool) database.Media {
	t.Helper()
	require.NoError(t, f.db.CreateMediaItems(t.Context(), []database.Media{{
		JellyfinID:      "jf-" + title,
		Title:           title,
		ArrID:           1,
		LibraryName:     "Movies",
		MediaType:       database.MediaTypeMovie,
		DefaultDeleteAt: time.Now().Add(30 * 24 * time.Hour),
		Unkeepable:      unkeepable,
		RequestedBy:     "secret@example.com",
	}}))
	items, err := f.db.GetMediaItems(t.Context(), true)
	require.NoError(t, err)
	for _, item := range items {
		if item.Title == title {
			return item
		}
	}
	t.Fatalf("media %q not found", title)
	return database.Media{}
}

func TestGetMediaItemsHidesRequester(t *testing.T) {
	f := newFixture(t)
	f.createMedia(t, "Movie", false)

	code, body := f.do(t, http.MethodGet, "/api/media")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, body["success"])
	items := body["mediaItems"].([]any)
	require.Len(t, items, 1)
	item := items[0].(map[string]any)
	require.Equal(t, "Movie", item["Title"])
	_, leaked := item["RequestedBy"]
	require.False(t, leaked, "the user endpoint must not expose who requested the media")
}

func TestRequestKeepMediaFlow(t *testing.T) {
	f := newFixture(t)
	media := f.createMedia(t, "Movie", false)
	id := itoa(media.ID)

	code, body := f.do(t, http.MethodPost, "/api/media/"+id+"/request-keep")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, false, body["autoApproved"])

	// Visible to admins as pending.
	code, body = f.do(t, http.MethodGet, "/admin/api/keep-requests")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, body["keepRequests"].([]any), 1)

	// Second request is rejected.
	code, body = f.do(t, http.MethodPost, "/api/media/"+id+"/request-keep")
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, false, body["success"])

	// Admin accepts: protected, no longer pending.
	code, _ = f.do(t, http.MethodPost, "/admin/api/keep-requests/"+id+"/accept")
	require.Equal(t, http.StatusOK, code)
	got, err := f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ProtectedUntil)
	require.Equal(t, database.RequestStatusApproved, got.Request.Status)

	code, body = f.do(t, http.MethodGet, "/admin/api/keep-requests")
	require.Equal(t, http.StatusOK, code)
	require.Empty(t, body["keepRequests"])
}

func TestDeclineKeepRequestMarksUnkeepable(t *testing.T) {
	f := newFixture(t)
	media := f.createMedia(t, "Movie", false)
	id := itoa(media.ID)

	code, _ := f.do(t, http.MethodPost, "/api/media/"+id+"/request-keep")
	require.Equal(t, http.StatusOK, code)
	code, _ = f.do(t, http.MethodPost, "/admin/api/keep-requests/"+id+"/decline")
	require.Equal(t, http.StatusOK, code)

	got, err := f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.True(t, got.Unkeepable)
	require.Equal(t, database.RequestStatusDenied, got.Request.Status)
}

func TestRequestKeepMediaErrors(t *testing.T) {
	f := newFixture(t)
	unkeepable := f.createMedia(t, "Doomed", true)

	code, body := f.do(t, http.MethodPost, "/api/media/"+itoa(unkeepable.ID)+"/request-keep")
	require.Equal(t, http.StatusBadRequest, code)
	require.Contains(t, body["error"], "cannot be kept")

	code, _ = f.do(t, http.MethodPost, "/api/media/not-a-number/request-keep")
	require.Equal(t, http.StatusBadRequest, code)

	code, _ = f.do(t, http.MethodPost, "/api/media/999/request-keep")
	require.Equal(t, http.StatusBadRequest, code)

	code, _ = f.do(t, http.MethodPost, "/api/media/1/keep-anonymous")
	require.Equal(t, http.StatusUnauthorized, code, "no user in context must be rejected")
}

func TestAdminMarkEndpoints(t *testing.T) {
	f := newFixture(t)
	media := f.createMedia(t, "Movie", true)
	id := itoa(media.ID)

	code, _ := f.do(t, http.MethodPost, "/admin/api/media/"+id+"/keep")
	require.Equal(t, http.StatusOK, code)
	got, err := f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ProtectedUntil)
	require.False(t, got.Unkeepable)

	code, _ = f.do(t, http.MethodPost, "/admin/api/media/"+id+"/delete")
	require.Equal(t, http.StatusOK, code)
	got, err = f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.True(t, got.Unkeepable)
	require.Nil(t, got.ProtectedUntil)
}

func TestMe(t *testing.T) {
	f := newFixture(t)
	code, body := f.do(t, http.MethodGet, "/api/me")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "alice", body["username"])
	require.Equal(t, false, body["isAdmin"])
}

func itoa(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
