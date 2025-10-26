package models

import (
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/database"
)

// ToUserMediaItem converts a database.Media to UserMediaItem for regular users.
// This excludes sensitive fields like RequestedBy.
func ToUserMediaItem(m database.Media, cfg *config.Config) UserMediaItem {
	item := UserMediaItem{
		ID:              m.ID,
		Title:           m.Title,
		Year:            m.Year,
		PosterURL:       m.PosterURL,
		MediaType:       MediaType(m.MediaType),
		LibraryName:     m.LibraryName,
		FileSize:        m.FileSize,
		DefaultDeleteAt: m.DefaultDeleteAt,
		Unkeepable:      m.Unkeepable,
	}

	// Add cleanup mode and keep count for TV series
	if m.MediaType == database.MediaTypeTV && cfg != nil {
		item.CleanupMode = string(cfg.GetCleanupMode())
		item.KeepCount = cfg.GetKeepCount()
	}

	// Include request info without revealing who requested
	if m.Request.ID != 0 {
		item.Request = &UserRequestInfo{
			ID:     m.Request.ID,
			Status: string(m.Request.Status),
		}
	}

	return item
}

// ToUserMediaItems converts a slice of database.Media to UserMediaItems.
func ToUserMediaItems(items []database.Media, cfg *config.Config) []UserMediaItem {
	result := make([]UserMediaItem, len(items))
	for i, item := range items {
		result[i] = ToUserMediaItem(item, cfg)
	}
	return result
}

// ToAdminMediaItem converts a database.Media to AdminMediaItem for admins.
func ToAdminMediaItem(m database.Media, cfg *config.Config) AdminMediaItem {
	item := AdminMediaItem{
		ID:              m.ID,
		JellyfinID:      m.JellyfinID,
		LibraryName:     m.LibraryName,
		ArrID:           m.ArrID,
		Title:           m.Title,
		TmdbId:          m.TmdbId,
		TvdbId:          m.TvdbId,
		Year:            m.Year,
		FileSize:        m.FileSize,
		PosterURL:       m.PosterURL,
		MediaType:       MediaType(m.MediaType),
		RequestedBy:     m.RequestedBy,
		DefaultDeleteAt: m.DefaultDeleteAt,
		ProtectedUntil:  m.ProtectedUntil,
		Unkeepable:      m.Unkeepable,
	}

	// Add cleanup mode and keep count for TV series
	if m.MediaType == database.MediaTypeTV && cfg != nil {
		item.CleanupMode = string(cfg.GetCleanupMode())
		item.KeepCount = cfg.GetKeepCount()
	}

	// Include full request info for admins
	if m.Request.ID != 0 {
		item.Request = &AdminRequestInfo{
			ID:        m.Request.ID,
			UserID:    m.Request.UserID,
			Username:  m.Request.User.Username,
			Status:    string(m.Request.Status),
			CreatedAt: m.Request.CreatedAt,
			UpdatedAt: m.Request.UpdatedAt,
		}
	}

	return item
}

// ToAdminMediaItems converts a slice of database.Media to AdminMediaItems.
func ToAdminMediaItems(items []database.Media, cfg *config.Config) []AdminMediaItem {
	result := make([]AdminMediaItem, len(items))
	for i, item := range items {
		result[i] = ToAdminMediaItem(item, cfg)
	}
	return result
}

// ToDeletedMediaItem converts a database.Media to DeletedMediaItem for history display.
func ToDeletedMediaItem(m database.Media) DeletedMediaItem {
	return DeletedMediaItem{
		ID:             m.ID,
		JellyfinID:     m.JellyfinID,
		LibraryName:    m.LibraryName,
		ArrID:          m.ArrID,
		Title:          m.Title,
		TmdbId:         m.TmdbId,
		TvdbId:         m.TvdbId,
		Year:           m.Year,
		FileSize:       m.FileSize,
		MediaType:      MediaType(m.MediaType),
		RequestedBy:    m.RequestedBy,
		DBDeleteReason: string(m.DBDeleteReason),
		DeletedAt:      m.DeletedAt.Time,
		CreatedAt:      m.CreatedAt,
	}
}

// ToDeletedMediaItems converts a slice of database.Media to DeletedMediaItems.
func ToDeletedMediaItems(items []database.Media) []DeletedMediaItem {
	result := make([]DeletedMediaItem, len(items))
	for i, item := range items {
		result[i] = ToDeletedMediaItem(item)
	}
	return result
}

// ToHistoryEventItem converts a database.HistoryEvent to HistoryEventItem.
func ToHistoryEventItem(e database.HistoryEvent) HistoryEventItem {
	username := ""
	if e.User != nil {
		username = e.User.Username
	}

	return HistoryEventItem{
		ID:          e.ID,
		MediaID:     e.MediaID,
		JellyfinID:  e.Media.JellyfinID,
		ArrID:       e.Media.ArrID,
		MediaType:   MediaType(e.Media.MediaType),
		Title:       e.Media.Title,
		Year:        e.Media.Year,
		LibraryName: e.Media.LibraryName,
		EventType:   string(e.EventType),
		Username:    username,
		EventTime:   e.EventTime,
		CreatedAt:   e.CreatedAt,
	}
}

// ToHistoryEventItems converts a slice of database.HistoryEvent to HistoryEventItems.
func ToHistoryEventItems(items []database.HistoryEvent) []HistoryEventItem {
	result := make([]HistoryEventItem, len(items))
	for i, item := range items {
		result[i] = ToHistoryEventItem(item)
	}
	return result
}
