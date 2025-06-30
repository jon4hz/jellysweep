package engine

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	radarr "github.com/devopsarr/radarr-go/radarr"
	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/jellyseerr"
	"github.com/jon4hz/jellysweep/jellystat"
	"github.com/jon4hz/jellysweep/notify/email"
	"github.com/jon4hz/jellysweep/notify/ntfy"
	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"
)

const (
	jellysweepTagPrefix         = "jellysweep-delete-"
	jellysweepKeepRequestPrefix = "jellysweep-keep-request-"
	jellysweepKeepPrefix        = "jellysweep-must-keep-"
	jellysweepDeleteForSureTag  = "jellysweep-must-delete-for-sure"
)

// Engine is the main engine for JellySweep, managing interactions with sonarr, radarr, and other services.
// It runs a cleanup job periodically to remove unwanted media.
type Engine struct {
	cfg        *config.Config
	jellystat  *jellystat.Client
	jellyseerr *jellyseerr.Client
	sonarr     *sonarr.APIClient
	radarr     *radarr.APIClient
	emailSvc   *email.NotificationService
	ntfySvc    *ntfy.Client

	data *data
}

// data contains any data collected during the cleanup process.
type data struct {
	jellystatItems []jellystat.LibraryItem

	sonarrItems []sonarr.SeriesResource
	sonarrTags  map[int32]string

	radarrItems []radarr.MovieResource
	radarrTags  map[int32]string

	libraryIDMap map[string]string
	mediaItems   map[string][]MediaItem

	// userNotifications tracks which users should be notified about which media items
	userNotifications map[string][]MediaItem // key: user email, value: media items
}

// New creates a new Engine instance.
func New(cfg *config.Config) (*Engine, error) {
	jellystatClient := jellystat.New(cfg.Jellystat)
	var err error
	var sonarrClient *sonarr.APIClient
	if cfg.Sonarr != nil {
		sonarrClient, err = newSonarrClient(cfg.Sonarr)
		if err != nil {
			return nil, err
		}
	} else {
		log.Warn("Sonarr configuration is missing, some features will be disabled")
	}

	var radarrClient *radarr.APIClient
	if cfg.Radarr != nil {
		radarrClient, err = newRadarrClient(cfg.Radarr)
		if err != nil {
			return nil, err
		}
	} else {
		log.Warn("Radarr configuration is missing, some features will be disabled")
	}

	var jellyseerrClient *jellyseerr.Client
	if cfg.Jellyseerr != nil {
		jellyseerrClient = jellyseerr.New(cfg.Jellyseerr)
	}

	// Initialize email notification service
	var emailService *email.NotificationService
	if cfg.JellySweep.Email != nil {
		emailService = email.New(cfg.JellySweep.Email)
	}

	// Initialize ntfy client
	var ntfyClient *ntfy.Client
	if cfg.JellySweep.Ntfy != nil && cfg.JellySweep.Ntfy.Enabled {
		ntfyConfig := &ntfy.Config{
			Enabled:   cfg.JellySweep.Ntfy.Enabled,
			ServerURL: cfg.JellySweep.Ntfy.ServerURL,
			Topic:     cfg.JellySweep.Ntfy.Topic,
			Username:  cfg.JellySweep.Ntfy.Username,
			Password:  cfg.JellySweep.Ntfy.Password,
			Token:     cfg.JellySweep.Ntfy.Token,
		}
		ntfyClient = ntfy.NewClient(ntfyConfig)
	}

	return &Engine{
		cfg:        cfg,
		jellystat:  jellystatClient,
		jellyseerr: jellyseerrClient,
		sonarr:     sonarrClient,
		radarr:     radarrClient,
		emailSvc:   emailService,
		ntfySvc:    ntfyClient,
		data: &data{
			userNotifications: make(map[string][]MediaItem),
		},
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
	e.removeExpiredKeepTags(ctx)
	e.cleanupOldTags(ctx)
	e.markForDeletion(ctx)
	e.removeRecentlyPlayedDeleteTags(ctx)
	e.cleanupMedia(ctx)

	// Set up a ticker to perform cleanup at the specified interval
	ticker := time.NewTicker(time.Duration(e.cfg.JellySweep.CleanupInterval) * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.removeExpiredKeepTags(ctx)
			e.cleanupOldTags(ctx)
			e.markForDeletion(ctx)
			e.removeRecentlyPlayedDeleteTags(ctx)
			e.cleanupMedia(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// removeRecentlyPlayedDeleteTags removes jellysweep-delete tags from media that has been played recently
func (e *Engine) removeRecentlyPlayedDeleteTags(ctx context.Context) {
	log.Info("Checking for recently played media with pending delete tags")

	if e.sonarr != nil {
		if err := e.removeRecentlyPlayedSonarrDeleteTags(ctx); err != nil {
			log.Errorf("failed to remove recently played Sonarr delete tags: %v", err)
		}
	}

	if e.radarr != nil {
		if err := e.removeRecentlyPlayedRadarrDeleteTags(ctx); err != nil {
			log.Errorf("failed to remove recently played Radarr delete tags: %v", err)
		}
	}
}

// removeExpiredKeepTags removes keep request tags that have expired
func (e *Engine) removeExpiredKeepTags(ctx context.Context) {
	log.Info("Removing expired keep request tags")
	if e.sonarr != nil {
		if err := e.removeExpiredSonarrKeepTags(ctx); err != nil {
			log.Errorf("failed to remove expired Sonarr keep tags: %v", err)

		}
	}

	if e.radarr != nil {
		if err := e.removeExpiredRadarrKeepTags(ctx); err != nil {
			log.Errorf("failed to remove expired Radarr keep tags: %v", err)
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
	if e.radarr != nil {
		radarrItems, err := e.getRadarrItems(ctx)
		if err != nil {
			log.Errorf("failed to get radarr delete candidates: %v", err)
			return
		}
		e.data.radarrItems = radarrItems

		radarrTags, err := e.getRadarrTags(ctx)
		if err != nil {
			log.Errorf("failed to get radarr tags: %v", err)
			return
		}
		e.data.radarrTags = radarrTags
	}

	e.mergeMediaItems()
	log.Info("Media items merged successfully")

	e.filterMediaTags()

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
			log.Info("Marking media item for deletion", "name", item.Title, "library", lib)
		}
	}
	log.Info("Media items filtered successfully")

	if err := e.markSonarrMediaItemsForDeletion(ctx, e.cfg.JellySweep.DryRun); err != nil {
		log.Errorf("failed to mark sonarr media items for deletion: %v", err)
		return
	}
	if err := e.markRadarrMediaItemsForDeletion(ctx, e.cfg.JellySweep.DryRun); err != nil {
		log.Errorf("failed to mark radarr media items for deletion: %v", err)
		return
	}

	// Send email notifications before marking for deletion
	if err := e.sendEmailNotifications(); err != nil {
		log.Errorf("failed to send email notifications: %v", err)
		// Don't return here, continue with the cleanup process
	}

	// Send ntfy deletion summary notification
	if err := e.sendNtfyDeletionSummary(); err != nil {
		log.Errorf("failed to send ntfy deletion summary: %v", err)
		// Don't return here, continue with the cleanup process
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
	MovieResource  radarr.MovieResource
	Title          string
	TmdbId         int32
	Tags           []string
	MediaType      MediaType
	// User information for the person who requested this media
	RequestedBy string    // User email or username
	RequestDate time.Time // When the media was requested
}

// mergeMediaItems merges jellystat items and sonarr/radarr items and groups them by their library.
func (e *Engine) mergeMediaItems() {
	mediaItems := make(map[string][]MediaItem, 0)
	for _, item := range e.data.jellystatItems {
		libraryName := e.data.libraryIDMap[item.ParentID]

		// Handle TV Series (Sonarr)
		if item.Type == jellystat.ItemTypeSeries {
			lo.ForEach(e.data.sonarrItems, func(s sonarr.SeriesResource, _ int) {
				if s.GetTitle() == item.Name && s.GetYear() == item.ProductionYear {
					if s.GetTmdbId() == 0 {
						log.Warnf("Sonarr series %s has no TMDB ID, skipping", s.GetTitle())
						return
					}

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

		// Handle Movies (Radarr)
		if item.Type == jellystat.ItemTypeMovie {
			lo.ForEach(e.data.radarrItems, func(m radarr.MovieResource, _ int) {
				if m.GetTitle() == item.Name {
					if m.GetTmdbId() == 0 {
						log.Warnf("Radarr movie %s has no TMDB ID, skipping", m.GetTitle())
						return
					}

					mediaItems[libraryName] = append(mediaItems[libraryName], MediaItem{
						JellystatID:   item.ID,
						MovieResource: m,
						TmdbId:        m.GetTmdbId(),
						Title:         item.Name,
						Tags:          lo.Map(m.GetTags(), func(tag int32, _ int) string { return e.data.radarrTags[tag] }),
						MediaType:     MediaTypeMovie,
					})
				}
			})
		}
	}
	e.data.mediaItems = mediaItems
	log.Infof("Merged %d media items across %d libraries", len(e.data.jellystatItems), len(e.data.mediaItems))
}

func (e *Engine) cleanupOldTags(ctx context.Context) {
	if e.sonarr != nil {
		if err := e.cleanupSonarrTags(ctx); err != nil {
			log.Errorf("failed to clean up Sonarr tags: %v", err)
		}
	}
	if e.radarr != nil {
		if err := e.cleanupRadarrTags(ctx); err != nil {
			log.Errorf("failed to clean up Radarr tags: %v", err)
		}
	}
}

func (e *Engine) cleanupMedia(ctx context.Context) {
	if e.sonarr != nil {
		if err := e.deleteSonarrMedia(ctx); err != nil {
			log.Errorf("failed to delete Sonarr media: %v", err)
		}
	}
	if e.radarr != nil {
		if err := e.deleteRadarrMedia(ctx); err != nil {
			log.Errorf("failed to delete Radarr media: %v", err)
		}
	}
}

// GetMediaItemsMarkedForDeletion returns all media items that are marked for deletion
func (e *Engine) GetMediaItemsMarkedForDeletion(ctx context.Context) (map[string][]models.MediaItem, error) {
	result := make(map[string][]models.MediaItem)

	// Get Sonarr items marked for deletion
	sonarrItems, err := e.getSonarrMediaItemsMarkedForDeletion(ctx)
	if err != nil {
		log.Errorf("failed to get sonarr media items marked for deletion: %v", err)
	} else {
		if len(sonarrItems) > 0 {
			result["TV Shows"] = sonarrItems
		}
	}

	// Get Radarr items marked for deletion
	radarrItems, err := e.getRadarrMediaItemsMarkedForDeletion(ctx)
	if err != nil {
		log.Errorf("failed to get radarr media items marked for deletion: %v", err)
	} else {
		if len(radarrItems) > 0 {
			result["Movies"] = radarrItems
		}
	}

	return result, nil
}

// RequestKeepMedia adds a keep request tag to the specified media item
func (e *Engine) RequestKeepMedia(ctx context.Context, mediaID, userID string) error {
	// Parse media ID to determine if it's a Sonarr or Radarr item
	log.Debug("Requesting keep media", "mediaID", mediaID, "userID", userID)

	var mediaTitle string
	var mediaType string
	var err error

	if strings.HasPrefix(mediaID, "sonarr-") {
		seriesIDStr := strings.TrimPrefix(mediaID, "sonarr-")
		seriesID, parseErr := strconv.ParseInt(seriesIDStr, 10, 32)
		if parseErr != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", parseErr)
		}

		// Get series title before adding tag
		if e.sonarr != nil {
			series, _, getErr := e.sonarr.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, e.cfg.Sonarr), int32(seriesID)).Execute()
			if getErr == nil {
				mediaTitle = series.GetTitle()
			}
		}
		mediaType = "TV Show"
		err = e.addSonarrKeepRequestTag(ctx, int32(seriesID))
	} else if strings.HasPrefix(mediaID, "radarr-") {
		movieIDStr := strings.TrimPrefix(mediaID, "radarr-")
		movieID, parseErr := strconv.ParseInt(movieIDStr, 10, 32)
		if parseErr != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", parseErr)
		}

		// Get movie title before adding tag
		if e.radarr != nil {
			movie, _, getErr := e.radarr.MovieAPI.GetMovieById(radarrAuthCtx(ctx, e.cfg.Radarr), int32(movieID)).Execute()
			if getErr == nil {
				mediaTitle = movie.GetTitle()
			}
		}
		mediaType = "Movie"
		err = e.addRadarrKeepRequestTag(ctx, int32(movieID))
	} else {
		return fmt.Errorf("unsupported media ID format: %s", mediaID)
	}

	// Send ntfy notification if the tag was added successfully
	if err == nil && e.ntfySvc != nil {
		if ntfyErr := e.ntfySvc.SendKeepRequest(mediaTitle, mediaType, userID); ntfyErr != nil {
			log.Errorf("Failed to send ntfy keep request notification: %v", ntfyErr)
			// Don't return error for notification failure, just log it
		}
	}

	return err
}

// GetKeepRequests returns all media items that have keep request tags
func (e *Engine) GetKeepRequests(ctx context.Context) ([]models.KeepRequest, error) {
	var result []models.KeepRequest

	// Get Sonarr keep requests
	sonarrKeepRequests, err := e.getSonarrKeepRequests(ctx)
	if err != nil {
		log.Errorf("failed to get sonarr keep requests: %v", err)
	} else {
		result = append(result, sonarrKeepRequests...)
	}

	// Get Radarr keep requests
	radarrKeepRequests, err := e.getRadarrKeepRequests(ctx)
	if err != nil {
		log.Errorf("failed to get radarr keep requests: %v", err)
	} else {
		result = append(result, radarrKeepRequests...)
	}

	return result, nil
}

// AcceptKeepRequest removes the keep request tag and delete tag from the media item
func (e *Engine) AcceptKeepRequest(ctx context.Context, mediaID string) error {
	// Parse media ID to determine if it's a Sonarr or Radarr item
	if strings.HasPrefix(mediaID, "sonarr-") {
		seriesIDStr := strings.TrimPrefix(mediaID, "sonarr-")
		seriesID, err := strconv.ParseInt(seriesIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", err)
		}
		return e.acceptSonarrKeepRequest(ctx, int32(seriesID))
	} else if strings.HasPrefix(mediaID, "radarr-") {
		movieIDStr := strings.TrimPrefix(mediaID, "radarr-")
		movieID, err := strconv.ParseInt(movieIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", err)
		}
		return e.acceptRadarrKeepRequest(ctx, int32(movieID))
	}

	return fmt.Errorf("unsupported media ID format: %s", mediaID)
}

// DeclineKeepRequest removes the keep request tag and adds a delete-for-sure tag
func (e *Engine) DeclineKeepRequest(ctx context.Context, mediaID string) error {
	// Parse media ID to determine if it's a Sonarr or Radarr item
	if strings.HasPrefix(mediaID, "sonarr-") {
		seriesIDStr := strings.TrimPrefix(mediaID, "sonarr-")
		seriesID, err := strconv.ParseInt(seriesIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", err)
		}
		return e.declineSonarrKeepRequest(ctx, int32(seriesID))
	} else if strings.HasPrefix(mediaID, "radarr-") {
		movieIDStr := strings.TrimPrefix(mediaID, "radarr-")
		movieID, err := strconv.ParseInt(movieIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", err)
		}
		return e.declineRadarrKeepRequest(ctx, int32(movieID))
	}

	return fmt.Errorf("unsupported media ID format: %s", mediaID)
}

// ResetAllTags removes all jellysweep tags from all media in Sonarr and Radarr
func (e *Engine) ResetAllTags(ctx context.Context) error {
	log.Info("Resetting all jellysweep tags...")

	g, ctx := errgroup.WithContext(ctx)
	// Reset Sonarr tags
	if e.sonarr != nil {
		g.Go(func() error {
			log.Info("Removing jellysweep tags from Sonarr series...")
			if err := e.resetSonarrTags(ctx); err != nil {
				return fmt.Errorf("failed to reset Sonarr tags: %w", err)
			}
			log.Info("Cleaning up all Sonarr jellysweep tags...")
			if err := e.cleanupAllSonarrTags(ctx); err != nil {
				return fmt.Errorf("failed to cleanup Sonarr tags: %w", err)
			}
			return nil
		})

	}

	// Reset Radarr tags
	if e.radarr != nil {
		g.Go(func() error {
			log.Info("Removing jellysweep tags from Radarr movies...")
			if err := e.resetRadarrTags(ctx); err != nil {
				return fmt.Errorf("failed to reset Radarr tags: %w", err)
			}
			log.Info("Cleaning up all Radarr jellysweep tags...")
			if err := e.cleanupAllRadarrTags(ctx); err != nil {
				return fmt.Errorf("failed to cleanup Radarr tags: %w", err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		log.Error(err)
		return fmt.Errorf("error while resetting tags")
	}

	log.Info("All jellysweep tags have been successfully reset!")
	return nil
}

// getCachedImageURL converts a direct image URL to a cached URL
func getCachedImageURL(imageURL string) string {
	if imageURL == "" {
		return ""
	}
	// Encode the original URL and return a cache endpoint URL
	encoded := url.QueryEscape(imageURL)
	return fmt.Sprintf("/api/images/cache?url=%s", encoded)
}
