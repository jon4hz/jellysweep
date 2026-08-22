// Package databasetest provides an in-memory sqlite database for tests.
//
// It returns both the migrated database.Client and the raw gorm handle so
// tests can manipulate rows directly, e.g. backdating timestamps to simulate
// elapsed time in lifecycle tests.
package databasetest

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/jon4hz/jellysweep/internal/database"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var dbCounter atomic.Int64

// New opens a fresh in-memory sqlite database, runs all migrations and
// returns the wrapped client together with the raw gorm handle.
func New(t *testing.T) (*database.Client, *gorm.DB) {
	t.Helper()

	// A plain ":memory:" DSN creates a separate database per pool connection,
	// so use a uniquely named shared-cache database and a single connection.
	name := strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", name, dbCounter.Add(1))

	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		sqlDB.Close() //nolint:errcheck,gosec
	})

	client, _, err := database.NewFromGorm(gdb)
	if err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	return client, gdb
}
