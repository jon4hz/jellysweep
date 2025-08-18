package database

import (
	"time"
)

// CleanupRunStatus represents the status of a cleanup run.
type CleanupRunStatus string

const (
	CleanupRunStatusRunning   CleanupRunStatus = "running"
	CleanupRunStatusCompleted CleanupRunStatus = "completed"
	CleanupRunStatusFailed    CleanupRunStatus = "failed"
	CleanupRunStatusCancelled CleanupRunStatus = "cancelled"
)

// CleanupRunStep represents the steps in a cleanup run.
type CleanupRunStep string

const (
	CleanupRunStepStarting              CleanupRunStep = "starting"
	CleanupRunStepRemoveExpiredKeepTags CleanupRunStep = "remove_expired_keep_tags"
	CleanupRunStepCleanupOldTags        CleanupRunStep = "cleanup_old_tags"
	CleanupRunStepMarkForDeletion       CleanupRunStep = "mark_for_deletion"
	CleanupRunStepRemoveRecentlyPlayed  CleanupRunStep = "remove_recently_played"
	CleanupRunStepCleanupMedia          CleanupRunStep = "cleanup_media"
	CleanupRunStepCompleted             CleanupRunStep = "completed"
)

// MediaAction represents actions taken on media items.
type MediaAction string

const (
	MediaActionMarkedForDeletion MediaAction = "marked_for_deletion"
	MediaActionDeleted           MediaAction = "deleted"
	MediaActionKept              MediaAction = "kept"
	MediaActionIgnored           MediaAction = "ignored"
	MediaActionUnmarked          MediaAction = "unmarked"
)

// CleanupRun represents a cleanup run record.
type CleanupRun struct {
	ID           int64            `db:"id"`
	StartedAt    time.Time        `db:"started_at"`
	CompletedAt  *time.Time       `db:"completed_at"`
	Status       CleanupRunStatus `db:"status"`
	ErrorMessage *string          `db:"error_message"`
	Step         CleanupRunStep   `db:"step"`
	CreatedAt    time.Time        `db:"created_at"`
	UpdatedAt    time.Time        `db:"updated_at"`
}

// CleanupMediaItem represents a media item tracked during cleanup.
type CleanupMediaItem struct {
	ID              int64       `db:"id"`
	CleanupRunID    int64       `db:"cleanup_run_id"`
	JellyfinID      string      `db:"jellyfin_id"`
	MediaID         string      `db:"media_id"`
	Title           string      `db:"title"`
	MediaType       string      `db:"media_type"`
	Year            *int        `db:"year"`
	Library         *string     `db:"library"`
	TmdbID          *int32      `db:"tmdb_id"`
	FileSize        int64       `db:"file_size"`
	RequestedBy     *string     `db:"requested_by"`
	RequestDate     *time.Time  `db:"request_date"`
	Action          MediaAction `db:"action"`
	ActionTimestamp time.Time   `db:"action_timestamp"`
	DeletionDate    *time.Time  `db:"deletion_date"`
	Tags            *string     `db:"tags"` // JSON array
	CreatedAt       time.Time   `db:"created_at"`
}

// CleanupRunStepRecord represents a step execution record.
type CleanupRunStepRecord struct {
	ID             int64            `db:"id"`
	CleanupRunID   int64            `db:"cleanup_run_id"`
	StepName       CleanupRunStep   `db:"step_name"`
	StartedAt      time.Time        `db:"started_at"`
	CompletedAt    *time.Time       `db:"completed_at"`
	Status         CleanupRunStatus `db:"status"`
	ErrorMessage   *string          `db:"error_message"`
	ItemsProcessed int              `db:"items_processed"`
}

// CleanupRunSummary provides a summary of a cleanup run.
type CleanupRunSummary struct {
	CleanupRun
	TotalItems        int     `db:"total_items"`
	MarkedForDeletion int     `db:"marked_for_deletion"`
	Deleted           int     `db:"deleted"`
	Kept              int     `db:"kept"`
	Ignored           int     `db:"ignored"`
	Unmarked          int     `db:"unmarked"`
	TotalSize         int64   `db:"total_size"`
	DeletedSize       int64   `db:"deleted_size"`
	Duration          *string `db:"duration"` // Human readable duration
}
