package jellystat

import (
	"net/http"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/httptestutil"
	"github.com/jon4hz/jellysweep/pkg/jellystat"
	"github.com/stretchr/testify/require"
)

func TestGetItemLastPlayed(t *testing.T) {
	server := httptestutil.New(t)
	played := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	server.Handle("POST /api/getItemHistory", func(w http.ResponseWriter, r *http.Request) {
		var body jellystat.ItemHistoryRequest
		require.NoError(t, jsonDecode(r, &body))
		resp := jellystat.ItemHistoryResponse{}
		if body.ItemID == "watched" {
			resp.Results = []jellystat.PlaybackHistory{{ActivityDateInserted: played}}
		}
		httptestutil.WriteJSON(t, w, resp)
	})
	s := New(&config.JellystatConfig{URL: server.URL, APIKey: "k"})

	got, err := s.GetItemLastPlayed(t.Context(), "watched")
	require.NoError(t, err)
	require.Equal(t, played, got)

	got, err = s.GetItemLastPlayed(t.Context(), "never")
	require.NoError(t, err)
	require.True(t, got.IsZero(), "no history means zero time, not an error")
}

func TestGetItemLastPlayedError(t *testing.T) {
	server := httptestutil.New(t)
	server.Handle("POST /api/getItemHistory", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	s := New(&config.JellystatConfig{URL: server.URL, APIKey: "k"})

	_, err := s.GetItemLastPlayed(t.Context(), "x")
	require.Error(t, err)
}
