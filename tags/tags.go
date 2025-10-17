/*
Legacy jellysweep tag parsing and constants.
This is now only used to migrate old tag-based deletions to the new database-driven system.
*/

package tags

import (
	"fmt"
	"strings"
	"time"
)

// Tag type constants for jellysweep tagging system.
const (
	// Tag prefixes for different types of jellysweep tags.
	JellysweepTagPrefix         = "jellysweep-delete-"
	JellysweepKeepRequestPrefix = "jellysweep-keep-request-"
	JellysweepKeepPrefix        = "jellysweep-must-keep-"

	// Special tags.
	JellysweepDeleteForSureTag = "jellysweep-must-delete-for-sure"
	JellysweepIgnoreTag        = "jellysweep-ignore"

	// jellysweepDiskUsageTagPrefix is the prefix for disk usage-based deletion tags.
	jellysweepDiskUsageTagPrefix = "jellysweep-delete-du"
)

// TagInfo contains information about a jellysweep tag.
type TagInfo struct {
	DiskUsage      float64 // For disk usage tags (du90, du70, etc.)
	DeletionDate   time.Time
	ProtectedUntil time.Time
	MustDelete     bool
}

// ParseJellysweepTag parses a jellysweep tag and returns information about it.
func ParseJellysweepTag(tagName string) (*TagInfo, error) {
	if !IsJellysweepTag(tagName) {
		return nil, fmt.Errorf("not a jellysweep tag: %s", tagName)
	}

	info := new(TagInfo)
	// Handle disk usage tags (jellysweep-delete-du90-2025-08-23)
	switch {
	case strings.HasPrefix(tagName, jellysweepDiskUsageTagPrefix):
		// Extract parts: jellysweep-delete-du90-2025-08-23
		parts := strings.Split(tagName, "-")
		if len(parts) < 6 {
			return nil, fmt.Errorf("invalid disk usage tag format: %s", tagName)
		}

		// Parse disk usage percentage (du90 -> 90.0)
		duPart := parts[2] // "du90"
		if !strings.HasPrefix(duPart, "du") {
			return nil, fmt.Errorf("invalid disk usage tag format, missing 'du' prefix: %s", tagName)
		}

		var err error
		if _, err = fmt.Sscanf(duPart, "du%f", &info.DiskUsage); err != nil {
			return nil, fmt.Errorf("failed to parse disk usage from tag %s: %v", tagName, err)
		}

		// Parse date (2025-08-23)
		dateStr := strings.Join(parts[3:], "-")
		info.DeletionDate, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse date from tag %s: %v", tagName, err)
		}

	case strings.HasPrefix(tagName, JellysweepTagPrefix):
		dateStr := strings.TrimPrefix(tagName, JellysweepTagPrefix)
		var err error
		info.DeletionDate, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse date from tag %s: %v", tagName, err)
		}

	case strings.HasPrefix(tagName, JellysweepKeepPrefix):
		protectedUntil, _, err := parseKeepTagWithRequester(tagName)
		if err != nil {
			return nil, fmt.Errorf("failed to parse protected date from tag %s: %v", tagName, err)
		}
		info.ProtectedUntil = protectedUntil

	case strings.HasPrefix(tagName, JellysweepDeleteForSureTag):
		info.MustDelete = true

	default:
		return nil, fmt.Errorf("unknown jellysweep tag format: %s", tagName)
	}

	return info, nil
}

// IsJellysweepTag checks if a tag is a jellysweep tag.
func IsJellysweepTag(tagName string) bool {
	return strings.HasPrefix(tagName, JellysweepTagPrefix) ||
		strings.HasPrefix(tagName, JellysweepKeepRequestPrefix) ||
		strings.HasPrefix(tagName, JellysweepKeepPrefix) ||
		strings.HasPrefix(tagName, jellysweepDiskUsageTagPrefix) ||
		tagName == JellysweepDeleteForSureTag ||
		tagName == JellysweepIgnoreTag
}

// parseKeepTagWithRequester extracts the date and requester from a jellysweep-must-keep tag.
// Format: jellysweep-must-keep-YYYY-MM-DD-requester.
func parseKeepTagWithRequester(tagName string) (time.Time, string, error) {
	if !strings.HasPrefix(tagName, JellysweepKeepPrefix) {
		return time.Time{}, "", fmt.Errorf("not a keep tag")
	}

	// Remove the prefix
	tagContent := strings.TrimPrefix(tagName, JellysweepKeepPrefix)

	// Split by dash to separate date and requester
	parts := strings.Split(tagContent, "-")

	// We need at least 3 parts for YYYY-MM-DD, and optionally a requester part
	if len(parts) < 3 {
		return time.Time{}, "", fmt.Errorf("invalid tag format")
	}

	// Parse date from first 3 parts
	dateStr := strings.Join(parts[:3], "-")
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("failed to parse date: %w", err)
	}

	var requester string
	// If there's a 4th part, it's the requester
	if len(parts) > 3 {
		requester = parts[3]
	}

	return date, requester, nil
}
