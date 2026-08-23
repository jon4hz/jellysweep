package policy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/stretchr/testify/require"
)

func diskUsageConfig(thresholds ...config.DiskUsageThreshold) *config.Config {
	return &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {DiskUsageThresholds: thresholds},
		},
	}
}

func staticUsage(percent float64) UsageFunc {
	return func(context.Context, database.MediaType) (float64, bool, error) {
		return percent, true, nil
	}
}

func unavailableUsage() UsageFunc {
	return func(context.Context, database.MediaType) (float64, bool, error) {
		return 0, false, nil
	}
}

func TestDiskUsageApplyCreatesPolicies(t *testing.T) {
	cfg := diskUsageConfig(
		config.DiskUsageThreshold{UsagePercent: 80, MaxCleanupDelay: 10},
		config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 2},
	)
	p := NewDiskUsageDelete(cfg, staticUsage(0))

	media := database.Media{Title: "A Movie", LibraryName: "Movies"}
	require.NoError(t, p.Apply(&media))
	require.Len(t, media.DiskUsageDeletePolicies, 2)
	require.Equal(t, 80.0, media.DiskUsageDeletePolicies[0].Threshold)
	require.WithinDuration(t, time.Now().Add(10*24*time.Hour), media.DiskUsageDeletePolicies[0].DeleteDate, time.Minute)
	require.Equal(t, 90.0, media.DiskUsageDeletePolicies[1].Threshold)
	require.WithinDuration(t, time.Now().Add(2*24*time.Hour), media.DiskUsageDeletePolicies[1].DeleteDate, time.Minute)
}

func TestDiskUsageApplyClearsStalePolicies(t *testing.T) {
	p := NewDiskUsageDelete(diskUsageConfig(), staticUsage(0))

	media := database.Media{
		Title:       "A Movie",
		LibraryName: "Movies",
		DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{
			{Threshold: 90, DeleteDate: time.Now()},
		},
	}
	require.NoError(t, p.Apply(&media))
	require.Nil(t, media.DiskUsageDeletePolicies, "stale policies from removed thresholds must be cleared")
}

func TestDiskUsageApplyUnknownLibraryErrors(t *testing.T) {
	p := NewDiskUsageDelete(&config.Config{}, staticUsage(0))
	media := database.Media{Title: "A Movie", LibraryName: "Nope"}
	require.Error(t, p.Apply(&media))
}

func TestDiskUsageShouldTriggerDeletion(t *testing.T) {
	cfg := diskUsageConfig(config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 2})

	pastDate := time.Now().Add(-time.Hour)
	futureDate := time.Now().Add(time.Hour)

	tests := []struct {
		name       string
		usage      UsageFunc
		deleteDate time.Time
		want       bool
	}{
		{"above threshold and past date triggers", staticUsage(95), pastDate, true},
		{"above threshold but future date waits", staticUsage(95), futureDate, false},
		{"below threshold never triggers", staticUsage(50), pastDate, false},
		{"exactly at threshold triggers", staticUsage(90), pastDate, true},
		{"unavailable usage never triggers", unavailableUsage(), pastDate, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewDiskUsageDelete(cfg, tt.usage)
			media := database.Media{
				Title:       "A Movie",
				LibraryName: "Movies",
				DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{
					{Threshold: 90, DeleteDate: tt.deleteDate},
				},
			}
			got, err := p.ShouldTriggerDeletion(t.Context(), media)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDiskUsageShouldTriggerDeletionPassesMediaType(t *testing.T) {
	cfg := diskUsageConfig(config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 2})
	var seen database.MediaType
	p := NewDiskUsageDelete(cfg, func(_ context.Context, mt database.MediaType) (float64, bool, error) {
		seen = mt
		return 0, true, nil
	})
	media := database.Media{
		LibraryName: "Movies",
		MediaType:   database.MediaTypeMovie,
		DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{
			{Threshold: 90, DeleteDate: time.Now().Add(-time.Hour)},
		},
	}
	_, err := p.ShouldTriggerDeletion(t.Context(), media)
	require.NoError(t, err)
	require.Equal(t, database.MediaTypeMovie, seen)
}

func TestDiskUsageShouldTriggerDeletionNoPolicies(t *testing.T) {
	p := NewDiskUsageDelete(diskUsageConfig(), staticUsage(100))
	got, err := p.ShouldTriggerDeletion(t.Context(), database.Media{LibraryName: "Movies"})
	require.NoError(t, err)
	require.False(t, got)
}

func TestDiskUsageShouldTriggerDeletionNoThresholdsConfigured(t *testing.T) {
	// Policies stored on the media but thresholds removed from the config.
	p := NewDiskUsageDelete(diskUsageConfig(), staticUsage(100))
	media := database.Media{
		LibraryName: "Movies",
		DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{
			{Threshold: 90, DeleteDate: time.Now().Add(-time.Hour)},
		},
	}
	got, err := p.ShouldTriggerDeletion(t.Context(), media)
	require.NoError(t, err)
	require.False(t, got)
}

func TestDiskUsageShouldTriggerDeletionUsageErrorPropagates(t *testing.T) {
	cfg := diskUsageConfig(config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 2})
	failing := func(context.Context, database.MediaType) (float64, bool, error) {
		return 0, false, errors.New("boom")
	}
	p := NewDiskUsageDelete(cfg, failing)
	media := database.Media{
		LibraryName: "Movies",
		DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{
			{Threshold: 90, DeleteDate: time.Now().Add(-time.Hour)},
		},
	}
	_, err := p.ShouldTriggerDeletion(t.Context(), media)
	require.Error(t, err)
}

func TestDiskUsageShouldTriggerDeletionZeroDeleteDateSkipped(t *testing.T) {
	cfg := diskUsageConfig(config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 2})
	p := NewDiskUsageDelete(cfg, staticUsage(95))
	media := database.Media{
		LibraryName: "Movies",
		DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{
			{Threshold: 90}, // zero DeleteDate
		},
	}
	got, err := p.ShouldTriggerDeletion(t.Context(), media)
	require.NoError(t, err)
	require.False(t, got)
}

func TestDiskUsageGetEstimatedDeleteAt(t *testing.T) {
	cfg := diskUsageConfig(
		config.DiskUsageThreshold{UsagePercent: 80, MaxCleanupDelay: 10},
		config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 2},
	)
	in10Days := time.Now().Add(10 * 24 * time.Hour)
	in2Days := time.Now().Add(2 * 24 * time.Hour)
	policies := []database.DiskUsageDeletePolicy{
		{Threshold: 80, DeleteDate: in10Days},
		{Threshold: 90, DeleteDate: in2Days},
	}

	tests := []struct {
		name  string
		usage UsageFunc
		want  time.Time
	}{
		{"both thresholds exceeded picks earliest", staticUsage(95), in2Days},
		{"only lower threshold exceeded", staticUsage(85), in10Days},
		{"below all thresholds estimates nothing", staticUsage(50), time.Time{}},
		{"usage unavailable estimates nothing", unavailableUsage(), time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewDiskUsageDelete(cfg, tt.usage)
			media := database.Media{Title: "A Movie", LibraryName: "Movies", DiskUsageDeletePolicies: policies}

			got, err := p.GetEstimatedDeleteAt(t.Context(), media)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDiskUsageGetEstimatedDeleteAtUsageErrorPropagates(t *testing.T) {
	cfg := diskUsageConfig(config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 1})
	p := NewDiskUsageDelete(cfg, func(context.Context, database.MediaType) (float64, bool, error) {
		return 0, false, errors.New("boom")
	})
	_, err := p.GetEstimatedDeleteAt(t.Context(), database.Media{
		LibraryName:             "Movies",
		DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{{Threshold: 90, DeleteDate: time.Now()}},
	})
	require.Error(t, err)
}

func TestDiskUsageGetEstimatedDeleteAtWithoutPoliciesOrThresholds(t *testing.T) {
	// Neither stored policies nor configured thresholds must consult the usage.
	touched := false
	usage := func(context.Context, database.MediaType) (float64, bool, error) {
		touched = true
		return 99, true, nil
	}

	p := NewDiskUsageDelete(diskUsageConfig(config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 1}), usage)
	got, err := p.GetEstimatedDeleteAt(t.Context(), database.Media{LibraryName: "Movies"})
	require.NoError(t, err)
	require.True(t, got.IsZero())

	p = NewDiskUsageDelete(diskUsageConfig(), usage)
	got, err = p.GetEstimatedDeleteAt(t.Context(), database.Media{
		LibraryName:             "Movies",
		DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{{Threshold: 90, DeleteDate: time.Now()}},
	})
	require.NoError(t, err)
	require.True(t, got.IsZero(), "stale policies without configured thresholds must be ignored")
	require.False(t, touched)
}
