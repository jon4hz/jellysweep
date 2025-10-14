package database

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// DB defines the interface for database operations.
type DB interface {
	UserDB
	MediaDB
	RequestDB
}

// MediaDB defines the interface for media-related database operations.
type MediaDB interface {
	CreateMediaItems(ctx context.Context, items []Media) error
	GetMediaItemByID(ctx context.Context, id uint) (*Media, error)
	GetMediaItems(ctx context.Context) ([]Media, error)
	GetMediaItemsByMediaType(ctx context.Context, mediaType MediaType) ([]Media, error)
	GetMediaWithPendingRequest(ctx context.Context) ([]Media, error)
	SetMediaProtectedUntil(ctx context.Context, mediaID uint, protectedUntil *time.Time) error
	MarkMediaAsUnkeepable(ctx context.Context, mediaID uint) error
	DeleteMediaItem(ctx context.Context, mediaID uint) error
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

var _ DB = (*Client)(nil) // Ensure Client implements DB

// Client wraps the gorm.DB instance.
type Client struct {
	db *gorm.DB
}

// New creates a new database connection and performs migrations.
func New(dbpath string) (*Client, error) {
	db, err := gorm.Open(sqlite.Open(path.Join(dbpath, "jellysweep.db")), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	if err := db.AutoMigrate(
		&Media{},
		&DiskUsageDeletePolicy{},
		&Request{},
		&User{},
		&UserSettings{},
		&EmailSettings{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return &Client{db: db}, nil
}
