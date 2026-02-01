package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	radarrAPI "github.com/devopsarr/radarr-go/radarr"
	sonarrAPI "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/cache"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	radarrImpl "github.com/jon4hz/jellysweep/internal/engine/arr/radarr"
	sonarrImpl "github.com/jon4hz/jellysweep/internal/engine/arr/sonarr"
	"github.com/jon4hz/jellysweep/internal/engine/jellyfin"
	"github.com/jon4hz/jellysweep/internal/engine/stats"
	"github.com/jon4hz/jellysweep/internal/engine/stats/jellystat"
	"github.com/jon4hz/jellysweep/internal/engine/stats/streamystats"
	"github.com/jon4hz/jellysweep/internal/filter"
	agefilter "github.com/jon4hz/jellysweep/internal/filter/age_filter"
	databasefilter "github.com/jon4hz/jellysweep/internal/filter/database_filter"
	seriesfilter "github.com/jon4hz/jellysweep/internal/filter/series_filter"
	sizefilter "github.com/jon4hz/jellysweep/internal/filter/size_filter"
	streamfilter "github.com/jon4hz/jellysweep/internal/filter/stream_filter"
	tagsfilter "github.com/jon4hz/jellysweep/internal/filter/tags_filter"
	tunarrfilter "github.com/jon4hz/jellysweep/internal/filter/tunarr_filter"
	"github.com/jon4hz/jellysweep/internal/notify/email"
	"github.com/jon4hz/jellysweep/internal/notify/ntfy"
	"github.com/jon4hz/jellysweep/internal/notify/webpush"
	"github.com/jon4hz/jellysweep/internal/policy"
	"github.com/jon4hz/jellysweep/internal/scheduler"
	"github.com/jon4hz/jellysweep/internal/tags"
	"github.com/jon4hz/jellysweep/pkg/jellyseerr"
	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"
)

var (
	// ErrRequestAlreadyProcessed indicates that a keep request has already been processed.
	ErrRequestAlreadyProcessed = errors.New("request already processed")
	// ErrUnkeepableMedia indicates that the specified media item cannot be kept.
	ErrUnkeepableMedia = errors.New("media cannot be kept")
)

// Engine is the main engine for Jellysweep, managing interactions with sonarr, radarr, and other services.
// It runs a cleanup job periodically to remove unwanted media.
type Engine struct {
	cfg        *config.Config
	db         database.DB
	filters    *filter.Filter
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

	// migrate old tag based items to database
	initialDBMigration bool

	data *data
}

// data contains any data collected during the cleanup process.
type data struct {
	// userNotifications tracks which users should be notified about which media items
	userNotifications map[string][]arr.MediaItem // key: user email, value: media items
}

// New creates a new Engine instance.
func New(cfg *config.Config, db database.DB, initialDBMigration bool) (*Engine, error) {
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
	jellyfinClient := jellyfin.New(cfg)

	var sonarrClient arr.Arrer
	if cfg.Sonarr != nil {
		sonarrClient = sonarrImpl.NewSonarr(cfg, statsClient, engineCache.SonarrTagsCache)
	} else {
		log.Warn("Sonarr configuration is missing, some features will be disabled")
	}

	var radarrClient arr.Arrer
	if cfg.Radarr != nil {
		radarrClient = radarrImpl.NewRadarr(cfg, statsClient, engineCache.RadarrTagsCache)
	} else {
		log.Warn("Radarr configuration is missing, some features will be disabled")
	}

	filterList := []filter.Filterer{
		databasefilter.New(db),
		seriesfilter.New(cfg),
		tagsfilter.New(cfg),
		sizefilter.New(cfg),
		agefilter.New(cfg, db, sonarrClient, radarrClient),
		streamfilter.New(cfg, statsClient),
	}

	if cfg.Tunarr != nil {
		tunarrF, err := tunarrfilter.New(cfg)
		if err != nil {
			log.Warnf("Failed to create Tunarr filter: %v", err)
		} else {
			filterList = append(filterList, tunarrF)
		}
	}

	filters := filter.New(filterList...)

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
		cfg:                cfg,
		db:                 db,
		initialDBMigration: initialDBMigration,
		filters:            filters,
		policy:             policy.NewEngine(),
		jellyfin:           jellyfinClient,
		stats:              statsClient,
		jellyseerr:         jellyseerrClient,
		sonarr:             sonarrClient,
		radarr:             radarrClient,
		email:              emailService,
		ntfy:               ntfyClient,
		webpush:            webpushClient,
		scheduler:          sched,
		data: &data{
			userNotifications: make(map[string][]arr.MediaItem),
		},
		imageCache: cache.NewImageCache("./data/cache/images", db),
		cache:      engineCache,
	}

	// Setup scheduled jobs
	if err := engine.setupJobs(); err != nil {
		return nil, fmt.Errorf("failed to setup jobs: %w", err)
	}

	return engine, nil
}

// runCleanupJob is the main cleanup job function.
func (e *Engine) runCleanupJob(ctx context.Context) (err error) {
	log.Info("Starting scheduled cleanup job")

	// Clear all caches to ensure fresh data
	e.cache.ClearAll(ctx)

	if e.initialDBMigration {
		// migrate old tag based items to database
		if err := e.migrateTagsToDatabase(ctx); err != nil {
			log.Error("An error occurred while migrating tags to database")
			return err
		}
	}

	e.removeProtectedExpiredItems(ctx)

	mediaItems, err := e.gatherMediaItems(ctx)
	if err != nil {
		log.Errorf("failed to gather media items: %v", err)
		return err
	}
	log.Info("Media items gathered successfully")

	if err := e.removeItemsNotFoundAnymore(ctx, mediaItems); err != nil {
		log.Error("An error occurred while removing items not found in Jellyfin")
	}

	if err = e.markForDeletion(ctx, mediaItems); err != nil {
		log.Error("An error occurred while marking media for deletion")
	}

	e.removeRecentlyPlayedItems(ctx)

	// only delete media if there was no previous error
	if err == nil {
		if err := e.cleanupMedia(ctx); err != nil {
			log.Error("An error occurred while deleting media")
			return err
		}
	}
	err = e.runEstimateDeletionsJob(ctx)
	if err != nil {
		log.Error("An error occurred while estimating deletions")
	}

	if err := e.createJellyfinLeavingCollections(ctx); err != nil {
		log.Error("An error occurred while creating Jellyfin leaving collections")
	}

	e.removeItemsFromLeavingCollections(ctx)

	log.Info("Scheduled cleanup job completed")
	return err
}

func (e *Engine) removeProtectedExpiredItems(ctx context.Context) {
	log.Info("Removing media items with expired protection from database")
	mediaItems, err := e.db.GetMediaExpiredProtection(ctx, time.Now())
	if err != nil {
		log.Error("Failed to get media items with expired protection from database", "error", err)
		return
	}
	if len(mediaItems) == 0 {
		log.Debug("No media items with expired protection found in database")
		return
	}
	for _, item := range mediaItems {
		item.DBDeleteReason = database.DBDeleteReasonProtectionExpired

		if err := e.db.DeleteMediaItem(ctx, &item); err != nil {
			log.Error("Failed to remove media item with expired protection from database", "title", item.Title, "jellyfinID", item.JellyfinID, "protectedUntil", item.ProtectedUntil, "error", err)
		}

		// Create history event for protection expiration before deletion
		if err := e.CreateProtectionExpiredEvent(ctx, &item); err != nil {
			log.Errorf("failed to create protection expired event for %s: %v", item.Title, err)
		}
	}
	log.Info("Media items with expired protection removal process completed")
}

func (e *Engine) removeRecentlyPlayedItems(ctx context.Context) {
	log.Info("Removing recently played items from database")

	mediaItems, err := e.db.GetMediaItems(ctx, true)
	if err != nil {
		log.Error("Failed to get media items from database", "error", err)
		return
	}

	if len(mediaItems) == 0 {
		log.Debug("No media items found in database to check for recent plays")
		return
	}

	for _, item := range mediaItems {
		lastPlayed, err := e.stats.GetItemLastPlayed(ctx, item.JellyfinID)
		if err != nil {
			log.Error("Failed to get last played time for item", "title", item.Title, "jellyfinID", item.JellyfinID, "error", err)
			continue
		}

		if lastPlayed.IsZero() {
			log.Debug("Item has never been played, skipping removal", "title", item.Title, "jellyfinID", item.JellyfinID)
			continue
		}

		libraryConfig := e.cfg.GetLibraryConfig(item.LibraryName)
		if libraryConfig == nil {
			log.Warn("Library config not found", "library", item.LibraryName)
			continue
		}

		timeSinceLastPlayed := time.Since(lastPlayed)
		thresholdDuration := time.Duration(libraryConfig.GetLastStreamThreshold()) * 24 * time.Hour
		if timeSinceLastPlayed > thresholdDuration {
			log.Debug("Item last played outside of threshold, skipping removal", "title", item.Title, "jellyfinID", item.JellyfinID, "lastPlayed", lastPlayed.Format(time.RFC3339))
			continue
		}
		item.DBDeleteReason = database.DBDeleteReasonStreamed
		// Create deletion event for streamed items
		if err := e.CreateStreamedEvent(ctx, &item); err != nil {
			log.Errorf("failed to create deletion event for %s: %v", item.Title, err)
		}

		if err := e.db.DeleteMediaItem(ctx, &item); err != nil {
			log.Error("Failed to remove recently played item from database", "title", item.Title, "jellyfinID", item.JellyfinID, "error", err)
			continue
		}
	}

	log.Info("Recently played items removal process completed")
}

func (e *Engine) removeItemsNotFoundAnymore(ctx context.Context, mediaItems []arr.MediaItem) error {
	log.Info("Removing items no longer present in Jellyfin from database")

	dbMediaItems, err := e.db.GetMediaItems(ctx, false)
	if err != nil {
		log.Error("Failed to get media items from database", "error", err)
		return err
	}

	jellyfinItemMap := make(map[string]struct{})
	for _, item := range mediaItems {
		jellyfinItemMap[item.JellyfinID] = struct{}{}
	}

	for _, dbItem := range dbMediaItems {
		if _, exists := jellyfinItemMap[dbItem.JellyfinID]; !exists {
			log.Info("Media item no longer present in Jellyfin, removing from database", "title", dbItem.Title, "jellyfinID", dbItem.JellyfinID)
			dbItem.DBDeleteReason = database.DBDeleteReasonMissingInJellyfin

			// Create deletion event for missing items
			if err := e.CreateNotFoundAnymoreEvent(ctx, &dbItem); err != nil {
				log.Errorf("failed to create not found anymore event for %s: %v", dbItem.Title, err)
			}

			if err := e.db.DeleteMediaItem(ctx, &dbItem); err != nil {
				log.Error("Failed to remove media item no longer present in Jellyfin from database", "title", dbItem.Title, "jellyfinID", dbItem.JellyfinID, "error", err)
				continue
			}
		}
	}

	log.Info("Removed items not found in Jellyfin from database successfully")
	return nil
}

func (e *Engine) markForDeletion(ctx context.Context, mediaItems []arr.MediaItem) error {
	mediaItems, err := e.filters.ApplyAll(ctx, mediaItems)
	if err != nil {
		return err
	}

	// Populate requester information from Jellyseerr
	log.Info("Populating requester information")
	mediaItems = e.populateRequesterInfo(ctx, mediaItems)

	// Populate user notifications for email sending
	if e.data.userNotifications == nil {
		e.data.userNotifications = make(map[string][]arr.MediaItem)
	}

	for _, item := range mediaItems {
		if item.RequestedBy != "" {
			e.data.userNotifications[item.RequestedBy] = append(e.data.userNotifications[item.RequestedBy], item)
		}
		log.Info("Marking media item for deletion", "name", item.Title, "library", item.LibraryName)
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
	e.sendEmailNotifications()

	// Send ntfy deletion summary notification
	if err := e.sendNtfyDeletionSummary(ctx, mediaItems); err != nil {
		log.Errorf("failed to send ntfy deletion summary: %v", err)
		// Don't return here, continue with the cleanup process
	}
	return nil
}

// gatherMediaItems gathers all media items from Jellyfin, Sonarr, and Radarr.
// It merges them into a single collection grouped by library.
func (e *Engine) gatherMediaItems(ctx context.Context) ([]arr.MediaItem, error) {
	jellyfinItems, libraryFoldersMap, err := e.jellyfin.GetJellyfinItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get jellyfin items: %w", err)
	}

	var sonarrItems []arr.MediaItem
	if e.sonarr != nil {
		sonarrItems, err = e.sonarr.GetItems(ctx, jellyfinItems)
		if err != nil {
			return nil, fmt.Errorf("failed to get sonarr items: %w", err)
		}
	}

	var radarrItems []arr.MediaItem
	if e.radarr != nil {
		radarrItems, err = e.radarr.GetItems(ctx, jellyfinItems)
		if err != nil {
			return nil, fmt.Errorf("failed to get radarr items: %w", err)
		}
	}

	// Merge all media items
	mediaItems := make([]arr.MediaItem, 0, len(sonarrItems)+len(radarrItems))
	mediaItems = append(mediaItems, sonarrItems...)
	mediaItems = append(mediaItems, radarrItems...)

	// Set deletion policies with freshly gathered library folders map
	e.policy.SetPolicies(
		policy.NewDefaultDelete(e.cfg),
		policy.NewDiskUsageDelete(e.cfg, libraryFoldersMap),
	)

	return mediaItems, nil
}

func arrMediaToDBMediaItem(item arr.MediaItem) database.Media {
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
		return database.Media{}
	}

	return dbItem
}

func (e *Engine) saveMediaItemsToDatabase(mediaItems []arr.MediaItem) error {
	dbMediaItems := make([]database.Media, 0)

	for _, item := range mediaItems {
		dbItem := arrMediaToDBMediaItem(item)
		if err := e.policy.ApplyAll(&dbItem); err != nil {
			log.Errorf("failed to apply policies to media item %s: %v", dbItem.Title, err)
			continue
		}
		dbMediaItems = append(dbMediaItems, dbItem)
	}

	if err := e.db.CreateMediaItems(context.Background(), dbMediaItems); err != nil {
		return fmt.Errorf("failed to create media items to database: %w", err)
	}

	// Create history events for newly picked up items
	for i := range dbMediaItems {
		if err := e.CreatePickedUpEvent(context.Background(), &dbMediaItems[i]); err != nil {
			log.Errorf("failed to create picked up event for %s: %v", dbMediaItems[i].Title, err)
		}
	}

	return nil
}

// resetAllTags removes all jellysweep tags from all media in Sonarr and Radarr.
// Legacy: also cleans up any remaining tags.
func (e *Engine) resetAllTags(ctx context.Context, additionalTags []string) error {
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

// migrateTagsToDatabase migrates existing jellysweep items to the database based on their tags in Sonarr and Radarr.
func (e *Engine) migrateTagsToDatabase(ctx context.Context) error {
	log.Info("Starting migration of jellysweep tags to database...")

	jellyfinItems, _, err := e.jellyfin.GetJellyfinItems(ctx)
	if err != nil {
		log.Error("Failed to get jellyfin items for migration", "error", err)
		return err
	}

	legacyitems := make([]arr.MediaItem, 0)
	if e.sonarr != nil {
		sonarrItems, err := e.sonarr.GetItems(ctx, jellyfinItems)
		if err != nil {
			log.Error("Failed to get sonarr items for migration", "error", err)
			return err
		}
		legacyitems = append(legacyitems, sonarrItems...)
	}
	if e.radarr != nil {
		radarrItems, err := e.radarr.GetItems(ctx, jellyfinItems)
		if err != nil {
			log.Error("Failed to get radarr items for migration", "error", err)
			return err
		}
		legacyitems = append(legacyitems, radarrItems...)
	}

	dbItems := make([]database.Media, 0)
	for _, item := range legacyitems {
		mustMigrate := false
		dbItem := arrMediaToDBMediaItem(item)
		for _, tagName := range item.Tags {
			tag, err := tags.ParseJellysweepTag(tagName)
			if err != nil {
				continue
			}

			if !tag.ProtectedUntil.IsZero() {
				dbItem.ProtectedUntil = &tag.ProtectedUntil
				mustMigrate = true
			}

			if tag.MustDelete {
				dbItem.Unkeepable = true
				mustMigrate = true
			}

			if tag.DiskUsage > 0 && !tag.DeletionDate.IsZero() {
				dbItem.DiskUsageDeletePolicies = append(dbItem.DiskUsageDeletePolicies, database.DiskUsageDeletePolicy{
					Threshold:  tag.DiskUsage,
					DeleteDate: tag.DeletionDate,
				})
				mustMigrate = true
			} else if !tag.DeletionDate.IsZero() {
				dbItem.DefaultDeleteAt = tag.DeletionDate
				mustMigrate = true
			}
		}

		if mustMigrate {
			dbItems = append(dbItems, dbItem)
			log.Info("Migrating item to database", "title", dbItem.Title, "library", dbItem.LibraryName)
		}
	}

	if len(dbItems) == 0 {
		log.Debug("No items found for migration")
		return nil
	}

	if err := e.db.CreateMediaItems(ctx, dbItems); err != nil {
		log.Error("Failed to migrate items to database", "error", err)
		return err
	}

	if err := e.resetAllTags(ctx, nil); err != nil {
		log.Error("Failed to reset tags after migration", "error", err)
		return err
	}

	log.Info("Migration of tags to database completed successfully")

	return nil
}
