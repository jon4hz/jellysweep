package models

import (
	"github.com/jon4hz/jellysweep/database"
)

// ToUserMediaItem converts a database.Media to UserMediaItem for regular users.
// This excludes sensitive fields like RequestedBy.
func ToUserMediaItem(m database.Media) UserMediaItem {
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
func ToUserMediaItems(items []database.Media) []UserMediaItem {
	result := make([]UserMediaItem, len(items))
	for i, item := range items {
		result[i] = ToUserMediaItem(item)
	}
	return result
}

// ToAdminMediaItem converts a database.Media to AdminMediaItem for admins.
func ToAdminMediaItem(m database.Media) AdminMediaItem {
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
func ToAdminMediaItems(items []database.Media) []AdminMediaItem {
	result := make([]AdminMediaItem, len(items))
	for i, item := range items {
		result[i] = ToAdminMediaItem(item)
	}
	return result
}
