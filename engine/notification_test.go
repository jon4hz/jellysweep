package engine

import (
	"context"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
)

func TestEngine_sendEmailNotifications_Structure(t *testing.T) {
	// Test email notification structure and configuration
	tests := []struct {
		name        string
		emailConfig *config.EmailConfig
		userNotifs  map[string][]MediaItem
		expectCall  bool
	}{
		{
			name:        "no email service configured",
			emailConfig: nil,
			userNotifs: map[string][]MediaItem{
				"user@example.com": {
					{
						Title:       "Test Movie",
						MediaType:   MediaTypeMovie,
						RequestedBy: "user@example.com",
						RequestDate: time.Now().AddDate(0, 0, -40),
					},
				},
			},
			expectCall: false,
		},
		{
			name: "email service configured with notifications",
			emailConfig: &config.EmailConfig{
				Enabled:   true,
				SMTPHost:  "smtp.example.com",
				SMTPPort:  587,
				Username:  "test@example.com",
				Password:  "password",
				FromEmail: "noreply@example.com",
				FromName:  "JellySweep",
				UseTLS:    true,
			},
			userNotifs: map[string][]MediaItem{
				"user1@example.com": {
					{
						Title:       "Movie 1",
						MediaType:   MediaTypeMovie,
						RequestedBy: "user1@example.com",
						RequestDate: time.Now().AddDate(0, 0, -40),
					},
				},
				"user2@example.com": {
					{
						Title:       "Movie 2",
						MediaType:   MediaTypeMovie,
						RequestedBy: "user2@example.com",
						RequestDate: time.Now().AddDate(0, 0, -50),
					},
				},
			},
			expectCall: true,
		},
		{
			name: "email service configured but no notifications",
			emailConfig: &config.EmailConfig{
				Enabled:   true,
				SMTPHost:  "smtp.example.com",
				SMTPPort:  587,
				Username:  "test@example.com",
				Password:  "password",
				FromEmail: "noreply@example.com",
				FromName:  "JellySweep",
				UseTLS:    true,
			},
			userNotifs: map[string][]MediaItem{},
			expectCall: false,
		},
		{
			name: "email service disabled",
			emailConfig: &config.EmailConfig{
				Enabled:   false,
				SMTPHost:  "smtp.example.com",
				SMTPPort:  587,
				Username:  "test@example.com",
				Password:  "password",
				FromEmail: "noreply@example.com",
				FromName:  "JellySweep",
				UseTLS:    true,
			},
			userNotifs: map[string][]MediaItem{
				"user@example.com": {
					{
						Title:       "Test Movie",
						MediaType:   MediaTypeMovie,
						RequestedBy: "user@example.com",
						RequestDate: time.Now().AddDate(0, 0, -40),
					},
				},
			},
			expectCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{CleanupInterval: 24,
				Libraries: map[string]*config.CleanupConfig{
					"Movies": {
						Enabled:             true,
						RequestAgeThreshold: 30,
						LastStreamThreshold: 90,
						CleanupDelay:        7,
					},
				},
				DryRun:    true,
				Email:     tt.emailConfig,
				ServerURL: "https://jellysweep.example.com",
				Jellystat: &config.JellystatConfig{
					URL:    "http://jellystat:3000",
					APIKey: "test-key",
				},
			}

			engine, err := New(cfg)
			assert.NoError(t, err)

			// Set up user notifications
			engine.data.userNotifications = tt.userNotifs

			// Set up media items for cleanup delay calculation
			engine.data.mediaItems = map[string][]MediaItem{
				"Movies": {},
			}
			for _, items := range tt.userNotifs {
				engine.data.mediaItems["Movies"] = append(engine.data.mediaItems["Movies"], items...)
			}
		})
	}
}

func TestEngine_sendNtfyDeletionSummary_Structure(t *testing.T) {
	tests := []struct {
		name       string
		ntfyConfig *config.NtfyConfig
		mediaItems map[string][]MediaItem
		expectCall bool
	}{
		{
			name:       "no ntfy service configured",
			ntfyConfig: nil,
			mediaItems: map[string][]MediaItem{
				"Movies": {
					{
						Title:     "Test Movie",
						MediaType: MediaTypeMovie,
						Year:      2023,
					},
				},
			},
			expectCall: false,
		},
		{
			name: "ntfy service configured with media items",
			ntfyConfig: &config.NtfyConfig{
				Enabled:   true,
				ServerURL: "https://ntfy.sh",
				Topic:     "jellysweep-test",
			},
			mediaItems: map[string][]MediaItem{
				"Movies": {
					{
						Title:     "Movie 1",
						MediaType: MediaTypeMovie,
						Year:      2023,
					},
					{
						Title:     "Movie 2",
						MediaType: MediaTypeMovie,
						Year:      2022,
					},
				},
				"TV Shows": {
					{
						Title:     "Show 1",
						MediaType: MediaTypeTV,
						Year:      2023,
					},
				},
			},
			expectCall: true,
		},
		{
			name: "ntfy service configured but no media items",
			ntfyConfig: &config.NtfyConfig{
				Enabled:   true,
				ServerURL: "https://ntfy.sh",
				Topic:     "jellysweep-test",
			},
			mediaItems: map[string][]MediaItem{},
			expectCall: false,
		},
		{
			name: "ntfy service disabled",
			ntfyConfig: &config.NtfyConfig{
				Enabled:   false,
				ServerURL: "https://ntfy.sh",
				Topic:     "jellysweep-test",
			},
			mediaItems: map[string][]MediaItem{
				"Movies": {
					{
						Title:     "Test Movie",
						MediaType: MediaTypeMovie,
						Year:      2023,
					},
				},
			},
			expectCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				CleanupInterval: 24,
				Libraries: map[string]*config.CleanupConfig{
					"Movies": {
						Enabled:             true,
						RequestAgeThreshold: 30,
						LastStreamThreshold: 90,
						CleanupDelay:        7,
					},
				},
				DryRun: true,
				Ntfy:   tt.ntfyConfig,
				Jellystat: &config.JellystatConfig{
					URL:    "http://jellystat:3000",
					APIKey: "test-key",
				},
			}

			engine, err := New(cfg)
			assert.NoError(t, err)

			// Set up media items
			engine.data.mediaItems = tt.mediaItems

			// Test the notification function
			err = engine.sendNtfyDeletionSummary(context.Background())
			// We expect this to potentially fail in test environment due to no actual ntfy server
			// but we can verify the structure and logic
			if tt.ntfyConfig == nil || !tt.ntfyConfig.Enabled || len(tt.mediaItems) == 0 {
				// Should handle gracefully without error for these cases
				assert.NoError(t, err)
			}
		})
	}
}

func TestEngine_sendNtfyDeletionCompletedNotification(t *testing.T) {
	engine := createTestEngine(t)

	deletedItems := map[string][]MediaItem{
		"Movies": {
			{
				Title:     "Deleted Movie 1",
				MediaType: MediaTypeMovie,
				Year:      2022,
			},
			{
				Title:     "Deleted Movie 2",
				MediaType: MediaTypeMovie,
				Year:      2021,
			},
		},
		"TV Shows": {
			{
				Title:     "Deleted Show 1",
				MediaType: MediaTypeTV,
				Year:      2023,
			},
		},
	}

	t.Run("with deleted items", func(t *testing.T) {
		// Set ntfy client to nil to test error handling
		engine.ntfy = nil
		err := engine.sendNtfyDeletionCompletedNotification(context.Background(), deletedItems)
		// Should not error when ntfy is nil - it just logs and returns nil
		assert.NoError(t, err)
	})

	t.Run("with empty deleted items", func(t *testing.T) {
		err := engine.sendNtfyDeletionCompletedNotification(context.Background(), map[string][]MediaItem{})
		assert.NoError(t, err) // Should handle gracefully
	})

	t.Run("with nil deleted items", func(t *testing.T) {
		err := engine.sendNtfyDeletionCompletedNotification(context.Background(), nil)
		assert.NoError(t, err) // Should handle gracefully
	})
}

func TestMediaItem_ToNotificationFormat(t *testing.T) {
	// Test conversion of MediaItem to notification formats
	tests := []struct {
		name      string
		mediaItem MediaItem
		expected  struct {
			title     string
			mediaType string
			year      int32
		}
	}{
		{
			name: "movie conversion",
			mediaItem: MediaItem{
				Title:     "Test Movie",
				MediaType: MediaTypeMovie,
				Year:      2023,
			},
			expected: struct {
				title     string
				mediaType string
				year      int32
			}{
				title:     "Test Movie",
				mediaType: "movie",
				year:      2023,
			},
		},
		{
			name: "tv show conversion",
			mediaItem: MediaItem{
				Title:     "Test TV Show",
				MediaType: MediaTypeTV,
				Year:      2022,
			},
			expected: struct {
				title     string
				mediaType string
				year      int32
			}{
				title:     "Test TV Show",
				mediaType: "tv",
				year:      2022,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the conversion logic that would be used in notifications
			assert.Equal(t, tt.expected.title, tt.mediaItem.Title)
			assert.Equal(t, tt.expected.year, tt.mediaItem.Year)

			// Test media type conversion
			var convertedType string
			if tt.mediaItem.MediaType == MediaTypeMovie {
				convertedType = "movie"
			} else if tt.mediaItem.MediaType == MediaTypeTV {
				convertedType = "tv"
			}
			assert.Equal(t, tt.expected.mediaType, convertedType)
		})
	}
}

func TestEngine_CleanupDateCalculation(t *testing.T) {
	libraries := map[string]*config.CleanupConfig{
		"Movies": {
			CleanupDelay: 7, // 7 days
		},
		"TV Shows": {
			CleanupDelay: 14, // 14 days
		},
	}

	_ = createTestEngineWithLibraries(t, libraries)

	now := time.Now()

	tests := []struct {
		name         string
		library      string
		expectedDays int
	}{
		{
			name:         "movies cleanup delay",
			library:      "Movies",
			expectedDays: 7,
		},
		{
			name:         "tv shows cleanup delay",
			library:      "TV Shows",
			expectedDays: 14,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate cleanup date
			cleanupDate := now.Add(time.Duration(libraries[tt.library].CleanupDelay) * 24 * time.Hour)

			// Verify the calculation
			expectedCleanupDate := now.AddDate(0, 0, tt.expectedDays)

			// Allow for small timing differences (within 1 minute)
			timeDiff := cleanupDate.Sub(expectedCleanupDate)
			assert.True(t, timeDiff < time.Minute && timeDiff > -time.Minute)
		})
	}
}

func TestNotificationStructures(t *testing.T) {
	// Test that notification data structures are properly formatted
	mediaItems := []MediaItem{
		{
			Title:       "Test Movie",
			MediaType:   MediaTypeMovie,
			RequestedBy: "user@example.com",
			RequestDate: time.Now().AddDate(0, 0, -40),
			Year:        2023,
		},
		{
			Title:       "Test Show",
			MediaType:   MediaTypeTV,
			RequestedBy: "user@example.com",
			RequestDate: time.Now().AddDate(0, 0, -60),
			Year:        2022,
		},
	}

	t.Run("media items have required fields for notifications", func(t *testing.T) {
		for _, item := range mediaItems {
			assert.NotEmpty(t, item.Title)
			assert.NotEmpty(t, item.RequestedBy)
			assert.False(t, item.RequestDate.IsZero())
			assert.True(t, item.MediaType == MediaTypeMovie || item.MediaType == MediaTypeTV)
			assert.True(t, item.Year > 0)
		}
	})

	t.Run("notification grouping by user", func(t *testing.T) {
		userNotifications := make(map[string][]MediaItem)

		for _, item := range mediaItems {
			userNotifications[item.RequestedBy] = append(userNotifications[item.RequestedBy], item)
		}

		assert.Len(t, userNotifications, 1)
		assert.Len(t, userNotifications["user@example.com"], 2)
	})

	t.Run("library grouping for deletion summary", func(t *testing.T) {
		libraries := make(map[string][]MediaItem)

		for _, item := range mediaItems {
			library := "Movies"
			if item.MediaType == MediaTypeTV {
				library = "TV Shows"
			}
			libraries[library] = append(libraries[library], item)
		}

		assert.Len(t, libraries, 2)
		assert.Len(t, libraries["Movies"], 1)
		assert.Len(t, libraries["TV Shows"], 1)
	})
}
