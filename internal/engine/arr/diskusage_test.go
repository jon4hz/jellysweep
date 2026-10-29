package arr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootFolderUsage(t *testing.T) {
	mounts := []Mount{
		{Path: "/", Free: 50, Total: 100},
		{Path: "/data", Free: 10, Total: 100},
		{Path: "/data/movies", Free: 80, Total: 100},
		{Path: "/broken", Free: 0, Total: 0},
	}

	tests := []struct {
		name  string
		roots []string
		want  map[string]float64
	}{
		{"longest mount prefix wins", []string{"/data/movies/4k"}, map[string]float64{"/data/movies/4k": 20}},
		{"parent mount used for sibling folder", []string{"/data/tv"}, map[string]float64{"/data/tv": 90}},
		{"root mount matches everything", []string{"/srv/media"}, map[string]float64{"/srv/media": 50}},
		{"trailing slash on root folder ignored", []string{"/data/movies/"}, map[string]float64{"/data/movies/": 20}},
		{"mount with zero total is skipped", []string{"/broken/x"}, map[string]float64{"/broken/x": 50}},
		{"no prefix match omitted", []string{"D:\\media"}, map[string]float64{}},
		{"exact mount match", []string{"/data"}, map[string]float64{"/data": 90}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, RootFolderUsage(tt.roots, mounts))
		})
	}
}

func TestRootFolderUsageNoMounts(t *testing.T) {
	require.Empty(t, RootFolderUsage([]string{"/data"}, nil))
}

func TestRootFolderUsageDoesNotMatchPartialSegment(t *testing.T) {
	mounts := []Mount{{Path: "/data", Free: 0, Total: 100}}
	// "/database" is not under "/data"; nothing matches, so it is omitted.
	require.Empty(t, RootFolderUsage([]string{"/database"}, mounts))
}
