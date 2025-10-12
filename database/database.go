package database

import (
	"fmt"
	"path"

	"github.com/glebarez/sqlite"
	"github.com/jon4hz/jellysweep/database/models"
	"gorm.io/gorm"
)

// DB wraps the gorm.DB instance.
type DB struct {
	db *gorm.DB
}

// New creates a new database connection and performs migrations.
func New(dbpath string) (*DB, error) {
	db, err := gorm.Open(sqlite.Open(path.Join(dbpath, "jellysweep.db")), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	if err := db.AutoMigrate(
		&models.Media{},
		&models.DiskUsageDeletePolicy{},
		&models.User{},
		&models.UserSettings{},
		&models.EmailSettings{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return &DB{db: db}, nil
}
