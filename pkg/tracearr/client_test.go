package tracearr

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/httptestutil"
	"github.com/stretchr/testify/require"
)

const (
	testAPIKey       = "trr_pub_test"
	watchedMediaPath = "/api/v2/public/watched-media"
)

var testSince = time.Date(2026, time.September, 1, 15, 30, 0, 0, time.UTC)

func newTestClient(t *testing.T) (*Client, *httptestutil.Server) {
	t.Helper()
	server := httptestutil.New(t)
	return New(&config.TracearrConfig{URL: server.URL, APIKey: testAPIKey}), server
}

func watchedRow(tmdbID, tvdbID, lastWatchedDay any) map[string]any {
	return map[string]any{
		"media_id":         "b6a1c1de-5a44-4b53-9a0b-0d6a5e1f2c11",
		"media_type":       "show",
		"title":            "A Title",
		"tmdb_id":          tmdbID,
		"tvdb_id":          tvdbID,
		"imdb_id":          nil,
		"watched_state":    "partial",
		"plays":            3,
		"last_watched_day": lastWatchedDay,
	}
}

func page(nextCursor any, rows ...map[string]any) map[string]any {
	if rows == nil {
		rows = []map[string]any{}
	}
	return map[string]any{"data": rows, "meta": map[string]any{"nextCursor": nextCursor, "pageSize": 1000}}
}

func day(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
}

func TestGetWatchedTitlesWalksEveryPage(t *testing.T) {
	c, server := newTestClient(t)
	server.Handle("GET "+watchedMediaPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			httptestutil.WriteJSON(t, w, page("next", watchedRow(1399, 121361, "2026-10-01")))
			return
		}
		httptestutil.WriteJSON(t, w, page(nil, watchedRow(nil, 81189, "2026-09-20")))
	})

	got, err := c.GetWatchedTitles(t.Context(), MediaTypeShow, testSince)
	require.NoError(t, err)
	require.Equal(t, []Title{
		{TmdbID: 1399, TvdbID: 121361, LastWatchedDay: day(2026, time.October, 1)},
		{TvdbID: 81189, LastWatchedDay: day(2026, time.September, 20)},
	}, got)

	requests := server.Requests(http.MethodGet, watchedMediaPath)
	require.Len(t, requests, 2)
	require.Equal(t, "Bearer "+testAPIKey, requests[0].Header.Get("Authorization"))
	require.Equal(t, "show", requests[0].Query.Get("media_type"))
	require.Equal(t, "partial", requests[0].Query.Get("min_state"), "the default leaves out unfinished titles")
	require.Equal(t, "1000", requests[0].Query.Get("pageSize"))
	require.Equal(t, "next", requests[1].Query.Get("cursor"))
	require.Equal(t, "show", requests[1].Query.Get("media_type"))
}

func TestGetWatchedTitlesStopsAtTitlesOlderThanSince(t *testing.T) {
	// Titles come newest first, so the first older one ends the walk.
	c, server := newTestClient(t)
	server.JSON("GET "+watchedMediaPath, page("next",
		watchedRow(1, nil, "2026-09-02"),
		watchedRow(2, nil, "2026-09-01"),
		watchedRow(3, nil, "2026-08-31"),
	))

	got, err := c.GetWatchedTitles(t.Context(), MediaTypeMovie, testSince)
	require.NoError(t, err)
	require.Equal(t, []Title{
		{TmdbID: 1, LastWatchedDay: day(2026, time.September, 2)},
		{TmdbID: 2, LastWatchedDay: day(2026, time.September, 1)},
	}, got, "a title played on the day of since is kept")
	require.Len(t, server.Requests(http.MethodGet, watchedMediaPath), 1)
	require.Equal(t, "movie", server.Requests(http.MethodGet, watchedMediaPath)[0].Query.Get("media_type"))
}

func TestGetWatchedTitlesFailuresAreNotAnEmptyList(t *testing.T) {
	// Each of these would otherwise read as "nothing was played" and make
	// watched media a deletion candidate.
	tests := []struct {
		name string
		body string
	}{
		{"missing data", `{"meta":{"nextCursor":null}}`},
		{"null data", `{"data":null,"meta":{"nextCursor":null}}`},
		{"missing last_watched_day", `{"data":[{"tmdb_id":1}],"meta":{"nextCursor":null}}`},
		{"null last_watched_day", `{"data":[{"tmdb_id":1,"last_watched_day":null}],"meta":{"nextCursor":null}}`},
		{"invalid last_watched_day", `{"data":[{"tmdb_id":1,"last_watched_day":"soon"}],"meta":{"nextCursor":null}}`},
		{"not json", `<html>login</html>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, server := newTestClient(t)
			server.Handle("GET "+watchedMediaPath, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			})

			_, err := c.GetWatchedTitles(t.Context(), MediaTypeMovie, testSince)
			require.Error(t, err)
		})
	}
}

func TestGetWatchedTitlesErrorStatusesFail(t *testing.T) {
	// A wrong URL, or a Tracearr older than 2.3.0 without this route, returns 404
	// and must fail the run loudly.
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError} {
		c, server := newTestClient(t)
		server.Handle("GET "+watchedMediaPath, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		})

		_, err := c.GetWatchedTitles(t.Context(), MediaTypeMovie, testSince)
		require.Error(t, err, "status %d", status)
	}
}

func TestGetWatchedTitlesRepeatedCursorFails(t *testing.T) {
	c, server := newTestClient(t)
	server.JSON("GET "+watchedMediaPath, page("same", watchedRow(1, nil, "2026-10-01")))

	_, err := c.GetWatchedTitles(t.Context(), MediaTypeMovie, testSince)
	require.Error(t, err)
	require.Len(t, server.Requests(http.MethodGet, watchedMediaPath), 2)
}

func TestRequestsRetryAfterRateLimit(t *testing.T) {
	c, server := newTestClient(t)
	calls := 0
	server.Handle("GET "+watchedMediaPath, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		httptestutil.WriteJSON(t, w, page(nil))
	})

	_, err := c.GetWatchedTitles(t.Context(), MediaTypeMovie, testSince)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestRequestsGiveUpWhenRateLimitPersists(t *testing.T) {
	c, server := newTestClient(t)
	server.Handle("GET "+watchedMediaPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := c.GetWatchedTitles(t.Context(), MediaTypeMovie, testSince)
	require.Error(t, err)
	require.Len(t, server.Requests(http.MethodGet, watchedMediaPath), maxAttempts)
}

func TestRequestsRefuseAnExcessiveRetryAfter(t *testing.T) {
	// A proxy asking for a day must not stall the cleanup job for a day.
	c, server := newTestClient(t)
	server.Handle("GET "+watchedMediaPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "86400")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := c.GetWatchedTitles(t.Context(), MediaTypeMovie, testSince)
	require.Error(t, err)
	require.Len(t, server.Requests(http.MethodGet, watchedMediaPath), 1)
}

func TestRequestsStopWaitingWhenContextEnds(t *testing.T) {
	c, server := newTestClient(t)
	server.Handle("GET "+watchedMediaPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := c.GetWatchedTitles(ctx, MediaTypeMovie, testSince)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestParseRetryAfter(t *testing.T) {
	require.Equal(t, 7*time.Second, parseRetryAfter("7"))
	require.Equal(t, time.Duration(0), parseRetryAfter("0"))
	require.Equal(t, time.Minute, parseRetryAfter(""), "must wait out the window, not retry in a hot loop")
	require.Equal(t, time.Minute, parseRetryAfter("-1"))
	require.Equal(t, time.Minute, parseRetryAfter("soon"))
}
