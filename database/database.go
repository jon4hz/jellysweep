package database

import (
	"context"
	"fmt"
	"path"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// DB defines the interface for database operations.
type DB interface {
	UserDB
	MediaDB
}

// MediaDB defines the interface for media-related database operations.
type MediaDB interface {
	CreateMediaItems(ctx context.Context, items []Media) error
	GetMediaItems(ctx context.Context) ([]Media, error)
	GetMediaItemsByMediaType(ctx context.Context, mediaType MediaType) ([]Media, error)
}

// UserDB defines the interface for user-related database operations.
type UserDB interface {
	CreateUser(ctx context.Context, username string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	GetOrCreateUser(ctx context.Context, username string) (*User, error)
}

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
		&User{},
		&UserSettings{},
		&EmailSettings{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return &Client{db: db}, nil
}
