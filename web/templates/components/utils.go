package components

import (
	"time"

	"github.com/dustin/go-humanize"
	"github.com/mergestat/timediff"
)

// FormatRelativeTime formats a time.Time as a relative time string like "in 3 days"
func FormatRelativeTime(t time.Time) string {
	return timediff.TimeDiff(t)
}

// FormatFileSize formats a file size in bytes to a human-readable string
func FormatFileSize(bytes int64) string {
	return humanize.Bytes(uint64(bytes))
}
