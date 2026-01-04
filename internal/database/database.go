package database

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var _ DB = (*Client)(nil) // Ensure Client implements DB

// Client wraps the gorm.DB instance.
type Client struct {
	db *gorm.DB
}

// New creates a new database connection and performs migrations.
func New(dbpath string) (*Client, error) {
	db, err := gorm.Open(sqlite.Open(dbpath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	if err := db.AutoMigrate(
		&Media{},
		&DiskUsageDeletePolicy{},
		&Request{},
		&User{},
		&UserSettings{},
		&UserPermissions{},
		&EmailSettings{},
		&HistoryEvent{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return &Client{db: db}, nil
}
