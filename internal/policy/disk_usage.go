package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
)

// UsageFunc reports the current disk usage in percent for a media type.
// ok is false when the usage could not be determined; that is not an error.
type UsageFunc func(ctx context.Context, mediaType database.MediaType) (usage float64, ok bool, err error)

// DiskUsageDelete applies when disk usage exceeds a certain threshold.
type DiskUsageDelete struct {
	cfg       *config.Config
	usageFunc UsageFunc
}

var _ Policy = (*DiskUsageDelete)(nil)

// NewDiskUsageDelete creates a new instance of DiskUsageDelete.
// usageFunc provides the current disk usage per media type.
func NewDiskUsageDelete(cfg *config.Config, usageFunc UsageFunc) *DiskUsageDelete {
	return &DiskUsageDelete{
		cfg:       cfg,
		usageFunc: usageFunc,
	}
}

// Apply adds a DiskUsageDeletePolicy if the library has a disk usage threshold set.
func (p *DiskUsageDelete) Apply(media *database.Media) error {
	libraryConfig := p.cfg.GetLibraryConfig(media.LibraryName)
	if libraryConfig == nil {
		return fmt.Errorf("no configuration found for library: %s", media.LibraryName)
	}
	if len(libraryConfig.DiskUsageThresholds) == 0 {
		// Clear any stale disk usage policies from previous configurations.
		media.DiskUsageDeletePolicies = nil
		return nil
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

	// Skip disk usage checks if no thresholds are configured for this library.
	libraryConfig := p.cfg.GetLibraryConfig(media.LibraryName)
	if libraryConfig == nil || len(libraryConfig.DiskUsageThresholds) == 0 {
		return false, nil
	}

	currentDiskUsage, ok, err := p.usageFunc(ctx, media.MediaType)
	if err != nil {
		return false, err
	}
	if !ok {
		// usage could not be determined: abort but dont return an error
		return false, nil
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

// GetEstimatedDeleteAt returns the earliest deletion date among disk usage policies
// whose thresholds are currently exceeded. Returns zero time if no thresholds are exceeded,
// if there are no disk usage policies, or if the disk usage could not be determined.
func (p *DiskUsageDelete) GetEstimatedDeleteAt(ctx context.Context, media database.Media) (time.Time, error) {
	if len(media.DiskUsageDeletePolicies) == 0 {
		return time.Time{}, nil
	}

	// Skip disk usage checks if no thresholds are configured for this library.
	libraryConfig := p.cfg.GetLibraryConfig(media.LibraryName)
	if libraryConfig == nil || len(libraryConfig.DiskUsageThresholds) == 0 {
		return time.Time{}, nil
	}

	currentDiskUsage, ok, err := p.usageFunc(ctx, media.MediaType)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return time.Time{}, nil
	}

	var earliest time.Time
	for _, policy := range media.DiskUsageDeletePolicies {
		if currentDiskUsage < policy.Threshold {
			continue
		}
		if policy.DeleteDate.IsZero() {
			log.Warn("Disk usage threshold exceeded but no delete date set in policy. This should not happen.")
			continue
		}
		if earliest.IsZero() || policy.DeleteDate.Before(earliest) {
			earliest = policy.DeleteDate
		}
	}
	return earliest, nil
}
