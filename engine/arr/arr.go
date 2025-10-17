package arr

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/devopsarr/radarr-go/radarr"
	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/api/models"
	jellyfin "github.com/sj14/jellyfin-go/api"
)

type MediaItem struct {
	JellyfinID     string
	LibraryName    string // Jellyfin library name this item belongs to
	SeriesResource sonarr.SeriesResource
	MovieResource  radarr.MovieResource
	Title          string
	TmdbId         int32
	Year           int32
	Tags           []string
	MediaType      models.MediaType
	// User information for the person who requested this media
	RequestedBy string // User email or username
}

type Arrer interface {
	GetItems(ctx context.Context, jellyfinItems []JellyfinItem) ([]MediaItem, error)
	DeleteMedia(ctx context.Context, arrID int32, title string) error

	// Bulk tag resets/cleanup
	ResetTags(ctx context.Context, additionalTags []string) error
	CleanupAllTags(ctx context.Context, additionalTags []string) error

	ResetAllTagsAndAddIgnore(ctx context.Context, id int32) error

	// History methods for getting import dates
	GetItemAddedDate(ctx context.Context, itemID int32) (*time.Time, error)
}

type JellyfinItem struct {
	jellyfin.BaseItemDto
	ParentLibraryName string `json:"parentLibraryName,omitempty"`
}

var ErrRequestAlreadyProcessed = errors.New("request already processed")

// GetCachedImageURL converts a direct image URL to a cached URL.
func GetCachedImageURL(imageURL string) string {
	if imageURL == "" {
		return ""
	}
	// Encode the original URL and return a cache endpoint URL
	encoded := url.QueryEscape(imageURL)
	return fmt.Sprintf("/api/images/cache?url=%s", encoded)
}
