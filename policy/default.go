package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/database"
)

// DefaultDelete applies by default to all media items.
type DefaultDelete struct {
	cfg *config.Config
}

var _ Policy = (*DefaultDelete)(nil)

// DefaultDelete creates a new instance of DefaultDelete.
func NewDefaultDelete(cfg *config.Config) *DefaultDelete {
	return &DefaultDelete{
		cfg: cfg,
	}
}

// Apply sets the DefaultDeleteAt field based on the library's cleanup delay.
func (p *DefaultDelete) Apply(media *database.Media) error {
	libraryConfig := p.cfg.GetLibraryConfig(media.LibraryName)
	if libraryConfig == nil {
		return fmt.Errorf("no configuration found for library: %s", media.LibraryName)
	}

	media.DefaultDeleteAt = time.Now().Add(
		time.Duration(libraryConfig.GetCleanupDelay()) * 24 * time.Hour,
	)
	log.Debug("Set default delete policy", "item", media.Title, "library", media.LibraryName, "deleteAt", media.DefaultDeleteAt)

	return nil
}

// ShouldTriggerDeletion returns whether the media should be deleted based on the default delete date.
func (p *DefaultDelete) ShouldTriggerDeletion(_ context.Context, media database.Media) (bool, error) {
	return time.Now().After(media.DefaultDeleteAt) && !media.DefaultDeleteAt.IsZero(), nil
}
