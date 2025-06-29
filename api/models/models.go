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
	Year         int
	Library      string
	DeletionDate time.Time
	PosterURL    string
	CanRequest   bool
	HasRequested bool
}
