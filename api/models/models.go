package models

import (
	"time"

	"github.com/jon4hz/jellysweep/config"
)

// User represents a user in the system, including their authentication details and admin status.
type User struct {
	Sub         string
	Email       string
	Name        string
	Username    string
	IsAdmin     bool
	GravatarURL string // URL to the user's Gravatar image, empty if not available
}

type MediaType string

const (
	MediaTypeTV    MediaType = "tv"
	MediaTypeMovie MediaType = "movie"
)

// MediaItem represents a media item for display in the UI and for deletion tracking.
type MediaItem struct {
	ID           string
	Title        string
	Type         MediaType
	Year         int32
	Library      string
	DeletionDate time.Time
	PosterURL    string
	CanRequest   bool
	HasRequested bool
	MustDelete   bool               // Indicates if this item is marked for deletion for sure
	FileSize     int64              // Size in bytes
	CleanupMode  config.CleanupMode // Cleanup mode for this item: "all", "keep_episodes", "keep_seasons"
	KeepCount    int                // Number of episodes/seasons to keep (when cleanup mode is not "all")
}

// KeepRequest represents a user request to keep a media item.
type KeepRequest struct {
	ID           string
	MediaID      string
	Title        string
	Type         MediaType
	Year         int
	Library      string
	DeletionDate time.Time
	PosterURL    string
	RequestDate  time.Time
	ExpiryDate   time.Time
}
