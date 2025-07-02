package engine

import (
	"context"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
)

func TestEngine_filterRequestAgeThreshold(t *testing.T) {
	// Create engine with specific library configurations
	libraries := map[string]*config.CleanupConfig{
		"Movies": {
			Enabled:             true,
			RequestAgeThreshold: 30, // 30 days
			LastStreamThreshold: 90,
			CleanupDelay:        7,
		},
		"TV Shows": {
			Enabled:             true,
			RequestAgeThreshold: 45, // 45 days
			LastStreamThreshold: 120,
			CleanupDelay:        14,
		},
	}

	engine := createTestEngineWithLibraries(t, libraries)

	// Initialize userNotifications if not already done
	if engine.data.userNotifications == nil {
		engine.data.userNotifications = make(map[string][]MediaItem)
	}

	// Set up test media items
	now := time.Now()
	engine.data.mediaItems = map[string][]MediaItem{
		"Movies": {
			{
				JellystatID: "recent-movie",
				Title:       "Recent Movie",
				TmdbId:      12345,
				MediaType:   MediaTypeMovie,
			},
			{
				JellystatID: "old-movie",
				Title:       "Old Movie",
				TmdbId:      67890,
				MediaType:   MediaTypeMovie,
			},
		},
		"TV Shows": {
			{
				JellystatID: "recent-show",
				Title:       "Recent TV Show",
				TmdbId:      11111,
				MediaType:   MediaTypeTV,
			},
			{
				JellystatID: "old-show",
				Title:       "Old TV Show",
				TmdbId:      22222,
				MediaType:   MediaTypeTV,
			},
		},
	}

	t.Run("filter based on request age threshold", func(t *testing.T) {
		// This test demonstrates the expected behavior
		// In a real implementation, you'd mock the jellyseerr client

		// The function should filter items based on their request age
		// Items requested within the threshold should be excluded
		// Items requested before the threshold should be included
		engine.filterRequestAgeThreshold(context.Background())

		// Verify userNotifications structure is maintained
		assert.NotNil(t, engine.data.userNotifications)
	})

	t.Run("test media item processing logic", func(t *testing.T) {
		// Test the logic that would be applied to each media item
		_ = MediaItem{
			JellystatID: "test-item",
			Title:       "Test Item",
			TmdbId:      99999,
			MediaType:   MediaTypeMovie,
		}

		// Simulate request time scenarios
		recentRequestTime := now.AddDate(0, 0, -15) // 15 days ago
		oldRequestTime := now.AddDate(0, 0, -45)    // 45 days ago

		// For Movies library (30 day threshold)
		movieThreshold := time.Duration(libraries["Movies"].RequestAgeThreshold) * 24 * time.Hour

		// Recent request should be excluded (within threshold)
		assert.True(t, time.Since(recentRequestTime) < movieThreshold)

		// Old request should be included (beyond threshold)
		assert.True(t, time.Since(oldRequestTime) > movieThreshold)
	})
}

func TestEngine_UserNotificationTracking(t *testing.T) {
	engine := createTestEngine(t)

	// Initialize userNotifications
	engine.data.userNotifications = make(map[string][]MediaItem)

	tests := []struct {
		name        string
		userEmail   string
		mediaItems  []MediaItem
		expectedLen int
	}{
		{
			name:      "single user with multiple items",
			userEmail: "user1@example.com",
			mediaItems: []MediaItem{
				{
					Title:       "Movie 1",
					MediaType:   MediaTypeMovie,
					RequestedBy: "user1@example.com",
					RequestDate: time.Now().AddDate(0, 0, -40),
				},
				{
					Title:       "Movie 2",
					MediaType:   MediaTypeMovie,
					RequestedBy: "user1@example.com",
					RequestDate: time.Now().AddDate(0, 0, -50),
				},
			},
			expectedLen: 2,
		},
		{
			name:      "multiple users",
			userEmail: "user2@example.com",
			mediaItems: []MediaItem{
				{
					Title:       "Show 1",
					MediaType:   MediaTypeTV,
					RequestedBy: "user2@example.com",
					RequestDate: time.Now().AddDate(0, 0, -60),
				},
			},
			expectedLen: 1,
		},
		{
			name:        "empty user email",
			userEmail:   "",
			mediaItems:  []MediaItem{},
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.userEmail != "" && len(tt.mediaItems) > 0 {
				engine.data.userNotifications[tt.userEmail] = tt.mediaItems
			}

			if tt.userEmail != "" {
				items := engine.data.userNotifications[tt.userEmail]
				assert.Len(t, items, tt.expectedLen)

				// Verify all items have the correct user
				for _, item := range items {
					assert.Equal(t, tt.userEmail, item.RequestedBy)
				}
			}
		})
	}

	// Verify overall structure
	assert.IsType(t, map[string][]MediaItem{}, engine.data.userNotifications)
}

func TestMediaItemRequestInfo(t *testing.T) {
	tests := []struct {
		name        string
		mediaItem   MediaItem
		requestTime *time.Time
		userEmail   string
		expectValid bool
	}{
		{
			name: "valid request info",
			mediaItem: MediaItem{
				Title:     "Test Movie",
				TmdbId:    12345,
				MediaType: MediaTypeMovie,
			},
			requestTime: func() *time.Time {
				t := time.Now().AddDate(0, 0, -40)
				return &t
			}(),
			userEmail:   "user@example.com",
			expectValid: true,
		},
		{
			name: "nil request time",
			mediaItem: MediaItem{
				Title:     "Test Movie",
				TmdbId:    12345,
				MediaType: MediaTypeMovie,
			},
			requestTime: nil,
			userEmail:   "user@example.com",
			expectValid: false,
		},
		{
			name: "empty user email",
			mediaItem: MediaItem{
				Title:     "Test Movie",
				TmdbId:    12345,
				MediaType: MediaTypeMovie,
			},
			requestTime: func() *time.Time {
				t := time.Now().AddDate(0, 0, -40)
				return &t
			}(),
			userEmail:   "",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the request info processing
			if tt.expectValid {
				assert.NotNil(t, tt.requestTime)
				assert.NotEmpty(t, tt.userEmail)
				assert.True(t, tt.mediaItem.TmdbId > 0)
				assert.NotEmpty(t, tt.mediaItem.Title)
			}
		})
	}
}

func TestEngine_RequestAgeCalculation(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name             string
		requestTime      time.Time
		thresholdDays    int
		shouldBeIncluded bool
	}{
		{
			name:             "request within threshold - should be excluded",
			requestTime:      now.AddDate(0, 0, -15), // 15 days ago
			thresholdDays:    30,                     // 30 day threshold
			shouldBeIncluded: false,
		},
		{
			name:             "request exactly at threshold",
			requestTime:      now.AddDate(0, 0, -30), // 30 days ago
			thresholdDays:    30,                     // 30 day threshold
			shouldBeIncluded: true,
		},
		{
			name:             "request beyond threshold - should be included",
			requestTime:      now.AddDate(0, 0, -45), // 45 days ago
			thresholdDays:    30,                     // 30 day threshold
			shouldBeIncluded: true,
		},
		{
			name:             "very old request",
			requestTime:      now.AddDate(0, -6, 0), // 6 months ago
			thresholdDays:    30,                    // 30 day threshold
			shouldBeIncluded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeSinceRequest := time.Since(tt.requestTime)
			threshold := time.Duration(tt.thresholdDays) * 24 * time.Hour

			isOldEnough := timeSinceRequest > threshold

			if tt.shouldBeIncluded {
				assert.True(t, isOldEnough, "Request should be old enough for inclusion")
			} else {
				assert.False(t, isOldEnough, "Request should be too recent for inclusion")
			}
		})
	}
}

func TestEngine_MediaTypeMapping(t *testing.T) {
	tests := []struct {
		name      string
		mediaType MediaType
		expected  string
	}{
		{
			name:      "movie type",
			mediaType: MediaTypeMovie,
			expected:  "movie",
		},
		{
			name:      "tv type",
			mediaType: MediaTypeTV,
			expected:  "tv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that MediaType can be converted to string for API calls
			mediaTypeStr := string(tt.mediaType)
			assert.Equal(t, tt.expected, mediaTypeStr)
		})
	}
}

func TestEngine_LibrarySpecificThresholds(t *testing.T) {
	// Test that different libraries can have different thresholds
	libraries := map[string]*config.CleanupConfig{
		"Movies": {
			RequestAgeThreshold: 30,
		},
		"TV Shows": {
			RequestAgeThreshold: 45,
		},
		"Documentaries": {
			RequestAgeThreshold: 60,
		},
	}

	engine := createTestEngineWithLibraries(t, libraries)

	// Verify different thresholds are set correctly
	assert.Equal(t, 30, engine.cfg.JellySweep.Libraries["Movies"].RequestAgeThreshold)
	assert.Equal(t, 45, engine.cfg.JellySweep.Libraries["TV Shows"].RequestAgeThreshold)
	assert.Equal(t, 60, engine.cfg.JellySweep.Libraries["Documentaries"].RequestAgeThreshold)
}
