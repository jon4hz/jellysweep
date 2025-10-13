package database

import (
	"context"
	"errors"
	"time"

	"github.com/charmbracelet/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MediaType represents the type of media, either TV show or Movie.
type MediaType string

const (
	// MediaTypeTV represents TV shows.
	MediaTypeTV MediaType = "tv"
	// MediaTypeMovie represents Movies.
	MediaTypeMovie MediaType = "movie"
)

// DiskUsageDeletePolicy represents the disk usage policy for media deletion.
type DiskUsageDeletePolicy struct {
	gorm.Model
	MediaID    uint      `gorm:"not null;index"`
	Threshold  float64   `gorm:"not null"` // Disk usage threshold percentage
	DeleteDate time.Time `gorm:"not null"` // Date when media should be deleted if threshold is exceeded
}

// DeletDBDeleteReasoneReason represents the reason why a media item was deleted from the database.
type DBDeleteReason string

const (
	// DBDeleteReasonDefault indicates the media was actually deleted in Jellyfin.
	DBDeleteReasonDefault DBDeleteReason = "default"
	// DBDeleteReasonStreamed indicates the media was deleted in the database only because it was streamed.
	DBDeleteReasonStreamed DBDeleteReason = "streamed"
)

// Media represents a media item in the database.
type Media struct {
	gorm.Model
	JellyfinID      string `gorm:"not null;uniqueIndex:idx_media_arr"`
	LibraryName     string
	ArrID           int32 `gorm:"not null;uniqueIndex:idx_media_arr"` // Sonarr or Radarr ID
	Title           string
	TmdbId          *int32
	TvdbId          *int32
	Year            int32
	FileSize        int64
	PosterURL       string
	MediaType       MediaType `gorm:"not null;uniqueIndex:idx_media_arr"`
	RequestedBy     string
	DefaultDeleteAt time.Time `gorm:"not null;index;uniqueIndex:idx_media_arr"`
	ProtectedUntil  *time.Time
	Unkeepable      bool
	// Reason why this item was deleted from the database.
	DBDeleteReason          DBDeleteReason
	DiskUsageDeletePolicies []DiskUsageDeletePolicy `gorm:"constraint:OnDelete:CASCADE;"`
	Request                 Request                 `gorm:"constraint:OnDelete:CASCADE;"`
}

func (c *Client) CreateMediaItems(ctx context.Context, mediaItems []Media) error {
	if len(mediaItems) == 0 {
		return errors.New("no media items to create")
	}
	result := c.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "arr_id"}, {Name: "jellyfin_id"}, {Name: "media_type"}, {Name: "default_delete_at"}},
		DoNothing: true,
	}).WithContext(ctx).Create(&mediaItems)
	if result.Error != nil {
		log.Error("failed to create media items", "error", result.Error)
	}
	return result.Error
}

func (c *Client) GetMediaItems(ctx context.Context) ([]Media, error) {
	var mediaItems []Media
	result := c.db.WithContext(ctx).Preload("DiskUsageDeletePolicies").Preload("Request").Find(&mediaItems)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get media items", "error", result.Error)
		return nil, result.Error
	}
	return mediaItems, nil
}

func (c *Client) GetMediaItemsByMediaType(ctx context.Context, mediaType MediaType) ([]Media, error) {
	var mediaItems []Media
	result := c.db.WithContext(ctx).Where("media_type = ?", mediaType).Find(&mediaItems)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get media items by type", "error", result.Error)
		return nil, result.Error
	}
	return mediaItems, nil
}
