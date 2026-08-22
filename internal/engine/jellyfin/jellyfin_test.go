package jellyfin

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/httptestutil"
	jellyfinAPI "github.com/sj14/jellyfin-go/api"
	"github.com/stretchr/testify/require"
)

const testAPIKey = "jellyfin-test-key"

func newTestClient(t *testing.T) (*Client, *httptestutil.Server) {
	t.Helper()
	server := httptestutil.New(t)
	cfg := &config.Config{
		Jellyfin: &config.JellyfinConfig{URL: server.URL, APIKey: testAPIKey},
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {Enabled: true},
			"TV":     {Enabled: true},
			"Music":  {Enabled: false},
		},
	}
	return New(cfg), server
}

func item(id, name string, kind jellyfinAPI.BaseItemKind) jellyfinAPI.BaseItemDto {
	dto := jellyfinAPI.NewBaseItemDto()
	dto.SetId(id)
	dto.SetName(name)
	dto.SetType(kind)
	return *dto
}

func queryResult(items []jellyfinAPI.BaseItemDto, total int32) jellyfinAPI.BaseItemDtoQueryResult {
	result := jellyfinAPI.NewBaseItemDtoQueryResult()
	result.SetItems(items)
	result.SetTotalRecordCount(total)
	return *result
}

func virtualFolder(name string, locations ...string) jellyfinAPI.VirtualFolderInfo {
	folder := jellyfinAPI.NewVirtualFolderInfo()
	folder.SetName(name)
	folder.SetLocations(locations)
	return *folder
}

func TestGetJellyfinItemsFiltersLibrariesAndPaginates(t *testing.T) {
	c, server := newTestClient(t)
	server.JSON("GET /Library/MediaFolders", queryResult([]jellyfinAPI.BaseItemDto{
		item("lib-movies", "Movies", jellyfinAPI.BASEITEMKIND_COLLECTION_FOLDER),
		item("lib-music", "Music", jellyfinAPI.BASEITEMKIND_COLLECTION_FOLDER),
		item("lib-unknown", "Photos", jellyfinAPI.BASEITEMKIND_COLLECTION_FOLDER),
	}, 3))
	server.JSON("GET /Library/VirtualFolders", []jellyfinAPI.VirtualFolderInfo{
		virtualFolder("Movies", "/data/movies", "/data/movies2"),
		virtualFolder("Music", "/data/music"),
	})
	// Movies library: 3 items served across two pages.
	server.Handle("GET /Items", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "lib-movies", r.URL.Query().Get("parentId"), "only enabled libraries may be queried")
		start, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
		page := []jellyfinAPI.BaseItemDto{
			item("m1", "Movie 1", jellyfinAPI.BASEITEMKIND_MOVIE),
			item("m2", "Movie 2", jellyfinAPI.BASEITEMKIND_MOVIE),
		}
		if start > 0 {
			page = []jellyfinAPI.BaseItemDto{item("m3", "Movie 3", jellyfinAPI.BASEITEMKIND_MOVIE)}
		}
		httptestutil.WriteJSON(t, w, queryResult(page, 3))
	})

	items, folders, err := c.GetJellyfinItems(t.Context())
	require.NoError(t, err)

	require.Len(t, items, 3, "both pages must be collected")
	for _, it := range items {
		require.Equal(t, "Movies", it.ParentLibraryName)
	}
	require.Equal(t, map[string][]string{"Movies": {"/data/movies", "/data/movies2"}}, folders,
		"disabled libraries must not appear in the folders map")

	itemCalls := server.Requests(http.MethodGet, "/Items")
	require.Len(t, itemCalls, 2, "pagination must stop once all records are fetched")
	require.Equal(t, "0", itemCalls[0].Query.Get("startIndex"))
	require.Equal(t, "2", itemCalls[1].Query.Get("startIndex"))

	for _, req := range server.Requests("", "") {
		require.Equal(t, `MediaBrowser Token="`+testAPIKey+`"`, req.Header.Get("Authorization"))
	}
}

func TestGetJellyfinItemsRequestsPagesOf100(t *testing.T) {
	c, server := newTestClient(t)
	server.JSON("GET /Library/MediaFolders", queryResult([]jellyfinAPI.BaseItemDto{
		item("lib-movies", "Movies", jellyfinAPI.BASEITEMKIND_COLLECTION_FOLDER),
	}, 1))
	server.JSON("GET /Library/VirtualFolders", []jellyfinAPI.VirtualFolderInfo{virtualFolder("Movies", "/data/movies")})
	server.JSON("GET /Items", queryResult([]jellyfinAPI.BaseItemDto{item("m1", "Movie 1", jellyfinAPI.BASEITEMKIND_MOVIE)}, 1))

	_, _, err := c.GetJellyfinItems(t.Context())
	require.NoError(t, err)

	itemCalls := server.Requests(http.MethodGet, "/Items")
	require.Len(t, itemCalls, 1)
	require.Equal(t, "100", itemCalls[0].Query.Get("limit"), "large pages time out on big libraries")
}

// A failed library fetch must abort the whole gather: returning the remaining
// libraries as if they were the complete set makes the engine purge every item
// of the failed library as "not found anymore".
func TestGetJellyfinItemsLibraryFailureAbortsGather(t *testing.T) {
	c, server := newTestClient(t)
	server.JSON("GET /Library/MediaFolders", queryResult([]jellyfinAPI.BaseItemDto{
		item("lib-movies", "Movies", jellyfinAPI.BASEITEMKIND_COLLECTION_FOLDER),
		item("lib-tv", "TV", jellyfinAPI.BASEITEMKIND_COLLECTION_FOLDER),
	}, 2))
	server.JSON("GET /Library/VirtualFolders", []jellyfinAPI.VirtualFolderInfo{
		virtualFolder("Movies", "/data/movies"),
		virtualFolder("TV", "/data/tv"),
	})
	server.Handle("GET /Items", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("parentId") == "lib-movies" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		httptestutil.WriteJSON(t, w, queryResult([]jellyfinAPI.BaseItemDto{item("s1", "Show 1", jellyfinAPI.BASEITEMKIND_SERIES)}, 1))
	})

	items, _, err := c.GetJellyfinItems(t.Context())
	require.Error(t, err)
	require.ErrorContains(t, err, "Movies")
	require.Nil(t, items, "a partial item list must never be returned")
}

func TestGetJellyfinItemsNoMediaFolders(t *testing.T) {
	c, server := newTestClient(t)
	server.JSON("GET /Library/MediaFolders", queryResult(nil, 0))

	_, _, err := c.GetJellyfinItems(t.Context())
	require.Error(t, err)
}

func TestRemoveItemWithCleanupModeMovieAlwaysDeletesItem(t *testing.T) {
	c, server := newTestClient(t)
	server.OK("DELETE /Items/{id}")

	err := c.RemoveItemWithCleanupMode(t.Context(), "m1", "Movie", jellyfinAPI.BASEITEMKIND_MOVIE, config.CleanupModeKeepEpisodes, 1)
	require.NoError(t, err)

	deletes := server.Requests(http.MethodDelete, "")
	require.Len(t, deletes, 1)
	require.Equal(t, "/Items/m1", deletes[0].Path)
}

func TestRemoveItemWithCleanupModeSeriesAll(t *testing.T) {
	c, server := newTestClient(t)
	server.OK("DELETE /Items/{id}")

	err := c.RemoveItemWithCleanupMode(t.Context(), "s1", "Show", jellyfinAPI.BASEITEMKIND_SERIES, config.CleanupModeAll, 1)
	require.NoError(t, err)
	require.Equal(t, "/Items/s1", server.Requests(http.MethodDelete, "")[0].Path)
}

// seriesFixture serves a series with a Specials season, season 1 (2 episodes)
// and season 2 (1 episode).
func seriesFixture(t *testing.T, server *httptestutil.Server) {
	t.Helper()
	episode := func(id string, season, number int32, parentID string) jellyfinAPI.BaseItemDto {
		e := item(id, id, jellyfinAPI.BASEITEMKIND_EPISODE)
		e.SetParentIndexNumber(season)
		e.SetIndexNumber(number)
		e.SetParentId(parentID)
		return e
	}
	server.Handle("GET /Items", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("parentId") {
		case "series":
			httptestutil.WriteJSON(t, w, queryResult([]jellyfinAPI.BaseItemDto{
				item("season-0", "Specials", jellyfinAPI.BASEITEMKIND_SEASON),
				item("season-1", "Season 1", jellyfinAPI.BASEITEMKIND_SEASON),
				item("season-2", "Season 2", jellyfinAPI.BASEITEMKIND_SEASON),
				item("season-3", "Season 3", jellyfinAPI.BASEITEMKIND_SEASON),
			}, 4))
		case "season-1":
			httptestutil.WriteJSON(t, w, queryResult([]jellyfinAPI.BaseItemDto{
				episode("s1e1", 1, 1, "season-1"),
				episode("s1e2", 1, 2, "season-1"),
			}, 2))
		case "season-2":
			httptestutil.WriteJSON(t, w, queryResult([]jellyfinAPI.BaseItemDto{
				episode("s2e1", 2, 1, "season-2"),
			}, 1))
		case "season-0":
			t.Errorf("the Specials season must never be queried")
		default: // season-3 has no episodes
			httptestutil.WriteJSON(t, w, queryResult(nil, 0))
		}
	})
	server.OK("DELETE /Items/{id}")
}

func deletedPaths(server *httptestutil.Server) []string {
	var paths []string
	for _, r := range server.Requests(http.MethodDelete, "") {
		paths = append(paths, r.Path)
	}
	return paths
}

func TestRemoveItemWithCleanupModeKeepEpisodes(t *testing.T) {
	c, server := newTestClient(t)
	seriesFixture(t, server)

	err := c.RemoveItemWithCleanupMode(t.Context(), "series", "Show", jellyfinAPI.BASEITEMKIND_SERIES, config.CleanupModeKeepEpisodes, 1)
	require.NoError(t, err)

	require.ElementsMatch(t, []string{
		"/Items/s1e2", "/Items/s2e1", // all but the first episode
		"/Items/season-2", // emptied by the deletion
		"/Items/season-3", // had no episodes to begin with
	}, deletedPaths(server))
}

func TestRemoveItemWithCleanupModeKeepSeasons(t *testing.T) {
	c, server := newTestClient(t)
	seriesFixture(t, server)

	err := c.RemoveItemWithCleanupMode(t.Context(), "series", "Show", jellyfinAPI.BASEITEMKIND_SERIES, config.CleanupModeKeepSeasons, 1)
	require.NoError(t, err)

	require.ElementsMatch(t, []string{"/Items/s2e1", "/Items/season-2", "/Items/season-3"}, deletedPaths(server))
	require.NotContains(t, deletedPaths(server), "/Items/series", "the series itself must survive in keep modes")
}

func TestRemoveItemWithCleanupModeKeepEpisodesNothingToDelete(t *testing.T) {
	c, server := newTestClient(t)
	seriesFixture(t, server)

	err := c.RemoveItemWithCleanupMode(t.Context(), "series", "Show", jellyfinAPI.BASEITEMKIND_SERIES, config.CleanupModeKeepEpisodes, 10)
	require.NoError(t, err)
	require.Empty(t, deletedPaths(server))
}

func TestRemoveItemWithCleanupModeUnsupportedType(t *testing.T) {
	c, server := newTestClient(t)
	err := c.RemoveItemWithCleanupMode(t.Context(), "x", "X", jellyfinAPI.BASEITEMKIND_AUDIO, config.CleanupModeKeepEpisodes, 1)
	require.Error(t, err)
	require.Empty(t, server.Requests("", ""))
}

func TestRemoveItemErrorPropagates(t *testing.T) {
	c, server := newTestClient(t)
	server.Handle("DELETE /Items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	require.Error(t, c.RemoveItem(t.Context(), "m1"))
}

func TestCollections(t *testing.T) {
	c, server := newTestClient(t)
	server.Handle("GET /Items", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("includeItemTypes") == "BoxSet":
			httptestutil.WriteJSON(t, w, queryResult([]jellyfinAPI.BaseItemDto{
				item("col-1", "Leaving Movies", jellyfinAPI.BASEITEMKIND_BOX_SET),
			}, 1))
		case r.URL.Query().Get("parentId") == "col-1":
			httptestutil.WriteJSON(t, w, queryResult([]jellyfinAPI.BaseItemDto{
				item("m1", "Movie 1", jellyfinAPI.BASEITEMKIND_MOVIE),
			}, 1))
		default:
			httptestutil.WriteJSON(t, w, queryResult(nil, 0))
		}
	})
	created := jellyfinAPI.NewCollectionCreationResult()
	created.SetId("col-new")
	server.JSON("POST /Collections", created)
	server.OK("POST /Collections/{id}/Items")
	server.OK("DELETE /Collections/{id}/Items")

	id, err := c.FindCollectionByName(t.Context(), "Leaving Movies")
	require.NoError(t, err)
	require.Equal(t, "col-1", id)

	id, err = c.FindCollectionByName(t.Context(), "Nope")
	require.NoError(t, err)
	require.Empty(t, id, "missing collection is not an error")

	items, err := c.GetCollectionItems(t.Context(), "col-1")
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"m1": true}, items)

	require.NoError(t, c.CreateCollection(t.Context(), "Leaving TV", []string{"s1", "s2"}))
	creates := server.Requests(http.MethodPost, "/Collections")
	require.Len(t, creates, 1)
	require.Equal(t, "Leaving TV", creates[0].Query.Get("name"))
	require.Equal(t, []string{"s1", "s2"}, creates[0].Query["ids"])

	require.NoError(t, c.AddItemsToCollection(t.Context(), "col-1", []string{"m2"}))
	adds := server.Requests(http.MethodPost, "/Collections/col-1/Items")
	require.Len(t, adds, 1)
	require.Equal(t, []string{"m2"}, adds[0].Query["ids"])

	require.NoError(t, c.RemoveItemsFromCollection(t.Context(), "col-1", []string{"m1"}))
	removes := server.Requests(http.MethodDelete, "/Collections/col-1/Items")
	require.Len(t, removes, 1)
	require.Equal(t, []string{"m1"}, removes[0].Query["ids"])
}

func TestCreateCollectionBatchesLargeItemLists(t *testing.T) {
	c, server := newTestClient(t)
	created := jellyfinAPI.NewCollectionCreationResult()
	created.SetId("col-new")
	server.JSON("POST /Collections", created)
	server.OK("POST /Collections/{id}/Items")

	ids := make([]string, 120)
	for i := range ids {
		ids[i] = "item-" + strconv.Itoa(i)
	}
	require.NoError(t, c.CreateCollection(t.Context(), "Big", ids))

	require.Len(t, server.Requests(http.MethodPost, "/Collections"), 1)
	require.Len(t, server.Requests(http.MethodPost, "/Collections/col-new/Items"), 2, "remaining 70 items in batches of 50")
}
