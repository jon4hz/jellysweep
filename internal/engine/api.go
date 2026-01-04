package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/cache"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/notify/webpush"
)

// GetImageCache returns the image cache instance for API access.
func (e *Engine) GetImageCache() *cache.ImageCache {
	return e.imageCache
}

// GetEngineCache returns the engine cache instance.
func (e *Engine) GetEngineCache() *cache.EngineCache {
	return e.cache
}

// RequestKeepMedia creates a new keep request for the specified media item in the database and sends a notification to admins.
// If the user has auto-approval permission, the request is automatically approved.
// Returns true if the request was auto-approved, false otherwise.
func (e *Engine) RequestKeepMedia(ctx context.Context, mediaID uint, userID uint, username string) (bool, error) {
	// Fetch user from database to get current permissions
	user, err := e.db.GetUserByID(ctx, userID)
	if err != nil {
		log.Errorf("failed to get user by ID: %v", err)
		return false, err
	}

	hasAutoApproval := user.UserPermissions.HasAutoApproval

	// Parse media ID to determine if it's a Sonarr or Radarr item
	log.Debug("Requesting to keep media", "mediaID", mediaID, "userID", userID, "hasAutoApproval", hasAutoApproval)

	media, err := e.db.GetMediaItemByID(ctx, mediaID)
	if err != nil {
		log.Errorf("failed to get media item by ID: %v", err)
		return false, err
	}

	if media.Unkeepable {
		log.Warn("Media is marked as unkeepable", "mediaID", mediaID, "type", media.MediaType, "title", media.Title)
		return false, ErrUnkeepableMedia
	}

	if media.Request.ID != 0 {
		log.Warn("Media already requested", "mediaID", mediaID, "type", media.MediaType, "title", media.Title)
		return false, ErrRequestAlreadyProcessed
	}

	_, err = e.db.CreateRequest(ctx, media.ID, userID)
	if err != nil {
		log.Errorf("failed to create keep request in database: %v", err)
		return false, err
	}

	// Create history event for request creation
	if err := e.CreateRequestCreatedEvent(ctx, media, userID); err != nil {
		log.Errorf("failed to create request created event for %s: %v", media.Title, err)
	}

	// If user has auto-approval permission, automatically approve the request
	if hasAutoApproval {
		log.Info("Auto-approving keep request for user with auto-approval permission", "username", username, "mediaID", mediaID, "title", media.Title)
		if err := e.HandleKeepRequest(ctx, userID, mediaID, true); err != nil {
			log.Errorf("failed to auto-approve request: %v", err)
			return false, err
		}

		return true, nil
	}

	// Send ntfy notification to admins if the request needs manual approval
	if e.ntfy != nil {
		if ntfyErr := e.ntfy.SendKeepRequest(ctx, media.Title, string(media.MediaType), username); ntfyErr != nil {
			log.Errorf("Failed to send ntfy keep request notification: %v", ntfyErr)
		}
	}

	return false, nil
}

// HandleKeepRequest accepts or declines a keep request for the specified media item.
func (e *Engine) HandleKeepRequest(ctx context.Context, userID, mediaID uint, accept bool) error {
	media, err := e.db.GetMediaItemByID(ctx, mediaID)
	if err != nil {
		log.Errorf("failed to get media item by ID: %v", err)
		return err
	}

	if media.Request.ID == 0 {
		log.Warn("Media has no pending keep request", "mediaID", mediaID, "type", media.MediaType, "title", media.Title)
		return nil
	}

	newStatus := database.RequestStatusDenied
	if accept {
		newStatus = database.RequestStatusApproved
	}

	err = e.db.UpdateRequestStatus(ctx, media.Request.ID, newStatus)
	if err != nil {
		log.Errorf("failed to update request status in database: %v", err)
		return err
	}

	if accept {
		libraryConfig := e.cfg.GetLibraryConfig(media.LibraryName)
		if libraryConfig == nil {
			log.Errorf("library config not found for library: %s", media.LibraryName)
			return fmt.Errorf("library config not found for library: %s", media.LibraryName)
		}

		protectedUntil := time.Now().Add(time.Hour * 24 * time.Duration(libraryConfig.GetProtectionPeriod()))
		err = e.db.SetMediaProtectedUntil(ctx, media.ID, &protectedUntil)
		if err != nil {
			log.Errorf("failed to set media protected until in database: %v", err)
			return err
		}

		// Create history event for request approval and protection
		if err := e.CreateRequestApprovedEvent(ctx, userID, media); err != nil {
			log.Errorf("failed to create request approved event for %s: %v", media.Title, err)
		}

		if err := e.CreateProtectedEvent(ctx, media); err != nil {
			log.Errorf("failed to create protected event for %s: %v", media.Title, err)
		}
	} else {
		err = e.db.MarkMediaAsUnkeepable(ctx, media.ID)
		if err != nil {
			log.Errorf("failed to mark media as unkeepable in database: %v", err)
			return err
		}

		// Create history event for request denial
		if err := e.CreateRequestDeniedEvent(ctx, userID, media); err != nil {
			log.Errorf("failed to create request denied event for %s: %v", media.Title, err)
		}
	}

	// get user who made the request
	user, err := e.db.GetUserByID(ctx, media.Request.UserID)
	if err != nil {
		log.Errorf("failed to get user by ID: %v", err)
		return err
	}

	if e.webpush != nil && user.Username != "" {
		if pushErr := e.webpush.SendKeepRequestNotification(ctx, user.Username, media.Title, string(media.MediaType), accept); pushErr != nil {
			log.Errorf("Failed to send webpush notification: %v", pushErr)
		}
	}

	return nil
}

// GetWebPushClient returns the webpush client.
func (e *Engine) GetWebPushClient() *webpush.Client {
	return e.webpush
}

// addIgnoreTag adds a jellysweep-ignore tag to the specified media item.
func (e *Engine) addIgnoreTag(ctx context.Context, media *database.Media) error {
	switch media.MediaType {
	case database.MediaTypeMovie:
		if e.radarr == nil {
			log.Warn("Radarr client not available, cannot add ignore tag", "mediaID", media.ID, "title", media.Title)
			return fmt.Errorf("radarr client not available")
		}
		if err := e.radarr.ResetAllTagsAndAddIgnore(ctx, media.ArrID); err != nil {
			log.Error("Failed to add ignore tag in radarr", "mediaID", media.ID, "title", media.Title, "error", err)
			return err
		}
	case database.MediaTypeTV:
		if e.sonarr == nil {
			log.Warn("Sonarr client not available, cannot add ignore tag", "mediaID", media.ID, "title", media.Title)
			return fmt.Errorf("sonarr client not available")
		}
		if err := e.sonarr.ResetAllTagsAndAddIgnore(ctx, media.ArrID); err != nil {
			log.Error("Failed to add ignore tag in sonarr", "mediaID", media.ID, "title", media.Title, "error", err)
			return err
		}
	default:
		return fmt.Errorf("unsupported media type: %s", media.MediaType)
	}

	return nil
}

// GetMediaItems retrieves all media items from the database.
func (e *Engine) GetMediaItems(ctx context.Context, includeProtected bool) ([]database.Media, error) {
	return e.db.GetMediaItems(ctx, includeProtected)
}

// GetMediaWithPendingRequest retrieves all media items with pending keep requests.
func (e *Engine) GetMediaWithPendingRequest(ctx context.Context) ([]database.Media, error) {
	return e.db.GetMediaWithPendingRequest(ctx)
}

// GetMediaItemsByMediaType retrieves all media items of a specific type.
func (e *Engine) GetMediaItemsByMediaType(ctx context.Context, mediaType database.MediaType) ([]database.Media, error) {
	return e.db.GetMediaItemsByMediaType(ctx, mediaType)
}

// MarkMediaAsProtected marks a media item as protected for the configured duration.
func (e *Engine) MarkMediaAsProtected(ctx context.Context, mediaID uint, adminID uint) error {
	media, err := e.db.GetMediaItemByID(ctx, mediaID)
	if err != nil {
		log.Error("Failed to get media item by ID", "mediaID", mediaID, "error", err)
		return fmt.Errorf("database error: %w", err)
	}

	libraryConfig := e.cfg.GetLibraryConfig(media.LibraryName)
	if libraryConfig == nil {
		log.Error("No library configuration found", "library", media.LibraryName)
		return fmt.Errorf("no library configuration found")
	}

	protectedUntil := time.Now().Add(time.Hour * 24 * time.Duration(libraryConfig.GetProtectionPeriod()))
	if err := e.db.SetMediaProtectedUntil(ctx, media.ID, &protectedUntil); err != nil {
		log.Error("Failed to set media protected until", "mediaID", mediaID, "error", err)
		return fmt.Errorf("failed to set media protected until: %w", err)
	}

	if err := e.CreateAdminKeepEvent(ctx, adminID, media); err != nil {
		log.Error("Failed to create admin keep event", "mediaID", mediaID, "error", err)
		return fmt.Errorf("failed to create admin keep event: %w", err)
	}

	return nil
}

// MarkMediaAsUnkeepable marks a media item as unkeepable and denies all keep requests.
func (e *Engine) MarkMediaAsUnkeepable(ctx context.Context, mediaID uint, adminID uint) error {
	media, err := e.db.GetMediaItemByID(ctx, mediaID)
	if err != nil {
		log.Error("Failed to get media item by ID", "mediaID", mediaID, "error", err)
		return fmt.Errorf("database error: %w", err)
	}

	if err := e.db.MarkMediaAsUnkeepable(ctx, media.ID); err != nil {
		log.Error("Failed to mark media as unkeepable", "mediaID", mediaID, "error", err)
		return fmt.Errorf("failed to mark media as unkeepable: %w", err)
	}

	if err := e.CreateAdminUnkeepEvent(ctx, adminID, media); err != nil {
		log.Error("Failed to create admin unkeep event", "mediaID", mediaID, "error", err)
		return fmt.Errorf("failed to create admin unkeep event: %w", err)
	}

	return nil
}

// MarkMediaAsKeepForever removes the media item from the database and adds an ignore tag.
func (e *Engine) MarkMediaAsKeepForever(ctx context.Context, mediaID uint, adminID uint) error {
	media, err := e.db.GetMediaItemByID(ctx, mediaID)
	if err != nil {
		log.Error("Failed to get media item by ID", "mediaID", mediaID, "error", err)
		return fmt.Errorf("database error: %w", err)
	}

	if err := e.addIgnoreTag(ctx, media); err != nil {
		log.Error("Failed to add ignore tag", "mediaID", mediaID, "error", err)
		return fmt.Errorf("engine error: %w", err)
	}

	media.DBDeleteReason = database.DBDeleteReasonKeepForever
	if err := e.db.DeleteMediaItem(ctx, media); err != nil {
		log.Error("Failed to delete media item", "mediaID", mediaID, "error", err)
		return fmt.Errorf("database error: %w", err)
	}

	if err := e.CreateKeepForeverEvent(ctx, adminID, media); err != nil {
		log.Error("Failed to create keep forever event", "mediaID", mediaID, "error", err)
		return fmt.Errorf("database error: %w", err)
	}

	return nil
}

// GetHistoryEvents retrieves paginated history events.
// If eventTypes is provided and not empty, only events of those types will be returned.
func (e *Engine) GetHistoryEvents(ctx context.Context, page, pageSize int, sortBy string, sortOrder database.SortOrder, eventTypes []database.HistoryEventType) ([]database.HistoryEvent, int64, error) {
	return e.db.GetHistoryEvents(ctx, page, pageSize, sortBy, sortOrder, eventTypes)
}

// GetHistoryEventsByJellyfinID retrieves all history events for a specific Jellyfin ID.
func (e *Engine) GetHistoryEventsByJellyfinID(ctx context.Context, jellyfinID string) ([]database.HistoryEvent, error) {
	return e.db.GetHistoryEventsByJellyfinID(ctx, jellyfinID)
}

// GetAllUsers retrieves all users from the database.
func (e *Engine) GetAllUsers(ctx context.Context) ([]database.User, error) {
	return e.db.GetAllUsers(ctx)
}

// UpdateUserAutoApproval updates a user's auto-approval permission.
func (e *Engine) UpdateUserAutoApproval(ctx context.Context, userID uint, hasAutoApproval bool) error {
	return e.db.UpdateUserAutoApproval(ctx, userID, hasAutoApproval)
}
