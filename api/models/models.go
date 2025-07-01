package models

import "time"

type User struct {
	Sub      string
	Email    string
	Name     string
	Username string
	IsAdmin  bool
}

// MediaItem represents a media item for display in the UI and for deletion tracking
type MediaItem struct {
	ID           string
	Title        string
	Type         string // "movie" or "tv"
	Year         int32
	Library      string
	DeletionDate time.Time
	PosterURL    string
	CanRequest   bool
	HasRequested bool
	MustDelete   bool  // Indicates if this item is marked for deletion for sure
	FileSize     int64 // Size in bytes
}

// KeepRequest represents a user request to keep a media item
type KeepRequest struct {
	ID           string
	MediaID      string
	Title        string
	Type         string // "movie" or "tv"
	Year         int
	Library      string
	DeletionDate time.Time
	PosterURL    string
	RequestedBy  string
	RequestDate  time.Time
	ExpiryDate   time.Time
}
