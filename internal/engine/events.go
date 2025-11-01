package engine

import (
	"context"

	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/samber/lo"
)

// CreatePickedUpEvent creates a history event when a media item is picked up by jellysweep.
func (e *Engine) CreatePickedUpEvent(ctx context.Context, media *database.Media) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventPickedUp,
	}
	return e.db.CreateHistoryEvent(ctx, event)
}

// CreateProtectedEvent creates a history event when a media item is protected.
func (e *Engine) CreateProtectedEvent(ctx context.Context, media *database.Media) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventProtected,
	}

	return e.db.CreateHistoryEvent(ctx, event)
}

// CreateUnprotectedEvent creates a history event when a media item's protection is removed.
func (e *Engine) CreateUnprotectedEvent(ctx context.Context, media *database.Media) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventUnprotected,
	}

	return e.db.CreateHistoryEvent(ctx, event)
}

// CreateProtectionExpiredEvent creates a history event when a media item's protection expires.
func (e *Engine) CreateProtectionExpiredEvent(ctx context.Context, media *database.Media) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventProtectionExpired,
	}

	return e.db.CreateHistoryEvent(ctx, event)
}

// CreateDeletedEvent creates a history event when a media item is deleted.
func (e *Engine) CreateDeletedEvent(ctx context.Context, media *database.Media) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventDeleted,
	}

	return e.db.CreateHistoryEvent(ctx, event)
}

// CreateStreamedEvent creates a history event when a media item is streamed.
func (e *Engine) CreateStreamedEvent(ctx context.Context, media *database.Media) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventStreamed,
	}

	return e.db.CreateHistoryEvent(ctx, event)
}

// CreateRequestCreatedEvent creates a history event when a keep request is created.
func (e *Engine) CreateRequestCreatedEvent(ctx context.Context, media *database.Media, requesterID uint) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventRequestCreated,
		UserID:    lo.ToPtr(requesterID),
	}

	return e.db.CreateHistoryEvent(ctx, event)
}

// CreateRequestApprovedEvent creates a history event when a keep request is approved.
func (e *Engine) CreateRequestApprovedEvent(ctx context.Context, approverID uint, media *database.Media) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventRequestApproved,
		UserID:    lo.ToPtr(approverID),
	}

	return e.db.CreateHistoryEvent(ctx, event)
}

// CreateRequestDeniedEvent creates a history event when a keep request is denied.
func (e *Engine) CreateRequestDeniedEvent(ctx context.Context, deniedByID uint, media *database.Media) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventRequestDenied,
		UserID:    lo.ToPtr(deniedByID),
	}

	return e.db.CreateHistoryEvent(ctx, event)
}

// CreateKeepForeverEvent creates a history event when a media item is set to keep forever.
func (e *Engine) CreateKeepForeverEvent(ctx context.Context, userID uint, media *database.Media) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventKeepForever,
		UserID:    lo.ToPtr(userID),
	}

	return e.db.CreateHistoryEvent(ctx, event)
}

// CreateAdminKeepEvent creates a history event when an admin keeps a media item.
func (e *Engine) CreateAdminKeepEvent(ctx context.Context, adminID uint, media *database.Media) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventAdminKeep,
		UserID:    lo.ToPtr(adminID),
	}

	return e.db.CreateHistoryEvent(ctx, event)
}

// CreateAdminUnkeepEvent creates a history event when an admin marks a media item as unkeepable.
func (e *Engine) CreateAdminUnkeepEvent(ctx context.Context, adminID uint, media *database.Media) error {
	event := database.HistoryEvent{
		MediaID:   media.ID,
		EventType: database.HistoryEventAdminUnkeep,
		UserID:    lo.ToPtr(adminID),
	}

	return e.db.CreateHistoryEvent(ctx, event)
}
