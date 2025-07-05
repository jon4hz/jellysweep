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
	"github.com/jon4hz/jellysweep/notify/webpush"
	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"
)

const (
	jellysweepTagPrefix         = "jellysweep-delete-"
	jellysweepKeepRequestPrefix = "jellysweep-keep-request-"
	jellysweepKeepPrefix        = "jellysweep-must-keep-"
	jellysweepDeleteForSureTag  = "jellysweep-must-delete-for-sure"
	jellysweepKeepTag           = "jellysweep-keep"
	jellysweepMustDeleteTag     = "must-delete"
	jellysweepIgnoreTag         = "jellysweep-ignore"
)

// Cleanup mode constants.
const (
	CleanupModeAll          = "all"
	CleanupModeKeepEpisodes = "keep_episodes"
	CleanupModeKeepSeasons  = "keep_seasons"
)

// Exported constants for API handlers.
const (
	TagKeep       = jellysweepKeepTag
	TagMustDelete = jellysweepMustDeleteTag
	TagIgnore     = jellysweepIgnoreTag
)

// Engine is the main engine for JellySweep, managing interactions with sonarr, radarr, and other services.
// It runs a cleanup job periodically to remove unwanted media.
type Engine struct {
	cfg        *config.Config
	jellystat  *jellystat.Client
	jellyseerr *jellyseerr.Client
	sonarr     *sonarr.APIClient
	radarr     *radarr.APIClient
	email      *email.NotificationService
	ntfy       *ntfy.Client
	webpush    *webpush.Client

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

	return &Engine{
		cfg:        cfg,
		jellystat:  jellystatClient,
		jellyseerr: jellyseerrClient,
		sonarr:     sonarrClient,
		radarr:     radarrClient,
		email:      emailService,
		ntfy:       ntfyClient,
		webpush:    webpushClient,
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
	ticker := time.NewTicker(time.Duration(e.cfg.CleanupInterval) * time.Hour)
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

// removeRecentlyPlayedDeleteTags removes jellysweep-delete tags from media that has been played recently.
func (e *Engine) removeRecentlyPlayedDeleteTags(ctx context.Context) {
	log.Info("Checking for recently played media with pending delete tags")

	if e.sonarr != nil {
		e.removeRecentlyPlayedSonarrDeleteTags(ctx)
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

	// Filter out series that already meet the keep criteria
	e.filterSeriesAlreadyMeetingKeepCriteria()

	// Populate requester information from Jellyseerr
	log.Info("Populating requester information")

	e.filterMediaTags()

	log.Info("Checking for streaming history")
	if err := e.filterLastStreamThreshold(ctx); err != nil {
		log.Errorf("failed to filter last stream threshold: %v", err)
		return
	}

	log.Info("Checking content age")
	if err := e.filterContentAgeThreshold(ctx); err != nil {
		log.Errorf("failed to filter content age threshold: %v", err)
		return
	}

	// Populate requester information from Jellyseerr
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
		return
	}
	if err := e.markRadarrMediaItemsForDeletion(ctx, e.cfg.DryRun); err != nil {
		log.Errorf("failed to mark radarr media items for deletion: %v", err)
		return
	}

	// Send email notifications before marking for deletion
	e.sendEmailNotifications()

	// Send ntfy deletion summary notification
	if err := e.sendNtfyDeletionSummary(ctx); err != nil {
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
	Year           int32
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
		libraryName := strings.ToLower(e.data.libraryIDMap[item.ParentID])

		// Handle TV Series (Sonarr)
		if item.Type == jellystat.ItemTypeSeries {
			lo.ForEach(e.data.sonarrItems, func(s sonarr.SeriesResource, _ int) {
				if s.GetTitle() == item.Name && s.GetYear() == item.ProductionYear && !item.Archived {
					if s.GetTmdbId() == 0 {
						log.Warnf("Sonarr series %s has no TMDB ID, skipping", s.GetTitle())
						return
					}

					mediaItems[libraryName] = append(mediaItems[libraryName], MediaItem{
						JellystatID:    item.ID,
						SeriesResource: s,
						TmdbId:         s.GetTmdbId(),
						Year:           s.GetYear(),
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
				if m.GetTitle() == item.Name && m.GetYear() == item.ProductionYear && !item.Archived {
					if m.GetTmdbId() == 0 {
						log.Warnf("Radarr movie %s has no TMDB ID, skipping", m.GetTitle())
						return
					}

					mediaItems[libraryName] = append(mediaItems[libraryName], MediaItem{
						JellystatID:   item.ID,
						MovieResource: m,
						TmdbId:        m.GetTmdbId(),
						Title:         item.Name,
						Year:          m.GetYear(),
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

// RequestKeepMedia adds a keep request tag to the specified media item.
func (e *Engine) RequestKeepMedia(ctx context.Context, mediaID, username string) error {
	// Parse media ID to determine if it's a Sonarr or Radarr item
	log.Debug("Requesting keep media", "mediaID", mediaID, "username", username)

	var mediaTitle string
	var mediaType string
	var err error

	if seriesIDStr, ok := strings.CutPrefix(mediaID, "sonarr-"); ok {
		seriesID, parseErr := strconv.ParseInt(seriesIDStr, 10, 32)
		if parseErr != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", parseErr)
		}

		// Get series title before adding tag
		if e.sonarr != nil {
			series, resp, getErr := e.sonarr.SeriesAPI.GetSeriesById(sonarrAuthCtx(ctx, e.cfg.Sonarr), int32(seriesID)).Execute()
			if getErr == nil {
				mediaTitle = series.GetTitle()
			}
			defer resp.Body.Close() //nolint: errcheck
		}
		mediaType = "TV Show"
		err = e.addSonarrKeepRequestTag(ctx, int32(seriesID), username)
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
		seriesID, err := strconv.ParseInt(seriesIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", err)
		}

		// Use dedicated tag-resetting functions based on the action
		switch tagName {
		case jellysweepKeepTag:
			// For "keep": remove all tags (including delete) before adding must-keep
			if err := e.resetSingleSonarrTagsForKeep(ctx, int32(seriesID)); err != nil {
				return fmt.Errorf("failed to reset sonarr tags for keep: %w", err)
			}
			// This will add a jellysweep-must-keep tag with expiry date
			return e.addSonarrKeepTag(ctx, int32(seriesID))
		case jellysweepMustDeleteTag:
			// For "must-delete": remove all tags except jellysweep-delete before adding must-delete-for-sure
			if err := e.resetSingleSonarrTagsForMustDelete(ctx, int32(seriesID)); err != nil {
				return fmt.Errorf("failed to reset sonarr tags for must-delete: %w", err)
			}
			// This will add a jellysweep-must-delete-for-sure tag
			return e.addSonarrDeleteForSureTag(ctx, int32(seriesID))
		case jellysweepIgnoreTag:
			// For "ignore": remove all jellysweep tags and add ignore tag in one operation
			return e.resetAllSonarrTagsAndAddIgnore(ctx, int32(seriesID))
		default:
			return fmt.Errorf("unsupported tag name: %s", tagName)
		}
	} else if movieIDStr, ok := strings.CutPrefix(mediaID, "radarr-"); ok {
		movieID, err := strconv.ParseInt(movieIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", err)
		}

		// Use dedicated tag-resetting functions based on the action
		switch tagName {
		case jellysweepKeepTag:
			// For "keep": remove all tags (including delete) before adding must-keep
			if err := e.resetSingleRadarrTagsForKeep(ctx, int32(movieID)); err != nil {
				return fmt.Errorf("failed to reset radarr tags for keep: %w", err)
			}
			// This will add a jellysweep-must-keep tag with expiry date
			return e.addRadarrKeepTag(ctx, int32(movieID))
		case jellysweepMustDeleteTag:
			// For "must-delete": remove all tags except jellysweep-delete before adding must-delete-for-sure
			if err := e.resetSingleRadarrTagsForMustDelete(ctx, int32(movieID)); err != nil {
				return fmt.Errorf("failed to reset radarr tags for must-delete: %w", err)
			}
			// This will add a jellysweep-must-delete-for-sure tag
			return e.addRadarrDeleteForSureTag(ctx, int32(movieID))
		case jellysweepIgnoreTag:
			// For "ignore": remove all jellysweep tags and add ignore tag in one operation
			return e.resetAllRadarrTagsAndAddIgnore(ctx, int32(movieID))
		default:
			return fmt.Errorf("unsupported tag name: %s", tagName)
		}
	}

	return fmt.Errorf("unsupported media ID format: %s", mediaID)
}

// RemoveConflictingTags removes jellysweep-keep-request and jellysweep-must-keep tags from a media item.
func (e *Engine) RemoveConflictingTags(ctx context.Context, mediaID string) error {
	// Parse media ID to determine if it's a Sonarr or Radarr item
	if strings.HasPrefix(mediaID, "sonarr-") {
		seriesIDStr := strings.TrimPrefix(mediaID, "sonarr-")
		seriesID, err := strconv.ParseInt(seriesIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid sonarr series ID: %w", err)
		}
		return e.removeSonarrKeepRequestAndDeleteTags(ctx, int32(seriesID))
	} else if strings.HasPrefix(mediaID, "radarr-") {
		movieIDStr := strings.TrimPrefix(mediaID, "radarr-")
		movieID, err := strconv.ParseInt(movieIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid radarr movie ID: %w", err)
		}
		return e.removeRadarrKeepRequestAndDeleteTags(ctx, int32(movieID))
	}
	return fmt.Errorf("unsupported media ID format: %s", mediaID)
}
