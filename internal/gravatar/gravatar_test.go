package gravatar

import (
	"testing"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestGenerateURL(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		config   *config.GravatarConfig
		expected string
	}{
		{
			name:     "disabled gravatar",
			email:    "test@example.com",
			config:   &config.GravatarConfig{Enabled: false},
			expected: "",
		},
		{
			name:     "nil config",
			email:    "test@example.com",
			config:   nil,
			expected: "",
		},
		{
			name:     "empty email",
			email:    "",
			config:   &config.GravatarConfig{Enabled: true},
			expected: "",
		},
		{
			name:  "basic enabled config",
			email: "test@example.com",
			config: &config.GravatarConfig{
				Enabled: true,
			},
			expected: "https://www.gravatar.com/avatar/973dfe463ec85785f5f95af5ba3906eedb2d931c24e69824a89ea65dba4e813b",
		},
		{
			name:  "config with default image",
			email: "test@example.com",
			config: &config.GravatarConfig{
				Enabled:      true,
				DefaultImage: "mp",
			},
			expected: "https://www.gravatar.com/avatar/973dfe463ec85785f5f95af5ba3906eedb2d931c24e69824a89ea65dba4e813b?d=mp",
		},
		{
			name:  "config with all options",
			email: "TEST@EXAMPLE.COM", // Test case normalization
			config: &config.GravatarConfig{
				Enabled:      true,
				DefaultImage: "identicon",
				Rating:       "pg",
				Size:         120,
			},
			expected: "https://www.gravatar.com/avatar/973dfe463ec85785f5f95af5ba3906eedb2d931c24e69824a89ea65dba4e813b?d=identicon&r=pg&s=120",
		},
		{
			name:  "email with whitespace",
			email: "  test@example.com  ",
			config: &config.GravatarConfig{
				Enabled: true,
			},
			expected: "https://www.gravatar.com/avatar/973dfe463ec85785f5f95af5ba3906eedb2d931c24e69824a89ea65dba4e813b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateURL(tt.email, tt.config)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsValidDefaultImage(t *testing.T) {
	validImages := []string{"404", "mp", "identicon", "monsterid", "wavatar", "retro", "robohash", "blank"}
	invalidImages := []string{"invalid", "", "test", "404x", "MP"}

	for _, img := range validImages {
		assert.True(t, IsValidDefaultImage(img), "Expected %s to be valid", img)
	}

	for _, img := range invalidImages {
		assert.False(t, IsValidDefaultImage(img), "Expected %s to be invalid", img)
	}
}

func TestIsValidRating(t *testing.T) {
	validRatings := []string{"g", "pg", "r", "x"}
	invalidRatings := []string{"invalid", "", "G", "PG", "nc17"}

	for _, rating := range validRatings {
		assert.True(t, IsValidRating(rating), "Expected %s to be valid", rating)
	}

	for _, rating := range invalidRatings {
		assert.False(t, IsValidRating(rating), "Expected %s to be invalid", rating)
	}
}

func TestIsValidSize(t *testing.T) {
	validSizes := []int{1, 80, 120, 200, 2048}
	invalidSizes := []int{0, -1, 2049, 3000}

	for _, size := range validSizes {
		assert.True(t, IsValidSize(size), "Expected %d to be valid", size)
	}

	for _, size := range invalidSizes {
		assert.False(t, IsValidSize(size), "Expected %d to be invalid", size)
	}
}
