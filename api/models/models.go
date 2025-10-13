package models

import (
	"time"
)

// User represents a user in the system, including their authentication details and admin status.
type User struct {
	ID          uint // ID from the database
	Name        string
	Username    string
	IsAdmin     bool
	Email       string // User's email address from the oidc token (used for gravatar)
	GravatarURL string // URL to the user's Gravatar image, empty if not available
}

type MediaType string

const (
	MediaTypeTV    MediaType = "tv"
	MediaTypeMovie MediaType = "movie"
)

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
