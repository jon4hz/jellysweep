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
	Username     string `gorm:"uniqueIndex;not null"`
	UserSettings UserSettings
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

func (c *Client) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var user User
	if err := c.db.WithContext(ctx).Preload("UserSettings").Where("username = ?", username).First(&user).Error; err != nil {
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
