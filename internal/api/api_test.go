package api_test

// Integration tests that drive the real route table through Server.Handler():
// real session middleware, real auth provider (against a fake Jellyfin), real
// engine and database. Only the external services are faked.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/internal/api"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/database/databasetest"
	"github.com/jon4hz/jellysweep/internal/engine"
	"github.com/jon4hz/jellysweep/internal/httptestutil"
	"github.com/jon4hz/jellysweep/internal/notify/webpush"
	jellyfinAPI "github.com/sj14/jellyfin-go/api"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	pluginAPIKey    = "plugin-key"
	correctPassword = "correct"
)

type fixture struct {
	t      *testing.T
	server *httptest.Server
	db     *database.Client
	gdb    *gorm.DB
	cfg    *config.Config
}

type fixtureOpt func(*config.Config)

func withoutPluginAPIKey() fixtureOpt {
	return func(cfg *config.Config) { cfg.APIKey = "" }
}

// newFixture boots the full HTTP stack. The fake Jellyfin accepts any
// username with the password "correct"; the user "admin" is an administrator.
func newFixture(t *testing.T, opts ...fixtureOpt) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Chdir(t.TempDir()) // the engine creates its image cache directory in cwd

	jellyfin := httptestutil.New(t)
	jellyfin.Handle("POST /Users/AuthenticateByName", func(w http.ResponseWriter, r *http.Request) {
		var body jellyfinAPI.AuthenticateUserByName
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		if body.GetPw() != correctPassword {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		policy := jellyfinAPI.NewUserPolicy("", "")
		policy.SetIsAdministrator(body.GetUsername() == "admin")
		user := jellyfinAPI.NewUserDto()
		user.SetId("jf-" + body.GetUsername())
		user.SetName(body.GetUsername())
		user.SetPolicy(*policy)
		result := jellyfinAPI.NewAuthenticationResult()
		result.SetUser(*user)
		result.SetAccessToken("token")
		httptestutil.WriteJSON(t, w, result)
	})

	db, gdb := databasetest.New(t)
	vapidPrivate, vapidPublic, err := webpush.GenerateVAPIDKeys()
	require.NoError(t, err)
	cfg := &config.Config{
		Listen:          "127.0.0.1:0",
		APIKey:          pluginAPIKey,
		SessionKey:      "test-session-key",
		SecureCookies:   false, // plain http in tests
		CleanupSchedule: "0 */12 * * *",
		Jellyfin:        &config.JellyfinConfig{URL: jellyfin.URL, APIKey: "k"},
		Auth:            &config.AuthConfig{Jellyfin: &config.JellyfinAuthConfig{Enabled: true}},
		Cache:           &config.CacheConfig{Type: config.CacheTypeMemory},
		Libraries:       map[string]*config.CleanupConfig{"Movies": {Enabled: true}},
		WebPush:         &config.WebPushConfig{Enabled: true, PublicKey: vapidPublic, PrivateKey: vapidPrivate},
	}
	for _, opt := range opts {
		opt(cfg)
	}

	eng, err := engine.New(cfg, db, false)
	require.NoError(t, err)
	t.Cleanup(func() { eng.Close() }) //nolint:errcheck

	srv, err := api.New(t.Context(), cfg, db, eng, true)
	require.NoError(t, err)
	handler, err := srv.Handler()
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &fixture{t: t, server: server, db: db, gdb: gdb, cfg: cfg}
}

// breakDB closes the underlying database connection so every subsequent
// engine call fails, which is the only way to reach the handlers' 500 paths.
func (f *fixture) breakDB() {
	f.t.Helper()
	sqlDB, err := f.gdb.DB()
	require.NoError(f.t, err)
	require.NoError(f.t, sqlDB.Close())
}

// client is an HTTP client with its own cookie jar that does not follow
// redirects, so auth redirects can be asserted.
type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func (f *fixture) anonymous() *client {
	jar, err := cookiejar.New(nil)
	require.NoError(f.t, err)
	return &client{
		t:    f.t,
		base: f.server.URL,
		http: &http.Client{
			Jar: jar,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (f *fixture) login(username, password string) (*client, *http.Response) {
	f.t.Helper()
	c := f.anonymous()
	resp, _ := c.postForm("/auth/jellyfin/login", url.Values{"username": {username}, "password": {password}})
	return c, resp
}

func (f *fixture) loggedIn(username string) *client {
	f.t.Helper()
	c, resp := f.login(username, correctPassword)
	require.Equal(f.t, http.StatusOK, resp.StatusCode, "login must succeed")
	return c
}

func (c *client) do(req *http.Request) (*http.Response, map[string]any) {
	c.t.Helper()
	resp, err := c.http.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)
	var body map[string]any
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") && len(raw) > 0 {
		require.NoError(c.t, json.Unmarshal(raw, &body), string(raw))
	}
	return resp, body
}

func (c *client) get(path string, headers ...string) (*http.Response, map[string]any) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(c.t.Context(), http.MethodGet, c.base+path, nil)
	require.NoError(c.t, err)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	return c.do(req)
}

func (c *client) postJSON(path string, payload any, headers ...string) (*http.Response, map[string]any) {
	c.t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		require.NoError(c.t, err)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(c.t.Context(), http.MethodPost, c.base+path, body)
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	return c.do(req)
}

func (c *client) putJSON(path string, payload any) (*http.Response, map[string]any) {
	c.t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(c.t, err)
	req, err := http.NewRequestWithContext(c.t.Context(), http.MethodPut, c.base+path, bytes.NewReader(raw))
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *client) postRaw(path, body string, headers ...string) (*http.Response, map[string]any) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(c.t.Context(), http.MethodPost, c.base+path, strings.NewReader(body))
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	return c.do(req)
}

func (c *client) postForm(path string, values url.Values) (*http.Response, map[string]any) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(c.t.Context(), http.MethodPost, c.base+path, strings.NewReader(values.Encode()))
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req)
}

func (f *fixture) createMedia(t *testing.T, title string, year int32) database.Media {
	t.Helper()
	require.NoError(t, f.db.CreateMediaItems(t.Context(), []database.Media{{
		JellyfinID:      "jf-" + title,
		Title:           title,
		Year:            year,
		ArrID:           1,
		LibraryName:     "Movies",
		MediaType:       database.MediaTypeMovie,
		DefaultDeleteAt: time.Now().Add(30 * 24 * time.Hour),
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

func mediaPath(id uint) string { return strconv.FormatUint(uint64(id), 10) }

// --- tests ---

func TestHealthIsPublic(t *testing.T) {
	f := newFixture(t)
	resp, body := f.anonymous().get("/health")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "ok", body["status"])
}

func TestUnauthenticatedRequestsRedirectToLogin(t *testing.T) {
	f := newFixture(t)
	c := f.anonymous()
	for _, path := range []string{"/", "/api/me", "/api/media", "/admin", "/admin/api/media"} {
		resp, _ := c.get(path)
		require.Equal(t, http.StatusFound, resp.StatusCode, path)
		require.Equal(t, "/login", resp.Header.Get("Location"), path)
	}
	resp, _ := c.postJSON("/api/media/1/request-keep", nil)
	require.Equal(t, http.StatusFound, resp.StatusCode, "mutations must be gated too")
}

func TestLoginValidation(t *testing.T) {
	f := newFixture(t)

	_, resp := f.login("alice", "wrong")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	_, resp = f.login("", correctPassword)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	c := f.anonymous()
	resp, _ = c.get("/api/me")
	require.Equal(t, http.StatusFound, resp.StatusCode, "a failed login must not create a session")
}

func TestLoginCreatesSessionAndUser(t *testing.T) {
	f := newFixture(t)
	c, resp := f.login("alice", correctPassword)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, body := c.get("/api/me")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "alice", body["username"])
	require.Equal(t, false, body["isAdmin"])

	user, err := f.db.GetUserByUsername(t.Context(), "alice")
	require.NoError(t, err)
	require.Equal(t, "alice", user.Username)

	// Logging in again reuses the database user.
	f.loggedIn("alice")
	users, err := f.db.GetAllUsers(t.Context())
	require.NoError(t, err)
	require.Len(t, users, 1)
}

func TestLogoutEndsSession(t *testing.T) {
	f := newFixture(t)
	c := f.loggedIn("alice")

	resp, _ := c.get("/logout")
	require.Equal(t, http.StatusFound, resp.StatusCode)

	resp, _ = c.get("/api/me")
	require.Equal(t, http.StatusFound, resp.StatusCode, "session must be gone after logout")
}

func TestAdminRoutesRequireAdmin(t *testing.T) {
	f := newFixture(t)
	f.createMedia(t, "Movie", 2001)

	alice := f.loggedIn("alice")
	for _, path := range []string{"/admin", "/admin/api/media", "/admin/api/keep-requests", "/admin/api/scheduler/jobs"} {
		resp, _ := alice.get(path)
		require.Equal(t, http.StatusForbidden, resp.StatusCode, path)
	}
	resp, _ := alice.postJSON("/admin/api/media/1/keep", nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	admin := f.loggedIn("admin")
	resp, body := admin.get("/api/me")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, true, body["isAdmin"])

	resp, body = admin.get("/admin/api/media")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, body["mediaItems"], 1)
}

func TestKeepRequestFlowOverRealRoutes(t *testing.T) {
	f := newFixture(t)
	media := f.createMedia(t, "Movie", 2001)
	alice := f.loggedIn("alice")
	admin := f.loggedIn("admin")

	resp, body := alice.postJSON("/api/media/"+mediaPath(media.ID)+"/request-keep", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, false, body["autoApproved"])

	resp, body = admin.get("/admin/api/keep-requests")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, body["keepRequests"], 1)

	resp, _ = admin.postJSON("/admin/api/keep-requests/"+mediaPath(media.ID)+"/accept", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// The user sees the approved request on their media item; the list of
	// unprotected items is now empty since the item is protected.
	resp, body = alice.get("/api/media")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, body["mediaItems"])

	got, err := f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ProtectedUntil)
	require.Equal(t, database.RequestStatusApproved, got.Request.Status)
}

func TestPluginRoutesRequireAPIKey(t *testing.T) {
	f := newFixture(t)
	c := f.anonymous()

	resp, _ := c.get("/plugin/health")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp, _ = c.get("/plugin/health", "X-API-Key", "wrong")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	resp, body := c.get("/plugin/health", "X-API-Key", pluginAPIKey)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "ok", body["status"])
}

func TestPluginCheckMediaItem(t *testing.T) {
	f := newFixture(t)
	media := f.createMedia(t, "Marked Movie", 2001)
	c := f.anonymous()
	check := func(payload any) (*http.Response, map[string]any) {
		return c.postJSON("/plugin/check", payload, "X-API-Key", pluginAPIKey)
	}

	resp, body := check(map[string]any{"name": "marked movie", "production_year": 2001, "media_type": "movie"})
	require.Equal(t, http.StatusOK, resp.StatusCode, "title match is case-insensitive")
	deletionDate, err := time.Parse(time.RFC3339, body["deletion_date"].(string))
	require.NoError(t, err)
	require.WithinDuration(t, media.DefaultDeleteAt, deletionDate, time.Second)

	resp, _ = check(map[string]any{"name": "Marked Movie", "media_type": "movie"})
	require.Equal(t, http.StatusOK, resp.StatusCode, "year is optional")

	resp, _ = check(map[string]any{"name": "Marked Movie", "production_year": 1999, "media_type": "movie"})
	require.Equal(t, http.StatusNotFound, resp.StatusCode, "a wrong year must not match")

	resp, _ = check(map[string]any{"name": "Other Movie", "media_type": "movie"})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp, _ = check(map[string]any{"name": "Marked Movie", "media_type": "book"})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp, _ = check(map[string]any{"name": "  ", "media_type": "movie"})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp, _ = c.postRaw("/plugin/check", "not json", "X-API-Key", pluginAPIKey)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPluginRoutesDisabledWithoutAPIKey(t *testing.T) {
	f := newFixture(t, withoutPluginAPIKey())
	resp, _ := f.anonymous().get("/plugin/health", "X-API-Key", "")
	require.Equal(t, http.StatusNotFound, resp.StatusCode, "plugin routes must not be mounted without an API key")
}

// --- admin: media, scheduler, cache, users, history ---

func TestAdminMediaEndpointsExposeRequester(t *testing.T) {
	f := newFixture(t)
	media := f.createMedia(t, "Movie", 2001)
	require.NoError(t, f.gdb.Model(&database.Media{}).Where("id = ?", media.ID).Update("requested_by", "alice@example.com").Error)
	admin := f.loggedIn("admin")

	resp, body := admin.get("/admin/api/media")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	item := body["mediaItems"].([]any)[0].(map[string]any)
	require.Equal(t, "alice@example.com", item["RequestedBy"], "admins must see who requested the media")
}

func TestAdminMarkMediaEndpoints(t *testing.T) {
	f := newFixture(t)
	media := f.createMedia(t, "Movie", 2001)
	id := mediaPath(media.ID)
	admin := f.loggedIn("admin")

	resp, _ := admin.postJSON("/admin/api/media/"+id+"/keep", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	got, err := f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ProtectedUntil)

	resp, _ = admin.postJSON("/admin/api/media/"+id+"/delete", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	got, err = f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)
	require.True(t, got.Unkeepable)
	require.Nil(t, got.ProtectedUntil)

	// No arr is configured, so keep-forever cannot set the ignore tag: the
	// request fails and the row must survive.
	resp, body := admin.postJSON("/admin/api/media/"+id+"/keep-forever", nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, false, body["success"])
	_, err = f.db.GetMediaItemByID(t.Context(), media.ID)
	require.NoError(t, err)

	for _, action := range []string{"keep", "delete", "keep-forever"} {
		resp, _ = admin.postJSON("/admin/api/media/abc/"+action, nil)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, action)
		resp, _ = admin.postJSON("/admin/api/media/999/"+action, nil)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, action)
	}
	for _, action := range []string{"accept", "decline"} {
		resp, _ = admin.postJSON("/admin/api/keep-requests/abc/"+action, nil)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, action)
	}
}

func TestAdminSchedulerEndpoints(t *testing.T) {
	f := newFixture(t)
	admin := f.loggedIn("admin")

	resp, body := admin.get("/admin/api/scheduler/jobs")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	jobs := body["jobs"].(map[string]any)
	require.Contains(t, jobs, "cleanup")
	require.Contains(t, jobs, "clear_image_cache")

	resp, _ = admin.postJSON("/admin/api/scheduler/jobs/cleanup/disable", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_, body = admin.get("/admin/api/scheduler/jobs")
	require.Equal(t, false, body["jobs"].(map[string]any)["cleanup"].(map[string]any)["enabled"])

	resp, _ = admin.postJSON("/admin/api/scheduler/jobs/cleanup/enable", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_, body = admin.get("/admin/api/scheduler/jobs")
	require.Equal(t, true, body["jobs"].(map[string]any)["cleanup"].(map[string]any)["enabled"])

	for _, action := range []string{"run", "enable", "disable"} {
		resp, body = admin.postJSON("/admin/api/scheduler/jobs/nope/"+action, nil)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, action)
		require.Equal(t, false, body["success"])
	}
}

func TestAdminCacheEndpoints(t *testing.T) {
	f := newFixture(t)
	admin := f.loggedIn("admin")

	resp, body := admin.get("/admin/api/scheduler/cache/stats")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, true, body["success"])

	resp, body = admin.postJSON("/admin/api/scheduler/cache/clear", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, true, body["success"])
}

func TestAdminUserEndpoints(t *testing.T) {
	f := newFixture(t)
	f.loggedIn("alice")
	admin := f.loggedIn("admin")

	resp, body := admin.get("/admin/api/users")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	users := body["users"].([]any)
	require.Len(t, users, 2)

	var aliceID string
	for _, u := range users {
		user := u.(map[string]any)
		if user["username"] == "alice" {
			require.Equal(t, false, user["hasAutoApproval"])
			aliceID = strconv.Itoa(int(user["id"].(float64)))
		}
	}
	require.NotEmpty(t, aliceID)

	resp, _ = admin.putJSON("/admin/api/users/"+aliceID+"/permissions", map[string]any{"hasAutoApproval": true})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_, body = admin.get("/admin/api/users")
	for _, u := range body["users"].([]any) {
		if user := u.(map[string]any); user["username"] == "alice" {
			require.Equal(t, true, user["hasAutoApproval"])
		}
	}

	resp, _ = admin.putJSON("/admin/api/users/abc/permissions", map[string]any{"hasAutoApproval": true})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp, _ = admin.putJSON("/admin/api/users/999/permissions", map[string]any{"hasAutoApproval": true})
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, f.server.URL+"/admin/api/users/"+aliceID+"/permissions", strings.NewReader("nope"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, _ = admin.do(req)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAdminHistoryEndpoint(t *testing.T) {
	f := newFixture(t)
	media := f.createMedia(t, "Movie", 2001)
	other := f.createMedia(t, "Other", 2002)
	alice := f.loggedIn("alice")
	admin := f.loggedIn("admin")

	// Generate a few events over the real routes.
	resp, _ := alice.postJSON("/api/media/"+mediaPath(media.ID)+"/request-keep", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp, _ = admin.postJSON("/admin/api/keep-requests/"+mediaPath(media.ID)+"/accept", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp, _ = admin.postJSON("/admin/api/media/"+mediaPath(other.ID)+"/delete", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, body := admin.get("/admin/api/history")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	data := body["data"].(map[string]any)
	require.EqualValues(t, 4, data["total"], "request_created, request_approved, protected, admin_unkeep")
	require.EqualValues(t, 1, data["page"])
	require.EqualValues(t, 50, data["pageSize"])
	require.EqualValues(t, 1, data["totalPages"])

	resp, body = admin.get("/admin/api/history?page=2&pageSize=3")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	data = body["data"].(map[string]any)
	require.Len(t, data["items"], 1)
	require.EqualValues(t, 2, data["totalPages"])

	resp, body = admin.get("/admin/api/history?includeEventTypes=request_approved,%20admin_unkeep")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 2, body["data"].(map[string]any)["total"])

	resp, body = admin.get("/admin/api/history?jellyfinId=" + other.JellyfinID)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	data = body["data"].(map[string]any)
	require.EqualValues(t, 1, data["total"])
	require.Equal(t, "admin_unkeep", data["items"].([]any)[0].(map[string]any)["EventType"])
	require.Equal(t, "admin", data["items"].([]any)[0].(map[string]any)["Username"])

	for _, query := range []string{"page=0", "page=abc", "pageSize=0", "pageSize=101", "pageSize=x"} {
		resp, _ = admin.get("/admin/api/history?" + query)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, query)
	}
}

// --- webpush ---

func TestWebPushEndpoints(t *testing.T) {
	f := newFixture(t)
	alice := f.loggedIn("alice")

	resp, body := alice.get("/api/webpush/vapid-key")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, f.cfg.WebPush.PublicKey, body["publicKey"])

	resp, body = alice.get("/api/webpush/status")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, false, body["subscribed"])

	subscribe := func(username, endpoint, p256dh, auth string) (*http.Response, map[string]any) {
		return alice.postJSON("/api/webpush/subscribe", map[string]any{
			"username": username,
			"subscription": map[string]any{
				"endpoint": endpoint,
				"keys":     map[string]string{"p256dh": p256dh, "auth": auth},
			},
		})
	}

	resp, _ = subscribe("alice", "", "k", "a")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "endpoint is required")
	resp, _ = subscribe("alice", "https://push/1", "", "a")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "keys are required")
	resp, _ = subscribe("", "https://push/1", "k", "a")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "username is required")
	resp, _ = subscribe("bob", "https://push/1", "k", "a")
	require.Equal(t, http.StatusForbidden, resp.StatusCode, "cannot subscribe on behalf of another user")
	resp, _ = alice.postRaw("/api/webpush/subscribe", "nope")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp, _ = subscribe("alice", "https://push/1", "k", "a")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, body = alice.get("/api/webpush/status?endpoint=https://push/1")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, true, body["subscribed"])
	require.EqualValues(t, 1, body["count"])

	resp, body = alice.get("/api/webpush/status?endpoint=https://push/unknown")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, false, body["subscribed"])

	resp, _ = alice.postJSON("/api/webpush/unsubscribe", map[string]any{"endpoint": ""})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp, _ = alice.postRaw("/api/webpush/unsubscribe", "nope")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp, _ = alice.postJSON("/api/webpush/unsubscribe", map[string]any{"endpoint": "https://push/1"})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, body = alice.get("/api/webpush/status?endpoint=https://push/1")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, false, body["subscribed"])
}

// --- error branches ---

func TestDatabaseFailuresSurfaceAsServerErrors(t *testing.T) {
	f := newFixture(t)
	f.createMedia(t, "Movie", 2001)
	alice := f.loggedIn("alice")
	admin := f.loggedIn("admin")
	plugin := f.anonymous()

	f.breakDB()

	for _, path := range []string{"/api/media"} {
		resp, body := alice.get(path)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode, path)
		require.Equal(t, false, body["success"])
	}
	for _, path := range []string{"/admin/api/media", "/admin/api/keep-requests", "/admin/api/users", "/admin/api/history", "/admin/api/history?jellyfinId=x"} {
		resp, body := admin.get(path)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode, path)
		require.Equal(t, false, body["success"])
	}

	resp, _ := alice.postJSON("/api/media/1/request-keep", nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp, _ = admin.postJSON("/admin/api/media/1/keep", nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp, _ = admin.putJSON("/admin/api/users/1/permissions", map[string]any{"hasAutoApproval": true})
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	resp, _ = plugin.postJSON("/plugin/check", map[string]any{"name": "Movie", "media_type": "movie"}, "X-API-Key", pluginAPIKey)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	// A broken database must not break login into a 500 that leaks details.
	_, resp = f.login("carol", correctPassword)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// --- pages, images, server lifecycle ---

func (c *client) getHTML(path string) (*http.Response, string) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(c.t.Context(), http.MethodGet, c.base+path, nil)
	require.NoError(c.t, err)
	resp, err := c.http.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)
	return resp, string(raw)
}

func TestLoginPage(t *testing.T) {
	f := newFixture(t)

	resp, html := f.anonymous().getHTML("/login")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	require.Contains(t, strings.ToLower(html), "<form")

	resp, _ = f.loggedIn("alice").getHTML("/login")
	require.Equal(t, http.StatusFound, resp.StatusCode, "a logged-in user is sent to the dashboard")
	require.Equal(t, "/", resp.Header.Get("Location"))
}

func TestDashboardPage(t *testing.T) {
	f := newFixture(t)
	media := f.createMedia(t, "Dashboard Movie", 2001)
	alice := f.loggedIn("alice")
	admin := f.loggedIn("admin")

	resp, html := alice.getHTML("/")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	require.Contains(t, html, "Dashboard Movie")
	require.Contains(t, html, "alice")

	// Admins see the pending-request indicator once a request exists.
	resp, _ = alice.postJSON("/api/media/"+mediaPath(media.ID)+"/request-keep", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp, html = admin.getHTML("/")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, html, "Dashboard Movie")
}

func TestAdminPages(t *testing.T) {
	f := newFixture(t)
	f.createMedia(t, "Admin Movie", 2001)
	admin := f.loggedIn("admin")

	for _, path := range []string{"/admin", "/admin/", "/admin/scheduler", "/admin/history"} {
		resp, html := admin.getHTML(path)
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		require.Contains(t, resp.Header.Get("Content-Type"), "text/html", path)
		require.NotEmpty(t, html, path)
	}
	_, html := admin.getHTML("/admin")
	require.Contains(t, html, "Admin Movie")
	_, html = admin.getHTML("/admin/scheduler")
	require.Contains(t, html, "cleanup")
}

func TestPagesWithBrokenDatabase(t *testing.T) {
	f := newFixture(t)
	alice := f.loggedIn("alice")
	admin := f.loggedIn("admin")
	f.breakDB()

	resp, _ := alice.getHTML("/")
	require.Equal(t, http.StatusOK, resp.StatusCode, "the dashboard degrades to an empty list")
	resp, _ = admin.getHTML("/")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp, _ = admin.getHTML("/admin")
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	resp, _ = admin.getHTML("/admin/history")
	require.Equal(t, http.StatusOK, resp.StatusCode, "the history page loads its data via the API")
}

func TestImageCacheRoute(t *testing.T) {
	f := newFixture(t)
	alice := f.loggedIn("alice")

	resp, _ := alice.get("/api/images/cache")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp, _ = alice.get("/api/images/cache?id=abc")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	media := f.createMedia(t, "No Poster", 2001)
	resp, _ = alice.get("/api/images/cache?id=" + mediaPath(media.ID))
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp, _ = alice.get("/api/images/cache?id=999")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestRunServesUntilContextIsCancelled(t *testing.T) {
	f := newFixture(t)
	// A second server instance on a random port, driven through Run.
	eng, err := engine.New(f.cfg, f.db, false)
	require.NoError(t, err)
	t.Cleanup(func() { eng.Close() }) //nolint:errcheck
	srv, err := api.New(t.Context(), f.cfg, f.db, eng, true)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	time.Sleep(100 * time.Millisecond) // let ListenAndServe bind
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "a cancelled context is a clean shutdown")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}
}

func TestRunFailsOnUnusableListenAddress(t *testing.T) {
	f := newFixture(t)
	cfg := *f.cfg
	cfg.Listen = "256.256.256.256:1"
	eng, err := engine.New(&cfg, f.db, false)
	require.NoError(t, err)
	t.Cleanup(func() { eng.Close() }) //nolint:errcheck
	srv, err := api.New(t.Context(), &cfg, f.db, eng, true)
	require.NoError(t, err)

	require.Error(t, srv.Run(t.Context()))
}
