package seriesfilter

import (
	"testing"

	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/stretchr/testify/require"
)

// seriesWithSeasons builds a series where each entry in episodeFiles is a
// season (season numbers starting at start) with that many episode files.
func seriesWithSeasons(title string, start int32, episodeFiles ...int32) arr.MediaItem {
	series := sonarr.SeriesResource{}
	series.SetId(1)
	series.SetTitle(title)

	seasons := make([]sonarr.SeasonResource, 0, len(episodeFiles))
	for i, count := range episodeFiles {
		season := sonarr.SeasonResource{}
		season.SetSeasonNumber(start + int32(i))
		stats := sonarr.SeasonStatisticsResource{}
		stats.SetEpisodeFileCount(count)
		season.SetStatistics(stats)
		seasons = append(seasons, season)
	}
	series.SetSeasons(seasons)

	return arr.MediaItem{
		Title:          title,
		LibraryName:    "TV",
		MediaType:      models.MediaTypeTV,
		SeriesResource: series,
	}
}

func TestApplyCleanupModeAllPassesEverything(t *testing.T) {
	f := New(&config.Config{CleanupMode: config.CleanupModeAll, KeepCount: 1})
	items := []arr.MediaItem{seriesWithSeasons("Small Show", 1, 1)}
	got, err := f.Apply(t.Context(), items)
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestApplyKeepEpisodesSkipsSeriesAtOrBelowKeepCount(t *testing.T) {
	f := New(&config.Config{CleanupMode: config.CleanupModeKeepEpisodes, KeepCount: 3})

	got, err := f.Apply(t.Context(), []arr.MediaItem{
		seriesWithSeasons("Exactly Three", 1, 3),
		seriesWithSeasons("Two Episodes", 1, 2),
		seriesWithSeasons("Four Episodes", 1, 4),
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Four Episodes", got[0].Title, "only series above the keep count remain candidates")
}

func TestApplyKeepEpisodesIgnoresSpecials(t *testing.T) {
	f := New(&config.Config{CleanupMode: config.CleanupModeKeepEpisodes, KeepCount: 3})

	// Season 0 has 10 specials, season 1 has 2 regular episodes.
	got, err := f.Apply(t.Context(), []arr.MediaItem{
		seriesWithSeasons("Specials Heavy", 0, 10, 2),
	})
	require.NoError(t, err)
	require.Empty(t, got, "season 0 specials must not count toward the keep count")
}

func TestApplyKeepSeasonsSkipsSeriesAtOrBelowKeepCount(t *testing.T) {
	f := New(&config.Config{CleanupMode: config.CleanupModeKeepSeasons, KeepCount: 2})

	got, err := f.Apply(t.Context(), []arr.MediaItem{
		seriesWithSeasons("Two Seasons", 1, 8, 8),
		seriesWithSeasons("Three Seasons", 1, 8, 8, 8),
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Three Seasons", got[0].Title)
}

func TestApplyMoviesPassThrough(t *testing.T) {
	f := New(&config.Config{CleanupMode: config.CleanupModeKeepEpisodes, KeepCount: 1})
	got, err := f.Apply(t.Context(), []arr.MediaItem{
		{Title: "A Movie", MediaType: models.MediaTypeMovie},
	})
	require.NoError(t, err)
	require.Len(t, got, 1, "non-TV items are unaffected by the series filter")
}

func TestApplyNoSeasonsDataStaysCandidate(t *testing.T) {
	f := New(&config.Config{CleanupMode: config.CleanupModeKeepEpisodes, KeepCount: 3})
	series := sonarr.SeriesResource{}
	series.SetId(1)
	series.SetTitle("No Data")
	got, err := f.Apply(t.Context(), []arr.MediaItem{{
		Title:          "No Data",
		MediaType:      models.MediaTypeTV,
		SeriesResource: series,
	}})
	require.NoError(t, err)
	require.Len(t, got, 1, "without season data the series cannot be skipped")
}
