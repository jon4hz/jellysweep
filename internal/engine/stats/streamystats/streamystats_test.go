package streamystats

import (
	"net/http"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/httptestutil"
	"github.com/jon4hz/jellysweep/pkg/streamystats"
	"github.com/stretchr/testify/require"
)

func TestGetItemLastPlayed(t *testing.T) {
	server := httptestutil.New(t)
	watched := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	server.JSON("GET /api/get-item-details/watched", streamystats.ItemDetails{LastWatched: watched})
	server.JSON("GET /api/get-item-details/never", streamystats.ItemDetails{})
	server.Handle("GET /api/get-item-details/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	s, err := New(&config.StreamystatsConfig{URL: server.URL, ServerID: 1}, "api-key")
	require.NoError(t, err)

	got, err := s.GetItemLastPlayed(t.Context(), "watched")
	require.NoError(t, err)
	require.Equal(t, watched, got)
	require.Equal(t, "1", server.Requests(http.MethodGet, "/api/get-item-details/watched")[0].Query.Get("serverId"))

	got, err = s.GetItemLastPlayed(t.Context(), "never")
	require.NoError(t, err)
	require.True(t, got.IsZero())

	_, err = s.GetItemLastPlayed(t.Context(), "missing")
	require.ErrorIs(t, err, streamystats.ErrItemNotFound, "not-found must surface as the sentinel the stream filter checks for")
}

func TestNewRejectsInvalidURL(t *testing.T) {
	_, err := New(&config.StreamystatsConfig{URL: "://bad"}, "k")
	require.Error(t, err)
}
