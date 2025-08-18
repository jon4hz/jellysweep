package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// SQLiteDB implements the DatabaseInterface using SQLite.
type SQLiteDB struct {
	db   *sql.DB
	path string
}

// NewSQLiteDB creates a new SQLite database instance.
func NewSQLiteDB(dbPath string) (*SQLiteDB, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := db.PingContext(context.Background()); err != nil {
		db.Close() //nolint: errcheck, gosec
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	sqliteDB := &SQLiteDB{
		db:   db,
		path: dbPath,
	}

	return sqliteDB, nil
}

// Migrate runs database migrations.
func (s *SQLiteDB) Migrate() error {
	goose.SetBaseFS(embedMigrations)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.Up(s.db, "migrations"); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Info("Database migrations completed successfully")
	return nil
}

// Close closes the database connection.
func (s *SQLiteDB) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// StartCleanupRun creates a new cleanup run record.
func (s *SQLiteDB) StartCleanupRun(ctx context.Context) (*CleanupRun, error) {
	now := time.Now()

	query := `
		INSERT INTO cleanup_runs (started_at, status, step)
		VALUES (?, ?, ?)
	`

	result, err := s.db.ExecContext(ctx, query, now, CleanupRunStatusRunning, CleanupRunStepStarting)
	if err != nil {
		return nil, fmt.Errorf("failed to create cleanup run: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get cleanup run ID: %w", err)
	}

	return &CleanupRun{
		ID:        id,
		StartedAt: now,
		Status:    CleanupRunStatusRunning,
		Step:      CleanupRunStepStarting,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// UpdateCleanupRunStep updates the current step of a cleanup run.
func (s *SQLiteDB) UpdateCleanupRunStep(ctx context.Context, runID int64, step CleanupRunStep) error {
	query := `UPDATE cleanup_runs SET step = ? WHERE id = ?`

	_, err := s.db.ExecContext(ctx, query, step, runID)
	if err != nil {
		return fmt.Errorf("failed to update cleanup run step: %w", err)
	}

	return nil
}

// CompleteCleanupRun marks a cleanup run as completed.
func (s *SQLiteDB) CompleteCleanupRun(ctx context.Context, runID int64, status CleanupRunStatus, errorMessage *string) error {
	now := time.Now()

	query := `
		UPDATE cleanup_runs
		SET completed_at = ?, status = ?, error_message = ?, step = ?
		WHERE id = ?
	`

	_, err := s.db.ExecContext(ctx, query, now, status, errorMessage, CleanupRunStepCompleted, runID)
	if err != nil {
		return fmt.Errorf("failed to complete cleanup run: %w", err)
	}

	return nil
}

// GetActiveCleanupRun returns the currently active cleanup run, if any.
func (s *SQLiteDB) GetActiveCleanupRun(ctx context.Context) (*CleanupRun, error) {
	query := `
		SELECT id, started_at, completed_at, status, error_message, step, created_at, updated_at
		FROM cleanup_runs
		WHERE status = ?
		ORDER BY started_at DESC
		LIMIT 1
	`

	var run CleanupRun
	row := s.db.QueryRowContext(ctx, query, CleanupRunStatusRunning)

	err := row.Scan(
		&run.ID, &run.StartedAt, &run.CompletedAt, &run.Status,
		&run.ErrorMessage, &run.Step, &run.CreatedAt, &run.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get active cleanup run: %w", err)
	}

	return &run, nil
}

// GetCleanupRun returns a specific cleanup run by ID.
func (s *SQLiteDB) GetCleanupRun(ctx context.Context, runID int64) (*CleanupRun, error) {
	query := `
		SELECT id, started_at, completed_at, status, error_message, step, created_at, updated_at
		FROM cleanup_runs
		WHERE id = ?
	`

	var run CleanupRun
	row := s.db.QueryRowContext(ctx, query, runID)

	err := row.Scan(
		&run.ID, &run.StartedAt, &run.CompletedAt, &run.Status,
		&run.ErrorMessage, &run.Step, &run.CreatedAt, &run.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get cleanup run: %w", err)
	}

	return &run, nil
}

// GetCleanupRunHistory returns historical cleanup runs with summaries.
func (s *SQLiteDB) GetCleanupRunHistory(ctx context.Context, limit, offset int) ([]CleanupRunSummary, error) {
	query := `
		SELECT
			cr.id, cr.started_at, cr.completed_at, cr.status, cr.error_message,
			cr.step, cr.created_at, cr.updated_at,
			COUNT(cmi.id) as total_items,
			COUNT(CASE WHEN cmi.action = 'marked_for_deletion' THEN 1 END) as marked_for_deletion,
			COUNT(CASE WHEN cmi.action = 'deleted' AND cmi.deletion_date IS NOT NULL THEN 1 END) as deleted,
			COUNT(CASE WHEN cmi.action = 'kept' THEN 1 END) as kept,
			COUNT(CASE WHEN cmi.action = 'ignored' THEN 1 END) as ignored,
			COUNT(CASE WHEN cmi.action = 'unmarked' THEN 1 END) as unmarked,
			COALESCE(SUM(cmi.file_size), 0) as total_size,
			COALESCE(SUM(CASE WHEN cmi.action = 'deleted' AND cmi.deletion_date IS NOT NULL THEN cmi.file_size ELSE 0 END), 0) as deleted_size
		FROM cleanup_runs cr
		LEFT JOIN cleanup_media_items cmi ON cr.id = cmi.cleanup_run_id
		GROUP BY cr.id
		ORDER BY cr.started_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get cleanup run history: %w", err)
	}
	defer rows.Close() //nolint: errcheck

	var summaries []CleanupRunSummary
	for rows.Next() {
		var summary CleanupRunSummary

		err := rows.Scan(
			&summary.ID, &summary.StartedAt, &summary.CompletedAt, &summary.Status,
			&summary.ErrorMessage, &summary.Step, &summary.CreatedAt, &summary.UpdatedAt,
			&summary.TotalItems, &summary.MarkedForDeletion, &summary.Deleted,
			&summary.Kept, &summary.Ignored, &summary.Unmarked,
			&summary.TotalSize, &summary.DeletedSize,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cleanup run summary: %w", err)
		}

		// Calculate duration in Go
		if summary.CompletedAt != nil {
			duration := summary.CompletedAt.Sub(summary.StartedAt)
			durationMinutes := duration.Minutes()
			formatted := fmt.Sprintf("%.0f minutes", durationMinutes)
			summary.Duration = &formatted
		}

		summaries = append(summaries, summary)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading cleanup run history: %w", err)
	}

	return summaries, nil
}

// StartCleanupStep records the start of a cleanup step.
func (s *SQLiteDB) StartCleanupStep(ctx context.Context, runID int64, stepName CleanupRunStep) error {
	query := `
		INSERT OR REPLACE INTO cleanup_run_steps (cleanup_run_id, step_name, started_at, status)
		VALUES (?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query, runID, stepName, time.Now(), CleanupRunStatusRunning)
	if err != nil {
		return fmt.Errorf("failed to start cleanup step: %w", err)
	}

	return nil
}

// CompleteCleanupStep marks a cleanup step as completed.
func (s *SQLiteDB) CompleteCleanupStep(ctx context.Context, runID int64, stepName CleanupRunStep, status CleanupRunStatus, errorMessage *string, itemsProcessed int) error {
	now := time.Now()

	query := `
		UPDATE cleanup_run_steps
		SET completed_at = ?, status = ?, error_message = ?, items_processed = ?
		WHERE cleanup_run_id = ? AND step_name = ?
	`

	_, err := s.db.ExecContext(ctx, query, now, status, errorMessage, itemsProcessed, runID, stepName)
	if err != nil {
		return fmt.Errorf("failed to complete cleanup step: %w", err)
	}

	return nil
}

// GetCleanupRunSteps returns all steps for a cleanup run.
func (s *SQLiteDB) GetCleanupRunSteps(ctx context.Context, runID int64) ([]CleanupRunStepRecord, error) {
	query := `
		SELECT id, cleanup_run_id, step_name, started_at, completed_at, status, error_message, items_processed
		FROM cleanup_run_steps
		WHERE cleanup_run_id = ?
		ORDER BY started_at
	`

	rows, err := s.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cleanup run steps: %w", err)
	}
	defer rows.Close() //nolint: errcheck

	var steps []CleanupRunStepRecord
	for rows.Next() {
		var step CleanupRunStepRecord

		err := rows.Scan(
			&step.ID, &step.CleanupRunID, &step.StepName, &step.StartedAt,
			&step.CompletedAt, &step.Status, &step.ErrorMessage, &step.ItemsProcessed,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cleanup step: %w", err)
		}

		steps = append(steps, step)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading cleanup steps: %w", err)
	}

	return steps, nil
}

// RecordMediaAction records a single media action.
func (s *SQLiteDB) RecordMediaAction(ctx context.Context, runID int64, item *CleanupMediaItem) error {
	var tagsJSON *string
	if item.Tags != nil {
		tagsJSON = item.Tags
	}

	query := `
		INSERT INTO cleanup_media_items (
			cleanup_run_id, jellyfin_id, media_id, title, media_type, year, library,
			tmdb_id, file_size, requested_by, request_date, action, action_timestamp,
			deletion_date, tags
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		runID, item.JellyfinID, item.MediaID, item.Title, item.MediaType,
		item.Year, item.Library, item.TmdbID, item.FileSize, item.RequestedBy,
		item.RequestDate, item.Action, item.ActionTimestamp, item.DeletionDate, tagsJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to record media action: %w", err)
	}

	return nil
}

// RecordMediaActions records multiple media actions in a transaction.
func (s *SQLiteDB) RecordMediaActions(ctx context.Context, runID int64, items []CleanupMediaItem) error {
	if len(items) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Error("failed to rollback transaction", "error", err)
		}
	}()

	query := `
		INSERT INTO cleanup_media_items (
			cleanup_run_id, jellyfin_id, media_id, title, media_type, year, library,
			tmdb_id, file_size, requested_by, request_date, action, action_timestamp,
			deletion_date, tags
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() {
		if err := stmt.Close(); err != nil {
			log.Error("failed to close statement", "error", err)
		}
	}()

	for _, item := range items {
		var tagsJSON *string
		if item.Tags != nil {
			tagsJSON = item.Tags
		}

		_, err := stmt.ExecContext(ctx,
			runID, item.JellyfinID, item.MediaID, item.Title, item.MediaType,
			item.Year, item.Library, item.TmdbID, item.FileSize, item.RequestedBy,
			item.RequestDate, item.Action, item.ActionTimestamp, item.DeletionDate, tagsJSON,
		)
		if err != nil {
			return fmt.Errorf("failed to record media action for %s: %w", item.MediaID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetMediaItemsForRun returns media items for a specific cleanup run.
func (s *SQLiteDB) GetMediaItemsForRun(ctx context.Context, runID int64, action *MediaAction) ([]CleanupMediaItem, error) {
	query := `
		SELECT id, cleanup_run_id, jellyfin_id, media_id, title, media_type, year, library,
			   tmdb_id, file_size, requested_by, request_date, action, action_timestamp,
			   deletion_date, tags, created_at
		FROM cleanup_media_items
		WHERE cleanup_run_id = ?
	`

	args := []interface{}{runID}

	if action != nil {
		query += " AND action = ?"
		args = append(args, *action)
	}

	query += " ORDER BY action_timestamp"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get media items for run: %w", err)
	}
	defer rows.Close() //nolint: errcheck

	var items []CleanupMediaItem
	for rows.Next() {
		var item CleanupMediaItem

		err := rows.Scan(
			&item.ID, &item.CleanupRunID, &item.JellyfinID, &item.MediaID,
			&item.Title, &item.MediaType, &item.Year, &item.Library,
			&item.TmdbID, &item.FileSize, &item.RequestedBy, &item.RequestDate,
			&item.Action, &item.ActionTimestamp, &item.DeletionDate, &item.Tags,
			&item.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan media item: %w", err)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading media items: %w", err)
	}

	return items, nil
}

// GetMediaItemHistory returns the action history for a specific media item.
func (s *SQLiteDB) GetMediaItemHistory(ctx context.Context, mediaID string, limit int) ([]CleanupMediaItem, error) {
	query := `
		SELECT id, cleanup_run_id, jellyfin_id, media_id, title, media_type, year, library,
			   tmdb_id, file_size, requested_by, request_date, action, action_timestamp,
			   deletion_date, tags, created_at
		FROM cleanup_media_items
		WHERE media_id = ?
		ORDER BY action_timestamp DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, mediaID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get media item history: %w", err)
	}
	defer rows.Close() //nolint: errcheck

	var items []CleanupMediaItem
	for rows.Next() {
		var item CleanupMediaItem

		err := rows.Scan(
			&item.ID, &item.CleanupRunID, &item.JellyfinID, &item.MediaID,
			&item.Title, &item.MediaType, &item.Year, &item.Library,
			&item.TmdbID, &item.FileSize, &item.RequestedBy, &item.RequestDate,
			&item.Action, &item.ActionTimestamp, &item.DeletionDate, &item.Tags,
			&item.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan media item: %w", err)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading media item history: %w", err)
	}

	return items, nil
}

// GetCleanupStats returns overall cleanup statistics.
func (s *SQLiteDB) GetCleanupStats(ctx context.Context, since *time.Time) (*CleanupStats, error) {
	query := `
		SELECT
			COUNT(DISTINCT cr.id) as total_runs,
			COUNT(DISTINCT CASE WHEN cr.status = 'completed' THEN cr.id END) as successful_runs,
			COUNT(DISTINCT CASE WHEN cr.status = 'failed' THEN cr.id END) as failed_runs,
			COUNT(cmi.id) as total_items_processed,
			COUNT(CASE WHEN cmi.action = 'deleted' AND cmi.deletion_date IS NOT NULL THEN 1 END) as total_items_deleted,
			COALESCE(SUM(CASE WHEN cmi.action = 'deleted' AND cmi.deletion_date IS NOT NULL THEN cmi.file_size ELSE 0 END), 0) as total_size_deleted,
			MAX(CASE WHEN cr.status = 'completed' THEN cr.completed_at END) as last_successful_run,
			COALESCE(MAX(run_deletions.deleted_count), 0) as most_deleted_in_single_run
		FROM cleanup_runs cr
		LEFT JOIN cleanup_media_items cmi ON cr.id = cmi.cleanup_run_id
		LEFT JOIN (
			SELECT cleanup_run_id, COUNT(*) as deleted_count
			FROM cleanup_media_items
			WHERE action = 'deleted' AND deletion_date IS NOT NULL
			GROUP BY cleanup_run_id
		) run_deletions ON cr.id = run_deletions.cleanup_run_id
	`

	args := make([]any, 0)
	if since != nil {
		query += " WHERE cr.started_at >= ?"
		args = append(args, *since)
	}

	var stats CleanupStats
	row := s.db.QueryRowContext(ctx, query, args...)

	var lastSuccessfulRunStr *string
	err := row.Scan(
		&stats.TotalRuns, &stats.SuccessfulRuns, &stats.FailedRuns,
		&stats.TotalItemsProcessed, &stats.TotalItemsDeleted, &stats.TotalSizeDeleted,
		&lastSuccessfulRunStr, &stats.MostDeletedInSingleRun,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get cleanup stats: %w", err)
	}

	// Parse the last successful run time if it exists
	if lastSuccessfulRunStr != nil && *lastSuccessfulRunStr != "" {
		if parsedTime, err := time.Parse("2006-01-02 15:04:05", *lastSuccessfulRunStr); err == nil {
			stats.LastSuccessfulRun = &parsedTime
		}
	}

	// Calculate average duration by fetching completed runs
	durationQuery := `
		SELECT started_at, completed_at
		FROM cleanup_runs
		WHERE status = 'completed' AND completed_at IS NOT NULL
	`
	durationArgs := make([]any, 0)
	if since != nil {
		durationQuery += " AND started_at >= ?"
		durationArgs = append(durationArgs, *since)
	}

	durationRows, err := s.db.QueryContext(ctx, durationQuery, durationArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to get duration data: %w", err)
	}
	defer durationRows.Close() //nolint: errcheck

	var totalDuration time.Duration
	var completedCount int

	for durationRows.Next() {
		var startedAt, completedAt time.Time
		err := durationRows.Scan(&startedAt, &completedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan duration data: %w", err)
		}

		duration := completedAt.Sub(startedAt)
		totalDuration += duration
		completedCount++
	}

	if err := durationRows.Err(); err != nil {
		return nil, fmt.Errorf("error reading duration data: %w", err)
	}

	// Calculate and format average duration
	if completedCount > 0 {
		avgDuration := totalDuration / time.Duration(completedCount)
		avgMinutes := avgDuration.Minutes()
		formatted := fmt.Sprintf("%.1f minutes", avgMinutes)
		stats.AverageRunDuration = &formatted
	}

	return &stats, nil
}

// GetMediaDeletionHistory returns recent media deletions.
func (s *SQLiteDB) GetMediaDeletionHistory(ctx context.Context, since *time.Time, limit, offset int) ([]CleanupMediaItem, error) {
	query := `
		SELECT id, cleanup_run_id, jellyfin_id, media_id, title, media_type, year, library,
			   tmdb_id, file_size, requested_by, request_date, action, action_timestamp,
			   deletion_date, tags, created_at
		FROM cleanup_media_items
		WHERE action = 'deleted'
	`

	args := make([]any, 0)
	if since != nil {
		query += " AND action_timestamp >= ?"
		args = append(args, *since)
	}

	query += " ORDER BY action_timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get media deletion history: %w", err)
	}
	defer rows.Close() //nolint: errcheck

	var items []CleanupMediaItem
	for rows.Next() {
		var item CleanupMediaItem

		err := rows.Scan(
			&item.ID, &item.CleanupRunID, &item.JellyfinID, &item.MediaID,
			&item.Title, &item.MediaType, &item.Year, &item.Library,
			&item.TmdbID, &item.FileSize, &item.RequestedBy, &item.RequestDate,
			&item.Action, &item.ActionTimestamp, &item.DeletionDate, &item.Tags,
			&item.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan media item: %w", err)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading media deletion history: %w", err)
	}

	return items, nil
}
