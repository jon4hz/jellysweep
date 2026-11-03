package tracearr

import (
	"net/http"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/stats"
	"github.com/jon4hz/jellysweep/internal/httptestutil"
	"github.com/stretchr/testify/require"
)

const watchedMediaPath = "/api/v2/public/watched-media"

var testNow = time.Date(2026, time.November, 3, 12, 0, 0, 0, time.UTC)

func title(tmdbID, tvdbID any, lastWatchedDay string) map[string]any {
	return map[string]any{"tmdb_id": tmdbID, "tvdb_id": tvdbID, "last_watched_day": lastWatchedDay}
}

func titles(rows ...map[string]any) []map[string]any {
	if rows == nil {
		rows = []map[string]any{}
	}
	return rows
}

// serveTitles answers /watched-media with one page per media type.
func serveTitles(t *testing.T, server *httptestutil.Server, movies, shows []map[string]any) {
	t.Helper()
	server.Handle("GET "+watchedMediaPath, func(w http.ResponseWriter, r *http.Request) {
		rows := movies
		if r.URL.Query().Get("media_type") == "show" {
			rows = shows
		}
		httptestutil.WriteJSON(t, w, map[string]any{"data": rows, "meta": map[string]any{"nextCursor": nil}})
	})
}

func newTestStatser(t *testing.T) (*tracearrClient, *httptestutil.Server) {
	t.Helper()
	server := httptestutil.New(t)
	cfg := &config.Config{
		Tracearr: &config.TracearrConfig{URL: server.URL, APIKey: "key"},
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {Filter: config.FilterConfig{LastStreamThreshold: 30}},
			"TV":     {Filter: config.FilterConfig{LastStreamThreshold: 90}},
		},
	}
	s, ok := New(cfg).(*tracearrClient)
	require.True(t, ok)
	s.now = func() time.Time { return testNow }
	return s, server
}

func movie(tmdbID int32) stats.Item {
	return stats.Item{JellyfinID: "jf-movie", MediaType: models.MediaTypeMovie, TmdbID: tmdbID}
}

func series(tmdbID, tvdbID int32) stats.Item {
	return stats.Item{JellyfinID: "jf-series", MediaType: models.MediaTypeTV, TmdbID: tmdbID, TvdbID: tvdbID}
}

// endOf is the time a play on the given UTC day counts as.
func endOf(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day+1, 0, 0, 0, 0, time.UTC)
}

func TestMovieIsFoundByTmdbID(t *testing.T) {
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(title(27205, nil, "2026-10-01")), titles())

	got, err := s.GetItemLastPlayed(t.Context(), movie(27205))
	require.NoError(t, err)
	require.Equal(t, endOf(2026, time.October, 1), got.UTC(), "a play counts as late in its day as possible")
}

func TestSeriesIsFoundByTvdbID(t *testing.T) {
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(), titles(title(nil, 121361, "2026-10-01")))

	got, err := s.GetItemLastPlayed(t.Context(), series(0, 121361))
	require.NoError(t, err)
	require.Equal(t, endOf(2026, time.October, 1), got.UTC())
}

func TestSeriesIsFoundByTmdbID(t *testing.T) {
	// Tracearr may have only a TMDB ID for a series that Sonarr knows by both.
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(), titles(title(1399, nil, "2026-10-01")))

	got, err := s.GetItemLastPlayed(t.Context(), series(1399, 121361))
	require.NoError(t, err)
	require.Equal(t, endOf(2026, time.October, 1), got.UTC())
}

func TestSeriesTakesTheLatestDayAcrossItsIDs(t *testing.T) {
	// Tracearr can hold one series as two titles, one matched on each ID.
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(), titles(
		title(1399, nil, "2026-10-05"),
		title(nil, 121361, "2026-10-01"),
	))

	got, err := s.GetItemLastPlayed(t.Context(), series(1399, 121361))
	require.NoError(t, err)
	require.Equal(t, endOf(2026, time.October, 5), got.UTC())
}

func TestNewestDayWinsForTheSameID(t *testing.T) {
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(
		title(27205, nil, "2026-09-01"),
		title(27205, nil, "2026-10-01"),
	), titles())

	got, err := s.GetItemLastPlayed(t.Context(), movie(27205))
	require.NoError(t, err)
	require.Equal(t, endOf(2026, time.October, 1), got.UTC())
}

func TestMoviesAndSeriesDoNotShareIDs(t *testing.T) {
	// TMDB numbers movies and series separately, so the same number is two titles.
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(), titles(title(1399, nil, "2026-10-01")))

	got, err := s.GetItemLastPlayed(t.Context(), movie(1399))
	require.NoError(t, err)
	require.True(t, got.IsZero())
}

func TestPlayToday(t *testing.T) {
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(title(27205, nil, "2026-11-03")), titles())

	got, err := s.GetItemLastPlayed(t.Context(), movie(27205))
	require.NoError(t, err)
	require.Equal(t, testNow, got.UTC(), "a play can't be later than now")
}

func TestItemWithoutRecentPlaysIsNeverPlayed(t *testing.T) {
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(title(27205, nil, "2026-10-01")), titles())

	got, err := s.GetItemLastPlayed(t.Context(), movie(603))
	require.NoError(t, err)
	require.True(t, got.IsZero())
}

func TestItemsTracearrCannotMatchFail(t *testing.T) {
	// Reading these as "never played" would make them deletion candidates.
	tests := map[string]stats.Item{
		"movie without tmdb id": movie(0),
		"series without any id": series(0, 0),
		"unknown media type":    {JellyfinID: "jf-x", MediaType: "music", TmdbID: 1},
		"missing media type":    {JellyfinID: "jf-x", TmdbID: 1},
	}
	for name, item := range tests {
		t.Run(name, func(t *testing.T) {
			s, server := newTestStatser(t)
			server.Handle("/", func(_ http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected Tracearr request: %s", r.URL.Path)
			})

			_, err := s.GetItemLastPlayed(t.Context(), item)
			require.Error(t, err)
		})
	}
}

func TestWindowCoversTheLongestLibraryThreshold(t *testing.T) {
	// 90 days from the TV library plus one day of margin reaches back to
	// 2026-08-04 12:00, so plays on that day still count.
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(
		title(1, nil, "2026-08-04"),
		title(2, nil, "2026-08-03"),
	), titles())

	got, err := s.GetItemLastPlayed(t.Context(), movie(1))
	require.NoError(t, err)
	require.False(t, got.IsZero())

	got, err = s.GetItemLastPlayed(t.Context(), movie(2))
	require.NoError(t, err)
	require.True(t, got.IsZero())
}

func TestSnapshotIsReusedUntilItExpires(t *testing.T) {
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(title(27205, nil, "2026-10-01")), titles())

	for _, item := range []stats.Item{movie(27205), series(1, 2), movie(603)} {
		_, err := s.GetItemLastPlayed(t.Context(), item)
		require.NoError(t, err)
	}
	require.Len(t, server.Requests(http.MethodGet, watchedMediaPath), 2, "one read of movies and one of series")

	s.now = func() time.Time { return testNow.Add(snapshotTTL + time.Second) }
	_, err := s.GetItemLastPlayed(t.Context(), movie(27205))
	require.NoError(t, err)
	require.Len(t, server.Requests(http.MethodGet, watchedMediaPath), 4)
}

func TestNothingPlayedIsNeverPlayed(t *testing.T) {
	// Wrong URLs and keys fail on their status, so an empty list is an idle server.
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(), titles())

	for range 2 {
		got, err := s.GetItemLastPlayed(t.Context(), movie(27205))
		require.NoError(t, err)
		require.True(t, got.IsZero())
	}
	require.Len(t, server.Requests(http.MethodGet, watchedMediaPath), 2, "an empty snapshot is still a snapshot")
}

func TestTitlesWithoutIDsFail(t *testing.T) {
	// Titles that all lack IDs mean the API changed, and reading them as
	// "never played" would queue the whole library.
	s, server := newTestStatser(t)
	serveTitles(t, server, titles(title(nil, nil, "2026-10-01")), titles(title(nil, nil, "2026-10-01")))

	_, err := s.GetItemLastPlayed(t.Context(), movie(27205))
	require.Error(t, err)
}

func TestFailuresAreNotCached(t *testing.T) {
	for _, failing := range []string{"movie", "show"} {
		t.Run(failing, func(t *testing.T) {
			s, server := newTestStatser(t)
			server.Handle("GET "+watchedMediaPath, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("media_type") == failing {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				httptestutil.WriteJSON(t, w, map[string]any{"data": titles(title(1, 2, "2026-10-01")), "meta": map[string]any{}})
			})

			for range 2 {
				_, err := s.GetItemLastPlayed(t.Context(), movie(1))
				require.Error(t, err)
			}
			requests := 0
			for _, r := range server.Requests(http.MethodGet, watchedMediaPath) {
				if r.Query.Get("media_type") == failing {
					requests++
				}
			}
			require.Equal(t, 2, requests, "a failed snapshot must be retried")
		})
	}
}
