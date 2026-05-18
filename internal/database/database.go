package database

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

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

	isNew := isNewDatabase(db)

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
		return postgres.Open(postgresDSNForConfig(cfg)), nil
	default:
		return nil, fmt.Errorf("unsupported database type %q", cfg.Type)
	}
}

func postgresDSNForConfig(cfg *config.DatabaseConfig) string {
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}

	u := &url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   cfg.Name,
	}
	if cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	} else {
		u.User = url.User(cfg.User)
	}

	q := u.Query()
	q.Set("sslmode", sslMode)
	u.RawQuery = q.Encode()

	return u.String()
}

func isNewDatabase(db *gorm.DB) bool {
	return !db.Migrator().HasTable(&Media{})
}
