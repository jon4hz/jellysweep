package database

import (
	"context"
	"errors"
	"time"

	"github.com/charmbracelet/log"
	"gorm.io/gorm"
)

// DiskUsageDeletePolicy represents the disk usage policy for media deletion.
type DiskUsageDeletePolicy struct {
	gorm.Model
	MediaID    uint      `gorm:"not null;index"`
	Threshold  float64   `gorm:"not null"` // Disk usage threshold percentage
	DeleteDate time.Time `gorm:"not null"` // Date when media should be deleted if threshold is exceeded
}

// Media represents a media item in the database.
type Media struct {
	gorm.Model
	JellyfinID      string `gorm:"not null;uniqueIndex:idx_media_arr"`
	LibraryName     string
	ArrID           int32 `gorm:"not null;uniqueIndex:idx_media_arr"` // Sonarr or Radarr ID
	Title           string
	TmdbId          *int32 `gorm:"index"`
	TvdbId          *int32 `gorm:"index"`
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
	result := c.db.WithContext(ctx).Create(&mediaItems)
	if result.Error != nil {
		log.Error("failed to create media items", "error", result.Error)
	}
	return result.Error
}

func (c *Client) GetMediaItemByID(ctx context.Context, id uint) (*Media, error) {
	var mediaItem Media
	result := c.db.WithContext(ctx).Preload("DiskUsageDeletePolicies").Preload("Request").First(&mediaItem, id)
	if result.Error != nil {
		log.Error("failed to get media item by ID", "error", result.Error)
		return nil, result.Error
	}
	return &mediaItem, nil
}

// GetMediaItems retrieves all unprotected media items from the database.
func (c *Client) GetMediaItems(ctx context.Context, includeProtected bool) ([]Media, error) {
	tx := c.db.WithContext(ctx).
		Preload("DiskUsageDeletePolicies").
		Preload("Request")

	if !includeProtected {
		tx = tx.Where("protected_until IS NULL OR protected_until < ?", time.Now())
	}

	var mediaItems []Media
	result := tx.Find(&mediaItems)

	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get media items", "error", result.Error)
		return nil, result.Error
	}
	return mediaItems, nil
}

func (c *Client) GetMediaItemsByMediaType(ctx context.Context, mediaType MediaType) ([]Media, error) {
	var mediaItems []Media
	result := c.db.WithContext(ctx).
		Where("media_type = ? AND (protected_until IS NULL OR protected_until < ?)", mediaType, time.Now()).
		Find(&mediaItems)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get media items by type", "error", result.Error)
		return nil, result.Error
	}
	return mediaItems, nil
}

func (c *Client) GetMediaWithPendingRequest(ctx context.Context) ([]Media, error) {
	var mediaItems []Media
	result := c.db.WithContext(ctx).
		Preload("Request").
		Where("requests.status = ? AND (protected_until IS NULL OR protected_until < ?)", RequestStatusPending, time.Now()).
		Joins("JOIN requests ON requests.media_id = media.id").
		Find(&mediaItems)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get media items with requests", "error", result.Error)
		return nil, result.Error
	}
	return mediaItems, nil
}

func (c *Client) GetMediaExpiredProtection(ctx context.Context, asOf time.Time) ([]Media, error) {
	var mediaItems []Media
	result := c.db.WithContext(ctx).
		Where("protected_until IS NOT NULL AND protected_until <= ?", asOf).
		Find(&mediaItems)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get media items with expired protection", "error", result.Error)
		return nil, result.Error
	}
	return mediaItems, nil
}

func (c *Client) GetDeletedMediaByTMDBID(ctx context.Context, tmdbID int32) ([]Media, error) {
	var mediaItems []Media
	result := c.db.WithContext(ctx).
		Unscoped().
		Where("deleted_at IS NOT NULL AND tmdb_id = ?", tmdbID).
		Find(&mediaItems)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get deleted media by TMDB ID", "error", result.Error)
		return nil, result.Error
	}
	return mediaItems, nil
}

func (c *Client) GetDeletedMediaByTVDBID(ctx context.Context, tvdbID int32) ([]Media, error) {
	var mediaItems []Media
	result := c.db.WithContext(ctx).
		Unscoped().
		Where("deleted_at IS NOT NULL AND tvdb_id = ?", tvdbID).
		Find(&mediaItems)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get deleted media by TVDB ID", "error", result.Error)
		return nil, result.Error
	}
	return mediaItems, nil
}

func (c *Client) SetMediaProtectedUntil(ctx context.Context, mediaID uint, protectedUntil *time.Time) error {
	result := c.db.WithContext(ctx).Model(&Media{}).
		Where("id = ?", mediaID).
		Updates(Media{ProtectedUntil: protectedUntil, Unkeepable: false})
	if result.Error != nil {
		log.Error("failed to set media protected until", "error", result.Error)
		return result.Error
	}
	return nil
}

func (c *Client) MarkMediaAsUnkeepable(ctx context.Context, mediaID uint) error {
	result := c.db.WithContext(ctx).Model(&Media{}).
		Where("id = ?", mediaID).
		Updates(Media{Unkeepable: true, ProtectedUntil: nil})
	if result.Error != nil {
		log.Error("failed to mark media as unkeepable", "error", result.Error)
		return result.Error
	}
	return nil
}

func (c *Client) DeleteMediaItem(ctx context.Context, mediaID uint, deleteReason DBDeleteReason) error {
	err := c.db.WithContext(ctx).Model(&Media{}).
		Where("id = ?", mediaID).
		Update("db_delete_reason", deleteReason).Error
	if err != nil {
		log.Error("failed to set media delete reason", "error", err)
		return err
	}

	result := c.db.WithContext(ctx).Delete(&Media{}, mediaID)
	if result.Error != nil {
		log.Error("failed to delete media item", "error", result.Error)
		return result.Error
	}
	return nil
}
