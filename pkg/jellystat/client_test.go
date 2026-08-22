package jellystat

import (
	"net/http"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/httptestutil"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T) (*Client, *httptestutil.Server) {
	t.Helper()
	server := httptestutil.New(t)
	return New(&config.JellystatConfig{URL: server.URL, APIKey: "secret"}), server
}

func TestGetItemHistorySendsBodyQueryAndToken(t *testing.T) {
	c, server := newTestClient(t)
	server.JSON("POST /api/getItemHistory", ItemHistoryResponse{})

	_, err := c.GetItemHistory(t.Context(), "item-1", &ItemHistoryParams{Size: 10, Page: 2, Sort: "x", Desc: true, Search: "s", Filters: "f"})
	require.NoError(t, err)

	calls := server.Requests(http.MethodPost, "/api/getItemHistory")
	require.Len(t, calls, 1)
	require.Equal(t, "secret", calls[0].Header.Get("x-api-token"))
	require.Equal(t, "application/json", calls[0].Header.Get("Content-Type"))
	var body ItemHistoryRequest
	calls[0].JSONBody(t, &body)
	require.Equal(t, "item-1", body.ItemID)
	require.Equal(t, "10", calls[0].Query.Get("size"))
	require.Equal(t, "2", calls[0].Query.Get("page"))
	require.Equal(t, "x", calls[0].Query.Get("sort"))
	require.Equal(t, "true", calls[0].Query.Get("desc"))
	require.Equal(t, "s", calls[0].Query.Get("search"))
	require.Equal(t, "f", calls[0].Query.Get("filters"))
}

func TestGetItemHistoryNilParams(t *testing.T) {
	c, server := newTestClient(t)
	server.JSON("POST /api/getItemHistory", ItemHistoryResponse{})
	_, err := c.GetItemHistory(t.Context(), "item-1", nil)
	require.NoError(t, err)
	require.Empty(t, server.Requests(http.MethodPost, "/api/getItemHistory")[0].Query)
}

func TestGetLastPlayed(t *testing.T) {
	c, server := newTestClient(t)
	newest := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	server.JSON("POST /api/getItemHistory", ItemHistoryResponse{Results: []PlaybackHistory{
		{UserName: "alice", NowPlayingItemName: "Movie", PlaybackDuration: 100, ActivityDateInserted: newest},
		{UserName: "bob", NowPlayingItemName: "Movie", PlaybackDuration: 50, ActivityDateInserted: newest.Add(-time.Hour)},
	}})

	info, err := c.GetLastPlayed(t.Context(), "item-1")
	require.NoError(t, err)
	require.Equal(t, "item-1", info.ItemID)
	require.Equal(t, 2, info.PlayCount)
	require.Equal(t, int64(150), info.TotalRuntime)
	require.Equal(t, "alice", info.LastUser)
	require.Equal(t, "Movie", info.ItemName)
	require.NotNil(t, info.LastPlayed)
	require.Equal(t, newest, *info.LastPlayed)

	call := server.Requests(http.MethodPost, "/api/getItemHistory")[0]
	require.Equal(t, "ActivityDateInserted", call.Query.Get("sort"))
	require.Equal(t, "true", call.Query.Get("desc"))
}

func TestGetLastPlayedNoHistory(t *testing.T) {
	c, server := newTestClient(t)
	server.JSON("POST /api/getItemHistory", ItemHistoryResponse{})

	info, err := c.GetLastPlayed(t.Context(), "item-1")
	require.NoError(t, err)
	require.Nil(t, info.LastPlayed)
	require.Zero(t, info.PlayCount)
}

func TestErrorStatusAndBadJSON(t *testing.T) {
	c, server := newTestClient(t)
	server.Handle("POST /api/getItemHistory", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	server.Handle("GET /stats/getLibraryMetadata", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})

	_, err := c.GetLastPlayed(t.Context(), "item-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
	require.Contains(t, err.Error(), "boom")

	_, err = c.GetLibraryMetadata(t.Context())
	require.Error(t, err)
}

func TestGetLibraryMetadataAndItems(t *testing.T) {
	c, server := newTestClient(t)
	server.JSON("GET /stats/getLibraryMetadata", []LibraryMetadata{{ID: "lib-1", Name: "Movies"}})
	server.JSON("POST /api/getLibraryItems", []LibraryItem{{ID: "m1", Name: "Movie", Type: "Movie", ProductionYear: 2001}})

	libraries, err := c.GetLibraryMetadata(t.Context())
	require.NoError(t, err)
	require.Equal(t, []LibraryMetadata{{ID: "lib-1", Name: "Movies"}}, libraries)
	require.Equal(t, "secret", server.Requests(http.MethodGet, "/stats/getLibraryMetadata")[0].Header.Get("x-api-token"))

	items, err := c.GetLibraryItems(t.Context(), "lib-1")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "m1", items[0].ID)
	var body LibraryItemsRequest
	server.Requests(http.MethodPost, "/api/getLibraryItems")[0].JSONBody(t, &body)
	require.Equal(t, "lib-1", body.LibraryID)
}
