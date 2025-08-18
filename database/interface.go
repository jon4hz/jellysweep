package database

import (
	"context"
	"time"
)

// DatabaseInterface defines the interface for database operations.
type DatabaseInterface interface {
	// Cleanup run management
	StartCleanupRun(ctx context.Context) (*CleanupRun, error)
	UpdateCleanupRunStep(ctx context.Context, runID int64, step CleanupRunStep) error
	CompleteCleanupRun(ctx context.Context, runID int64, status CleanupRunStatus, errorMessage *string) error
	GetActiveCleanupRun(ctx context.Context) (*CleanupRun, error)
	GetCleanupRun(ctx context.Context, runID int64) (*CleanupRun, error)
	GetCleanupRunHistory(ctx context.Context, limit, offset int) ([]CleanupRunSummary, error)

	// Step tracking
	StartCleanupStep(ctx context.Context, runID int64, stepName CleanupRunStep) error
	CompleteCleanupStep(ctx context.Context, runID int64, stepName CleanupRunStep, status CleanupRunStatus, errorMessage *string, itemsProcessed int) error
	GetCleanupRunSteps(ctx context.Context, runID int64) ([]CleanupRunStepRecord, error)

	// Media item tracking
	RecordMediaAction(ctx context.Context, runID int64, item *CleanupMediaItem) error
	RecordMediaActions(ctx context.Context, runID int64, items []CleanupMediaItem) error
	GetMediaItemsForRun(ctx context.Context, runID int64, action *MediaAction) ([]CleanupMediaItem, error)
	GetMediaItemHistory(ctx context.Context, mediaID string, limit int) ([]CleanupMediaItem, error)

	// Statistics and reporting
	GetCleanupStats(ctx context.Context, since *time.Time) (*CleanupStats, error)
	GetMediaDeletionHistory(ctx context.Context, since *time.Time, limit, offset int) ([]CleanupMediaItem, error)

	// Utility
	Close() error
	Migrate() error
}

// CleanupStats provides overall statistics.
type CleanupStats struct {
	TotalRuns              int        `db:"total_runs"`
	SuccessfulRuns         int        `db:"successful_runs"`
	FailedRuns             int        `db:"failed_runs"`
	TotalItemsProcessed    int        `db:"total_items_processed"`
	TotalItemsDeleted      int        `db:"total_items_deleted"`
	TotalSizeDeleted       int64      `db:"total_size_deleted"`
	AverageRunDuration     *string    `db:"average_run_duration"`
	LastSuccessfulRun      *time.Time `db:"last_successful_run"`
	MostDeletedInSingleRun int        `db:"most_deleted_in_single_run"`
}
