package database

import (
	"os"
	"path/filepath"
)

// New creates a new database instance.
func New(dbPath string) (DatabaseInterface, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
	}

	db, err := NewSQLiteDB(dbPath)
	if err != nil {
		return nil, err
	}

	// Run migrations
	if err := db.Migrate(); err != nil {
		db.Close() //nolint: errcheck, gosec
		return nil, err
	}

	return db, nil
}
