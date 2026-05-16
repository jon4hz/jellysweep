package database

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"github.com/jon4hz/jellysweep/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var _ DB = (*Client)(nil) // Ensure Client implements DB

// Client wraps the gorm.DB instance.
type Client struct {
	db *gorm.DB
}

// New creates a new database connection and performs migrations.
func New(cfg *config.DatabaseConfig) (*Client, bool, error) {
	dialector, err := dialectorForConfig(cfg)
	if err != nil {
		return nil, false, err
	}

	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, false, fmt.Errorf("failed to connect database: %w", err)
	}

	isNew, err := isNewDatabase(db)
	if err != nil {
		return nil, false, fmt.Errorf("failed to inspect database schema: %w", err)
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
		return nil, false, fmt.Errorf("failed to migrate database: %w", err)
	}

	return &Client{db: db}, isNew, nil
}

func dialectorForConfig(cfg *config.DatabaseConfig) (gorm.Dialector, error) {
	if cfg == nil {
		return nil, fmt.Errorf("missing database config")
	}

	switch cfg.Type {
	case "", config.DatabaseTypeSQLite:
		return sqlite.Open(cfg.Path), nil
	case config.DatabaseTypePostgres:
		return postgres.Open(cfg.URL), nil
	default:
		return nil, fmt.Errorf("unsupported database type %q", cfg.Type)
	}
}

func isNewDatabase(db *gorm.DB) (bool, error) {
	return !db.Migrator().HasTable(&Media{}), nil
}
