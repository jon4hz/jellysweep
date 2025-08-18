-- +goose Up
-- +goose StatementBegin

-- Table to track cleanup runs
CREATE TABLE cleanup_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at DATETIME NOT NULL,
    completed_at DATETIME,
    status VARCHAR(20) NOT NULL DEFAULT 'running', -- 'running', 'completed', 'failed', 'cancelled'
    error_message TEXT,
    step VARCHAR(50) NOT NULL DEFAULT 'starting', -- 'starting', 'remove_expired_keep_tags', 'cleanup_old_tags', 'mark_for_deletion', 'remove_recently_played', 'cleanup_media', 'completed'
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Table to track media items and their states during cleanup runs
CREATE TABLE cleanup_media_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cleanup_run_id INTEGER NOT NULL,
    jellyfin_id VARCHAR(255) NOT NULL,
    media_id VARCHAR(255) NOT NULL, -- sonarr-123 or radarr-456
    title VARCHAR(255) NOT NULL,
    media_type VARCHAR(10) NOT NULL, -- 'tv' or 'movie'
    year INTEGER,
    library VARCHAR(255),
    tmdb_id INTEGER,
    file_size INTEGER DEFAULT 0,
    requested_by VARCHAR(255),
    request_date DATETIME,

    -- State tracking
    action VARCHAR(50) NOT NULL, -- 'marked_for_deletion', 'deleted', 'kept', 'ignored', 'unmarked'
    action_timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deletion_date DATETIME, -- When it's scheduled to be deleted
    tags TEXT, -- JSON array of tags

    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (cleanup_run_id) REFERENCES cleanup_runs(id) ON DELETE CASCADE
);

-- Table to track cleanup run steps completion
CREATE TABLE cleanup_run_steps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cleanup_run_id INTEGER NOT NULL,
    step_name VARCHAR(50) NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    status VARCHAR(20) NOT NULL DEFAULT 'running', -- 'running', 'completed', 'failed'
    error_message TEXT,
    items_processed INTEGER DEFAULT 0,

    FOREIGN KEY (cleanup_run_id) REFERENCES cleanup_runs(id) ON DELETE CASCADE,
    UNIQUE(cleanup_run_id, step_name)
);

-- Indexes for performance
CREATE INDEX idx_cleanup_runs_status ON cleanup_runs(status);
CREATE INDEX idx_cleanup_runs_started_at ON cleanup_runs(started_at);
CREATE INDEX idx_cleanup_media_items_cleanup_run_id ON cleanup_media_items(cleanup_run_id);
CREATE INDEX idx_cleanup_media_items_media_id ON cleanup_media_items(media_id);
CREATE INDEX idx_cleanup_media_items_action ON cleanup_media_items(action);
CREATE INDEX idx_cleanup_media_items_jellyfin_id ON cleanup_media_items(jellyfin_id);
CREATE INDEX idx_cleanup_run_steps_cleanup_run_id ON cleanup_run_steps(cleanup_run_id);

-- Triggers to automatically update the updated_at timestamp
CREATE TRIGGER update_cleanup_runs_updated_at
    AFTER UPDATE ON cleanup_runs
    FOR EACH ROW
BEGIN
    UPDATE cleanup_runs SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS update_cleanup_runs_updated_at;
DROP INDEX IF EXISTS idx_cleanup_run_steps_cleanup_run_id;
DROP INDEX IF EXISTS idx_cleanup_media_items_jellyfin_id;
DROP INDEX IF EXISTS idx_cleanup_media_items_action;
DROP INDEX IF EXISTS idx_cleanup_media_items_media_id;
DROP INDEX IF EXISTS idx_cleanup_media_items_cleanup_run_id;
DROP INDEX IF EXISTS idx_cleanup_runs_started_at;
DROP INDEX IF EXISTS idx_cleanup_runs_status;
DROP TABLE IF EXISTS cleanup_run_steps;
DROP TABLE IF EXISTS cleanup_media_items;
DROP TABLE IF EXISTS cleanup_runs;

-- +goose StatementEnd
