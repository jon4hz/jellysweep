package sizefilter

import (
	"testing"

	"github.com/devopsarr/radarr-go/radarr"
	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/stretchr/testify/require"
)

const gigabyte = int64(1024 * 1024 * 1024)

func movieOfSize(title string, size int64) arr.MediaItem {
	movie := radarr.MovieResource{}
	movie.SetTitle(title)
	movie.SetSizeOnDisk(size)
	return arr.MediaItem{
		Title:         title,
		LibraryName:   "Movies",
		MediaType:     models.MediaTypeMovie,
		MovieResource: movie,
	}
}

func seriesOfSize(title string, size int64) arr.MediaItem {
	series := sonarr.SeriesResource{}
	series.SetTitle(title)
	stats := sonarr.SeriesStatisticsResource{}
	stats.SetSizeOnDisk(size)
	series.SetStatistics(stats)
	return arr.MediaItem{
		Title:          title,
		LibraryName:    "TV",
		MediaType:      models.MediaTypeTV,
		SeriesResource: series,
	}
}

func TestApplyFiltersBelowThreshold(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {Filter: config.FilterConfig{ContentSizeThreshold: 2 * gigabyte}},
			"TV":     {Filter: config.FilterConfig{ContentSizeThreshold: 2 * gigabyte}},
		},
	}
	f := New(cfg)

	got, err := f.Apply(t.Context(), []arr.MediaItem{
		movieOfSize("Small Movie", gigabyte),
		movieOfSize("Big Movie", 3*gigabyte),
		seriesOfSize("Small Show", gigabyte),
		seriesOfSize("Big Show", 3*gigabyte),
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "Big Movie", got[0].Title)
	require.Equal(t, "Big Show", got[1].Title)
}

func TestApplyThresholdBoundaryIsInclusive(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {Filter: config.FilterConfig{ContentSizeThreshold: 2 * gigabyte}},
		},
	}
	f := New(cfg)
	got, err := f.Apply(t.Context(), []arr.MediaItem{movieOfSize("Exact", 2*gigabyte)})
	require.NoError(t, err)
	require.Len(t, got, 1, "size == threshold is eligible for deletion")
}

func TestApplyZeroThresholdDisablesFilter(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{"Movies": {}},
	}
	f := New(cfg)
	got, err := f.Apply(t.Context(), []arr.MediaItem{movieOfSize("Tiny", 1)})
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestApplyNoLibraryConfigIncludesItem(t *testing.T) {
	f := New(&config.Config{})
	got, err := f.Apply(t.Context(), []arr.MediaItem{movieOfSize("Movie", 1)})
	require.NoError(t, err)
	require.Len(t, got, 1)
}
