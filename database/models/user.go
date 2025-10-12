package models

import "gorm.io/gorm"

// User represents a user in the database.
// It contains a unique username and associated user settings.
// It explicitly doesn't track if a user is an admin.
// The admin status is always determined during the login process and stored in the session.
type User struct {
	gorm.Model
	Username     string `gorm:"uniqueIndex;not null"`
	UserSettings UserSettings
}
