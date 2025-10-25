package database

import (
	"context"

	"github.com/charmbracelet/log"
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
	MediaID uint          `gorm:"not null;index;unique"`
	Status  RequestStatus `gorm:"not null;default:'pending';index"`
	UserID  uint          `gorm:"not null;index"`
	User    User
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

func (c *Client) GetRequests(ctx context.Context) ([]Request, error) {
	var requests []Request
	result := c.db.WithContext(ctx).Find(&requests)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Error("failed to get requests", "error", result.Error)
		return nil, result.Error
	}
	return requests, nil
}

func (c *Client) UpdateRequestStatus(ctx context.Context, requestID uint, status RequestStatus) error {
	result := c.db.WithContext(ctx).Model(&Request{}).Where("id = ?", requestID).Update("status", status)
	if result.Error != nil {
		log.Error("failed to update request status", "error", result.Error)
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
