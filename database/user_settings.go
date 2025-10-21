package database

import "gorm.io/gorm"

// UserSettings holds all settings specific to a user.
type UserSettings struct {
	gorm.Model
	UserID        uint `gorm:"uniqueIndex;not null"`
	EmailSettings EmailSettings
}

// EmailSettings holds the email configuration for a user.
type EmailSettings struct {
	gorm.Model
	Enabled        bool
	Email          string `gorm:"not null;unique"`
	UserSettingsID uint   `gorm:"uniqueIndex;not null"`
}
