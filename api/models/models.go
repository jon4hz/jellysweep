package models

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
