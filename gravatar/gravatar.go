package gravatar

import (
	"crypto/md5"
	"fmt"
	"net/url"
	"strings"

	"github.com/jon4hz/jellysweep/config"
)

// GenerateURL generates a Gravatar URL for the given email address using the provided configuration.
// Returns an empty string if Gravatar is disabled or email is empty.
func GenerateURL(email string, cfg *config.GravatarConfig) string {
	if cfg == nil || !cfg.Enabled || email == "" {
		return ""
	}

	// Normalize email (trim whitespace and convert to lowercase)
	email = strings.TrimSpace(strings.ToLower(email))

	// Generate MD5 hash of the email
	hash := md5.Sum([]byte(email))
	hashStr := fmt.Sprintf("%x", hash)

	// Build Gravatar URL with parameters
	baseURL := fmt.Sprintf("https://www.gravatar.com/avatar/%s", hashStr)

	params := url.Values{}

	// Add default image parameter
	if cfg.DefaultImage != "" {
		params.Add("d", cfg.DefaultImage)
	}

	// Add rating parameter
	if cfg.Rating != "" {
		params.Add("r", cfg.Rating)
	}

	// Add size parameter
	if cfg.Size > 0 {
		params.Add("s", fmt.Sprintf("%d", cfg.Size))
	}

	// Append parameters to URL if any exist
	if len(params) > 0 {
		baseURL = baseURL + "?" + params.Encode()
	}

	return baseURL
}

// IsValidDefaultImage checks if the provided default image value is valid for Gravatar.
func IsValidDefaultImage(defaultImage string) bool {
	validDefaults := map[string]bool{
		"404":       true,
		"mp":        true,
		"identicon": true,
		"monsterid": true,
		"wavatar":   true,
		"retro":     true,
		"robohash":  true,
		"blank":     true,
	}
	return validDefaults[defaultImage]
}

// IsValidRating checks if the provided rating value is valid for Gravatar.
func IsValidRating(rating string) bool {
	validRatings := map[string]bool{
		"g":  true,
		"pg": true,
		"r":  true,
		"x":  true,
	}
	return validRatings[rating]
}

// IsValidSize checks if the provided size value is valid for Gravatar (1-2048 pixels).
func IsValidSize(size int) bool {
	return size >= 1 && size <= 2048
}
