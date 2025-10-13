package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"
	radarrAPI "github.com/devopsarr/radarr-go/radarr"
	sonarrAPI "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/go-co-op/gocron/v2"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/cache"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/database"
	"github.com/jon4hz/jellysweep/engine/arr"
	radarrImpl "github.com/jon4hz/jellysweep/engine/arr/radarr"
	sonarrImpl "github.com/jon4hz/jellysweep/engine/arr/sonarr"
	"github.com/jon4hz/jellysweep/engine/jellyfin"
	"github.com/jon4hz/jellysweep/engine/stats"
	"github.com/jon4hz/jellysweep/engine/stats/jellystat"
	"github.com/jon4hz/jellysweep/engine/stats/streamystats"
	"github.com/jon4hz/jellysweep/jellyseerr"
	"github.com/jon4hz/jellysweep/notify/email"
	"github.com/jon4hz/jellysweep/notify/ntfy"
	"github.com/jon4hz/jellysweep/notify/webpush"
	"github.com/jon4hz/jellysweep/policy"
	"github.com/jon4hz/jellysweep/scheduler"
	"github.com/jon4hz/jellysweep/tags"
	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"
)

type mediaItemsMap map[string][]arr.MediaItem

// ErrRequestAlreadyProcessed indicates that a keep request has already been processed.
var ErrRequestAlreadyProcessed = errors.New("request already processed")

// Engine is the main engine for Jellysweep, managing interactions with sonarr, radarr, and other services.
// It runs a cleanup job periodically to remove unwanted media.
type Engine struct {
	cfg        *config.Config
	db         database.DB
	policy     *policy.Engine
	jellyfin   *jellyfin.Client
	stats      stats.Statser
	jellyseerr *jellyseerr.Client
	sonarr     arr.Arrer
	radarr     arr.Arrer
	email      *email.NotificationService
	ntfy       *ntfy.Client
	webpush    *webpush.Client
	scheduler  *scheduler.Scheduler

	imageCache *cache.ImageCache
	cache      *cache.EngineCache // Cache for engine-specific data

	data *data
}

// data contains any data collected during the cleanup process.
type data struct {
	// library name to library paths
	libraryFoldersMap map[string][]string

	// userNotifications tracks which users should be notified about which media items
	userNotifications map[string][]arr.MediaItem // key: user email, value: media items
}

// New creates a new Engine instance.
func New(cfg *config.Config, db database.DB) (*Engine, error) {
	// Create scheduler first
	sched, err := scheduler.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduler: %w", err)
	}

	var statsClient stats.Statser
	if cfg.Jellystat != nil {
		statsClient = jellystat.New(cfg.Jellystat)
	}

	if cfg.Streamystats != nil {
		statsClient, err = streamystats.New(cfg.Streamystats, cfg.Jellyfin.APIKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create StreamyStats client: %w", err)
		}
	}

	engineCache, err := cache.NewEngineCache(cfg.Cache)
	if err != nil {
		return nil, fmt.Errorf("failed to create engine cache: %w", err)
	}

	// Create Jellyfin client
	jellyfinClient := jellyfin.New(cfg, engineCache.JellyfinItemsCache)

	var sonarrClient arr.Arrer
	if cfg.Sonarr != nil {
		rawSonarrClient := newSonarrClient(cfg.Sonarr)
		sonarrClient = sonarrImpl.NewSonarr(rawSonarrClient, cfg, statsClient, engineCache.SonarrItemsCache, engineCache.SonarrTagsCache)
	} else {
		log.Warn("Sonarr configuration is missing, some features will be disabled")
	}

	var radarrClient arr.Arrer
	if cfg.Radarr != nil {
		rawRadarrClient := newRadarrClient(cfg.Radarr)
		radarrClient = radarrImpl.NewRadarr(rawRadarrClient, cfg, statsClient, engineCache.RadarrItemsCache, engineCache.RadarrTagsCache)
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

	engine := &Engine{
		cfg:        cfg,
		db:         db,
		policy:     policy.NewEngine(),
		jellyfin:   jellyfinClient,
		stats:      statsClient,
		jellyseerr: jellyseerrClient,
		sonarr:     sonarrClient,
		radarr:     radarrClient,
		email:      emailService,
		ntfy:       ntfyClient,
		webpush:    webpushClient,
		scheduler:  sched,
		data: &data{
			userNotifications: make(mediaItemsMap),
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
	if err = e.markForDeletion(ctx); err != nil {
		log.Error("An error occurred while marking media for deletion")
	}
	e.removeRecentlyPlayedDeleteTags(ctx)

	// only delete media if there was no previous error
	if err == nil {
		if err := e.cleanupMedia(ctx); err != nil {
			log.Error("An error occurred while deleting media")
			return err
		}
	}

	log.Info("Scheduled cleanup job completed")
	return err
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

	jellyfinItems, _, err := e.jellyfin.GetJellyfinItems(ctx, false)
	if err != nil {
		log.Error("Failed to get jellyfin items from cache", "error", err)
		return
	}

	if e.sonarr != nil {
		if err := e.sonarr.RemoveRecentlyPlayedDeleteTags(ctx, jellyfinItems); err != nil {
			log.Error("Failed to remove recently played Sonarr delete tags", "error", err)
		}
	}

	if e.radarr != nil {
		if err := e.radarr.RemoveRecentlyPlayedDeleteTags(ctx, jellyfinItems); err != nil {
			log.Error("Failed to remove recently played Radarr delete tags", "error", err)
		}
	}
}

// removeExpiredKeepTags removes keep request tags that have expired.
func (e *Engine) removeExpiredKeepTags(ctx context.Context) {
	log.Info("Removing expired keep request tags")
	if e.sonarr != nil {
		if err := e.sonarr.RemoveExpiredKeepTags(ctx); err != nil {
			log.Errorf("failed to remove expired Sonarr keep tags: %v", err)
		}
	}

	if e.radarr != nil {
		if err := e.radarr.RemoveExpiredKeepTags(ctx); err != nil {
			log.Errorf("failed to remove expired Radarr keep tags: %v", err)
		}
	}
}

func (e *Engine) markForDeletion(ctx context.Context) error {
	mediaItems, err := e.gatherMediaItems(ctx)
	if err != nil {
		log.Errorf("failed to gather media items: %v", err)
		return err
	}
	log.Info("Media items gathered successfully")

	// Filter out items that are already marked for deletion in the database
	log.Info("Filtering out items already marked for deletion in the database")
	mediaItems, err = e.filterAlreadyMarkedForDeletion(mediaItems)
	if err != nil {
		log.Errorf("failed to filter already marked for deletion: %v", err)
		return err
	}

	// Filter out series that already meet the keep criteria (if cleanup mode is set to keep episodes or seasons)
	log.Info("Filtering series that already meet keep criteria")
	mediaItems = e.filterSeriesAlreadyMeetingKeepCriteria(mediaItems)

	// Filter media items based on tags
	log.Info("Filtering media items based on tags")
	mediaItems = e.filterMediaTags(mediaItems)

	log.Info("Checking content age")
	mediaItems, err = e.filterContentAgeThreshold(ctx, mediaItems)
	if err != nil {
		log.Errorf("failed to filter content age threshold: %v", err)
		return err
	}

	log.Info("Checking content size")
	mediaItems, err = e.filterContentSizeThreshold(ctx, mediaItems)
	if err != nil {
		log.Errorf("failed to filter content size threshold: %v", err)
		return err
	}

	log.Info("Checking for streaming history")
	mediaItems, err = e.filterLastStreamThreshold(ctx, mediaItems)
	if err != nil {
		log.Errorf("failed to filter last stream threshold: %v", err)
		return err
	}

	// Populate requester information from Jellyseerr
	log.Info("Populating requester information")
	mediaItems = e.populateRequesterInfo(ctx, mediaItems)

	// Populate user notifications for email sending
	if e.data.userNotifications == nil {
		e.data.userNotifications = make(mediaItemsMap)
	}

	for lib, items := range mediaItems {
		for _, item := range items {
			e.data.userNotifications[item.RequestedBy] = append(e.data.userNotifications[item.RequestedBy], item)
			log.Info("Marking media item for deletion", "name", item.Title, "library", lib)
		}
	}
	log.Info("Media items filtered successfully")

	if len(mediaItems) == 0 {
		log.Info("No media items marked for deletion after filtering")
		return nil
	}

	// save items to database
	if err := e.saveMediaItemsToDatabase(mediaItems); err != nil {
		log.Errorf("failed to save media items to database: %v", err)
		return err
	}
	log.Info("Media items saved to database successfully")

	// Send email notifications before marking for deletion
	e.sendEmailNotifications(mediaItems)

	// Send ntfy deletion summary notification
	if err := e.sendNtfyDeletionSummary(ctx, mediaItems); err != nil {
		log.Errorf("failed to send ntfy deletion summary: %v", err)
		// Don't return here, continue with the cleanup process
	}
	return nil
}

// gatherMediaItems gathers all media items from Jellyfin, Sonarr, and Radarr.
// It merges them into a single collection grouped by library.
func (e *Engine) gatherMediaItems(ctx context.Context) (mediaItemsMap, error) {
	jellyfinItems, libraryFoldersMap, err := e.jellyfin.GetJellyfinItems(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get jellyfin items: %w", err)
	}
	e.data.libraryFoldersMap = libraryFoldersMap

	var sonarrItems mediaItemsMap
	if e.sonarr != nil {
		sonarrItems, err = e.sonarr.GetItems(ctx, jellyfinItems, true)
		if err != nil {
			return nil, fmt.Errorf("failed to get sonarr items: %w", err)
		}
	}

	var radarrItems mediaItemsMap
	if e.radarr != nil {
		radarrItems, err = e.radarr.GetItems(ctx, jellyfinItems, true)
		if err != nil {
			return nil, fmt.Errorf("failed to get radarr items: %w", err)
		}
	}

	// Merge all media items
	mediaItems := make(mediaItemsMap)
	totalCount := 0
	for lib, items := range sonarrItems {
		mediaItems[lib] = append(mediaItems[lib], items...)
		totalCount += len(items)
	}
	for lib, items := range radarrItems {
		mediaItems[lib] = append(mediaItems[lib], items...)
		totalCount += len(items)
	}

	log.Infof("Merged %d media items across %d libraries", totalCount, len(mediaItems))

	// Set deletion policies with freshly gathered library folders map
	e.policy.SetPolicies(
		policy.NewDefaultDelete(e.cfg),
		policy.NewDiskUsageDelete(e.cfg, libraryFoldersMap),
	)

	return mediaItems, nil
}

func (e *Engine) saveMediaItemsToDatabase(mediaItems mediaItemsMap) error {
	dbMediaItems := make([]database.Media, 0)
	for _, items := range mediaItems {
		for _, item := range items {
			dbItem := database.Media{
				JellyfinID:  item.JellyfinID,
				LibraryName: item.LibraryName,
				RequestedBy: item.RequestedBy,
			}

			switch item.MediaType {
			case models.MediaTypeTV:
				dbItem.MediaType = database.MediaTypeTV
				dbItem.ArrID = item.SeriesResource.GetId()
				dbItem.Title = item.SeriesResource.GetTitle()
				dbItem.Year = item.SeriesResource.GetYear()
				dbItem.FileSize = item.SeriesResource.Statistics.GetSizeOnDisk()
				dbItem.TvdbId = lo.ToPtr(item.SeriesResource.GetTvdbId())
				dbItem.TmdbId = lo.ToPtr(item.SeriesResource.GetTmdbId())

				for _, img := range item.SeriesResource.GetImages() {
					if img.GetCoverType() == sonarrAPI.MEDIACOVERTYPES_POSTER {
						dbItem.PosterURL = img.GetRemoteUrl()
					}
				}

			case models.MediaTypeMovie:
				dbItem.MediaType = database.MediaTypeMovie
				dbItem.ArrID = item.MovieResource.GetId()
				dbItem.Title = item.MovieResource.GetTitle()
				dbItem.Year = item.MovieResource.GetYear()
				dbItem.FileSize = item.MovieResource.Statistics.GetSizeOnDisk()
				dbItem.TmdbId = lo.ToPtr(item.MovieResource.GetTmdbId())

				for _, img := range item.MovieResource.GetImages() {
					if img.GetCoverType() == radarrAPI.MEDIACOVERTYPES_POSTER {
						dbItem.PosterURL = img.GetRemoteUrl()
					}
				}

			default:
				return fmt.Errorf("unsupported media type: %s", item.MediaType)
			}

			if err := e.policy.ApplyAll(&dbItem); err != nil {
				log.Errorf("failed to apply policies to media item %s: %v", dbItem.Title, err)
				continue
			}

			dbMediaItems = append(dbMediaItems, dbItem)
		}
	}

	if err := e.db.CreateMediaItems(context.Background(), dbMediaItems); err != nil {
		return fmt.Errorf("failed to create media items to database: %w", err)
	}

	return nil
}

func (e *Engine) cleanupMedia(ctx context.Context) error {
	deletedItems := make(mediaItemsMap)

	mediaItems, err := e.db.GetMediaItems(ctx)
	if err != nil {
		log.Errorf("failed to get media items from database: %v", err)
		return err
	}

	for _, item := range mediaItems {
		// since the deletion policies were already set during the scaning phase, we can just use the existing policy engine.
		if ok, err := e.policy.ShouldTriggerDeletion(ctx, item); err != nil {
			log.Errorf("failed to check deletion policy for media item %s: %v", item.Title, err)
			continue
		} else if !ok {
			log.Infof("Skipping deletion for media item %s, no policies triggered", item.Title)
			continue
		}

		switch item.MediaType {
		case database.MediaTypeTV:
			if e.sonarr == nil {
				log.Warnf("Sonarr client not configured, cannot delete TV show %s", item.Title)
				continue
			}
			if err := e.sonarr.DeleteMedia(ctx, item.ArrID, item.Title); err != nil {
				log.Errorf("failed to delete Sonarr media %s: %v", item.Title, err)
				continue
			}
			deletedItems["TV Shows"] = append(deletedItems["TV Shows"], arr.MediaItem{
				Title:     item.Title,
				Year:      item.Year,
				MediaType: models.MediaTypeTV,
			})

		case database.MediaTypeMovie:
			if e.radarr == nil {
				log.Warnf("Radarr client not configured, cannot delete movie %s", item.Title)
				continue
			}
			if err := e.radarr.DeleteMedia(ctx, item.ArrID, item.Title); err != nil {
				log.Errorf("failed to delete Radarr media %s: %v", item.Title, err)
				continue
			}
			deletedItems["Movies"] = append(deletedItems["Movies"], arr.MediaItem{
				Title:     item.Title,
				Year:      item.Year,
				MediaType: models.MediaTypeMovie,
			})

		default:
			log.Errorf("unsupported media type for deletion: %s", item.MediaType)
			continue
		}
	}

	// Send completion notification if any items were deleted
	if len(deletedItems) > 0 {
		if err := e.sendNtfyDeletionCompletedNotification(ctx, deletedItems); err != nil {
			log.Errorf("failed to send deletion completed notification: %v", err)
		}
	}

	return nil
}

// GetMediaItemsMarkedForDeletion returns all media items that are marked for deletion.
func (e *Engine) GetMediaItemsMarkedForDeletion(ctx context.Context) ([]database.Media, error) {
	return e.db.GetMediaItems(ctx)
}

// GetMediaItemsMarkedForDeletionByType returns media items marked for deletion by type.
func (e *Engine) GetMediaItemsMarkedForDeletionByType(ctx context.Context, mediaType database.MediaType) ([]database.Media, error) {
	return e.db.GetMediaItemsByMediaType(ctx, mediaType)
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

		mediaTitle, mediaType, err = e.sonarr.AddKeepRequest(ctx, int32(seriesID), username)
	} else if movieIDStr, ok := strings.CutPrefix(mediaID, "radarr-"); ok {
		if e.radarr == nil {
			return fmt.Errorf("radarr client not available")
		}

		movieID, parseErr := strconv.ParseInt(movieIDStr, 10, 32)
		if parseErr != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", parseErr)
		}

		mediaTitle, mediaType, err = e.radarr.AddKeepRequest(ctx, int32(movieID), username)
	} else {
		return fmt.Errorf("unsupported media ID format: %s", mediaID)
	}

	// Send ntfy notification if the tag was added successfully
	if err == nil && e.ntfy != nil {
		if ntfyErr := e.ntfy.SendKeepRequest(ctx, mediaTitle, mediaType, username); ntfyErr != nil {
			log.Errorf("Failed to send ntfy keep request notification: %v", ntfyErr)
		}
	}

	return err
}

// GetKeepRequests returns all media items that have keep request tags.
func (e *Engine) GetKeepRequests(ctx context.Context, forceRefresh bool) ([]models.KeepRequest, error) {
	var result []models.KeepRequest

	// Get Sonarr keep requests
	if e.sonarr != nil {
		sonarrKeepRequests, err := e.sonarr.GetKeepRequests(ctx, e.data.libraryFoldersMap, forceRefresh)
		if err != nil {
			log.Errorf("failed to get sonarr keep requests: %v", err)
		} else {
			result = append(result, sonarrKeepRequests...)
		}
	}

	// Get Radarr keep requests
	if e.radarr != nil {
		radarrKeepRequests, err := e.radarr.GetKeepRequests(ctx, e.data.libraryFoldersMap, forceRefresh)
		if err != nil {
			log.Errorf("failed to get radarr keep requests: %v", err)
		} else {
			result = append(result, radarrKeepRequests...)
		}
	}

	return result, nil
}

// AcceptKeepRequest removes the keep request tag and delete tag from the media item.
func (e *Engine) AcceptKeepRequest(ctx context.Context, mediaID string) error {
	var resp *arr.KeepRequestResponse
	// Parse media ID to determine if it's a Sonarr or Radarr item
	if seriesIDStr, ok := strings.CutPrefix(mediaID, "sonarr-"); ok {
		seriesID, err := strconv.ParseInt(seriesIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", err)
		}
		if e.sonarr == nil {
			return fmt.Errorf("sonarr client not available")
		}
		resp, err = e.sonarr.AcceptKeepRequest(ctx, int32(seriesID))
		if err != nil {
			return err
		}
	} else if movieIDStr, ok := strings.CutPrefix(mediaID, "radarr-"); ok {
		movieID, err := strconv.ParseInt(movieIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", err)
		}
		if e.radarr == nil {
			return fmt.Errorf("radarr client not available")
		}
		resp, err = e.radarr.AcceptKeepRequest(ctx, int32(movieID))
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unsupported media ID format: %s", mediaID)
	}

	if e.webpush != nil && resp.Requester != "" {
		if pushErr := e.webpush.SendKeepRequestNotification(ctx, resp.Requester, resp.Title, resp.MediaType, resp.Approved); pushErr != nil {
			log.Errorf("Failed to send webpush notification: %v", pushErr)
		}
	}

	return nil
}

// DeclineKeepRequest removes the keep request tag and adds a delete-for-sure tag.
func (e *Engine) DeclineKeepRequest(ctx context.Context, mediaID string) error {
	var resp *arr.KeepRequestResponse
	// Parse media ID to determine if it's a Sonarr or Radarr item
	if seriesIDStr, ok := strings.CutPrefix(mediaID, "sonarr-"); ok {
		seriesID, err := strconv.ParseInt(seriesIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", err)
		}
		if e.sonarr == nil {
			return fmt.Errorf("sonarr client not available")
		}
		resp, err = e.sonarr.DeclineKeepRequest(ctx, int32(seriesID))
		if err != nil {
			return err
		}
	} else if movieIDStr, ok := strings.CutPrefix(mediaID, "radarr-"); ok {
		movieID, err := strconv.ParseInt(movieIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", err)
		}
		if e.radarr == nil {
			return fmt.Errorf("radarr client not available")
		}
		resp, err = e.radarr.DeclineKeepRequest(ctx, int32(movieID))
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unsupported media ID format: %s", mediaID)
	}

	if e.webpush != nil && resp.Requester != "" {
		if pushErr := e.webpush.SendKeepRequestNotification(ctx, resp.Requester, resp.Title, resp.MediaType, resp.Approved); pushErr != nil {
			log.Errorf("Failed to send webpush notification: %v", pushErr)
		}
	}

	return nil
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
			if err := e.sonarr.ResetTags(ctx, additionalTags); err != nil {
				return fmt.Errorf("failed to reset Sonarr tags: %w", err)
			}
			log.Info("Cleaning up all Sonarr jellysweep tags...")
			if err := e.sonarr.CleanupAllTags(ctx, additionalTags); err != nil {
				return fmt.Errorf("failed to cleanup Sonarr tags: %w", err)
			}
			return nil
		})
	}

	// Reset Radarr tags
	if e.radarr != nil {
		g.Go(func() error {
			log.Info("Removing jellysweep tags from Radarr movies...")
			if err := e.radarr.ResetTags(ctx, additionalTags); err != nil {
				return fmt.Errorf("failed to reset Radarr tags: %w", err)
			}
			log.Info("Cleaning up all Radarr jellysweep tags...")
			if err := e.radarr.CleanupAllTags(ctx, additionalTags); err != nil {
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

		if e.sonarr == nil {
			return fmt.Errorf("sonarr client not available")
		}

		// Use dedicated tag operations based on the tag name
		switch tagName {
		case tags.JellysweepKeepPrefix:
			// For "keep": remove all tags (including delete) before adding must-keep
			if err := e.sonarr.ResetSingleTagsForKeep(ctx, int32(seriesID)); err != nil {
				return fmt.Errorf("failed to reset sonarr tags for keep: %w", err)
			}
			// This will add a jellysweep-must-keep tag with expiry date
			return e.sonarr.AddKeepTag(ctx, int32(seriesID))
		case tags.JellysweepDeleteForSureTag:
			// For "must-delete": remove all tags except jellysweep-delete before adding must-delete-for-sure
			if err := e.sonarr.ResetSingleTagsForMustDelete(ctx, int32(seriesID)); err != nil {
				return fmt.Errorf("failed to reset sonarr tags for must-delete: %w", err)
			}
			return e.sonarr.AddDeleteForSureTag(ctx, int32(seriesID))
		case tags.JellysweepIgnoreTag:
			// For "ignore": remove all jellysweep tags and add ignore tag in one operation
			return e.sonarr.ResetAllTagsAndAddIgnore(ctx, int32(seriesID))
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

		if e.radarr == nil {
			return fmt.Errorf("radarr client not available")
		}

		// Use dedicated tag operations based on the tag name
		switch tagName {
		case tags.JellysweepKeepPrefix:
			// For "keep": remove all tags (including delete) before adding must-keep
			if err := e.radarr.ResetSingleTagsForKeep(ctx, int32(movieID)); err != nil {
				return fmt.Errorf("failed to reset radarr tags for keep: %w", err)
			}
			// This will add a jellysweep-must-keep tag with expiry date
			return e.radarr.AddKeepTag(ctx, int32(movieID))
		case tags.JellysweepDeleteForSureTag:
			// For "must-delete": remove all tags except jellysweep-delete before adding must-delete-for-sure
			if err := e.radarr.ResetSingleTagsForMustDelete(ctx, int32(movieID)); err != nil {
				return fmt.Errorf("failed to reset radarr tags for must-delete: %w", err)
			}
			return e.radarr.AddDeleteForSureTag(ctx, int32(movieID))
		case tags.JellysweepIgnoreTag:
			// For "ignore": remove all jellysweep tags and add ignore tag in one operation
			return e.radarr.ResetAllTagsAndAddIgnore(ctx, int32(movieID))
		default:
			return fmt.Errorf("unsupported tag name: %s", tagName)
		}
	}

	return fmt.Errorf("unsupported media ID format: %s", mediaID)
}
