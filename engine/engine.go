package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	radarr "github.com/devopsarr/radarr-go/radarr"
	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/go-co-op/gocron/v2"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/cache"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/jellyseerr"
	"github.com/jon4hz/jellysweep/jellystat"
	"github.com/jon4hz/jellysweep/notify/email"
	"github.com/jon4hz/jellysweep/notify/ntfy"
	"github.com/jon4hz/jellysweep/notify/webpush"
	"github.com/jon4hz/jellysweep/scheduler"
	"github.com/jon4hz/jellysweep/streamystats"
	"github.com/samber/lo"
	jellyfin "github.com/sj14/jellyfin-go/api"
	"golang.org/x/sync/errgroup"
)

// Cleanup mode constants.
const (
	CleanupModeAll          = "all"
	CleanupModeKeepEpisodes = "keep_episodes"
	CleanupModeKeepSeasons  = "keep_seasons"
)

// ErrRequestAlreadyProcessed indicates that a keep request has already been processed.
var ErrRequestAlreadyProcessed = errors.New("request already processed")

// Engine is the main engine for JellySweep, managing interactions with sonarr, radarr, and other services.
// It runs a cleanup job periodically to remove unwanted media.
type Engine struct {
	cfg          *config.Config
	jellyfin     *jellyfin.APIClient
	jellystat    *jellystat.Client
	streamystats *streamystats.Client
	jellyseerr   *jellyseerr.Client
	sonarr       *sonarr.APIClient
	radarr       *radarr.APIClient
	email        *email.NotificationService
	ntfy         *ntfy.Client
	webpush      *webpush.Client
	scheduler    *scheduler.Scheduler

	imageCache *cache.ImageCache
	cache      *cache.EngineCache // Cache for engine-specific data

	data *data
}

// data contains any data collected during the cleanup process.
type data struct {
	jellyfinItems []jellyfinItem

	// library ID to name
	libraryIDMap map[string]string
	// library name to library paths
	libraryFoldersMap map[string][]string

	mediaItems map[string][]MediaItem

	// userNotifications tracks which users should be notified about which media items
	userNotifications map[string][]MediaItem // key: user email, value: media items
}

// New creates a new Engine instance.
func New(cfg *config.Config) (*Engine, error) {
	// Create scheduler first
	sched, err := scheduler.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduler: %w", err)
	}

	var jellystatClient *jellystat.Client
	if cfg.Jellystat != nil {
		jellystatClient = jellystat.New(cfg.Jellystat)
	}

	var streamystatsClient *streamystats.Client
	if cfg.Streamystats != nil {
		streamystatsClient, err = streamystats.New(cfg.Streamystats, cfg.Jellyfin)
		if err != nil {
			return nil, fmt.Errorf("failed to create StreamyStats client: %w", err)
		}
	}

	var sonarrClient *sonarr.APIClient
	if cfg.Sonarr != nil {
		sonarrClient = newSonarrClient(cfg.Sonarr)
	} else {
		log.Warn("Sonarr configuration is missing, some features will be disabled")
	}

	var radarrClient *radarr.APIClient
	if cfg.Radarr != nil {
		radarrClient = newRadarrClient(cfg.Radarr)
	} else {
		log.Warn("Radarr configuration is missing, some features will be disabled")
	}

	var jellyseerrClient *jellyseerr.Client
	if cfg.Jellyseerr != nil {
		jellyseerrClient = jellyseerr.New(cfg.Jellyseerr)
	}

	// Initialize email notification service
	var emailService *email.NotificationService
	if cfg.Email != nil {
		emailService = email.New(cfg.Email)
	}

	// Initialize ntfy client
	var ntfyClient *ntfy.Client
	if cfg.Ntfy != nil && cfg.Ntfy.Enabled {
		ntfyConfig := &ntfy.Config{
			Enabled:   cfg.Ntfy.Enabled,
			ServerURL: cfg.Ntfy.ServerURL,
			Topic:     cfg.Ntfy.Topic,
			Username:  cfg.Ntfy.Username,
			Password:  cfg.Ntfy.Password,
			Token:     cfg.Ntfy.Token,
		}
		ntfyClient = ntfy.NewClient(ntfyConfig)
	}

	// Initialize webpush client
	var webpushClient *webpush.Client
	if cfg.WebPush != nil && cfg.WebPush.Enabled {
		webpushClient = webpush.NewClient(cfg.WebPush)
	}

	engineCache, err := cache.NewEngineCache(cfg.Cache)
	if err != nil {
		return nil, fmt.Errorf("failed to create engine cache: %w", err)
	}

	engine := &Engine{
		cfg:          cfg,
		jellyfin:     newJellyfinClient(cfg.Jellyfin),
		jellystat:    jellystatClient,
		streamystats: streamystatsClient,
		jellyseerr:   jellyseerrClient,
		sonarr:       sonarrClient,
		radarr:       radarrClient,
		email:        emailService,
		ntfy:         ntfyClient,
		webpush:      webpushClient,
		scheduler:    sched,
		data: &data{
			userNotifications: make(map[string][]MediaItem),
			libraryIDMap:      make(map[string]string),
			libraryFoldersMap: make(map[string][]string),
		},
		imageCache: cache.NewImageCache("./data/cache/images"),
		cache:      engineCache,
	}

	// Setup scheduled jobs
	if err := engine.setupJobs(); err != nil {
		return nil, fmt.Errorf("failed to setup jobs: %w", err)
	}

	return engine, nil
}

// Run starts the engine and all its background jobs.
func (e *Engine) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// Start the scheduler
	e.scheduler.Start()

	// Wait for context cancellation
	<-ctx.Done()
	return nil
}

// Close stops the engine and cleans up resources.
func (e *Engine) Close() error {
	return e.scheduler.Stop()
}

// setupJobs configures all scheduled jobs.
func (e *Engine) setupJobs() error {
	// Add cleanup job as singleton (only one instance can run at a time)
	cleanupJobDef := gocron.CronJob(e.cfg.CleanupSchedule, false)
	if err := e.scheduler.AddSingletonJob(
		"cleanup",
		"Media Cleanup",
		"Runs the cleanup loop",
		e.cfg.CleanupSchedule,
		cleanupJobDef,
		e.runCleanupJob,
		true,
	); err != nil {
		return fmt.Errorf("failed to add cleanup job: %w", err)
	}

	// Add job to clear image cache once a week
	clearImageCacheJobDef := gocron.CronJob("0 0 * * 0", false) // Every Sunday at midnight
	if err := e.scheduler.AddSingletonJob(
		"clear_image_cache",
		"Clear Image Cache",
		"Clears the image cache to free up space",
		"0 0 * * 0", // Every Sunday at midnight
		clearImageCacheJobDef,
		func(ctx context.Context) error {
			return e.imageCache.Clear(ctx)
		},
		false, // Not a singleton, can run multiple times
	); err != nil {
		return fmt.Errorf("failed to add clear image cache job: %w", err)
	}

	log.Info("Scheduled jobs configured successfully")
	return nil
}

// runCleanupJob is the main cleanup job function.
func (e *Engine) runCleanupJob(ctx context.Context) (err error) {
	log.Info("Starting scheduled cleanup job")

	// Clear all caches to ensure fresh data
	e.cache.ClearAll(ctx)

	e.removeExpiredKeepTags(ctx)
	e.cleanupOldTags(ctx)
	if err = e.markForDeletion(ctx); err != nil {
		log.Error("An error occurred while marking media for deletion")
	}
	e.removeRecentlyPlayedDeleteTags(ctx)

	// only delete media if there was no previous error
	if err == nil {
		e.cleanupMedia(ctx)
	}

	log.Info("Scheduled cleanup job completed")
	return
}

// GetScheduler returns the scheduler instance for API access.
func (e *Engine) GetScheduler() *scheduler.Scheduler {
	return e.scheduler
}

// GetImageCache returns the image cache instance for API access.
func (e *Engine) GetImageCache() *cache.ImageCache {
	return e.imageCache
}

// GetEngineCache returns the engine cache instance.
func (e *Engine) GetEngineCache() *cache.EngineCache {
	return e.cache
}

// removeRecentlyPlayedDeleteTags removes jellysweep-delete tags from media that has been played recently.
func (e *Engine) removeRecentlyPlayedDeleteTags(ctx context.Context) {
	log.Info("Checking for recently played media with pending delete tags")

	if e.sonarr != nil {
		if err := e.removeRecentlyPlayedSonarrDeleteTags(ctx); err != nil {
			log.Error("Failed to remove recently played Sonarr delete tags", "error", err)
		}
	}

	if e.radarr != nil {
		e.removeRecentlyPlayedRadarrDeleteTags(ctx)
	}
}

// removeExpiredKeepTags removes keep request tags that have expired.
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

func (e *Engine) markForDeletion(ctx context.Context) error {
	if err := e.gatherMediaItems(ctx); err != nil {
		log.Errorf("failed to gather media items: %v", err)
		return err
	}
	log.Info("Media items gathered successfully")

	// Filter out series that already meet the keep criteria (if cleanup mode is set to keep episodes or seasons)
	log.Info("Filtering series that already meet keep criteria")
	e.filterSeriesAlreadyMeetingKeepCriteria()

	// Filter media items based on tags
	log.Info("Filtering media items based on tags")
	e.filterMediaTags()

	log.Info("Checking content age")
	if err := e.filterContentAgeThreshold(ctx); err != nil {
		log.Errorf("failed to filter content age threshold: %v", err)
		return err
	}

	log.Info("Checking content size")
	if err := e.filterContentSizeThreshold(ctx); err != nil {
		log.Errorf("failed to filter content size threshold: %v", err)
		return err
	}

	log.Info("Checking for streaming history")
	if err := e.filterLastStreamThreshold(ctx); err != nil {
		log.Errorf("failed to filter last stream threshold: %v", err)
		return err
	}

	// Populate requester information from Jellyseerr
	log.Info("Populating requester information")
	e.populateRequesterInfo(ctx)

	// Populate user notifications for email sending
	if e.data.userNotifications == nil {
		e.data.userNotifications = make(map[string][]MediaItem)
	}

	for lib, items := range e.data.mediaItems {
		for _, item := range items {
			e.data.userNotifications[item.RequestedBy] = append(e.data.userNotifications[item.RequestedBy], item)
			log.Info("Marking media item for deletion", "name", item.Title, "library", lib)
		}
	}
	log.Info("Media items filtered successfully")

	if err := e.markSonarrMediaItemsForDeletion(ctx, e.cfg.DryRun); err != nil {
		log.Errorf("failed to mark sonarr media items for deletion: %v", err)
		return err
	}
	if err := e.markRadarrMediaItemsForDeletion(ctx, e.cfg.DryRun); err != nil {
		log.Errorf("failed to mark radarr media items for deletion: %v", err)
		return err
	}

	// Send email notifications before marking for deletion
	e.sendEmailNotifications()

	// Send ntfy deletion summary notification
	if err := e.sendNtfyDeletionSummary(ctx); err != nil {
		log.Errorf("failed to send ntfy deletion summary: %v", err)
		// Don't return here, continue with the cleanup process
	}
	return nil
}

type MediaItem struct {
	JellyfinID     string
	SeriesResource sonarr.SeriesResource
	MovieResource  radarr.MovieResource
	Title          string
	TmdbId         int32
	Year           int32
	Tags           []string
	MediaType      models.MediaType
	// User information for the person who requested this media
	RequestedBy string    // User email or username
	RequestDate time.Time // When the media was requested
}

// gatherMediaItems gathers all media items from Jellyfin, Sonarr, and Radarr.
// It merges them into a single collection grouped by library.
func (e *Engine) gatherMediaItems(ctx context.Context) error {
	jellyfinItems, err := e.getJellyfinItems(ctx)
	if err != nil {
		return fmt.Errorf("failed to get jellyfin items: %w", err)
	}
	e.data.jellyfinItems = jellyfinItems

	sonarrItems, err := e.getSonarrItems(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to get sonarr items: %w", err)
	}

	sonarrTags, err := e.getSonarrTags(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	radarrItems, err := e.getRadarrItems(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to get radarr items: %w", err)
	}

	radarrTags, err := e.getRadarrTags(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to get radarr tags: %w", err)
	}

	mediaItems := make(map[string][]MediaItem, 0)
	for _, item := range e.data.jellyfinItems {
		libraryName := strings.ToLower(e.getLibraryNameByID(item.ParentLibraryID))
		if libraryName == "" {
			log.Error("Library name is empty for Jellyfin item, skipping", "item_id", item.GetId(), "item_name", item.GetName())
			continue
		}

		// Handle TV Series (Sonarr)
		if item.GetType() == jellyfin.BASEITEMKIND_SERIES {
			lo.ForEach(sonarrItems, func(s sonarr.SeriesResource, _ int) {
				if s.GetTitle() == item.GetName() && s.GetYear() == item.GetProductionYear() {
					if s.GetTmdbId() == 0 {
						log.Warnf("Sonarr series %s has no TMDB ID, skipping", s.GetTitle())
						return
					}
					log.Debug("Merging Jellyfin item with Sonarr series", "jellyfin_id", item.GetId(), "sonarr_id", s.GetId(), "title", item.GetName(), "year", item.GetProductionYear())

					mediaItems[libraryName] = append(mediaItems[libraryName], MediaItem{
						JellyfinID:     item.GetId(),
						SeriesResource: s,
						TmdbId:         s.GetTmdbId(),
						Year:           s.GetYear(),
						Title:          item.GetName(),
						Tags:           lo.Map(s.GetTags(), func(tag int32, _ int) string { return sonarrTags[tag] }),
						MediaType:      models.MediaTypeTV,
					})
				}
			})
		}

		// Handle Movies (Radarr)
		if item.GetType() == jellyfin.BASEITEMKIND_MOVIE {
			lo.ForEach(radarrItems, func(m radarr.MovieResource, _ int) {
				if m.GetTitle() == item.GetName() && m.GetYear() == item.GetProductionYear() {
					if m.GetTmdbId() == 0 {
						log.Warnf("Radarr movie %s has no TMDB ID, skipping", m.GetTitle())
						return
					}

					log.Debug("Merging Jellyfin item with Radarr movie", "jellyfin_id", item.GetId(), "radarr_id", m.GetId(), "title", item.GetName(), "year", item.GetProductionYear())

					mediaItems[libraryName] = append(mediaItems[libraryName], MediaItem{
						JellyfinID:    item.GetId(),
						MovieResource: m,
						TmdbId:        m.GetTmdbId(),
						Title:         item.GetName(),
						Year:          m.GetYear(),
						Tags:          lo.Map(m.GetTags(), func(tag int32, _ int) string { return radarrTags[tag] }),
						MediaType:     models.MediaTypeMovie,
					})
				}
			})
		}
	}
	e.data.mediaItems = mediaItems
	log.Infof("Merged %d media items across %d libraries", len(e.data.jellyfinItems), len(e.data.mediaItems))

	return nil
}

func (e *Engine) filterLastStreamThreshold(ctx context.Context) error {
	if e.jellystat != nil {
		return e.filterJellystatLastStreamThreshold(ctx)
	}
	if e.streamystats != nil {
		return e.filterStreamystatsLastStreamThreshold(ctx)
	}
	return nil
}

func (e *Engine) getItemLastPlayed(ctx context.Context, itemID string) (time.Time, error) {
	if e.jellystat != nil {
		return e.getJellystatMediaItemLastStreamed(ctx, itemID)
	}
	if e.streamystats != nil {
		return e.getStreamystatsMediaItemLastStreamed(ctx, itemID) // Reuse the same function for StreamyStats
	}
	return time.Time{}, fmt.Errorf("no stats provider available")
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
	deletedItems := make(map[string][]MediaItem)

	if e.sonarr != nil {
		if sonarrDeleted, err := e.deleteSonarrMedia(ctx); err != nil {
			log.Errorf("failed to delete Sonarr media: %v", err)
		} else if len(sonarrDeleted) > 0 {
			deletedItems["TV Shows"] = sonarrDeleted
		}
	}
	if e.radarr != nil {
		if radarrDeleted, err := e.deleteRadarrMedia(ctx); err != nil {
			log.Errorf("failed to delete Radarr media: %v", err)
		} else if len(radarrDeleted) > 0 {
			deletedItems["Movies"] = radarrDeleted
		}
	}

	// Send completion notification if any items were deleted
	if len(deletedItems) > 0 {
		if err := e.sendNtfyDeletionCompletedNotification(ctx, deletedItems); err != nil {
			log.Errorf("failed to send deletion completed notification: %v", err)
		}
	}
}

// GetMediaItemsMarkedForDeletion returns all media items that are marked for deletion.
func (e *Engine) GetMediaItemsMarkedForDeletion(ctx context.Context, forceRefresh bool) (map[string][]models.MediaItem, error) {
	result := make(map[string][]models.MediaItem)

	// Get Sonarr items marked for deletion
	sonarrItems, err := e.getSonarrMediaItemsMarkedForDeletion(ctx, forceRefresh)
	if err != nil {
		log.Errorf("failed to get sonarr media items marked for deletion: %v", err)
	} else {
		if len(sonarrItems) > 0 {
			result["TV Shows"] = sonarrItems
		}
	}

	// Get Radarr items marked for deletion
	radarrItems, err := e.getRadarrMediaItemsMarkedForDeletion(ctx, forceRefresh)
	if err != nil {
		log.Errorf("failed to get radarr media items marked for deletion: %v", err)
	} else {
		if len(radarrItems) > 0 {
			result["Movies"] = radarrItems
		}
	}

	return result, nil
}

// GetMediaItemsMarkedForDeletionByType returns media items marked for deletion by type.
func (e *Engine) GetMediaItemsMarkedForDeletionByType(ctx context.Context, mediaType models.MediaType, forceRefresh bool) ([]models.MediaItem, error) {
	var result []models.MediaItem

	switch mediaType {
	case models.MediaTypeTV:
		// Get Sonarr items marked for deletion
		sonarrItems, err := e.getSonarrMediaItemsMarkedForDeletion(ctx, forceRefresh)
		if err != nil {
			log.Errorf("failed to get sonarr media items marked for deletion: %v", err)
		} else {
			result = append(result, sonarrItems...)
		}
	case models.MediaTypeMovie:
		// Get Radarr items marked for deletion
		radarrItems, err := e.getRadarrMediaItemsMarkedForDeletion(ctx, forceRefresh)
		if err != nil {
			log.Errorf("failed to get radarr media items marked for deletion: %v", err)
		} else {
			result = append(result, radarrItems...)
		}
	default:
		return nil, fmt.Errorf("unsupported media type: %s", mediaType)
	}

	return result, nil
}

// RequestKeepMedia adds a keep request tag to the specified media item.
func (e *Engine) RequestKeepMedia(ctx context.Context, mediaID, username string) error {
	// Parse media ID to determine if it's a Sonarr or Radarr item
	log.Debug("Requesting keep media", "mediaID", mediaID, "username", username)

	var mediaTitle string
	var mediaType string
	var err error

	if seriesIDStr, ok := strings.CutPrefix(mediaID, "sonarr-"); ok {
		if e.sonarr == nil {
			return fmt.Errorf("sonarr client not available")
		}

		seriesID, parseErr := strconv.ParseInt(seriesIDStr, 10, 32)
		if parseErr != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", parseErr)
		}

		var wantedSeries sonarr.SeriesResource
		series, err := e.getSonarrItems(ctx, true)
		if err != nil {
			return fmt.Errorf("failed to get Sonarr series from cache: %w", err)
		}
		if len(series) == 0 {
			return fmt.Errorf("no Sonarr series found in cache")
		}
		for _, s := range series {
			if s.GetId() == int32(seriesID) {
				wantedSeries = s
				mediaTitle = s.GetTitle()
				break
			}
		}
		if mediaTitle == "" {
			return fmt.Errorf("sonarr series with ID %d not found", seriesID)
		}
		mediaType = "TV Show"
		if err := e.addSonarrKeepRequestTag(ctx, wantedSeries, username); err != nil {
			return fmt.Errorf("failed to add Sonarr keep request tag: %w", err)
		}
	} else if movieIDStr, ok := strings.CutPrefix(mediaID, "radarr-"); ok {
		movieID, parseErr := strconv.ParseInt(movieIDStr, 10, 32)
		if parseErr != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", parseErr)
		}

		// Get movie title before adding tag
		if e.radarr != nil {
			movie, resp, getErr := e.radarr.MovieAPI.GetMovieById(radarrAuthCtx(ctx, e.cfg.Radarr), int32(movieID)).Execute()
			if getErr == nil {
				mediaTitle = movie.GetTitle()
			}
			defer resp.Body.Close() //nolint: errcheck
		}
		mediaType = "Movie"
		err = e.addRadarrKeepRequestTag(ctx, int32(movieID), username)
	} else {
		return fmt.Errorf("unsupported media ID format: %s", mediaID)
	}

	// Send ntfy notification if the tag was added successfully
	if err == nil && e.ntfy != nil {
		if ntfyErr := e.ntfy.SendKeepRequest(ctx, mediaTitle, mediaType, username); ntfyErr != nil {
			log.Errorf("Failed to send ntfy keep request notification: %v", ntfyErr)
			// Don't return error for notification failure, just log it
		}
	}

	return err
}

// GetKeepRequests returns all media items that have keep request tags.
func (e *Engine) GetKeepRequests(ctx context.Context, forceRefresh bool) ([]models.KeepRequest, error) {
	var result []models.KeepRequest

	// Get Sonarr keep requests
	sonarrKeepRequests, err := e.getSonarrKeepRequests(ctx, forceRefresh)
	if err != nil {
		log.Errorf("failed to get sonarr keep requests: %v", err)
	} else {
		result = append(result, sonarrKeepRequests...)
	}

	// Get Radarr keep requests
	radarrKeepRequests, err := e.getRadarrKeepRequests(ctx, forceRefresh)
	if err != nil {
		log.Errorf("failed to get radarr keep requests: %v", err)
	} else {
		result = append(result, radarrKeepRequests...)
	}

	return result, nil
}

// AcceptKeepRequest removes the keep request tag and delete tag from the media item.
func (e *Engine) AcceptKeepRequest(ctx context.Context, mediaID string) error {
	// Parse media ID to determine if it's a Sonarr or Radarr item
	if seriesIDStr, ok := strings.CutPrefix(mediaID, "sonarr-"); ok {
		seriesID, err := strconv.ParseInt(seriesIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", err)
		}
		return e.acceptSonarrKeepRequest(ctx, int32(seriesID))
	} else if movieIDStr, ok := strings.CutPrefix(mediaID, "radarr-"); ok {
		movieID, err := strconv.ParseInt(movieIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", err)
		}
		return e.acceptRadarrKeepRequest(ctx, int32(movieID))
	}

	return fmt.Errorf("unsupported media ID format: %s", mediaID)
}

// DeclineKeepRequest removes the keep request tag and adds a delete-for-sure tag.
func (e *Engine) DeclineKeepRequest(ctx context.Context, mediaID string) error {
	// Parse media ID to determine if it's a Sonarr or Radarr item
	if seriesIDStr, ok := strings.CutPrefix(mediaID, "sonarr-"); ok {
		seriesID, err := strconv.ParseInt(seriesIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", err)
		}
		return e.declineSonarrKeepRequest(ctx, int32(seriesID))
	} else if movieIDStr, ok := strings.CutPrefix(mediaID, "radarr-"); ok {
		movieID, err := strconv.ParseInt(movieIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", err)
		}
		return e.declineRadarrKeepRequest(ctx, int32(movieID))
	}

	return fmt.Errorf("unsupported media ID format: %s", mediaID)
}

// ResetAllTags removes all jellysweep tags from all media in Sonarr and Radarr.
func (e *Engine) ResetAllTags(ctx context.Context, additionalTags []string) error {
	log.Info("Resetting all jellysweep tags...")

	if e.sonarr == nil && e.radarr == nil {
		return fmt.Errorf("no Sonarr or Radarr client configured, cannot reset tags")
	}

	g, ctx := errgroup.WithContext(ctx)
	// Reset Sonarr tags
	if e.sonarr != nil {
		g.Go(func() error {
			log.Info("Removing jellysweep tags from Sonarr series...")
			if err := e.resetSonarrTags(ctx, additionalTags); err != nil {
				return fmt.Errorf("failed to reset Sonarr tags: %w", err)
			}
			log.Info("Cleaning up all Sonarr jellysweep tags...")
			if err := e.cleanupAllSonarrTags(ctx, additionalTags); err != nil {
				return fmt.Errorf("failed to cleanup Sonarr tags: %w", err)
			}
			return nil
		})
	}

	// Reset Radarr tags
	if e.radarr != nil {
		g.Go(func() error {
			log.Info("Removing jellysweep tags from Radarr movies...")
			if err := e.resetRadarrTags(ctx, additionalTags); err != nil {
				return fmt.Errorf("failed to reset Radarr tags: %w", err)
			}
			log.Info("Cleaning up all Radarr jellysweep tags...")
			if err := e.cleanupAllRadarrTags(ctx, additionalTags); err != nil {
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

// GetWebPushClient returns the webpush client.
func (e *Engine) GetWebPushClient() *webpush.Client {
	return e.webpush
}

// getCachedImageURL converts a direct image URL to a cached URL.
func getCachedImageURL(imageURL string) string {
	if imageURL == "" {
		return ""
	}
	// Encode the original URL and return a cache endpoint URL
	encoded := url.QueryEscape(imageURL)
	return fmt.Sprintf("/api/images/cache?url=%s", encoded)
}

// AddTagToMedia adds a specific tag to a media item (supports jellysweep-keep and must-delete).
func (e *Engine) AddTagToMedia(ctx context.Context, mediaID string, tagName string) error {
	// Parse media ID to determine if it's a Sonarr or Radarr item
	if seriesIDStr, ok := strings.CutPrefix(mediaID, "sonarr-"); ok {
		defer func() {
			_ = e.cache.SonarrItemsCache.Clear(ctx)
		}()

		seriesID, err := strconv.ParseInt(seriesIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", err)
		}

		// Use dedicated tag-resetting functions based on the action
		switch tagName {
		case JellysweepKeepPrefix:
			// For "keep": remove all tags (including delete) before adding must-keep
			if err := e.resetSingleSonarrTagsForKeep(ctx, int32(seriesID)); err != nil {
				return fmt.Errorf("failed to reset sonarr tags for keep: %w", err)
			}
			// This will add a jellysweep-must-keep tag with expiry date
			return e.addSonarrKeepTag(ctx, int32(seriesID))
		case JellysweepDeleteForSureTag:
			// For "must-delete": remove all tags except jellysweep-delete before adding must-delete-for-sure
			if err := e.resetSingleSonarrTagsForMustDelete(ctx, int32(seriesID)); err != nil {
				return fmt.Errorf("failed to reset sonarr tags for must-delete: %w", err)
			}
			// This will add a jellysweep-must-delete-for-sure tag
			return e.addSonarrDeleteForSureTag(ctx, int32(seriesID))
		case JellysweepIgnoreTag:
			// For "ignore": remove all jellysweep tags and add ignore tag in one operation
			return e.resetAllSonarrTagsAndAddIgnore(ctx, int32(seriesID))
		default:
			return fmt.Errorf("unsupported tag name: %s", tagName)
		}
	} else if movieIDStr, ok := strings.CutPrefix(mediaID, "radarr-"); ok {
		defer func() {
			_ = e.cache.RadarrItemsCache.Clear(ctx)
		}()

		movieID, err := strconv.ParseInt(movieIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", err)
		}

		// Use dedicated tag-resetting functions based on the action
		switch tagName {
		case JellysweepKeepPrefix:
			// For "keep": remove all tags (including delete) before adding must-keep
			if err := e.resetSingleRadarrTagsForKeep(ctx, int32(movieID)); err != nil {
				return fmt.Errorf("failed to reset radarr tags for keep: %w", err)
			}
			// This will add a jellysweep-must-keep tag with expiry date
			return e.addRadarrKeepTag(ctx, int32(movieID))
		case JellysweepDeleteForSureTag:
			// For "must-delete": remove all tags except jellysweep-delete before adding must-delete-for-sure
			if err := e.resetSingleRadarrTagsForMustDelete(ctx, int32(movieID)); err != nil {
				return fmt.Errorf("failed to reset radarr tags for must-delete: %w", err)
			}
			// This will add a jellysweep-must-delete-for-sure tag
			return e.addRadarrDeleteForSureTag(ctx, int32(movieID))
		case JellysweepIgnoreTag:
			// For "ignore": remove all jellysweep tags and add ignore tag in one operation
			return e.resetAllRadarrTagsAndAddIgnore(ctx, int32(movieID))
		default:
			return fmt.Errorf("unsupported tag name: %s", tagName)
		}
	}

	return fmt.Errorf("unsupported media ID format: %s", mediaID)
}
