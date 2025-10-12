package models

import (
	"time"

	"gorm.io/gorm"
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

// Media represents a media item in the database.
type Media struct {
	gorm.Model
	JellyfinID              string `gorm:"uniqueIndex;not null"`
	LibraryName             string
	ArrID                   int32 `gorm:"not null"` // Sonarr or Radarr ID
	Title                   string
	TmdbId                  *int32
	TvdbId                  *int32
	Year                    int32
	MediaType               MediaType
	RequestedBy             string
	DefaultDeleteAt         time.Time
	DiskUsageDeletePolicies []DiskUsageDeletePolicy `gorm:"constraint:OnDelete:CASCADE;"`
}
