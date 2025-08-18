package engine

import (
	"context"
	"encoding/json"
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
	"github.com/jon4hz/jellysweep/database"
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

// Engine is the main engine for Jellysweep, managing interactions with sonarr, radarr, and other services.
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
	db           database.DatabaseInterface

	imageCache *cache.ImageCache
	cache      *cache.EngineCache // Cache for engine-specific data
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

	// Initialize database
	db, err := database.New("./data/jellysweep.db")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
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
		db:           db,
		imageCache:   cache.NewImageCache("./data/cache/images"),
		cache:        engineCache,
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
	if e.db != nil {
		if err := e.db.Close(); err != nil {
			log.Errorf("failed to close database: %v", err)
		}
	}
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

	// Check if there's already an active cleanup run
	activeRun, err := e.db.GetActiveCleanupRun(ctx)
	if err != nil {
		log.Errorf("failed to check for active cleanup run: %v", err)
		return err
	}

	var cleanupRun *database.CleanupRun
	if activeRun != nil {
		log.Info("Resuming existing cleanup run", "runID", activeRun.ID, "step", activeRun.Step)
		cleanupRun = activeRun
	} else {
		// Start new cleanup run
		cleanupRun, err = e.db.StartCleanupRun(ctx)
		if err != nil {
			log.Errorf("failed to start cleanup run: %v", err)
			return err
		}
		log.Info("Started new cleanup run", "runID", cleanupRun.ID)
	}

	// Clear all caches to ensure fresh data
	e.cache.ClearAll(ctx)

	// Execute cleanup steps based on current state
	defer func() {
		// Complete the cleanup run
		status := database.CleanupRunStatusCompleted
		var errorMessage *string
		if err != nil {
			status = database.CleanupRunStatusFailed
			errMsg := err.Error()
			errorMessage = &errMsg
		}

		if completeErr := e.db.CompleteCleanupRun(ctx, cleanupRun.ID, status, errorMessage); completeErr != nil {
			log.Errorf("failed to complete cleanup run: %v", completeErr)
		}

		if err == nil {
			log.Info("Scheduled cleanup job completed successfully", "runID", cleanupRun.ID)
		} else {
			log.Error("Scheduled cleanup job failed", "runID", cleanupRun.ID, "error", err)
		}
	}()

	// Execute cleanup steps in order, but skip already completed ones
	// Define step closures so we can pass needed state without using shared in-memory engine fields
	steps := []struct {
		step database.CleanupRunStep
		fn   func(context.Context) error
	}{
		{database.CleanupRunStepRemoveExpiredKeepTags, func(ctx context.Context) error {
			e.removeExpiredKeepTags(ctx)
			return nil
		}},
		{database.CleanupRunStepCleanupOldTags, func(ctx context.Context) error {
			e.cleanupOldTags(ctx)
			return nil
		}},
		{database.CleanupRunStepMarkForDeletion, func(ctx context.Context) error {
			// Compute and mark items for deletion; record to DB; send notifications
			mediaItems, userNotifications, err := e.markForDeletion(ctx)
			if err != nil {
				return err
			}

			// Record media items marked for deletion
			if len(mediaItems) > 0 {
				var mediaActions []database.CleanupMediaItem
				for library, items := range mediaItems {
					for _, item := range items {
						var tagsJSON *string
						if len(item.Tags) > 0 {
							if jsonStr := stringSliceToJSON(item.Tags); jsonStr != nil {
								tagsJSON = jsonStr
							}
						}
						dbItem := database.CleanupMediaItem{
							CleanupRunID:    cleanupRun.ID,
							JellyfinID:      item.JellyfinID,
							MediaID:         getMediaID(item),
							Title:           item.Title,
							MediaType:       string(item.MediaType),
							Year:            lo.ToPtr(int(item.Year)),
							Library:         &library,
							TmdbID:          &item.TmdbId,
							FileSize:        getFileSize(item),
							RequestedBy:     lo.ToPtr(item.RequestedBy),
							RequestDate:     lo.ToPtr(item.RequestDate),
							Action:          database.MediaActionMarkedForDeletion,
							ActionTimestamp: time.Now(),
							DeletionDate:    getDeletionDate(item),
							Tags:            tagsJSON,
						}
						mediaActions = append(mediaActions, dbItem)
					}
				}
				if len(mediaActions) > 0 {
					if recErr := e.db.RecordMediaActions(ctx, cleanupRun.ID, mediaActions); recErr != nil {
						log.Errorf("failed to record media actions: %v", recErr)
					}
				}
			}

			// Send email and ntfy notifications
			e.sendEmailNotifications(userNotifications, mediaItems)
			if err := e.sendNtfyDeletionSummary(ctx, mediaItems); err != nil {
				log.Errorf("failed to send ntfy deletion summary: %v", err)
			}
			return nil
		}},
		{database.CleanupRunStepRemoveRecentlyPlayed, func(ctx context.Context) error {
			return e.removeRecentlyPlayedDeleteTags(ctx)
		}},
		{database.CleanupRunStepCleanupMedia, func(ctx context.Context) error {
			// Perform deletion and record deleted items
			deleted := e.cleanupMedia(ctx)
			if len(deleted) > 0 {
				var actions []database.CleanupMediaItem
				for library, items := range deleted {
					for _, item := range items {
						var tagsJSON *string
						if len(item.Tags) > 0 {
							if jsonStr := stringSliceToJSON(item.Tags); jsonStr != nil {
								tagsJSON = jsonStr
							}
						}
						actions = append(actions, database.CleanupMediaItem{
							CleanupRunID:    cleanupRun.ID,
							JellyfinID:      item.JellyfinID,
							MediaID:         getMediaID(item),
							Title:           item.Title,
							MediaType:       string(item.MediaType),
							Year:            lo.ToPtr(int(item.Year)),
							Library:         &library,
							TmdbID:          &item.TmdbId,
							FileSize:        getFileSize(item),
							RequestedBy:     lo.ToPtr(item.RequestedBy),
							RequestDate:     lo.ToPtr(item.RequestDate),
							Action:          database.MediaActionDeleted,
							ActionTimestamp: time.Now(),
							DeletionDate:    getDeletionDate(item),
							Tags:            tagsJSON,
						})
					}
				}
				if recErr := e.db.RecordMediaActions(ctx, cleanupRun.ID, actions); recErr != nil {
					log.Errorf("failed to record deleted media actions: %v", recErr)
				}
			}
			return nil
		}},
	}

	for _, stepInfo := range steps {
		// Skip if we're past this step already
		if e.shouldSkipStep(cleanupRun.Step, stepInfo.step) {
			log.Debug("Skipping already completed step", "step", stepInfo.step)
			continue
		}

		// Update current step
		if err = e.db.UpdateCleanupRunStep(ctx, cleanupRun.ID, stepInfo.step); err != nil {
			log.Errorf("failed to update cleanup run step: %v", err)
			return err
		}

		// Start step tracking
		if err = e.db.StartCleanupStep(ctx, cleanupRun.ID, stepInfo.step); err != nil {
			log.Errorf("failed to start cleanup step tracking: %v", err)
			// Don't fail the whole process for tracking issues
		}

		log.Info("Executing cleanup step", "step", stepInfo.step)

		// Execute the step
		stepErr := stepInfo.fn(ctx)

		// Complete step tracking
		stepStatus := database.CleanupRunStatusCompleted
		var stepErrorMessage *string
		itemsProcessed := 0 // TODO: Track items processed per step

		if stepErr != nil {
			stepStatus = database.CleanupRunStatusFailed
			errMsg := stepErr.Error()
			stepErrorMessage = &errMsg
		}

		if completeStepErr := e.db.CompleteCleanupStep(ctx, cleanupRun.ID, stepInfo.step, stepStatus, stepErrorMessage, itemsProcessed); completeStepErr != nil {
			log.Errorf("failed to complete cleanup step tracking: %v", completeStepErr)
		}

		// If step failed, return error
		if stepErr != nil {
			log.Errorf("cleanup step %s failed: %v", stepInfo.step, stepErr)
			return stepErr
		}

		log.Info("Completed cleanup step", "step", stepInfo.step)
	}

	return nil
}

// shouldSkipStep determines if a step should be skipped based on current progress.
func (e *Engine) shouldSkipStep(currentStep, targetStep database.CleanupRunStep) bool {
	stepOrder := map[database.CleanupRunStep]int{
		database.CleanupRunStepStarting:              0,
		database.CleanupRunStepRemoveExpiredKeepTags: 1,
		database.CleanupRunStepCleanupOldTags:        2,
		database.CleanupRunStepMarkForDeletion:       3,
		database.CleanupRunStepRemoveRecentlyPlayed:  4,
		database.CleanupRunStepCleanupMedia:          5,
		database.CleanupRunStepCompleted:             6,
	}

	currentOrder := stepOrder[currentStep]
	targetOrder := stepOrder[targetStep]

	return currentOrder > targetOrder
}

// Tracking wrapper functions removed; steps now use closures and explicit persistence

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

// GetDatabase returns the database interface.
func (e *Engine) GetDatabase() database.DatabaseInterface {
	return e.db
}

// removeRecentlyPlayedDeleteTags removes jellysweep-delete tags from media that has been played recently.
func (e *Engine) removeRecentlyPlayedDeleteTags(ctx context.Context) error {
	log.Info("Checking for recently played media with pending delete tags")

	// Fetch Jellyfin items and maps to correlate with Radarr/Sonarr
	jellyfinItems, libraryIDMap, _, err := e.getJellyfinItems(ctx)
	if err != nil {
		log.Errorf("Failed to get Jellyfin items for recently played check: %v", err)
		return err
	}

	if e.sonarr != nil {
		if err := e.removeRecentlyPlayedSonarrDeleteTags(ctx, jellyfinItems, libraryIDMap); err != nil {
			log.Error("Failed to remove recently played Sonarr delete tags", "error", err)
		}
	}

	if e.radarr != nil {
		e.removeRecentlyPlayedRadarrDeleteTags(ctx, jellyfinItems, libraryIDMap)
	}
	return nil
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

func (e *Engine) markForDeletion(ctx context.Context) (map[string][]MediaItem, map[string][]MediaItem, error) {
	// Gather fresh media items
	mediaItems, err := e.gatherMediaItems(ctx)
	if err != nil {
		log.Errorf("failed to gather media items: %v", err)
		return nil, nil, err
	}
	log.Info("Media items gathered successfully")

	// Filter out series that already meet the keep criteria (if cleanup mode is set to keep episodes or seasons)
	log.Info("Filtering series that already meet keep criteria")
	mediaItems = e.filterSeriesAlreadyMeetingKeepCriteria(mediaItems)

	// Filter media items based on tags
	log.Info("Filtering media items based on tags")
	mediaItems = e.filterMediaTags(mediaItems)

	log.Info("Checking content age")
	if filtered, err := e.filterContentAgeThreshold(ctx, mediaItems); err != nil {
		log.Errorf("failed to filter content age threshold: %v", err)
		return nil, nil, err
	} else {
		mediaItems = filtered
	}

	log.Info("Checking content size")
	if filtered, err := e.filterContentSizeThreshold(ctx, mediaItems); err != nil {
		log.Errorf("failed to filter content size threshold: %v", err)
		return nil, nil, err
	} else {
		mediaItems = filtered
	}

	log.Info("Checking for streaming history")
	if filtered, err := e.filterLastStreamThreshold(ctx, mediaItems); err != nil {
		log.Errorf("failed to filter last stream threshold: %v", err)
		return nil, nil, err
	} else {
		mediaItems = filtered
	}

	// Populate requester information from Jellyseerr
	log.Info("Populating requester information")
	mediaItems = e.populateRequesterInfo(ctx, mediaItems)

	// Populate user notifications for email sending
	userNotifications := make(map[string][]MediaItem)
	for lib, items := range mediaItems {
		for _, item := range items {
			userNotifications[item.RequestedBy] = append(userNotifications[item.RequestedBy], item)
			log.Info("Marking media item for deletion", "name", item.Title, "library", lib)
		}
	}
	log.Info("Media items filtered successfully")

	if err := e.markSonarrMediaItemsForDeletion(ctx, mediaItems, e.cfg.DryRun); err != nil {
		log.Errorf("failed to mark sonarr media items for deletion: %v", err)
		return nil, nil, err
	}
	if err := e.markRadarrMediaItemsForDeletion(ctx, mediaItems, e.cfg.DryRun); err != nil {
		log.Errorf("failed to mark radarr media items for deletion: %v", err)
		return nil, nil, err
	}
	return mediaItems, userNotifications, nil
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
func (e *Engine) gatherMediaItems(ctx context.Context) (map[string][]MediaItem, error) {
	jellyfinItems, libraryIDMap, _, err := e.getJellyfinItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get jellyfin items: %w", err)
	}

	sonarrItems, err := e.getSonarrItems(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get sonarr items: %w", err)
	}

	sonarrTags, err := e.getSonarrTags(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get sonarr tags: %w", err)
	}

	radarrItems, err := e.getRadarrItems(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get radarr items: %w", err)
	}

	radarrTags, err := e.getRadarrTags(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get radarr tags: %w", err)
	}

	mediaItems := make(map[string][]MediaItem, 0)
	for _, item := range jellyfinItems {
		libraryName := strings.ToLower(getLibraryNameByID(libraryIDMap, item.ParentLibraryID))
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
	log.Infof("Merged %d media items across %d libraries", len(jellyfinItems), len(mediaItems))
	return mediaItems, nil
}

func (e *Engine) filterLastStreamThreshold(ctx context.Context, mediaItems map[string][]MediaItem) (map[string][]MediaItem, error) {
	if e.jellystat != nil {
		return e.filterJellystatLastStreamThreshold(ctx, mediaItems)
	}
	if e.streamystats != nil {
		return e.filterStreamystatsLastStreamThreshold(ctx, mediaItems)
	}
	return mediaItems, nil
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

func (e *Engine) cleanupMedia(ctx context.Context) map[string][]MediaItem {
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
	return deletedItems
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

// Helper functions for database operations.
func stringSliceToJSON(slice []string) *string {
	if len(slice) == 0 {
		return nil
	}

	jsonBytes, err := json.Marshal(slice)
	if err != nil {
		return nil
	}

	jsonStr := string(jsonBytes)
	return &jsonStr
}

func getMediaID(item MediaItem) string {
	if item.MediaType == models.MediaTypeTV && item.SeriesResource.Id != nil {
		return fmt.Sprintf("sonarr-%d", *item.SeriesResource.Id)
	}
	if item.MediaType == models.MediaTypeMovie && item.MovieResource.Id != nil {
		return fmt.Sprintf("radarr-%d", *item.MovieResource.Id)
	}
	return ""
}

func getFileSize(item MediaItem) int64 {
	if item.MediaType == models.MediaTypeTV {
		return getSeriesFileSize(item.SeriesResource)
	}
	if item.MediaType == models.MediaTypeMovie {
		return item.MovieResource.GetSizeOnDisk()
	}
	return 0
}

func getDeletionDate(item MediaItem) *time.Time {
	// This would need to be extracted from the tags or calculated
	// For now, return nil - this could be enhanced later
	return nil
}
