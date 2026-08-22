package radarr

import (
	"net/http"
	"sync"
	"testing"
	"time"

	radarrAPI "github.com/devopsarr/radarr-go/radarr"
	"github.com/jon4hz/jellysweep/internal/cache"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/httptestutil"
	jellyfinAPI "github.com/sj14/jellyfin-go/api"
	"github.com/stretchr/testify/require"
)

const testAPIKey = "radarr-test-key"

func newTestRadarr(t *testing.T, cfgOpts ...func(*config.Config)) (*Radarr, *httptestutil.Server) {
	t.Helper()
	server := httptestutil.New(t)
	cfg := &config.Config{
		Radarr: &config.RadarrConfig{URL: server.URL, APIKey: testAPIKey},
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {Enabled: true},
		},
	}
	for _, opt := range cfgOpts {
		opt(cfg)
	}
	engineCache, err := cache.NewEngineCache(&config.CacheConfig{Type: config.CacheTypeMemory})
	require.NoError(t, err)
	return NewRadarr(cfg, nil, engineCache.RadarrTagsCache), server
}

func movie(id int32, title string, year, tmdb int32, tags ...int32) radarrAPI.MovieResource {
	m := radarrAPI.MovieResource{}
	m.SetId(id)
	m.SetTitle(title)
	m.SetYear(year)
	m.SetTmdbId(tmdb)
	m.SetTags(tags)
	return m
}

func tag(id int32, label string) radarrAPI.TagResource {
	t := radarrAPI.TagResource{}
	t.SetId(id)
	t.SetLabel(label)
	return t
}

func jellyfinMovie(id, name string, year int32, providerIDs map[string]string) arr.JellyfinItem {
	dto := jellyfinAPI.NewBaseItemDto()
	dto.SetId(id)
	dto.SetName(name)
	dto.SetType(jellyfinAPI.BASEITEMKIND_MOVIE)
	dto.SetProductionYear(year)
	if providerIDs != nil {
		dto.SetProviderIds(providerIDs)
	}
	return arr.JellyfinItem{BaseItemDto: *dto, ParentLibraryName: "Movies"}
}

func TestGetItemsMatchesByTmdbAndTitle(t *testing.T) {
	r, server := newTestRadarr(t)
	server.JSON("GET /api/v3/tag", []radarrAPI.TagResource{tag(1, "jellysweep-ignore")})
	server.JSON("GET /api/v3/movie", []radarrAPI.MovieResource{
		movie(10, "By TMDB", 2001, 111, 1),
		movie(11, "By Title", 2002, 0),
		movie(12, "Unrelated", 2003, 999),
	})

	seriesDto := jellyfinAPI.NewBaseItemDto()
	seriesDto.SetId("jf-series")
	seriesDto.SetType(jellyfinAPI.BASEITEMKIND_SERIES)

	items, err := r.GetItems(t.Context(), []arr.JellyfinItem{
		jellyfinMovie("jf-1", "Other Name", 2001, map[string]string{"Tmdb": "111"}),
		jellyfinMovie("jf-2", "BY TITLE", 2002, nil),
		jellyfinMovie("jf-3", "No Match", 1999, map[string]string{"Tmdb": "5"}),
		{BaseItemDto: *seriesDto, ParentLibraryName: "Movies"},
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, int32(10), items[0].MovieResource.GetId())
	require.Equal(t, []string{"jellysweep-ignore"}, items[0].Tags)
	require.Equal(t, int32(11), items[1].MovieResource.GetId())

	for _, req := range server.Requests("", "") {
		require.Equal(t, testAPIKey, req.Header.Get("X-Api-Key"))
	}
}

func TestDeleteMediaSendsDeleteFiles(t *testing.T) {
	r, server := newTestRadarr(t)
	server.OK("DELETE /api/v3/movie/{id}")

	require.NoError(t, r.DeleteMedia(t.Context(), 42, "Movie"))

	deletes := server.Requests(http.MethodDelete, "/api/v3/movie/42")
	require.Len(t, deletes, 1)
	require.Equal(t, "true", deletes[0].Query.Get("deleteFiles"), "files on disk must be deleted, not just the entry")
}

func TestDeleteMediaDryRunSendsNothing(t *testing.T) {
	r, server := newTestRadarr(t, func(c *config.Config) { c.DryRun = true })
	require.NoError(t, r.DeleteMedia(t.Context(), 42, "Movie"))
	require.Empty(t, server.Requests("", ""))
}

func TestDeleteMediaErrorPropagates(t *testing.T) {
	r, server := newTestRadarr(t)
	server.Handle("DELETE /api/v3/movie/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	require.Error(t, r.DeleteMedia(t.Context(), 42, "Movie"))
}

func TestUnmonitorMedia(t *testing.T) {
	r, server := newTestRadarr(t)
	server.OK("PUT /api/v3/movie/editor")

	require.NoError(t, r.UnmonitorMedia(t.Context(), 42, "Movie"))

	calls := server.Requests(http.MethodPut, "/api/v3/movie/editor")
	require.Len(t, calls, 1)
	var body radarrAPI.MovieEditorResource
	calls[0].JSONBody(t, &body)
	require.Equal(t, []int32{42}, body.GetMovieIds())
	require.False(t, body.GetMonitored())
}

func TestUnmonitorMediaDryRun(t *testing.T) {
	r, server := newTestRadarr(t, func(c *config.Config) { c.DryRun = true })
	require.NoError(t, r.UnmonitorMedia(t.Context(), 42, "Movie"))
	require.Empty(t, server.Requests("", ""))
}

func TestResetTagsRemovesOnlyJellysweepTags(t *testing.T) {
	r, server := newTestRadarr(t)
	server.JSON("GET /api/v3/tag", []radarrAPI.TagResource{tag(1, "jellysweep-delete-2025-01-01"), tag(2, "4k")})
	server.JSON("GET /api/v3/movie", []radarrAPI.MovieResource{
		movie(10, "Tagged", 2001, 111, 1, 2),
		movie(11, "Clean", 2002, 222, 2),
	})
	server.Handle("PUT /api/v3/movie/{id}", func(w http.ResponseWriter, _ *http.Request) {
		httptestutil.WriteJSON(t, w, radarrAPI.MovieResource{})
	})

	require.NoError(t, r.ResetTags(t.Context(), nil))

	updates := server.Requests(http.MethodPut, "")
	require.Len(t, updates, 1)
	require.Equal(t, "/api/v3/movie/10", updates[0].Path)
	var body radarrAPI.MovieResource
	updates[0].JSONBody(t, &body)
	require.Equal(t, []int32{2}, body.GetTags())
}

func TestCleanupAllTags(t *testing.T) {
	r, server := newTestRadarr(t)
	detail := func(id int32, label string) radarrAPI.TagDetailsResource {
		d := radarrAPI.TagDetailsResource{}
		d.SetId(id)
		d.SetLabel(label)
		return d
	}
	server.JSON("GET /api/v3/tag/detail", []radarrAPI.TagDetailsResource{
		detail(1, "jellysweep-must-delete-for-sure"),
		detail(2, "4k"),
		detail(3, "jellysweep-ignore"),
	})
	server.OK("DELETE /api/v3/tag/{id}")

	require.NoError(t, r.CleanupAllTags(t.Context(), nil))

	var deleted []string
	for _, req := range server.Requests(http.MethodDelete, "") {
		deleted = append(deleted, req.Path)
	}
	require.Equal(t, []string{"/api/v3/tag/1"}, deleted)
}

func TestResetAllTagsAndAddIgnoreReusesExistingTag(t *testing.T) {
	r, server := newTestRadarr(t)
	server.JSON("GET /api/v3/movie/{id}", movie(10, "Movie", 2001, 111, 1, 2))
	server.JSON("GET /api/v3/tag", []radarrAPI.TagResource{
		tag(1, "jellysweep-delete-2025-01-01"), tag(2, "4k"), tag(7, "jellysweep-ignore"),
	})
	server.Handle("PUT /api/v3/movie/{id}", func(w http.ResponseWriter, _ *http.Request) {
		httptestutil.WriteJSON(t, w, radarrAPI.MovieResource{})
	})

	require.NoError(t, r.ResetAllTagsAndAddIgnore(t.Context(), 10))

	require.Empty(t, server.Requests(http.MethodPost, "/api/v3/tag"), "existing ignore tag must be reused")
	updates := server.Requests(http.MethodPut, "/api/v3/movie/10")
	require.Len(t, updates, 1)
	var body radarrAPI.MovieResource
	updates[0].JSONBody(t, &body)
	require.ElementsMatch(t, []int32{2, 7}, body.GetTags())
}

func TestGetItemAddedDate(t *testing.T) {
	r, server := newTestRadarr(t)
	history := func(eventType radarrAPI.MovieHistoryEventType, date time.Time) radarrAPI.HistoryResource {
		h := radarrAPI.HistoryResource{}
		h.SetEventType(eventType)
		h.SetDate(date)
		return h
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	page := radarrAPI.HistoryResourcePagingResource{}
	page.SetTotalRecords(4)
	page.SetRecords([]radarrAPI.HistoryResource{
		history(radarrAPI.MOVIEHISTORYEVENTTYPE_GRABBED, base.Add(-time.Hour)),
		// An import from before the last deletion must not count: the item was
		// re-downloaded since, and only the new import reflects its age.
		history(radarrAPI.MOVIEHISTORYEVENTTYPE_DOWNLOAD_FOLDER_IMPORTED, base.Add(-24*time.Hour)),
		history(radarrAPI.MOVIEHISTORYEVENTTYPE_MOVIE_FOLDER_IMPORTED, base.Add(48*time.Hour)),
		history(radarrAPI.MOVIEHISTORYEVENTTYPE_DOWNLOAD_FOLDER_IMPORTED, base.Add(24*time.Hour)),
	})
	server.JSON("GET /api/v3/history", page)

	got, err := r.GetItemAddedDate(t.Context(), 42, base)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, base.Add(24*time.Hour), got.UTC(), "earliest import after the last deletion wins")
	require.Equal(t, "42", server.Requests(http.MethodGet, "/api/v3/history")[0].Query.Get("movieIds"))
}

func TestGetItemAddedDateNoImports(t *testing.T) {
	r, server := newTestRadarr(t)
	page := radarrAPI.HistoryResourcePagingResource{}
	page.SetTotalRecords(0)
	server.JSON("GET /api/v3/history", page)

	got, err := r.GetItemAddedDate(t.Context(), 42, time.Time{})
	require.NoError(t, err)
	require.Nil(t, got)
}

// tagServer registers a single /tag handler whose response can be swapped by
// the test (the mux does not allow re-registering a pattern).
func tagServer(t *testing.T, server *httptestutil.Server) (setTags func(...radarrAPI.TagResource), fail func()) {
	var (
		mu     sync.Mutex
		tags   []radarrAPI.TagResource
		status = http.StatusOK
	)
	server.Handle("GET /api/v3/tag", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		httptestutil.WriteJSON(t, w, tags)
	})
	setTags = func(t ...radarrAPI.TagResource) {
		mu.Lock()
		defer mu.Unlock()
		tags, status = t, http.StatusOK
	}
	fail = func() {
		mu.Lock()
		defer mu.Unlock()
		status = http.StatusBadGateway
	}
	return setTags, fail
}

func TestGetTagsCacheSemantics(t *testing.T) {
	r, server := newTestRadarr(t)
	setTags, _ := tagServer(t, server)
	setTags(tag(1, "old"))

	first, err := r.getTags(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, cache.TagMap{1: "old"}, first)
	require.Len(t, server.Requests(http.MethodGet, "/api/v3/tag"), 1)

	// Cached reads are served without an API call.
	cached, err := r.getTags(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, first, cached)
	require.Len(t, server.Requests(http.MethodGet, "/api/v3/tag"), 1)

	// A forced refresh bypasses the cache and replaces it.
	setTags(tag(1, "new"))
	fresh, err := r.getTags(t.Context(), true)
	require.NoError(t, err)
	require.Equal(t, cache.TagMap{1: "new"}, fresh)
	require.Len(t, server.Requests(http.MethodGet, "/api/v3/tag"), 2)

	cached, err = r.getTags(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, fresh, cached)
	require.Len(t, server.Requests(http.MethodGet, "/api/v3/tag"), 2)
}

func TestGetTagsFailedRefreshDropsStaleCache(t *testing.T) {
	r, server := newTestRadarr(t)
	setTags, fail := tagServer(t, server)
	setTags(tag(1, "old"))
	_, err := r.getTags(t.Context(), true)
	require.NoError(t, err)

	fail()
	_, err = r.getTags(t.Context(), true)
	require.Error(t, err)

	// The stale map must not be served to cached readers after a failed refresh.
	_, err = r.getTags(t.Context(), false)
	require.Error(t, err, "cached read must refetch, not serve the stale map")
	require.Len(t, server.Requests(http.MethodGet, "/api/v3/tag"), 3)
}
