package engine

import (
	"errors"
	"testing"

	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/stretchr/testify/require"
)

func TestDiskUsageFuncSnapshotsPerMediaType(t *testing.T) {
	h := newHarness(t, withEngineDiskUsage())
	h.sonarr.rootFolderUsage = map[string]float64{"/data/tv": 40, "/data/tv2": 70}
	h.radarr.rootFolderUsage = map[string]float64{"/data/movies": 95}

	usage := h.e.newDiskUsageFunc(t.Context())

	got, ok, err := usage(t.Context(), database.MediaTypeTV)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 70.0, got, "highest root folder usage wins")

	got, ok, err = usage(t.Context(), database.MediaTypeMovie)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 95.0, got)

	// Answering from the snapshot must not hit the arrs again.
	require.Equal(t, 1, h.sonarr.rootFolderUsageCalls)
	require.Equal(t, 1, h.radarr.rootFolderUsageCalls)
}

func TestDiskUsageFuncArrFailureIsNotOK(t *testing.T) {
	h := newHarness(t, withEngineDiskUsage())
	h.sonarr.rootFolderUsageErr = errors.New("sonarr down")
	h.radarr.rootFolderUsage = map[string]float64{"/data/movies": 95}

	usage := h.e.newDiskUsageFunc(t.Context())

	_, ok, err := usage(t.Context(), database.MediaTypeTV)
	require.NoError(t, err, "an unreachable arr must not fail the run")
	require.False(t, ok)

	got, ok, err := usage(t.Context(), database.MediaTypeMovie)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 95.0, got)
}

func TestDiskUsageFuncNoRootFoldersIsNotOK(t *testing.T) {
	h := newHarness(t, withEngineDiskUsage())
	usage := h.e.newDiskUsageFunc(t.Context())
	_, ok, err := usage(t.Context(), database.MediaTypeMovie)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDiskUsageFuncNilArrIsNotOK(t *testing.T) {
	h := newHarness(t, withEngineDiskUsage())
	h.e.sonarr = nil
	usage := h.e.newDiskUsageFunc(t.Context())
	_, ok, err := usage(t.Context(), database.MediaTypeTV)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCleanupRunFetchesDiskUsageOncePerArr(t *testing.T) {
	h := newHarness(t, withEngineDiskUsage(), withDiskThresholds("Movies", 90, 1), withDiskThresholds("TV", 80, 1))
	h.radarr.rootFolderUsage = map[string]float64{"/data/movies": 95}
	h.sonarr.rootFolderUsage = map[string]float64{"/data/tv": 95}
	for i := 0; i < 5; i++ {
		h.addMovie("Movie")
		h.addSeries("Series")
	}

	h.mustRunCleanup()

	require.Equal(t, 1, h.radarr.rootFolderUsageCalls, "usage must be fetched once per run, not per item")
	require.Equal(t, 1, h.sonarr.rootFolderUsageCalls)
}
