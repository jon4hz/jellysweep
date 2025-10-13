package database

import (
	"context"

	"gorm.io/gorm"
)

type RequestStatus string

const (
	RequestStatusPending     RequestStatus = "pending"
	RequestStatusUnavailable RequestStatus = "unavailable"
	RequestStatusApproved    RequestStatus = "approved"
	RequestStatusDenied      RequestStatus = "denied"
)

// Request represents a media keep request made by a user.
type Request struct {
	gorm.Model
	MediaID uint          `gorm:"not null;index;uniqueIndex:idx_request_media_user"`
	UserID  uint          `gorm:"not null;index;uniqueIndex:idx_request_media_user"`
	Status  RequestStatus `gorm:"not null;default:'pending';index"`
}

func (c *Client) CreateRequest(ctx context.Context, mediaID uint, userID uint) (*Request, error) {
	request := Request{
		MediaID: mediaID,
		UserID:  userID,
	}
	if err := c.db.WithContext(ctx).Create(&request).Error; err != nil {
		return nil, err
	}
	return &request, nil
}
