package database

import (
	"context"
	"time"
)

type SortOrder string

const (
	SortOrderAsc  SortOrder = "asc"
	SortOrderDesc SortOrder = "desc"
)

// MediaType represents the type of media, either TV show or Movie.
type MediaType string

const (
	// MediaTypeTV represents TV shows.
	MediaTypeTV MediaType = "tv"
	// MediaTypeMovie represents Movies.
	MediaTypeMovie MediaType = "movie"
)

// DBDeleteReason represents the reason why a media item was deleted from the database.
type DBDeleteReason string

const (
	// DBDeleteReasonDefault indicates the media was actually deleted in Jellyfin.
	DBDeleteReasonDefault DBDeleteReason = "default"
	// DBDeleteReasonStreamed indicates the media was deleted in the database only because it was streamed.
	DBDeleteReasonStreamed DBDeleteReason = "streamed"
	// DBDeleteReasonKeepForever indicates the media was deleted in the database only because it was marked to keep forever.
	DBDeleteReasonKeepForever DBDeleteReason = "keep_forever"
	// DBDeleteReasonProtectionExpired indicates the media was deleted in the database only because its protection period expired.
	DBDeleteReasonProtectionExpired DBDeleteReason = "protection_expired"
)

// DB defines the interface for database operations.
type DB interface {
	UserDB
	MediaDB
	RequestDB
	HistoryDB
}

// MediaDB defines the interface for media-related database operations.
type MediaDB interface {
	CreateMediaItems(ctx context.Context, items []Media) error
	GetMediaItemByID(ctx context.Context, id uint) (*Media, error)
	GetMediaItems(ctx context.Context, includeProtected bool) ([]Media, error)
	GetMediaItemsByMediaType(ctx context.Context, mediaType MediaType) ([]Media, error)
	GetMediaWithPendingRequest(ctx context.Context) ([]Media, error)
	GetMediaExpiredProtection(ctx context.Context, asOf time.Time) ([]Media, error)
	GetDeletedMediaByTMDBID(ctx context.Context, tmdbID int32) ([]Media, error)
	GetDeletedMediaByTVDBID(ctx context.Context, tvdbID int32) ([]Media, error)
	GetDeletedMedia(ctx context.Context, page, pageSize int, sortBy string, sortOrder SortOrder) ([]Media, int64, error)
	SetMediaProtectedUntil(ctx context.Context, mediaID uint, protectedUntil *time.Time) error
	MarkMediaAsUnkeepable(ctx context.Context, mediaID uint) error
	DeleteMediaItem(ctx context.Context, media *Media) error
}

// RequestDB defines the interface for request-related database operations.
type RequestDB interface {
	CreateRequest(ctx context.Context, mediaID uint, userID uint) (*Request, error)
	UpdateRequestStatus(ctx context.Context, requestID uint, status RequestStatus) error
}

// UserDB defines the interface for user-related database operations.
type UserDB interface {
	CreateUser(ctx context.Context, username string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	GetUserByID(ctx context.Context, id uint) (*User, error)
	GetOrCreateUser(ctx context.Context, username string) (*User, error)
}
