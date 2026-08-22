package tags

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseJellysweepTag(t *testing.T) {
	date := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

	tests := []struct {
		tag  string
		want TagInfo
	}{
		{"jellysweep-delete-2025-08-23", TagInfo{DeletionDate: date(2025, time.August, 23)}},
		{"jellysweep-delete-du90-2025-08-23", TagInfo{DiskUsage: 90, DeletionDate: date(2025, time.August, 23)}},
		{"jellysweep-delete-du75.5-2025-01-02", TagInfo{DiskUsage: 75.5, DeletionDate: date(2025, time.January, 2)}},
		{"jellysweep-must-keep-2026-12-31-alice", TagInfo{ProtectedUntil: date(2026, time.December, 31)}},
		{"jellysweep-must-keep-2026-12-31", TagInfo{ProtectedUntil: date(2026, time.December, 31)}},
		{"jellysweep-must-delete-for-sure", TagInfo{MustDelete: true}},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			got, err := ParseJellysweepTag(tt.tag)
			require.NoError(t, err)
			require.Equal(t, tt.want, *got)
		})
	}
}

func TestParseJellysweepTagErrors(t *testing.T) {
	for _, tag := range []string{
		"not-a-jellysweep-tag",
		"jellysweep-delete-not-a-date",
		"jellysweep-delete-du90",
		"jellysweep-delete-duXX-2025-08-23",
		"jellysweep-must-keep-2026-13-45-alice",
		"jellysweep-keep-request-alice", // known prefix, but no parseable payload
	} {
		t.Run(tag, func(t *testing.T) {
			_, err := ParseJellysweepTag(tag)
			require.Error(t, err)
		})
	}
}

func TestIsJellysweepTag(t *testing.T) {
	require.True(t, IsJellysweepTag("jellysweep-delete-2025-08-23"))
	require.True(t, IsJellysweepTag("jellysweep-ignore"))
	require.True(t, IsJellysweepTag("jellysweep-keep-request-alice"))
	require.False(t, IsJellysweepTag("4k"))
	require.False(t, IsJellysweepTag("jellysweeper"))

	require.True(t, IsJellysweepTagWithoutIgnore("jellysweep-delete-2025-08-23"))
	require.False(t, IsJellysweepTagWithoutIgnore("jellysweep-ignore"), "the ignore tag must be preserved during resets")

	require.True(t, IsJellysweepOrAdditionalTag("custom", []string{"custom"}))
	require.False(t, IsJellysweepOrAdditionalTag("jellysweep-ignore", nil))
	require.False(t, IsJellysweepOrAdditionalTag("4k", []string{"custom"}))
}
