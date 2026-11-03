package streamfilter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/engine/stats"
	"github.com/jon4hz/jellysweep/pkg/streamystats"
	"github.com/stretchr/testify/require"
)

type stubStats struct {
	lastPlayed map[string]time.Time
	err        map[string]error
	requested  []stats.Item
}

func (s *stubStats) GetItemLastPlayed(_ context.Context, item stats.Item) (time.Time, error) {
	s.requested = append(s.requested, item)
	if err := s.err[item.JellyfinID]; err != nil {
		return time.Time{}, err
	}
	return s.lastPlayed[item.JellyfinID], nil
}

func streamConfig() *config.Config {
	return &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {}, // default LastStreamThreshold: 30 days
		},
	}
}

func item(title string) arr.MediaItem {
	return arr.MediaItem{JellyfinID: "jf-" + title, Title: title, LibraryName: "Movies"}
}

func TestApplyExcludesRecentlyStreamed(t *testing.T) {
	stats := &stubStats{lastPlayed: map[string]time.Time{
		"jf-Recent": time.Now().Add(-24 * time.Hour),
		"jf-Old":    time.Now().Add(-60 * 24 * time.Hour),
	}}
	f := New(streamConfig(), stats)

	got, err := f.Apply(t.Context(), []arr.MediaItem{item("Recent"), item("Old")})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Old", got[0].Title)
}

func TestApplyPassesProviderIDs(t *testing.T) {
	// Backends that can't look items up by Jellyfin ID match on these instead.
	stub := &stubStats{}
	f := New(streamConfig(), stub)
	show := arr.MediaItem{JellyfinID: "jf-show", Title: "Show", LibraryName: "Movies", MediaType: models.MediaTypeTV, TmdbId: 1399, TvdbId: 121361}

	_, err := f.Apply(t.Context(), []arr.MediaItem{show})
	require.NoError(t, err)
	require.Equal(t, []stats.Item{{JellyfinID: "jf-show", MediaType: models.MediaTypeTV, TmdbID: 1399, TvdbID: 121361}}, stub.requested)
}

func TestApplyNeverStreamedStaysCandidate(t *testing.T) {
	f := New(streamConfig(), &stubStats{})
	got, err := f.Apply(t.Context(), []arr.MediaItem{item("Fresh")})
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestApplyItemNotFoundIsExcluded(t *testing.T) {
	// An item unknown to the stats backend has no streaming history we can
	// trust, so it is excluded from deletion.
	stats := &stubStats{err: map[string]error{"jf-Unknown": streamystats.ErrItemNotFound}}
	f := New(streamConfig(), stats)
	got, err := f.Apply(t.Context(), []arr.MediaItem{item("Unknown"), item("Fine")})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Fine", got[0].Title)
}

func TestApplyOtherErrorsAbortTheRun(t *testing.T) {
	// KNOWN-BEHAVIOR: any other stats error aborts the whole filter chain.
	// This is fail-safe for deletion (a failed markForDeletion blocks
	// cleanupMedia), at the cost of blocking unrelated libraries too.
	stats := &stubStats{err: map[string]error{"jf-Broken": errors.New("stats down")}}
	f := New(streamConfig(), stats)
	_, err := f.Apply(t.Context(), []arr.MediaItem{item("Broken")})
	require.Error(t, err)
}

func TestApplyNoLibraryConfigKeepsStreamedItem(t *testing.T) {
	// Without a library config the threshold cannot be evaluated; streamed
	// items are excluded (fail-safe).
	stats := &stubStats{lastPlayed: map[string]time.Time{"jf-X": time.Now().Add(-500 * 24 * time.Hour)}}
	f := New(&config.Config{}, stats)
	got, err := f.Apply(t.Context(), []arr.MediaItem{{JellyfinID: "jf-X", Title: "X", LibraryName: "Elsewhere"}})
	require.NoError(t, err)
	require.Empty(t, got)
}
