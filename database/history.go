package database

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
	"gorm.io/gorm"
)

// HistoryEventType represents the type of history event.
type HistoryEventType string

const (
	// HistoryEventPickedUp indicates a media item was picked up by jellysweep.
	HistoryEventPickedUp HistoryEventType = "picked_up"
	// HistoryEventProtected indicates a media item was marked for protection.
	HistoryEventProtected HistoryEventType = "protected"
	// HistoryEventUnprotected indicates a media item's protection was removed.
	HistoryEventUnprotected HistoryEventType = "unprotected"
	// HistoryEventProtectionExpired indicates a media item's protection period expired.
	HistoryEventProtectionExpired HistoryEventType = "protection_expired"
	// HistoryEventStreamed indicates a media item was streamed.
	HistoryEventStreamed HistoryEventType = "streamed"
	// HistoryEventKeepForever indicates a media item was set to keep forever.
	HistoryEventKeepForever HistoryEventType = "keep_forever"
	// HistoryEventAdminKeep indicates a media item was kept by an admin.
	HistoryEventAdminKeep HistoryEventType = "admin_keep"
	// HistoryEventAdminUnkeep indicates a media item was marked as unkeepable by an admin.
	HistoryEventAdminUnkeep HistoryEventType = "admin_unkeep"
	// HistoryEventDeleted indicates a media item was deleted.
	HistoryEventDeleted HistoryEventType = "deleted"
	// HistoryEventRequestCreated indicates a keep request was created.
	HistoryEventRequestCreated HistoryEventType = "request_created"
	// HistoryEventRequestApproved indicates a keep request was approved.
	HistoryEventRequestApproved HistoryEventType = "request_approved"
	// HistoryEventRequestDenied indicates a keep request was denied.
	HistoryEventRequestDenied HistoryEventType = "request_denied"
)

// HistoryEvent represents a historical event for a media item.
type HistoryEvent struct {
	gorm.Model
	// Media item identifier (references Media.ID, even if soft-deleted)
	MediaID uint `gorm:"not null;index"`
	// Media item (will be loaded with Unscoped to include soft-deleted items)
	Media Media `gorm:"constraint:OnDelete:CASCADE;"`
	// Event type
	EventType HistoryEventType `gorm:"not null;index"`
	// User who triggered the event (optional, can be null for system events)
	UserID *uint `gorm:"index"`
	User   *User
	// Timestamp when the event occurred
	EventTime time.Time `gorm:"not null;index"`
}

// HistoryDB defines the interface for history-related database operations.
type HistoryDB interface {
	CreateHistoryEvent(ctx context.Context, event HistoryEvent) error
	GetHistoryEvents(ctx context.Context, page, pageSize int, sortBy string, sortOrder SortOrder) ([]HistoryEvent, int64, error)
	GetHistoryEventsByMediaID(ctx context.Context, mediaID uint) ([]HistoryEvent, error)
	GetHistoryEventsByJellyfinID(ctx context.Context, jellyfinID string) ([]HistoryEvent, error)
	GetHistoryEventsByEventType(ctx context.Context, eventType HistoryEventType, page, pageSize int) ([]HistoryEvent, int64, error)
}

// CreateHistoryEvent creates a new history event.
func (c *Client) CreateHistoryEvent(ctx context.Context, event HistoryEvent) error {
	// Set EventTime to now if not already set
	if event.EventTime.IsZero() {
		event.EventTime = time.Now()
	}

	result := c.db.WithContext(ctx).Create(&event)
	if result.Error != nil {
		log.Error("failed to create history event", "error", result.Error)
		return result.Error
	}
	return nil
}

// GetHistoryEvents retrieves paginated history events.
func (c *Client) GetHistoryEvents(ctx context.Context, page, pageSize int, sortBy string, sortOrder SortOrder) ([]HistoryEvent, int64, error) {
	var events []HistoryEvent
	var total int64

	// Count total events
	if err := c.db.WithContext(ctx).
		Model(&HistoryEvent{}).
		Count(&total).Error; err != nil {
		log.Error("failed to count history events", "error", err)
		return nil, 0, err
	}

	// Validate and set sort field
	validSortFields := map[string]string{
		"title":      "media.title",
		"year":       "media.year",
		"media_type": "media.media_type",
		"library":    "media.library_name",
		"event_type": "event_type",
		"username":   "username",
		"event_time": "event_time",
	}

	// TODO: fix sorting by username

	sortField, ok := validSortFields[sortBy]
	if !ok || sortField == "" {
		sortField = "event_time"
	}

	// Validate sort order
	if sortOrder != SortOrderAsc && sortOrder != SortOrderDesc {
		sortOrder = SortOrderDesc
	}

	// Get paginated events
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	orderClause := sortField + " " + string(sortOrder)
	result := c.db.WithContext(ctx).
		Preload("Media", func(db *gorm.DB) *gorm.DB {
			return db.Unscoped() // Include soft-deleted media items
		}).
		Preload("User"). // Include user information
		Joins("LEFT JOIN media ON media.id = history_events.media_id").
		Order(orderClause).
		Limit(pageSize).
		Offset(offset).
		Find(&events)

	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get history events", "error", result.Error)
		return nil, 0, result.Error
	}

	return events, total, nil
}

// GetHistoryEventsByMediaID retrieves all history events for a specific media item.
func (c *Client) GetHistoryEventsByMediaID(ctx context.Context, mediaID uint) ([]HistoryEvent, error) {
	var events []HistoryEvent
	result := c.db.WithContext(ctx).
		Preload("Media", func(db *gorm.DB) *gorm.DB {
			return db.Unscoped() // Include soft-deleted media items
		}).
		Preload("User"). // Include user information
		Where("media_id = ?", mediaID).
		Order("event_time DESC").
		Find(&events)

	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get history events by media ID", "error", result.Error)
		return nil, result.Error
	}

	return events, nil
}

// GetHistoryEventsByJellyfinID retrieves all history events for a specific Jellyfin ID.
// This is useful for getting the full history even after media has been deleted.
func (c *Client) GetHistoryEventsByJellyfinID(ctx context.Context, jellyfinID string) ([]HistoryEvent, error) {
	var events []HistoryEvent

	// First get the media ID(s) for this Jellyfin ID (including soft-deleted)
	var mediaIDs []uint
	if err := c.db.WithContext(ctx).
		Unscoped().
		Model(&Media{}).
		Where("jellyfin_id = ?", jellyfinID).
		Pluck("id", &mediaIDs).Error; err != nil {
		log.Error("failed to get media IDs by Jellyfin ID", "error", err)
		return nil, err
	}

	if len(mediaIDs) == 0 {
		return []HistoryEvent{}, nil
	}

	// Then get all history events for those media IDs
	result := c.db.WithContext(ctx).
		Preload("Media", func(db *gorm.DB) *gorm.DB {
			return db.Unscoped() // Include soft-deleted media items
		}).
		Preload("User"). // Include user information
		Where("media_id IN ?", mediaIDs).
		Order("event_time DESC").
		Find(&events)

	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get history events by media IDs", "error", result.Error)
		return nil, result.Error
	}

	return events, nil
}

// GetHistoryEventsByEventType retrieves paginated history events filtered by event type.
func (c *Client) GetHistoryEventsByEventType(ctx context.Context, eventType HistoryEventType, page, pageSize int) ([]HistoryEvent, int64, error) {
	var events []HistoryEvent
	var total int64

	// Count total events of this type
	if err := c.db.WithContext(ctx).
		Model(&HistoryEvent{}).
		Where("event_type = ?", eventType).
		Count(&total).Error; err != nil {
		log.Error("failed to count history events by type", "error", err)
		return nil, 0, err
	}

	// Get paginated events
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	result := c.db.WithContext(ctx).
		Preload("Media", func(db *gorm.DB) *gorm.DB {
			return db.Unscoped() // Include soft-deleted media items
		}).
		Preload("User"). // Include user information
		Where("event_type = ?", eventType).
		Order("event_time DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&events)

	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get history events by type", "error", result.Error)
		return nil, 0, result.Error
	}

	return events, total, nil
}
