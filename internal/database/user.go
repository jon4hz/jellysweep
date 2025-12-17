package database

import (
	"context"

	"github.com/charmbracelet/log"
	"gorm.io/gorm"
)

// User represents a user in the database.
// It contains a unique username and associated user settings.
// It explicitly doesn't track if a user is an admin.
// The admin status is always determined during the login process and stored in the session.
type User struct {
	gorm.Model
	Username        string `gorm:"uniqueIndex;not null"`
	UserSettings    UserSettings
	UserPermissions UserPermissions
	Requests        []Request `gorm:"constraint:OnDelete:SET NULL;"`
}

// UserPermissions represents permissions for a user.
type UserPermissions struct {
	gorm.Model
	UserID          uint `gorm:"uniqueIndex;not null"`
	HasAutoApproval bool `gorm:"default:false"` // Whether user's keep requests are automatically approved
}

func (c *Client) CreateUser(ctx context.Context, username string) (*User, error) {
	user := User{
		Username: username,
	}
	if err := c.db.WithContext(ctx).Create(&user).Error; err != nil {
		log.Error("failed to create user", "error", err)
		return nil, err
	}
	return &user, nil
}

func (c *Client) GetUserByID(ctx context.Context, id uint) (*User, error) {
	var user User
	if err := c.db.WithContext(ctx).Preload("UserPermissions").First(&user, id).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			log.Error("failed to get user by ID", "error", err)
		}
		return nil, err
	}
	return &user, nil
}

func (c *Client) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var user User
	if err := c.db.WithContext(ctx).Preload("UserSettings").Preload("UserPermissions").Where("username = ?", username).First(&user).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			log.Error("failed to get user by username", "error", err)
		}
		return nil, err
	}
	return &user, nil
}

func (c *Client) GetOrCreateUser(ctx context.Context, username string) (*User, error) {
	user, err := c.GetUserByUsername(ctx, username)
	if err == nil {
		return user, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	user, err = c.CreateUser(ctx, username)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (c *Client) GetAllUsers(ctx context.Context) ([]User, error) {
	var users []User
	if err := c.db.WithContext(ctx).Preload("UserSettings").Preload("UserPermissions").Find(&users).Error; err != nil {
		log.Error("failed to get all users", "error", err)
		return nil, err
	}
	return users, nil
}

func (c *Client) UpdateUserAutoApproval(ctx context.Context, userID uint, hasAutoApproval bool) error {
	// First, check if UserPermissions exists for this user, if not create it
	var permissions UserPermissions
	err := c.db.WithContext(ctx).Where("user_id = ?", userID).First(&permissions).Error
	if err == gorm.ErrRecordNotFound {
		// Create new permissions record
		permissions = UserPermissions{
			UserID:          userID,
			HasAutoApproval: hasAutoApproval,
		}
		if err := c.db.WithContext(ctx).Create(&permissions).Error; err != nil {
			log.Error("failed to create user permissions", "error", err)
			return err
		}
		return nil
	} else if err != nil {
		log.Error("failed to get user permissions", "error", err)
		return err
	}

	// Update existing permissions
	result := c.db.WithContext(ctx).Model(&permissions).Update("has_auto_approval", hasAutoApproval)
	if result.Error != nil {
		log.Error("failed to update user auto approval", "error", result.Error)
		return result.Error
	}
	return nil
}
