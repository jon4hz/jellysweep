package models

import "time"

// User represents a user in the system, including their authentication details and admin status.
type User struct {
	Sub         string
	Email       string
	Name        string
	Username    string
	IsAdmin     bool
	GravatarURL string // URL to the user's Gravatar image, empty if not available
}

// MediaItem represents a media item for display in the UI and for deletion tracking.
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

// KeepRequest represents a user request to keep a media item.
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
