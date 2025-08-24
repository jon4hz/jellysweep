package tags

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/config"
	"github.com/shirou/gopsutil/v3/disk"
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
	FullTag      string
	Prefix       string
	DiskUsage    float64 // For disk usage tags (du90, du70, etc.)
	DeletionDate time.Time
	IsExpired    bool
}

// ParseJellysweepTag parses a jellysweep tag and returns information about it.
func ParseJellysweepTag(tagName string) (*TagInfo, error) {
	if !IsJellysweepTag(tagName) {
		return nil, fmt.Errorf("not a jellysweep tag: %s", tagName)
	}

	info := &TagInfo{
		FullTag: tagName,
	}

	// Handle disk usage tags (jellysweep-delete-du90-2025-08-23)
	if strings.HasPrefix(tagName, jellysweepDiskUsageTagPrefix) {
		info.Prefix = jellysweepDiskUsageTagPrefix

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
	} else if strings.HasPrefix(tagName, JellysweepTagPrefix) {
		// Handle regular jellysweep-delete tags
		info.Prefix = JellysweepTagPrefix

		dateStr := strings.TrimPrefix(tagName, JellysweepTagPrefix)
		var err error
		info.DeletionDate, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse date from tag %s: %v", tagName, err)
		}
	} else {
		return nil, fmt.Errorf("unknown jellysweep tag format: %s", tagName)
	}

	info.IsExpired = info.DeletionDate.Before(time.Now())
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

// IsJellysweepDeleteTag checks if a tag is a jellysweep delete tag (including disk usage tags).
func IsJellysweepDeleteTag(tagName string) bool {
	return strings.HasPrefix(tagName, JellysweepTagPrefix) ||
		strings.HasPrefix(tagName, jellysweepDiskUsageTagPrefix) ||
		tagName == JellysweepDeleteForSureTag
}

// GenerateDeletionTags creates deletion tags based on library configuration and disk usage.
// It returns all applicable tags that should be added to the media item.
func GenerateDeletionTags(ctx context.Context, cfg *config.Config, libraryName string, libraryFoldersMap map[string][]string) ([]string, error) {
	libraryConfig := cfg.GetLibraryConfig(libraryName)
	if libraryConfig == nil {
		return nil, fmt.Errorf("no configuration found for library: %s", libraryName)
	}

	var tags []string

	// Always add the default cleanup tag
	cleanupDelay := libraryConfig.CleanupDelay
	if cleanupDelay <= 0 {
		cleanupDelay = 1
	}
	deletionDate := time.Now().Add(time.Duration(cleanupDelay) * 24 * time.Hour)
	defaultTag := fmt.Sprintf("%s%s", JellysweepTagPrefix, deletionDate.Format("2006-01-02"))
	tags = append(tags, defaultTag)

	// Add disk usage threshold tags if configured
	if len(libraryConfig.DiskUsageThresholds) > 0 {
		for _, threshold := range libraryConfig.DiskUsageThresholds {
			deletionDate := time.Now().Add(time.Duration(threshold.MaxCleanupDelay) * 24 * time.Hour)
			duTag := fmt.Sprintf("%s%.0f-%s",
				jellysweepDiskUsageTagPrefix,
				threshold.UsagePercent,
				deletionDate.Format("2006-01-02"))
			tags = append(tags, duTag)

			log.Debugf("Added disk usage tag for library %s: %s (threshold: %.1f%%, days: %d)",
				libraryName, duTag, threshold.UsagePercent, threshold.MaxCleanupDelay)
		}
	}

	return tags, nil
}

// ShouldTriggerDeletion checks if a media item should be deleted based on its tags.
func ShouldTriggerDeletion(tagNames []string) bool {
	for _, tagName := range tagNames {
		if !IsJellysweepDeleteTag(tagName) {
			continue
		}

		// Skip JellysweepDeleteForSureTag
		if tagName == JellysweepDeleteForSureTag {
			continue
		}

		if strings.HasPrefix(tagName, jellysweepDiskUsageTagPrefix) {
			continue
		}

		// Parse the tag to check if it's expired
		tagInfo, err := ParseJellysweepTag(tagName)
		if err != nil {
			log.Warnf("Failed to parse jellysweep tag %s: %v", tagName, err)
			continue
		}

		if tagInfo.IsExpired {
			return true
		}
	}

	return false
}

// getLibraryDiskUsage gets disk usage in percentage for a given library path.
func getLibraryDiskUsage(ctx context.Context, path string) (float64, error) {
	usage, err := disk.UsageWithContext(ctx, path)
	if err != nil {
		return 0, err
	}
	return usage.UsedPercent, nil
}

// ShouldTriggerDeletionBasedOnDiskUsage checks if a media item should be deleted based on current disk usage.
// It checks the current disk usage against the library's thresholds and determines if any of the item's tags
// should trigger deletion.
func ShouldTriggerDeletionBasedOnDiskUsage(ctx context.Context, cfg *config.Config, libraryName string, tagNames []string, libraryFoldersMap map[string][]string) bool {
	libraryConfig := cfg.GetLibraryConfig(libraryName)
	if libraryConfig == nil || len(libraryConfig.DiskUsageThresholds) == 0 {
		// If no config, fall back to basic expired tag check
		return ShouldTriggerDeletion(tagNames)
	}

	// Get library paths for disk usage calculation
	libraryPaths, exists := libraryFoldersMap[libraryName]
	if !exists || len(libraryPaths) == 0 {
		log.Warnf("No library paths found for %s, using basic tag expiration check", libraryName)
		return ShouldTriggerDeletion(tagNames)
	}

	// Get current disk usage
	var currentDiskUsage float64
	var diskUsageError error
	for _, path := range libraryPaths {
		usage, err := getLibraryDiskUsage(ctx, path)
		if err != nil {
			log.Error("failed to get disk usage", "path", path, "error", err)
			diskUsageError = err
			continue
		}
		// Use the highest disk usage among all paths
		if usage > currentDiskUsage {
			currentDiskUsage = usage
		}
	}

	if diskUsageError != nil && currentDiskUsage == 0 {
		log.Warnf("Could not determine disk usage for library %s, using basic tag expiration check", libraryName)
		return ShouldTriggerDeletion(tagNames)
	}

	log.Debugf("Current disk usage for library %s: %.1f%%", libraryName, currentDiskUsage)

	// Find the most restrictive threshold that applies to current disk usage
	var applicableThreshold *config.DiskUsageThreshold
	if len(libraryConfig.DiskUsageThresholds) > 0 {
		for _, threshold := range libraryConfig.DiskUsageThresholds {
			if currentDiskUsage >= threshold.UsagePercent {
				if applicableThreshold == nil || threshold.MaxCleanupDelay < applicableThreshold.MaxCleanupDelay {
					applicableThreshold = &threshold
				}
			}
		}
	}

	// Check each tag to see if it should trigger deletion
	for _, tagName := range tagNames {
		if !IsJellysweepDeleteTag(tagName) {
			continue
		}

		// Skip JellysweepDeleteForSureTag
		if tagName == JellysweepDeleteForSureTag {
			continue
		}

		// Parse the tag to check expiration
		tagInfo, err := ParseJellysweepTag(tagName)
		if err != nil {
			log.Warnf("Failed to parse jellysweep tag %s: %v", tagName, err)
			continue
		}

		// For disk usage tags, check if they match the current applicable threshold
		if strings.HasPrefix(tagName, jellysweepDiskUsageTagPrefix) && applicableThreshold != nil {
			if tagInfo.DiskUsage == applicableThreshold.UsagePercent && tagInfo.IsExpired {
				log.Debugf("Item should be deleted due to disk usage tag %s (current usage: %.1f%%, threshold: %.1f%%)",
					tagName, currentDiskUsage, applicableThreshold.UsagePercent)
				return true
			}
		}

		// For regular delete tags, check if they're expired and no more restrictive threshold applies
		if strings.HasPrefix(tagName, JellysweepTagPrefix) && !strings.HasPrefix(tagName, jellysweepDiskUsageTagPrefix) && tagInfo.IsExpired {
			if applicableThreshold == nil {
				// No disk pressure, use regular expiration
				log.Debugf("Item should be deleted due to expired regular tag %s (no disk pressure)", tagName)
				return true
			}
			// If there's disk pressure but no applicable disk usage tag, still check regular expiration
			// This handles cases where the item was tagged before disk usage thresholds were configured
			log.Debugf("Item should be deleted due to expired regular tag %s (fallback with disk pressure)", tagName)
			return true
		}
	}

	return false
}

// ParseDeletionDateFromTag calculates the earliest deletion date based on current disk usage and all delete tags.
// This method checks all jellysweep delete tags on the media item and returns the earliest applicable deletion date
// based on current disk usage thresholds.
func ParseDeletionDateFromTag(ctx context.Context, cfg *config.Config, tagNames []string, libraryName string, libraryFoldersMap map[string][]string) (time.Time, error) {
	libraryConfig := cfg.GetLibraryConfig(libraryName)
	if libraryConfig == nil {
		return time.Time{}, fmt.Errorf("no configuration found for library: %s", libraryName)
	}

	// Get library paths for disk usage calculation
	libraryPaths, exists := libraryFoldersMap[libraryName]
	if !exists || len(libraryPaths) == 0 || len(libraryConfig.DiskUsageThresholds) == 0 {
		// No library paths available, use default cleanup delay
		cleanupDelay := libraryConfig.CleanupDelay
		if cleanupDelay <= 0 {
			cleanupDelay = 1
		}
		return time.Now().Add(time.Duration(cleanupDelay) * 24 * time.Hour), nil
	}

	// Get current disk usage
	var currentDiskUsage float64
	for _, path := range libraryPaths {
		usage, err := getLibraryDiskUsage(ctx, path)
		if err != nil {
			log.Error("failed to get disk usage", "path", path, "error", err)
			continue
		}
		// Use the highest disk usage among all paths
		if usage > currentDiskUsage {
			currentDiskUsage = usage
		}
	}

	log.Debugf("Current disk usage for library %s: %.1f%%", libraryName, currentDiskUsage)

	// Find the most restrictive (smallest) delay that applies to current disk usage
	var smallestDelay int
	found := false

	// Check all delete tags to find applicable delays
	for _, tagName := range tagNames {
		if !IsJellysweepDeleteTag(tagName) {
			continue
		}

		// For JellysweepDeleteForSureTag, use the smallest possible delay
		if tagName == JellysweepDeleteForSureTag {
			// Find the smallest delay from disk usage thresholds or use default cleanup delay
			smallestPossibleDelay := libraryConfig.CleanupDelay
			if smallestPossibleDelay <= 0 {
				smallestPossibleDelay = 1
			}

			// Check if any disk usage threshold has a smaller delay
			for _, threshold := range libraryConfig.DiskUsageThresholds {
				if threshold.MaxCleanupDelay < smallestPossibleDelay {
					smallestPossibleDelay = threshold.MaxCleanupDelay
				}
			}

			if !found || smallestPossibleDelay < smallestDelay {
				smallestDelay = smallestPossibleDelay
				found = true
			}
			log.Debugf("JellysweepDeleteForSureTag found, using smallest possible delay: %d days", smallestPossibleDelay)
			continue
		}

		// Parse tag info
		tagInfo, err := ParseJellysweepTag(tagName)
		if err != nil {
			log.Warnf("Failed to parse jellysweep tag %s: %v", tagName, err)
			continue
		}

		var applicableDelay int
		isApplicable := false

		// For disk usage tags, check if current usage meets the threshold
		if strings.HasPrefix(tagName, jellysweepDiskUsageTagPrefix) {
			// Find matching threshold in config
			for _, threshold := range libraryConfig.DiskUsageThresholds {
				if threshold.UsagePercent == tagInfo.DiskUsage && currentDiskUsage >= threshold.UsagePercent {
					applicableDelay = threshold.MaxCleanupDelay
					isApplicable = true
					log.Debugf("Disk usage tag %s is applicable (current: %.1f%%, threshold: %.1f%%, delay: %d days)",
						tagName, currentDiskUsage, threshold.UsagePercent, applicableDelay)
					break
				}
			}
		} else if strings.HasPrefix(tagName, JellysweepTagPrefix) {
			// Regular delete tags are always applicable
			applicableDelay = libraryConfig.CleanupDelay
			if applicableDelay <= 0 {
				applicableDelay = 1
			}
			isApplicable = true
			log.Debugf("Regular delete tag %s is applicable (delay: %d days)", tagName, applicableDelay)
		}

		// Track the smallest applicable delay
		if isApplicable {
			if !found || applicableDelay < smallestDelay {
				smallestDelay = applicableDelay
				found = true
			}
		}
	}

	// If no applicable tags found, use default cleanup delay
	if !found {
		smallestDelay = libraryConfig.CleanupDelay
		if smallestDelay <= 0 {
			smallestDelay = 1
		}
		log.Debugf("No applicable delete tags found, using default delay: %d days", smallestDelay)
	}

	deletionDate := time.Now().Add(time.Duration(smallestDelay) * 24 * time.Hour)
	log.Debugf("Calculated earliest deletion date for library %s with disk usage %.1f%%, using delay of %d days (deletion: %s)",
		libraryName, currentDiskUsage, smallestDelay, deletionDate.Format("2006-01-02"))

	return deletionDate, nil
}

// ParseKeepTagWithRequester extracts the date and requester from a jellysweep-must-keep tag.
// Format: jellysweep-must-keep-YYYY-MM-DD-requester.
func ParseKeepTagWithRequester(tagName string) (time.Time, string, error) {
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

// CreateKeepTagWithRequester creates a jellysweep-must-keep tag with requester information.
// Format: jellysweep-must-keep-YYYY-MM-DD-requester.
func CreateKeepTagWithRequester(date time.Time, requester string) string {
	dateStr := date.Format("2006-01-02")
	var tagLabel string
	if requester != "" {
		// Sanitize requester to avoid issues with special characters
		sanitizedRequester := strings.ReplaceAll(requester, "-", "_")
		sanitizedRequester = strings.ReplaceAll(sanitizedRequester, " ", "_")
		tagLabel = fmt.Sprintf("%s%s-%s", JellysweepKeepPrefix, dateStr, sanitizedRequester)
	} else {
		tagLabel = fmt.Sprintf("%s%s", JellysweepKeepPrefix, dateStr)
	}
	return strings.ToLower(tagLabel)
}

// ParseKeepRequestTagWithRequester extracts the date and requester from a jellysweep-keep-request tag.
// Format: jellysweep-keep-request-YYYY-MM-DD-requester.
func ParseKeepRequestTagWithRequester(tagName string) (date time.Time, requester string, err error) {
	if !strings.HasPrefix(tagName, JellysweepKeepRequestPrefix) {
		return time.Time{}, "", fmt.Errorf("not a keep request tag")
	}

	// Remove the prefix
	tagContent := strings.TrimPrefix(tagName, JellysweepKeepRequestPrefix)

	// Split by dash to separate date and requester
	parts := strings.Split(tagContent, "-")

	// We need at least 3 parts for YYYY-MM-DD, and optionally a requester part
	if len(parts) < 3 {
		return time.Time{}, "", fmt.Errorf("invalid tag format")
	}

	// Parse date from first 3 parts
	dateStr := strings.Join(parts[:3], "-")
	date, err = time.Parse("2006-01-02", dateStr)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("failed to parse date: %w", err)
	}

	// If there's a 4th part, it's the requester
	if len(parts) > 3 {
		requester = parts[3]
	}

	return date, requester, nil
}

// CreateKeepRequestTagWithRequester creates a jellysweep-keep-request tag with requester information.
// Format: jellysweep-keep-request-YYYY-MM-DD-requester.
func CreateKeepRequestTagWithRequester(date time.Time, requester string) string {
	dateStr := date.Format("2006-01-02")
	var tagLabel string
	if requester != "" {
		// Sanitize requester to avoid issues with special characters
		sanitizedRequester := strings.ReplaceAll(requester, "-", "_")
		sanitizedRequester = strings.ReplaceAll(sanitizedRequester, " ", "_")
		tagLabel = fmt.Sprintf("%s%s-%s", JellysweepKeepRequestPrefix, dateStr, sanitizedRequester)
	} else {
		tagLabel = fmt.Sprintf("%s%s", JellysweepKeepRequestPrefix, dateStr)
	}
	return strings.ToLower(tagLabel)
}

// FilterTagsForMustDelete filters tags to preserve jellysweep-delete tags while removing other jellysweep tags.
// This is used when adding a must-delete-for-sure tag to ensure deletion timing is preserved.
func FilterTagsForMustDelete(allTagIDs []int32, tagMap map[int32]string) []int32 {
	var newTags []int32
	for _, tagID := range allTagIDs {
		tagName := tagMap[tagID]

		// Keep non-jellysweep tags
		if !IsJellysweepTag(tagName) {
			newTags = append(newTags, tagID)
			continue
		}

		// Keep jellysweep-delete tags (including must-delete-for-sure and disk usage tags)
		if IsJellysweepDeleteTag(tagName) {
			newTags = append(newTags, tagID)
			continue
		}

		// Remove other jellysweep tags (keep-request, must-keep, etc.)
		// These tags will be filtered out
	}
	return newTags
}
