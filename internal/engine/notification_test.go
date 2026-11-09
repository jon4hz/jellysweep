package engine

import (
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/stretchr/testify/require"
)

func TestNotificationCleanupDateMatchesRequesterEmail(t *testing.T) {
	e := &Engine{cfg: &config.Config{Libraries: map[string]*config.CleanupConfig{
		"Movies": {CleanupDelay: 14},
	}}}
	before := time.Now()

	cleanupDate := e.notificationCleanupDate("user@example.com", []arr.MediaItem{{
		LibraryName:    "Movies",
		RequestedBy:    "Jellyfin User",
		RequesterEmail: "user@example.com",
	}})

	require.WithinDuration(t, before.Add(14*24*time.Hour), cleanupDate, time.Second)
}
