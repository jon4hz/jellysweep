package policy

import (
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/stretchr/testify/require"
)

func TestDefaultDeleteApplySetsDeleteDate(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {CleanupDelay: 7},
		},
	}
	p := NewDefaultDelete(cfg)

	media := database.Media{Title: "A Movie", LibraryName: "Movies"}
	require.NoError(t, p.Apply(&media))
	require.WithinDuration(t, time.Now().Add(7*24*time.Hour), media.DefaultDeleteAt, time.Minute)
}

func TestDefaultDeleteApplyUsesDefaultDelay(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {CleanupDelay: 0}, // unset -> 30 days default
		},
	}
	p := NewDefaultDelete(cfg)

	media := database.Media{Title: "A Movie", LibraryName: "Movies"}
	require.NoError(t, p.Apply(&media))
	require.WithinDuration(t, time.Now().Add(30*24*time.Hour), media.DefaultDeleteAt, time.Minute)
}

func TestDefaultDeleteApplyUnknownLibraryErrors(t *testing.T) {
	p := NewDefaultDelete(&config.Config{})

	media := database.Media{Title: "A Movie", LibraryName: "Nope"}
	require.Error(t, p.Apply(&media))
	require.True(t, media.DefaultDeleteAt.IsZero())
}

func TestDefaultDeleteShouldTriggerDeletion(t *testing.T) {
	p := NewDefaultDelete(&config.Config{})

	tests := []struct {
		name            string
		defaultDeleteAt time.Time
		want            bool
	}{
		{"past date triggers", time.Now().Add(-time.Hour), true},
		{"future date does not trigger", time.Now().Add(time.Hour), false},
		{"zero date never triggers", time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.ShouldTriggerDeletion(t.Context(), database.Media{DefaultDeleteAt: tt.defaultDeleteAt})
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultDeleteGetEstimatedDeleteAt(t *testing.T) {
	p := NewDefaultDelete(&config.Config{})
	deleteAt := time.Now().Add(7 * 24 * time.Hour)

	got, err := p.GetEstimatedDeleteAt(t.Context(), database.Media{DefaultDeleteAt: deleteAt})
	require.NoError(t, err)
	require.Equal(t, deleteAt, got, "the default policy always estimates its own delete date")
}
