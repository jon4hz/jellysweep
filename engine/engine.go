package engine

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/jellyseerr"
	"github.com/jon4hz/jellysweep/jellystat"
	"github.com/samber/lo"
)

// Engine is the main engine for Jellysweep, managing interactions with sonarr, radarr, and other services.
// It runs a cleanup job periodically to remove unwanted media.
type Engine struct {
	cfg        *config.Config
	jellystat  *jellystat.Client
	jellyseerr *jellyseerr.Client
	sonarr     *sonarr.APIClient

	data *data
}

// data contains any data collected during the cleanup process.
type data struct {
	jellystatItems []jellystat.LibraryItem

	sonarrItems []sonarr.SeriesResource
	sonarrTags  map[int32]string

	libraryIDMap map[string]string
	mediaItems   map[string][]MediaItem
}

// New creates a new Engine instance.
func New(cfg *config.Config) (*Engine, error) {
	jellystatClient := jellystat.New(cfg.Jellystat)

	sonarrClient, err := newSonarrClient(cfg.Sonarr)
	if err != nil {
		return nil, err
	}

	var jellyseerrClient *jellyseerr.Client
	if cfg.Jellyseerr != nil {
		jellyseerrClient = jellyseerr.New(cfg.Jellyseerr)
	}

	return &Engine{
		cfg:        cfg,
		jellystat:  jellystatClient,
		jellyseerr: jellyseerrClient,
		sonarr:     sonarrClient,
		data:       new(data),
	}, nil
}

// Run starts the engine and all its background jobs.
func (e *Engine) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	go e.cleanupLoop(ctx)
	<-ctx.Done()
	return nil
}

// Close stops the engine and cleans up resources.
func (e *Engine) Close() error {
	return nil
}

func (e *Engine) cleanupLoop(ctx context.Context) {
	// Perform an initial cleanup immediately
	e.cleanupOldTags(ctx)
	e.markForDeletion(ctx)
	e.cleanupMedia(ctx)

	// Set up a ticker to perform cleanup at the specified interval
	ticker := time.NewTicker(time.Duration(e.cfg.Jellysweep.CleanupInterval) * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.cleanupOldTags(ctx)
			e.markForDeletion(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (e *Engine) markForDeletion(ctx context.Context) {
	if e.jellystat != nil {
		jellystatItems, err := e.getJellystatItems(ctx)
		if err != nil {
			log.Errorf("failed to get jellystat items: %v", err)
			return
		}
		e.data.jellystatItems = jellystatItems
	}
	if e.sonarr != nil {
		sonarrItems, err := e.getSonarrItems(ctx)
		if err != nil {
			log.Errorf("failed to get sonarr delete candidates: %v", err)
			return
		}
		e.data.sonarrItems = sonarrItems

		sonarrTags, err := e.getSonarrTags(ctx)
		if err != nil {
			log.Errorf("failed to get sonarr tags: %v", err)
			return
		}
		e.data.sonarrTags = sonarrTags
	}

	e.mergeMediaItems()
	log.Info("Media items merged successfully")

	if err := e.filterSonarrTags(); err != nil {
		log.Errorf("failed to filter sonarr tags: %v", err)
		return
	}
	if err := e.filterLastStreamThreshold(ctx); err != nil {
		log.Errorf("failed to filter last stream threshold: %v", err)
		return
	}
	if err := e.filterRequestAgeThreshold(ctx); err != nil {
		log.Errorf("failed to filter request age threshold: %v", err)
		return
	}
	for lib, items := range e.data.mediaItems {
		for _, item := range items {
			log.Debugf("Media item for deletion: %s (library: %s)", item.Title, lib)
		}
	}
	log.Info("Media items filtered successfully")

	if err := e.markSonarrMediaItemsForDeletion(ctx, e.cfg.Jellysweep.DryRun); err != nil {
		log.Errorf("failed to mark sonarr media items for deletion: %v", err)
		return
	}
}

type MediaType string

const (
	MediaTypeTV    MediaType = "tv"
	MediaTypeMovie MediaType = "movie"
)

type MediaItem struct {
	JellystatID    string
	SeriesResource sonarr.SeriesResource
	Title          string
	TmdbId         int32
	Tags           []string
	MediaType      MediaType
}

// mergeMediaItems merges jellystat items and sonarr items and groups them by their library.
func (e *Engine) mergeMediaItems() {
	mediaItems := make(map[string][]MediaItem, 0)
	for _, item := range e.data.jellystatItems {
		// map jellystat items to sonarr items
		lo.ForEach(e.data.sonarrItems, func(s sonarr.SeriesResource, _ int) {
			if item.Type != jellystat.ItemTypeSeries {
				return
			}
			if s.GetTitle() == item.Name {
				if s.GetTmdbId() == 0 {
					log.Warnf("Sonarr series %s has no TMDB ID, skipping", s.GetTitle())
					return
				}

				libraryName := e.data.libraryIDMap[item.ParentID]
				mediaItems[libraryName] = append(mediaItems[libraryName], MediaItem{
					JellystatID:    item.ID,
					SeriesResource: s,
					TmdbId:         s.GetTmdbId(),
					Title:          item.Name,
					Tags:           lo.Map(s.GetTags(), func(tag int32, _ int) string { return e.data.sonarrTags[tag] }),
					MediaType:      MediaTypeTV,
				})
			}
		})
	}
	e.data.mediaItems = mediaItems
	log.Infof("Merged %d media items across %d libraries", len(e.data.jellystatItems), len(e.data.mediaItems))
}

func (e *Engine) cleanupOldTags(ctx context.Context) error {
	if e.sonarr != nil {
		if err := e.cleanupSonarrTags(ctx); err != nil {
			log.Errorf("failed to clean up Sonarr tags: %v", err)
		}
	}
	return nil
}

func (e *Engine) cleanupMedia(ctx context.Context) error {
	if e.sonarr != nil {
		if err := e.deleteSonarrMedia(ctx); err != nil {
			log.Errorf("failed to delete Sonarr media: %v", err)
		}
	}
	return nil
}
