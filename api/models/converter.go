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
