package engine

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/database/databasetest"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/pkg/jellyseerr"
	"github.com/stretchr/testify/require"
)

func TestPopulateRequesterInfoKeepsJellyfinUserWithoutEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/movie/123", r.URL.Path)
		_, err := w.Write([]byte(`{
			"id": 123,
			"title": "A Movie",
			"mediaInfo": {
				"tmdbId": 123,
				"requests": [{
					"createdAt": "2026-01-01T00:00:00Z",
					"requestedBy": {"id": 42, "username": "jellyfin-user", "displayName": "Jellyfin User"}
				}]
			}
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	e := &Engine{jellyseerr: jellyseerr.New(&config.JellyseerrConfig{URL: server.URL})}
	items := e.populateRequesterInfo(t.Context(), []arr.MediaItem{{Title: "A Movie", TmdbId: 123, MediaType: "movie"}})

	require.Equal(t, "Jellyfin User", items[0].RequestedBy)
	require.Empty(t, items[0].RequesterEmail)
}

func TestPopulateRequesterInfoSeparatesDisplayNameAndEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`{
			"id": 123,
			"title": "A Movie",
			"mediaInfo": {
				"tmdbId": 123,
				"requests": [{
					"createdAt": "2026-01-01T00:00:00Z",
					"requestedBy": {"id": 42, "email": "user@example.com", "username": "jellyfin-user", "displayName": "Jellyfin User"}
				}]
			}
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	e := &Engine{jellyseerr: jellyseerr.New(&config.JellyseerrConfig{URL: server.URL})}
	items := e.populateRequesterInfo(t.Context(), []arr.MediaItem{{Title: "A Movie", TmdbId: 123, MediaType: "movie"}})

	require.Equal(t, "Jellyfin User", items[0].RequestedBy)
	require.Equal(t, "user@example.com", items[0].RequesterEmail)
}

func TestRefreshMissingRequesterInfoBackfillsQueuedMedia(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{
			"id": 123,
			"title": "A Movie",
			"mediaInfo": {
				"tmdbId": 123,
				"requests": [{
					"createdAt": "2026-01-01T00:00:00Z",
					"requestedBy": {"username": "jellyfin-user"}
				}]
			}
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	db, _ := databasetest.New(t)
	queued := database.Media{
		JellyfinID:      "jf-a-movie",
		ArrID:           1,
		Title:           "A Movie",
		MediaType:       database.MediaTypeMovie,
		DefaultDeleteAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, db.CreateMediaItems(t.Context(), []database.Media{queued}))

	e := &Engine{db: db, jellyseerr: jellyseerr.New(&config.JellyseerrConfig{URL: server.URL})}
	e.refreshMissingRequesterInfo(t.Context(), []arr.MediaItem{{
		JellyfinID: "jf-a-movie",
		Title:      "A Movie",
		TmdbId:     123,
		MediaType:  "movie",
	}})

	items, err := db.GetMediaItems(t.Context(), true)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "jellyfin-user", items[0].RequestedBy)
}
