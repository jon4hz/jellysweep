package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/shirou/gopsutil/v3/disk"
)

// DiskUsageDelete applies when disk usage exceeds a certain threshold.
type DiskUsageDelete struct {
	cfg               *config.Config
	libraryFoldersMap map[string][]string
}

var _ Policy = (*DiskUsageDelete)(nil)

// NewDiskUsageDelete creates a new instance of DiskUsageDelete.
func NewDiskUsageDelete(cfg *config.Config, libraryFoldersMap map[string][]string) *DiskUsageDelete {
	return &DiskUsageDelete{
		cfg:               cfg,
		libraryFoldersMap: libraryFoldersMap,
	}
}

// Apply adds a DiskUsageDeletePolicy if the library has a disk usage threshold set.
func (p *DiskUsageDelete) Apply(media *database.Media) error {
	libraryConfig := p.cfg.GetLibraryConfig(media.LibraryName)
	if libraryConfig == nil {
		return fmt.Errorf("no configuration found for library: %s", media.LibraryName)
	}
	if len(libraryConfig.DiskUsageThresholds) > 0 {
		media.DiskUsageDeletePolicies = make([]database.DiskUsageDeletePolicy, 0, len(libraryConfig.DiskUsageThresholds))
		for _, threshold := range libraryConfig.DiskUsageThresholds {
			deletionDate := time.Now().Add(time.Duration(threshold.MaxCleanupDelay) * 24 * time.Hour)
			media.DiskUsageDeletePolicies = append(media.DiskUsageDeletePolicies, database.DiskUsageDeletePolicy{
				Threshold:  threshold.UsagePercent,
				DeleteDate: deletionDate,
			})
			log.Debug("Added disk usage delete policy",
				"item", media.Title,
				"library", media.LibraryName,
				"threshold", threshold.UsagePercent,
				"deleteAt", deletionDate,
			)
		}
	}
	return nil
}

// ShouldTriggerDeletion checks if any disk usage policy thresholds are exceeded.
func (p *DiskUsageDelete) ShouldTriggerDeletion(ctx context.Context, media database.Media) (bool, error) {
	if len(media.DiskUsageDeletePolicies) == 0 {
		return false, nil
	}

	currentDiskUsage, err := p.getCurrentDiskUsage(ctx, media.LibraryName)
	if err != nil {
		return false, err
	}

	for _, policy := range media.DiskUsageDeletePolicies {
		if currentDiskUsage >= policy.Threshold {
			if policy.DeleteDate.IsZero() {
				log.Warn("Disk usage threshold exceeded but no delete date set in policy. This should not happen.")
				continue
			}

			if time.Now().After(policy.DeleteDate) {
				log.Info("Disk usage threshold exceeded, marking media for deletion",
					"item", media.Title,
					"library", media.LibraryName,
					"currentUsage", currentDiskUsage,
					"threshold", policy.Threshold,
					"deleteAt", policy.DeleteDate,
				)
				return true, nil
			}
			log.Debug("Disk usage threshold exceeded, but not yet time to delete",
				"item", media.Title,
				"library", media.LibraryName,
				"currentUsage", currentDiskUsage,
				"threshold", policy.Threshold,
				"deleteAt", policy.DeleteDate,
			)
		} else {
			log.Debug("Disk usage below threshold, no deletion needed",
				"item", media.Title,
				"library", media.LibraryName,
				"currentUsage", currentDiskUsage,
				"threshold", policy.Threshold,
			)
		}
	}

	return false, nil
}

func (p *DiskUsageDelete) GetEstimatedDeleteAt(ctx context.Context, media database.Media) (time.Time, error) {
	if len(media.DiskUsageDeletePolicies) == 0 {
		return time.Time{}, nil
	}

	currentDiskUsage, err := p.getCurrentDiskUsage(ctx, media.LibraryName)
	if err != nil {
		return time.Time{}, err
	}

	var earliestDeleteDate time.Time
	for _, policy := range media.DiskUsageDeletePolicies {
		if currentDiskUsage >= policy.Threshold {
			if policy.DeleteDate.IsZero() {
				log.Warn("Disk usage threshold exceeded but no delete date set in policy. This should not happen.")
				continue
			}

			if earliestDeleteDate.IsZero() || policy.DeleteDate.Before(earliestDeleteDate) {
				earliestDeleteDate = policy.DeleteDate
			}
		}
	}

	return earliestDeleteDate, nil
}

// getCurrentDiskUsage retrieves the current disk usage for a library.
// Returns the highest usage percentage among all library folders.
func (p *DiskUsageDelete) getCurrentDiskUsage(ctx context.Context, libraryName string) (float64, error) {
	folders, ok := p.libraryFoldersMap[libraryName]
	if !ok || len(folders) == 0 {
		return 0, fmt.Errorf("no library folders found for library: %s", libraryName)
	}

	var currentDiskUsage float64
	var diskUsageError error

	for _, path := range folders {
		usage, err := disk.UsageWithContext(ctx, path)
		if err != nil {
			log.Error("failed to get disk usage", "path", path, "error", err)
			diskUsageError = err
			continue
		}
		if usage.UsedPercent > currentDiskUsage {
			currentDiskUsage = usage.UsedPercent
		}
	}

	if diskUsageError != nil && currentDiskUsage == 0 {
		log.Warnf("Could not determine disk usage for library %s", libraryName)
		return 0, nil
	}

	return currentDiskUsage, nil
}
