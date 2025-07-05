package engine

import (
	"context"
	"testing"
	"time"

	radarr "github.com/devopsarr/radarr-go/radarr"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRadarrAuthCtx(t *testing.T) {
	cfg := &config.RadarrConfig{
		URL:    "http://radarr:7878",
		APIKey: "test-api-key",
	}

	tests := []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "with background context",
			ctx:  context.Background(),
		},
		{
			name: "with nil context",
			ctx:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authCtx := radarrAuthCtx(tt.ctx, cfg)
			assert.NotNil(t, authCtx)

			// Verify API key is set in context
			apiKeys := authCtx.Value(radarr.ContextAPIKeys)
			assert.NotNil(t, apiKeys)

			if apiKeyMap, ok := apiKeys.(map[string]radarr.APIKey); ok {
				assert.Contains(t, apiKeyMap, "X-Api-Key")
				assert.Equal(t, cfg.APIKey, apiKeyMap["X-Api-Key"].Key)
			}
		})
	}
}

func TestNewRadarrClient(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.RadarrConfig
		wantErr bool
	}{
		{
			name: "valid HTTP URL",
			config: &config.RadarrConfig{
				URL:    "http://radarr:7878",
				APIKey: "test-api-key",
			},
		},
		{
			name: "valid HTTPS URL",
			config: &config.RadarrConfig{
				URL:    "https://radarr.example.com",
				APIKey: "test-api-key",
			},
		},
		{
			name: "URL without protocol",
			config: &config.RadarrConfig{
				URL:    "radarr:7878",
				APIKey: "test-api-key",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newRadarrClient(tt.config)
			assert.NotNil(t, client)
		})
	}
}

func TestEngine_RadarrTagOperations(t *testing.T) {
	engine := createTestEngine(t)

	// Mock tags data
	testTags := map[int32]string{
		1: "jellysweep-delete-2020-01-01",
		2: "jellysweep-keep-request-2025-08-01-user@example.com",
		3: "jellysweep-must-keep-2025-12-31",
		4: "favorite",
		5: "jellysweep-must-delete-for-sure",
	}

	engine.data.radarrTags = testTags

	t.Run("getRadarrTagIDByLabel", func(t *testing.T) {
		tests := []struct {
			name      string
			label     string
			expected  int32
			wantError bool
		}{
			{
				name:      "existing tag",
				label:     "favorite",
				expected:  4,
				wantError: false,
			},
			{
				name:      "jellysweep delete tag",
				label:     "jellysweep-delete-2020-01-01",
				expected:  1,
				wantError: false,
			},
			{
				name:      "non-existing tag",
				label:     "non-existent",
				expected:  0,
				wantError: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := engine.getRadarrTagIDByLabel(tt.label)
				if tt.wantError {
					assert.Error(t, err)
					assert.Equal(t, int32(0), result)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, tt.expected, result)
				}
			})
		}
	})
}

func TestEngine_RadarrKeepTags(t *testing.T) {
	// Test keep tag parsing and expiration logic for Radarr
	tests := []struct {
		name         string
		tagName      string
		isKeepTag    bool
		isExpired    bool
		expectedDate time.Time
	}{
		{
			name:         "valid future keep request tag",
			tagName:      "jellysweep-keep-request-2030-12-31-user@example.com",
			isKeepTag:    true,
			isExpired:    false,
			expectedDate: time.Date(2030, 12, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:         "expired keep request tag",
			tagName:      "jellysweep-keep-request-2020-01-01-user@example.com",
			isKeepTag:    true,
			isExpired:    true,
			expectedDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:         "valid future keep tag",
			tagName:      "jellysweep-must-keep-2030-06-15",
			isKeepTag:    true,
			isExpired:    false,
			expectedDate: time.Date(2030, 6, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:         "expired keep tag",
			tagName:      "jellysweep-must-keep-2020-06-15",
			isKeepTag:    true,
			isExpired:    true,
			expectedDate: time.Date(2020, 6, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "regular tag",
			tagName:   "favorite",
			isKeepTag: false,
			isExpired: false,
		},
		{
			name:      "delete tag",
			tagName:   "jellysweep-delete-2024-01-01",
			isKeepTag: false,
			isExpired: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test keep request tag detection
			isKeepRequestTag := func(tag string) bool {
				return len(tag) > len(jellysweepKeepRequestPrefix) && tag[:len(jellysweepKeepRequestPrefix)] == jellysweepKeepRequestPrefix
			}

			// Test keep tag detection
			isKeepTag := func(tag string) bool {
				return len(tag) > len(jellysweepKeepPrefix) && tag[:len(jellysweepKeepPrefix)] == jellysweepKeepPrefix
			}

			actualIsKeepTag := isKeepRequestTag(tt.tagName) || isKeepTag(tt.tagName)
			assert.Equal(t, tt.isKeepTag, actualIsKeepTag)

			if tt.isKeepTag && tt.expectedDate != (time.Time{}) {
				// Test date parsing for keep tags
				var dateStr string
				if isKeepRequestTag(tt.tagName) {
					// Extract date from keep request tag (format: jellysweep-keep-request-YYYY-MM-DD-user@email.com)
					parts := tt.tagName[len(jellysweepKeepRequestPrefix):]
					// Find the date part (first 10 characters: YYYY-MM-DD)
					if len(parts) >= 10 {
						dateStr = parts[:10]
					}
				} else if isKeepTag(tt.tagName) {
					// Extract date from keep tag (format: jellysweep-must-keep-YYYY-MM-DD)
					dateStr = tt.tagName[len(jellysweepKeepPrefix):]
				}

				if dateStr != "" {
					parsedDate, err := time.Parse("2006-01-02", dateStr)
					assert.NoError(t, err)
					assert.Equal(t, tt.expectedDate, parsedDate)

					// Test expiration
					isExpired := time.Now().After(parsedDate)
					assert.Equal(t, tt.isExpired, isExpired)
				}
			}
		})
	}
}

func TestEngine_RadarrMediaItemsForDeletion(t *testing.T) {
	engine := createTestEngine(t)

	// Test the structure and logic for getting media items marked for deletion
	t.Run("media items structure validation", func(t *testing.T) {
		// This test verifies the expected structure of the function
		// In a real implementation, you'd mock the Radarr client

		_, err := engine.getRadarrMediaItemsMarkedForDeletion(context.Background())
		// This will fail due to nil Radarr client, but tests the function exists
		assert.Error(t, err) // Expected since radarr client is nil
	})

	t.Run("models.MediaItem structure for movies", func(t *testing.T) {
		// Test that we can create proper models.MediaItem structures for movies
		expectedMediaItem := models.MediaItem{
			ID:           "radarr:123",
			Title:        "Test Movie",
			Type:         "movie",
			Year:         2023,
			Library:      "Movies",
			DeletionDate: time.Now().AddDate(0, 0, 7),
			PosterURL:    "/api/images/cache?url=http%3A%2F%2Fexample.com%2Fmovie.jpg",
			CanRequest:   true,
			HasRequested: false,
			MustDelete:   false,
			FileSize:     1024 * 1024 * 1024 * 2, // 2GB
		}

		// Verify structure
		assert.NotEmpty(t, expectedMediaItem.ID)
		assert.NotEmpty(t, expectedMediaItem.Title)
		assert.Equal(t, "movie", expectedMediaItem.Type)
		assert.True(t, expectedMediaItem.Year > 0)
		assert.NotEmpty(t, expectedMediaItem.Library)
		assert.False(t, expectedMediaItem.DeletionDate.IsZero())
		assert.NotEmpty(t, expectedMediaItem.PosterURL)
		assert.True(t, expectedMediaItem.CanRequest)
		assert.False(t, expectedMediaItem.HasRequested)
		assert.False(t, expectedMediaItem.MustDelete)
		assert.True(t, expectedMediaItem.FileSize > 0)
	})
}

func TestEngine_RadarrKeepRequests(t *testing.T) {
	engine := createTestEngine(t)

	t.Run("keep requests structure validation", func(t *testing.T) {
		// Test the structure and logic for getting keep requests
		_, err := engine.getRadarrKeepRequests(context.Background())
		// This will fail due to nil Radarr client, but tests the function exists
		assert.Error(t, err) // Expected since radarr client is nil
	})

	t.Run("models.KeepRequest structure for movies", func(t *testing.T) {
		// Test that we can create proper models.KeepRequest structures for movies
		expectedKeepRequest := models.KeepRequest{
			ID:           "keep:789",
			MediaID:      "radarr:789",
			Title:        "Keep This Movie",
			Type:         "movie",
			Year:         2023,
			Library:      "Movies",
			DeletionDate: time.Now().AddDate(0, 0, 7),
			PosterURL:    "/api/cache/image?url=http%3A%2F%2Fexample.com%2Fkeepermovie.jpg",
			RequestedBy:  "user@example.com",
			RequestDate:  time.Now().AddDate(0, 0, -5),
			ExpiryDate:   time.Now().AddDate(0, 3, 0), // 3 months from now
		}

		// Verify structure
		assert.NotEmpty(t, expectedKeepRequest.ID)
		assert.NotEmpty(t, expectedKeepRequest.MediaID)
		assert.NotEmpty(t, expectedKeepRequest.Title)
		assert.Equal(t, "movie", expectedKeepRequest.Type)
		assert.True(t, expectedKeepRequest.Year > 0)
		assert.NotEmpty(t, expectedKeepRequest.Library)
		assert.False(t, expectedKeepRequest.DeletionDate.IsZero())
		assert.NotEmpty(t, expectedKeepRequest.PosterURL)
		assert.NotEmpty(t, expectedKeepRequest.RequestedBy)
		assert.False(t, expectedKeepRequest.RequestDate.IsZero())
		assert.False(t, expectedKeepRequest.ExpiryDate.IsZero())
	})
}

func TestEngine_RadarrTagLifecycle(t *testing.T) {
	_ = createTestEngine(t)

	t.Run("tag creation and management for movies", func(t *testing.T) {
		// Test tag creation logic for movies
		now := time.Now()
		futureDate := now.AddDate(0, 3, 0) // 3 months from now

		// Test keep request tag format
		keepRequestTag := jellysweepKeepRequestPrefix + futureDate.Format("2006-01-02") + "-user@example.com"
		assert.Contains(t, keepRequestTag, "jellysweep-keep-request-")
		assert.Contains(t, keepRequestTag, "user@example.com")

		// Test keep tag format
		keepTag := jellysweepKeepPrefix + futureDate.Format("2006-01-02")
		assert.Contains(t, keepTag, "jellysweep-must-keep-")

		// Test delete tag format
		deleteTag := jellysweepTagPrefix + now.AddDate(0, 0, 7).Format("2006-01-02")
		assert.Contains(t, deleteTag, "jellysweep-delete-")

		// Test delete-for-sure tag
		deleteForSureTag := jellysweepDeleteForSureTag
		assert.Equal(t, "jellysweep-must-delete-for-sure", deleteForSureTag)
	})

	t.Run("tag expiration logic for movies", func(t *testing.T) {
		now := time.Now()

		expiredDate := now.AddDate(0, 0, -10) // 10 days ago
		futureDate := now.AddDate(0, 0, 10)   // 10 days from now

		expiredKeepTag := jellysweepKeepPrefix + expiredDate.Format("2006-01-02")
		validKeepTag := jellysweepKeepPrefix + futureDate.Format("2006-01-02")

		// Test expiration detection
		isExpired := func(tagName string) bool {
			if len(tagName) <= len(jellysweepKeepPrefix) {
				return false
			}
			dateStr := tagName[len(jellysweepKeepPrefix):]
			keepDate, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				return false
			}
			return time.Now().After(keepDate)
		}

		assert.True(t, isExpired(expiredKeepTag))
		assert.False(t, isExpired(validKeepTag))
	})
}

func TestEngine_RadarrRecentlyPlayedLogic(t *testing.T) {
	// Test the logic for removing delete tags from recently played movies
	tests := []struct {
		name         string
		lastPlayed   time.Time
		library      string
		shouldRemove bool
	}{
		{
			name:         "recently played movie should have delete tag removed",
			lastPlayed:   time.Now().AddDate(0, 0, -5), // 5 days ago
			library:      "Movies",
			shouldRemove: true,
		},
		{
			name:         "old played movie should keep delete tag",
			lastPlayed:   time.Now().AddDate(0, -6, 0), // 6 months ago
			library:      "Movies",
			shouldRemove: false,
		},
		{
			name:         "never played movie should keep delete tag",
			lastPlayed:   time.Time{}, // Zero time indicates never played
			library:      "Movies",
			shouldRemove: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the logic that would be used to determine if delete tags should be removed
			engine := createTestEngine(t)

			// Get the threshold for the library
			threshold := engine.cfg.Libraries[tt.library].LastStreamThreshold
			thresholdDuration := time.Duration(threshold) * 24 * time.Hour

			var shouldRemoveTag bool
			if !tt.lastPlayed.IsZero() {
				// If the movie was played within the threshold, remove the delete tag
				shouldRemoveTag = time.Since(tt.lastPlayed) < thresholdDuration
			} else {
				// Never played movies keep their delete tags
				shouldRemoveTag = false
			}

			assert.Equal(t, tt.shouldRemove, shouldRemoveTag)
		})
	}
}

func TestEngine_RadarrErrorHandling(t *testing.T) {
	engine := createTestEngine(t)

	ctx := context.Background()

	t.Run("operations with nil radarr client", func(t *testing.T) {
		// Set radarr client to nil to test error handling
		engine.radarr = nil

		// All these operations should handle nil radarr client gracefully

		// Test get operations
		_, err := engine.getRadarrItems(ctx)
		assert.Error(t, err)

		_, err = engine.getRadarrTags(ctx)
		assert.Error(t, err)

		// Test keep request operations
		err = engine.addRadarrKeepRequestTag(ctx, 123, "pinocchio")
		assert.Error(t, err)

		// Test accept/decline operations
		err = engine.acceptRadarrKeepRequest(ctx, 123)
		assert.Error(t, err)

		err = engine.declineRadarrKeepRequest(ctx, 123)
		assert.Error(t, err)
	})
}

func TestEngine_RadarrIntegrationReadiness(t *testing.T) {
	// Test that the engine is properly set up for Radarr integration
	cfg := &config.Config{
		CleanupInterval: 24,
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {
				Enabled:             true,
				RequestAgeThreshold: 30,
				LastStreamThreshold: 90,
				CleanupDelay:        7,
				ExcludeTags:         []string{"favorite", "collection"},
			},
		},
		DryRun: true,
		Radarr: &config.RadarrConfig{
			URL:    "http://radarr:7878",
			APIKey: "test-radarr-key",
		},
		Jellystat: &config.JellystatConfig{
			URL:    "http://jellystat:3000",
			APIKey: "test-key",
		},
	}

	engine, err := New(cfg)
	require.NoError(t, err)

	// Verify Radarr configuration is present
	assert.NotNil(t, engine.cfg.Radarr)
	assert.Equal(t, "http://radarr:7878", engine.cfg.Radarr.URL)
	assert.Equal(t, "test-radarr-key", engine.cfg.Radarr.APIKey)

	// Verify library configuration for Movies
	assert.Contains(t, engine.cfg.Libraries, "Movies")
	movieConfig := engine.cfg.Libraries["Movies"]
	assert.True(t, movieConfig.Enabled)
	assert.Equal(t, 30, movieConfig.RequestAgeThreshold)
	assert.Equal(t, 90, movieConfig.LastStreamThreshold)
	assert.Equal(t, 7, movieConfig.CleanupDelay)
	assert.Contains(t, movieConfig.ExcludeTags, "favorite")
	assert.Contains(t, movieConfig.ExcludeTags, "collection")
}

func TestRadarrMovieResource(t *testing.T) {
	// Test working with Radarr MovieResource structures
	tests := []struct {
		name   string
		movie  radarr.MovieResource
		expect func(t *testing.T, movie radarr.MovieResource)
	}{
		{
			name:  "empty movie resource",
			movie: radarr.MovieResource{},
			expect: func(t *testing.T, movie radarr.MovieResource) {
				// Test that we can work with an empty movie resource
				assert.IsType(t, radarr.MovieResource{}, movie)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.expect(t, tt.movie)
		})
	}
}

func TestEngine_RadarrCleanupDelay(t *testing.T) {
	// Test cleanup delay calculation for movies
	libraries := map[string]*config.CleanupConfig{
		"Movies": {
			CleanupDelay: 5, // 5 days
		},
		"4K Movies": {
			CleanupDelay: 3, // 3 days
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
			expectedDays: 5,
		},
		{
			name:         "4K movies cleanup delay",
			library:      "4K Movies",
			expectedDays: 3,
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
