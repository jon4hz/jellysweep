package sonarr

import (
	"net/http"
	"testing"
	"time"

	sonarrAPI "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/cache"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/httptestutil"
	jellyfinAPI "github.com/sj14/jellyfin-go/api"
	"github.com/stretchr/testify/require"
)

const testAPIKey = "sonarr-test-key"

func newTestSonarr(t *testing.T, cfgOpts ...func(*config.Config)) (*Sonarr, *httptestutil.Server) {
	t.Helper()
	server := httptestutil.New(t)
	cfg := &config.Config{
		Sonarr: &config.SonarrConfig{URL: server.URL, APIKey: testAPIKey},
		Libraries: map[string]*config.CleanupConfig{
			"TV": {Enabled: true},
		},
	}
	for _, opt := range cfgOpts {
		opt(cfg)
	}
	engineCache, err := cache.NewEngineCache(&config.CacheConfig{Type: config.CacheTypeMemory})
	require.NoError(t, err)
	return NewSonarr(cfg, nil, engineCache.SonarrTagsCache), server
}

func series(id int32, title string, year, tvdb, tmdb int32, tags ...int32) sonarrAPI.SeriesResource {
	s := sonarrAPI.SeriesResource{}
	s.SetId(id)
	s.SetTitle(title)
	s.SetYear(year)
	s.SetTvdbId(tvdb)
	s.SetTmdbId(tmdb)
	s.SetTags(tags)
	return s
}

func tag(id int32, label string) sonarrAPI.TagResource {
	t := sonarrAPI.TagResource{}
	t.SetId(id)
	t.SetLabel(label)
	return t
}

func jellyfinSeries(id, name string, year int32, providerIDs map[string]string) arr.JellyfinItem {
	dto := jellyfinAPI.NewBaseItemDto()
	dto.SetId(id)
	dto.SetName(name)
	dto.SetType(jellyfinAPI.BASEITEMKIND_SERIES)
	dto.SetProductionYear(year)
	if providerIDs != nil {
		dto.SetProviderIds(providerIDs)
	}
	return arr.JellyfinItem{BaseItemDto: *dto, ParentLibraryName: "TV"}
}

func episode(id, season, number, fileID int32, hasFile bool, aired time.Time) sonarrAPI.EpisodeResource {
	e := sonarrAPI.EpisodeResource{}
	e.SetId(id)
	e.SetSeasonNumber(season)
	e.SetEpisodeNumber(number)
	e.SetHasFile(hasFile)
	if hasFile {
		e.SetEpisodeFileId(fileID)
	}
	e.SetAirDateUtc(aired)
	return e
}

func episodeFile(id int32) sonarrAPI.EpisodeFileResource {
	f := sonarrAPI.EpisodeFileResource{}
	f.SetId(id)
	return f
}

func TestGetItemsMatchesByProviderIDsAndTitle(t *testing.T) {
	s, server := newTestSonarr(t)
	server.JSON("GET /api/v3/tag", []sonarrAPI.TagResource{tag(1, "jellysweep-ignore"), tag(2, "4k")})
	server.JSON("GET /api/v3/series", []sonarrAPI.SeriesResource{
		series(10, "By TVDB", 2001, 111, 0, 1),
		series(11, "By TMDB", 2002, 0, 222),
		series(12, "By Title", 2003, 0, 0, 2),
		series(13, "Unrelated", 2004, 999, 0),
	})

	items, err := s.GetItems(t.Context(), []arr.JellyfinItem{
		jellyfinSeries("jf-1", "Different Name", 2001, map[string]string{"Tvdb": "111"}),
		jellyfinSeries("jf-2", "Different Name", 2002, map[string]string{"Tmdb": "222"}),
		jellyfinSeries("jf-3", "by title", 2003, nil),
		jellyfinSeries("jf-4", "No Match", 1999, map[string]string{"Tvdb": "42"}),
		{BaseItemDto: func() jellyfinAPI.BaseItemDto { // a movie must be ignored
			d := jellyfinAPI.NewBaseItemDto()
			d.SetId("jf-5")
			d.SetType(jellyfinAPI.BASEITEMKIND_MOVIE)
			return *d
		}(), ParentLibraryName: "TV"},
	})
	require.NoError(t, err)
	require.Len(t, items, 3)

	byJellyfinID := map[string]arr.MediaItem{}
	for _, item := range items {
		byJellyfinID[item.JellyfinID] = item
	}
	seriesID := func(jellyfinID string) int32 {
		sr := byJellyfinID[jellyfinID].SeriesResource
		return sr.GetId()
	}
	require.Equal(t, int32(10), seriesID("jf-1"), "TVDB id is the primary match")
	require.Equal(t, []string{"jellysweep-ignore"}, byJellyfinID["jf-1"].Tags, "tag ids must be resolved to labels")
	require.Equal(t, int32(11), seriesID("jf-2"), "TMDB id is the fallback")
	require.Equal(t, int32(12), seriesID("jf-3"), "title+year is the last resort, case-insensitive")
	require.Equal(t, "TV", byJellyfinID["jf-1"].LibraryName)

	// The API key must be sent on every request.
	for _, r := range server.Requests("", "") {
		require.Equal(t, testAPIKey, r.Header.Get("X-Api-Key"))
	}
}

func TestGetItemsSkipsItemsWithoutLibrary(t *testing.T) {
	s, server := newTestSonarr(t)
	server.JSON("GET /api/v3/tag", []sonarrAPI.TagResource{})
	server.JSON("GET /api/v3/series", []sonarrAPI.SeriesResource{series(10, "Show", 2001, 111, 0)})

	item := jellyfinSeries("jf-1", "Show", 2001, map[string]string{"Tvdb": "111"})
	item.ParentLibraryName = ""
	items, err := s.GetItems(t.Context(), []arr.JellyfinItem{item})
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestDeleteMediaModeAllSendsDeleteFiles(t *testing.T) {
	// The single most dangerous call in the app: make sure it deletes the
	// files on disk, not just the Sonarr entry.
	s, server := newTestSonarr(t, func(c *config.Config) { c.CleanupMode = config.CleanupModeAll })
	server.OK("DELETE /api/v3/series/{id}")

	require.NoError(t, s.DeleteMedia(t.Context(), 42, "Show"))

	deletes := server.Requests(http.MethodDelete, "/api/v3/series/42")
	require.Len(t, deletes, 1)
	require.Equal(t, "true", deletes[0].Query.Get("deleteFiles"))
}

func TestDeleteMediaDryRunSendsNothing(t *testing.T) {
	s, server := newTestSonarr(t, func(c *config.Config) { c.DryRun = true })
	require.NoError(t, s.DeleteMedia(t.Context(), 42, "Show"))
	require.Empty(t, server.Requests("", ""), "dry run must not talk to Sonarr at all")
}

func TestDeleteMediaKeepEpisodes(t *testing.T) {
	s, server := newTestSonarr(t, func(c *config.Config) {
		c.CleanupMode = config.CleanupModeKeepEpisodes
		c.KeepCount = 1
	})
	aired := time.Now().Add(-30 * 24 * time.Hour)
	server.JSON("GET /api/v3/episode", []sonarrAPI.EpisodeResource{
		episode(100, 0, 1, 1000, true, aired),                      // special: always kept
		episode(101, 1, 1, 1001, true, aired),                      // first regular episode: kept
		episode(102, 1, 2, 1002, true, aired),                      // deleted
		episode(103, 2, 1, 1003, true, aired),                      // deleted
		episode(104, 2, 2, 0, false, time.Now().Add(24*time.Hour)), // unaired, no file
	})
	server.JSON("GET /api/v3/episodefile", []sonarrAPI.EpisodeFileResource{
		episodeFile(1000), episodeFile(1001), episodeFile(1002), episodeFile(1003),
	})
	server.OK("DELETE /api/v3/episodefile/{id}")
	server.OK("PUT /api/v3/episode/monitor")

	require.NoError(t, s.DeleteMedia(t.Context(), 42, "Show"))

	require.Empty(t, server.Requests(http.MethodDelete, "/api/v3/series/42"), "keep mode must never delete the series")
	var deletedFiles []string
	for _, r := range server.Requests(http.MethodDelete, "") {
		deletedFiles = append(deletedFiles, r.Path)
	}
	require.ElementsMatch(t, []string{"/api/v3/episodefile/1002", "/api/v3/episodefile/1003"}, deletedFiles)

	// Deleted, aired episodes are unmonitored; the unaired one is left alone.
	monitorCalls := server.Requests(http.MethodPut, "/api/v3/episode/monitor")
	require.Len(t, monitorCalls, 1)
	var body sonarrAPI.EpisodesMonitoredResource
	monitorCalls[0].JSONBody(t, &body)
	require.ElementsMatch(t, []int32{102, 103}, body.GetEpisodeIds())
	require.False(t, body.GetMonitored())
}

func TestDeleteMediaKeepSeasons(t *testing.T) {
	s, server := newTestSonarr(t, func(c *config.Config) {
		c.CleanupMode = config.CleanupModeKeepSeasons
		c.KeepCount = 1
	})
	aired := time.Now().Add(-30 * 24 * time.Hour)
	server.JSON("GET /api/v3/episode", []sonarrAPI.EpisodeResource{
		episode(100, 0, 1, 1000, true, aired),
		episode(101, 1, 1, 1001, true, aired),
		episode(102, 1, 2, 1002, true, aired),
		episode(103, 2, 1, 1003, true, aired),
	})
	server.JSON("GET /api/v3/episodefile", []sonarrAPI.EpisodeFileResource{
		episodeFile(1000), episodeFile(1001), episodeFile(1002), episodeFile(1003),
	})
	server.OK("DELETE /api/v3/episodefile/{id}")
	server.OK("PUT /api/v3/episode/monitor")

	require.NoError(t, s.DeleteMedia(t.Context(), 42, "Show"))

	var deletedFiles []string
	for _, r := range server.Requests(http.MethodDelete, "") {
		deletedFiles = append(deletedFiles, r.Path)
	}
	require.Equal(t, []string{"/api/v3/episodefile/1003"}, deletedFiles, "only season 2 is removed; specials and season 1 stay")
}

func TestDeleteMediaKeepEpisodesNothingToDelete(t *testing.T) {
	s, server := newTestSonarr(t, func(c *config.Config) {
		c.CleanupMode = config.CleanupModeKeepEpisodes
		c.KeepCount = 5
	})
	aired := time.Now().Add(-30 * 24 * time.Hour)
	server.JSON("GET /api/v3/episode", []sonarrAPI.EpisodeResource{episode(101, 1, 1, 1001, true, aired)})
	server.JSON("GET /api/v3/episodefile", []sonarrAPI.EpisodeFileResource{episodeFile(1001)})

	require.NoError(t, s.DeleteMedia(t.Context(), 42, "Show"))
	require.Empty(t, server.Requests(http.MethodDelete, ""))
	require.Empty(t, server.Requests(http.MethodPut, ""))
}

func TestUnmonitorMedia(t *testing.T) {
	s, server := newTestSonarr(t)
	aired := time.Now().Add(-30 * 24 * time.Hour)
	server.JSON("GET /api/v3/episode", []sonarrAPI.EpisodeResource{
		episode(101, 1, 1, 1001, true, aired),
		episode(102, 1, 2, 0, false, aired),
	})
	server.OK("PUT /api/v3/episode/monitor")

	require.NoError(t, s.UnmonitorMedia(t.Context(), 42, "Show"))

	calls := server.Requests(http.MethodPut, "/api/v3/episode/monitor")
	require.Len(t, calls, 1)
	require.Equal(t, "42", server.Requests(http.MethodGet, "/api/v3/episode")[0].Query.Get("seriesId"))
	var body sonarrAPI.EpisodesMonitoredResource
	calls[0].JSONBody(t, &body)
	require.Equal(t, []int32{101}, body.GetEpisodeIds(), "only episodes with files are unmonitored")
	require.False(t, body.GetMonitored())
}

func TestResetTagsRemovesOnlyJellysweepTags(t *testing.T) {
	s, server := newTestSonarr(t)
	server.JSON("GET /api/v3/tag", []sonarrAPI.TagResource{
		tag(1, "jellysweep-delete-2025-01-01"), tag(2, "4k"), tag(3, "extra"),
	})
	server.JSON("GET /api/v3/series", []sonarrAPI.SeriesResource{
		series(10, "Tagged", 2001, 111, 0, 1, 2, 3),
		series(11, "Clean", 2002, 222, 0, 2),
	})
	server.Handle("PUT /api/v3/series/{id}", func(w http.ResponseWriter, _ *http.Request) {
		httptestutil.WriteJSON(t, w, sonarrAPI.SeriesResource{})
	})

	require.NoError(t, s.ResetTags(t.Context(), []string{"extra"}))

	updates := server.Requests(http.MethodPut, "")
	require.Len(t, updates, 1, "untouched series must not be updated")
	require.Equal(t, "/api/v3/series/10", updates[0].Path)
	var body sonarrAPI.SeriesResource
	updates[0].JSONBody(t, &body)
	require.Equal(t, []int32{2}, body.GetTags(), "jellysweep and additional tags removed, others kept")
}

func TestCleanupAllTagsDeletesOnlyJellysweepTags(t *testing.T) {
	s, server := newTestSonarr(t)
	detail := func(id int32, label string) sonarrAPI.TagDetailsResource {
		d := sonarrAPI.TagDetailsResource{}
		d.SetId(id)
		d.SetLabel(label)
		return d
	}
	server.JSON("GET /api/v3/tag/detail", []sonarrAPI.TagDetailsResource{
		detail(1, "jellysweep-delete-2025-01-01"),
		detail(2, "4k"),
		detail(3, "jellysweep-ignore"),
	})
	server.OK("DELETE /api/v3/tag/{id}")

	require.NoError(t, s.CleanupAllTags(t.Context(), nil))

	var deleted []string
	for _, r := range server.Requests(http.MethodDelete, "") {
		deleted = append(deleted, r.Path)
	}
	require.Equal(t, []string{"/api/v3/tag/1"}, deleted, "the ignore tag and user tags must survive")
}

func TestResetAllTagsAndAddIgnoreCreatesTagWhenMissing(t *testing.T) {
	s, server := newTestSonarr(t)
	server.JSON("GET /api/v3/series/{id}", series(10, "Show", 2001, 111, 0, 1, 2))
	server.JSON("GET /api/v3/tag", []sonarrAPI.TagResource{tag(1, "jellysweep-delete-2025-01-01"), tag(2, "4k")})
	server.JSON("POST /api/v3/tag", tag(7, "jellysweep-ignore"))
	server.Handle("PUT /api/v3/series/{id}", func(w http.ResponseWriter, _ *http.Request) {
		httptestutil.WriteJSON(t, w, sonarrAPI.SeriesResource{})
	})

	require.NoError(t, s.ResetAllTagsAndAddIgnore(t.Context(), 10))

	creates := server.Requests(http.MethodPost, "/api/v3/tag")
	require.Len(t, creates, 1)
	var created sonarrAPI.TagResource
	creates[0].JSONBody(t, &created)
	require.Equal(t, "jellysweep-ignore", created.GetLabel())

	updates := server.Requests(http.MethodPut, "/api/v3/series/10")
	require.Len(t, updates, 1)
	var body sonarrAPI.SeriesResource
	updates[0].JSONBody(t, &body)
	require.ElementsMatch(t, []int32{2, 7}, body.GetTags(), "jellysweep tag removed, ignore tag added, user tag kept")
}

func TestGetItemAddedDateFindsEarliestImportAfterSince(t *testing.T) {
	s, server := newTestSonarr(t)
	history := func(eventType sonarrAPI.EpisodeHistoryEventType, date time.Time) sonarrAPI.HistoryResource {
		h := sonarrAPI.HistoryResource{}
		h.SetEventType(eventType)
		h.SetDate(date)
		return h
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	page := sonarrAPI.HistoryResourcePagingResource{}
	page.SetTotalRecords(4)
	page.SetRecords([]sonarrAPI.HistoryResource{
		history(sonarrAPI.EPISODEHISTORYEVENTTYPE_GRABBED, base.Add(-48*time.Hour)),                  // not an import
		history(sonarrAPI.EPISODEHISTORYEVENTTYPE_DOWNLOAD_FOLDER_IMPORTED, base.Add(-24*time.Hour)), // before since
		history(sonarrAPI.EPISODEHISTORYEVENTTYPE_DOWNLOAD_FOLDER_IMPORTED, base.Add(72*time.Hour)),
		history(sonarrAPI.EPISODEHISTORYEVENTTYPE_SERIES_FOLDER_IMPORTED, base.Add(24*time.Hour)),
	})
	server.JSON("GET /api/v3/history", page)

	got, err := s.GetItemAddedDate(t.Context(), 42, base)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, base.Add(24*time.Hour), got.UTC(), "earliest import after the last deletion wins")
	require.Equal(t, "42", server.Requests(http.MethodGet, "/api/v3/history")[0].Query.Get("seriesIds"))
}

func TestGetItemAddedDateIgnoresImportsBeforeSince(t *testing.T) {
	// Regression: the first import record used to be accepted without checking
	// `since`, so a pre-deletion import made re-downloaded content look old.
	s, server := newTestSonarr(t)
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	old := sonarrAPI.HistoryResource{}
	old.SetEventType(sonarrAPI.EPISODEHISTORYEVENTTYPE_DOWNLOAD_FOLDER_IMPORTED)
	old.SetDate(base.Add(-24 * time.Hour))
	page := sonarrAPI.HistoryResourcePagingResource{}
	page.SetTotalRecords(1)
	page.SetRecords([]sonarrAPI.HistoryResource{old})
	server.JSON("GET /api/v3/history", page)

	got, err := s.GetItemAddedDate(t.Context(), 42, base)
	require.NoError(t, err)
	require.Nil(t, got, "imports before the last deletion must not count")
}

func TestGetItemAddedDateNoHistory(t *testing.T) {
	s, server := newTestSonarr(t)
	page := sonarrAPI.HistoryResourcePagingResource{}
	page.SetTotalRecords(0)
	server.JSON("GET /api/v3/history", page)

	got, err := s.GetItemAddedDate(t.Context(), 42, time.Time{})
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetItemAddedDatePaginates(t *testing.T) {
	s, server := newTestSonarr(t)
	imported := func(date time.Time) sonarrAPI.HistoryResource {
		h := sonarrAPI.HistoryResource{}
		h.SetEventType(sonarrAPI.EPISODEHISTORYEVENTTYPE_DOWNLOAD_FOLDER_IMPORTED)
		h.SetDate(date)
		return h
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	server.Handle("GET /api/v3/history", func(w http.ResponseWriter, r *http.Request) {
		page := sonarrAPI.HistoryResourcePagingResource{}
		page.SetTotalRecords(2)
		switch r.URL.Query().Get("page") {
		case "1":
			page.SetRecords([]sonarrAPI.HistoryResource{imported(base.Add(48 * time.Hour))})
		case "2":
			page.SetRecords([]sonarrAPI.HistoryResource{imported(base.Add(24 * time.Hour))})
		}
		httptestutil.WriteJSON(t, w, page)
	})

	got, err := s.GetItemAddedDate(t.Context(), 42, time.Time{})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, base.Add(24*time.Hour), got.UTC(), "the older import on page 2 must be found")
	require.Len(t, server.Requests(http.MethodGet, "/api/v3/history"), 2)
}

func TestAPIErrorsPropagate(t *testing.T) {
	s, server := newTestSonarr(t)
	server.Handle("GET /api/v3/tag", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := s.GetItems(t.Context(), nil)
	require.Error(t, err)
}
