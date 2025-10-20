package models

import "time"

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

// UserMediaItem represents media information exposed to regular users.
type UserMediaItem struct {
	ID              uint      `json:"ID"`
	Title           string    `json:"Title"`
	Year            int32     `json:"Year"`
	PosterURL       string    `json:"PosterURL"`
	MediaType       MediaType `json:"MediaType"`
	LibraryName     string    `json:"LibraryName"`
	FileSize        int64     `json:"FileSize"`
	DefaultDeleteAt time.Time `json:"DefaultDeleteAt"`
	Unkeepable      bool      `json:"Unkeepable"`
	// Request info without revealing who requested
	Request *UserRequestInfo `json:"Request,omitempty"`
}

// UserRequestInfo represents request information visible to users.
type UserRequestInfo struct {
	ID     uint   `json:"ID"`
	Status string `json:"Status"`
}

// AdminMediaItem represents media information exposed to admins.
type AdminMediaItem struct {
	ID              uint       `json:"ID"`
	JellyfinID      string     `json:"JellyfinID"`
	LibraryName     string     `json:"LibraryName"`
	ArrID           int32      `json:"ArrID"`
	Title           string     `json:"Title"`
	TmdbId          *int32     `json:"TmdbId,omitempty"`
	TvdbId          *int32     `json:"TvdbId,omitempty"`
	Year            int32      `json:"Year"`
	FileSize        int64      `json:"FileSize"`
	PosterURL       string     `json:"PosterURL"`
	MediaType       MediaType  `json:"MediaType"`
	RequestedBy     string     `json:"RequestedBy"`
	DefaultDeleteAt time.Time  `json:"DefaultDeleteAt"`
	ProtectedUntil  *time.Time `json:"ProtectedUntil,omitempty"`
	Unkeepable      bool       `json:"Unkeepable"`
	// Request with full details
	Request *AdminRequestInfo `json:"Request,omitempty"`
}

// AdminRequestInfo represents full request information for admins.
type AdminRequestInfo struct {
	ID        uint      `json:"ID"`
	UserID    uint      `json:"UserID"`
	Status    string    `json:"Status"`
	CreatedAt time.Time `json:"CreatedAt"`
	UpdatedAt time.Time `json:"UpdatedAt"`
}
