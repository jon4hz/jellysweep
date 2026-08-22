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
	return func(context.Context, string) (float64, error) {
		return percent, nil
	}
}

var moviesFolders = map[string][]string{"Movies": {"/data/movies"}}

func TestDiskUsageApplyCreatesPolicies(t *testing.T) {
	cfg := diskUsageConfig(
		config.DiskUsageThreshold{UsagePercent: 80, MaxCleanupDelay: 10},
		config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 2},
	)
	p := NewDiskUsageDelete(cfg, moviesFolders)

	media := database.Media{Title: "A Movie", LibraryName: "Movies"}
	require.NoError(t, p.Apply(&media))
	require.Len(t, media.DiskUsageDeletePolicies, 2)
	require.Equal(t, 80.0, media.DiskUsageDeletePolicies[0].Threshold)
	require.WithinDuration(t, time.Now().Add(10*24*time.Hour), media.DiskUsageDeletePolicies[0].DeleteDate, time.Minute)
	require.Equal(t, 90.0, media.DiskUsageDeletePolicies[1].Threshold)
	require.WithinDuration(t, time.Now().Add(2*24*time.Hour), media.DiskUsageDeletePolicies[1].DeleteDate, time.Minute)
}

func TestDiskUsageApplyClearsStalePolicies(t *testing.T) {
	p := NewDiskUsageDelete(diskUsageConfig(), moviesFolders)

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
	p := NewDiskUsageDelete(&config.Config{}, moviesFolders)
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewDiskUsageDelete(cfg, moviesFolders, WithUsageFunc(tt.usage))
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

func TestDiskUsageShouldTriggerDeletionNoPolicies(t *testing.T) {
	p := NewDiskUsageDelete(diskUsageConfig(), moviesFolders, WithUsageFunc(staticUsage(100)))
	got, err := p.ShouldTriggerDeletion(t.Context(), database.Media{LibraryName: "Movies"})
	require.NoError(t, err)
	require.False(t, got)
}

func TestDiskUsageShouldTriggerDeletionNoThresholdsConfigured(t *testing.T) {
	// Policies stored on the media but thresholds removed from the config.
	p := NewDiskUsageDelete(diskUsageConfig(), moviesFolders, WithUsageFunc(staticUsage(100)))
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

func TestDiskUsageShouldTriggerDeletionMissingFoldersErrors(t *testing.T) {
	cfg := diskUsageConfig(config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 2})
	p := NewDiskUsageDelete(cfg, map[string][]string{}, WithUsageFunc(staticUsage(100)))
	media := database.Media{
		LibraryName: "Movies",
		DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{
			{Threshold: 90, DeleteDate: time.Now().Add(-time.Hour)},
		},
	}
	_, err := p.ShouldTriggerDeletion(t.Context(), media)
	require.Error(t, err)
}

func TestDiskUsageShouldTriggerDeletionUnreadableDiskFailsClosed(t *testing.T) {
	cfg := diskUsageConfig(config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 2})
	failing := func(context.Context, string) (float64, error) {
		return 0, errors.New("disk gone")
	}
	p := NewDiskUsageDelete(cfg, moviesFolders, WithUsageFunc(failing))
	media := database.Media{
		LibraryName: "Movies",
		DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{
			{Threshold: 90, DeleteDate: time.Now().Add(-time.Hour)},
		},
	}
	got, err := p.ShouldTriggerDeletion(t.Context(), media)
	require.NoError(t, err, "unreadable disk must not fail the run")
	require.False(t, got, "unreadable disk must never trigger a deletion")
}

func TestDiskUsageShouldTriggerDeletionHighestUsageWins(t *testing.T) {
	cfg := diskUsageConfig(config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 2})
	folders := map[string][]string{"Movies": {"/data/a", "/data/b"}}
	usageByPath := func(_ context.Context, path string) (float64, error) {
		if path == "/data/b" {
			return 95, nil
		}
		return 10, nil
	}
	p := NewDiskUsageDelete(cfg, folders, WithUsageFunc(usageByPath))
	media := database.Media{
		LibraryName: "Movies",
		DiskUsageDeletePolicies: []database.DiskUsageDeletePolicy{
			{Threshold: 90, DeleteDate: time.Now().Add(-time.Hour)},
		},
	}
	got, err := p.ShouldTriggerDeletion(t.Context(), media)
	require.NoError(t, err)
	require.True(t, got, "the fullest folder determines the usage")
}

func TestDiskUsageShouldTriggerDeletionZeroDeleteDateSkipped(t *testing.T) {
	cfg := diskUsageConfig(config.DiskUsageThreshold{UsagePercent: 90, MaxCleanupDelay: 2})
	p := NewDiskUsageDelete(cfg, moviesFolders, WithUsageFunc(staticUsage(95)))
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
