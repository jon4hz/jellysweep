package tunarrfilter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/pkg/tunarr"
	"github.com/stretchr/testify/require"
)

// newTunarrServer serves one channel whose programs include a jellyfin movie
// and a jellyfin episode belonging to a show.
func newTunarrServer(t *testing.T, movieJellyfinID, showJellyfinID string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/channels", func(w http.ResponseWriter, _ *http.Request) {
		err := json.NewEncoder(w).Encode([]tunarr.Channel{
			{ID: "ch1", Name: "Channel One", Number: 1, ProgramCount: 2},
		})
		require.NoError(t, err)
	})
	mux.HandleFunc("/api/channels/ch1/programming", func(w http.ResponseWriter, _ *http.Request) {
		err := json.NewEncoder(w).Encode(tunarr.ProgrammingResponse{
			Name:          "Channel One",
			TotalPrograms: 2,
			Programs: map[string]tunarr.Program{
				"p1": {
					ID:                 "p1",
					Type:               "content",
					Subtype:            "movie",
					Title:              "Channel Movie",
					ExternalSourceType: "jellyfin",
					ExternalKey:        movieJellyfinID,
				},
				"p2": {
					ID:                 "p2",
					Type:               "content",
					Subtype:            "episode",
					Title:              "Channel Episode",
					ExternalSourceType: "jellyfin",
					Grandparent:        &tunarr.MediaParent{ExternalKey: showJellyfinID},
				},
			},
		})
		require.NoError(t, err)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func tunarrTestConfig(serverURL string, tunarrEnabled bool) *config.Config {
	return &config.Config{
		Tunarr: &config.TunarrConfig{URL: serverURL},
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {Filter: config.FilterConfig{TunarrEnabled: tunarrEnabled}},
			"TV":     {Filter: config.FilterConfig{TunarrEnabled: tunarrEnabled}},
		},
	}
}

func TestApplyExcludesChannelContent(t *testing.T) {
	server := newTunarrServer(t, "jf-channel-movie", "jf-channel-show")
	f, err := New(tunarrTestConfig(server.URL, true))
	require.NoError(t, err)

	got, err := f.Apply(t.Context(), []arr.MediaItem{
		{Title: "Channel Movie", JellyfinID: "jf-channel-movie", LibraryName: "Movies", MediaType: models.MediaTypeMovie},
		{Title: "Free Movie", JellyfinID: "jf-free-movie", LibraryName: "Movies", MediaType: models.MediaTypeMovie},
		{Title: "Channel Show", JellyfinID: "jf-channel-show", LibraryName: "TV", MediaType: models.MediaTypeTV},
		{Title: "Free Show", JellyfinID: "jf-free-show", LibraryName: "TV", MediaType: models.MediaTypeTV},
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "Free Movie", got[0].Title)
	require.Equal(t, "Free Show", got[1].Title)
}

func TestApplyDisabledLibraryIsUntouched(t *testing.T) {
	server := newTunarrServer(t, "jf-channel-movie", "jf-channel-show")
	f, err := New(tunarrTestConfig(server.URL, false))
	require.NoError(t, err)

	got, err := f.Apply(t.Context(), []arr.MediaItem{
		{Title: "Channel Movie", JellyfinID: "jf-channel-movie", LibraryName: "Movies", MediaType: models.MediaTypeMovie},
	})
	require.NoError(t, err)
	require.Len(t, got, 1, "tunarr filtering only applies to libraries with tunarr_enabled")
}

func TestApplyAPIErrorAborts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	f, err := New(tunarrTestConfig(server.URL, true))
	require.NoError(t, err)

	_, err = f.Apply(t.Context(), []arr.MediaItem{{Title: "Movie", LibraryName: "Movies", MediaType: models.MediaTypeMovie}})
	require.Error(t, err, "an unreachable Tunarr must abort the run rather than risk deleting channel content")
}

func TestNewRequiresTunarrConfig(t *testing.T) {
	_, err := New(&config.Config{})
	require.Error(t, err)
}
