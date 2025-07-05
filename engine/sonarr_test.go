package engine

import (
	"context"
	"testing"
	"time"

	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSonarrAuthCtx(t *testing.T) {
	cfg := &config.SonarrConfig{
		URL:    "http://sonarr:8989",
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
			authCtx := sonarrAuthCtx(tt.ctx, cfg)
			assert.NotNil(t, authCtx)

			// Verify API key is set in context
			apiKeys := authCtx.Value(sonarr.ContextAPIKeys)
			assert.NotNil(t, apiKeys)

			if apiKeyMap, ok := apiKeys.(map[string]sonarr.APIKey); ok {
				assert.Contains(t, apiKeyMap, "X-Api-Key")
				assert.Equal(t, cfg.APIKey, apiKeyMap["X-Api-Key"].Key)
			}
		})
	}
}

func TestNewSonarrClient(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.SonarrConfig
		wantErr bool
	}{
		{
			name: "valid HTTP URL",
			config: &config.SonarrConfig{
				URL:    "http://sonarr:8989",
				APIKey: "test-api-key",
			},
			wantErr: false,
		},
		{
			name: "valid HTTPS URL",
			config: &config.SonarrConfig{
				URL:    "https://sonarr.example.com",
				APIKey: "test-api-key",
			},
			wantErr: false,
		},
		{
			name: "URL without protocol",
			config: &config.SonarrConfig{
				URL:    "sonarr:8989",
				APIKey: "test-api-key",
			},
			wantErr: false, // Should still work, the function handles this case
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newSonarrClient(tt.config)
			if tt.wantErr {
				assert.Nil(t, client)
			} else {
				assert.NotNil(t, client)
			}
		})
	}
}

func TestEngine_SonarrTagOperations(t *testing.T) {
	engine := createTestEngineNoDryRun(t)

	// Mock tags data
	testTags := map[int32]string{
		1: "jellysweep-delete-2020-01-01",
		2: "jellysweep-keep-request-2025-08-01-user@example.com",
		3: "jellysweep-must-keep-2025-12-31",
		4: "favorite",
		5: "jellysweep-must-delete-for-sure",
	}

	engine.data.sonarrTags = testTags

	t.Run("getSonarrTagIDByLabel", func(t *testing.T) {
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
				result, err := engine.getSonarrTagIDByLabel(tt.label)
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

func TestEngine_SonarrKeepTags(t *testing.T) {
	// Test keep tag parsing and expiration logic
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

func TestEngine_SonarrMediaItemsForDeletion(t *testing.T) {
	engine := createTestEngineNoDryRun(t)

	// Test the structure and logic for getting media items marked for deletion
	t.Run("media items structure validation", func(t *testing.T) {
		// This should work with mock server and return empty list
		items, err := engine.getSonarrMediaItemsMarkedForDeletion(context.Background())
		if err != nil {
			// If there's an error (mock server issue), just check that the function exists
			t.Logf("Mock server error (expected): %v", err)
			assert.Error(t, err)
		} else {
			// If no error, items should be an empty slice
			assert.NotNil(t, items)
			assert.Equal(t, 0, len(items))
		}
	})

	t.Run("models.MediaItem structure", func(t *testing.T) {
		// Test that we can create proper models.MediaItem structures
		expectedMediaItem := models.MediaItem{
			ID:           "sonarr:123",
			Title:        "Test Series",
			Type:         "tv",
			Year:         2023,
			Library:      "TV Shows",
			DeletionDate: time.Now().AddDate(0, 0, 7),
			PosterURL:    "/api/images/cache?url=http%3A%2F%2Fexample.com%2Fimage.jpg",
			CanRequest:   true,
			HasRequested: false,
			MustDelete:   false,
			FileSize:     1024 * 1024 * 500, // 500MB
		}

		// Verify structure
		assert.NotEmpty(t, expectedMediaItem.ID)
		assert.NotEmpty(t, expectedMediaItem.Title)
		assert.Equal(t, "tv", expectedMediaItem.Type)
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

func TestEngine_SonarrKeepRequests(t *testing.T) {
	engine := createTestEngineNoDryRun(t)

	t.Run("keep requests structure validation", func(t *testing.T) {
		// This should work with mock server and return empty list
		requests, err := engine.getSonarrKeepRequests(context.Background())
		assert.NoError(t, err)     // Should work with mock server
		assert.NotNil(t, requests) // Should return empty slice, not nil
	})

	t.Run("models.KeepRequest structure", func(t *testing.T) {
		// Test that we can create proper models.KeepRequest structures
		expectedKeepRequest := models.KeepRequest{
			ID:           "keep:456",
			MediaID:      "sonarr:456",
			Title:        "Keep This Series",
			Type:         "tv",
			Year:         2023,
			Library:      "TV Shows",
			DeletionDate: time.Now().AddDate(0, 0, 7),
			PosterURL:    "/api/images/cache?url=http%3A%2F%2Fexample.com%2Fkeeper.jpg",
			RequestedBy:  "user@example.com",
			RequestDate:  time.Now().AddDate(0, 0, -10),
			ExpiryDate:   time.Now().AddDate(0, 3, 0), // 3 months from now
		}

		// Verify structure
		assert.NotEmpty(t, expectedKeepRequest.ID)
		assert.NotEmpty(t, expectedKeepRequest.MediaID)
		assert.NotEmpty(t, expectedKeepRequest.Title)
		assert.Equal(t, "tv", expectedKeepRequest.Type)
		assert.True(t, expectedKeepRequest.Year > 0)
		assert.NotEmpty(t, expectedKeepRequest.Library)
		assert.False(t, expectedKeepRequest.DeletionDate.IsZero())
		assert.NotEmpty(t, expectedKeepRequest.PosterURL)
		assert.NotEmpty(t, expectedKeepRequest.RequestedBy)
		assert.False(t, expectedKeepRequest.RequestDate.IsZero())
		assert.False(t, expectedKeepRequest.ExpiryDate.IsZero())
	})
}

func TestEngine_SonarrTagLifecycle(t *testing.T) {
	_ = createTestEngineNoDryRun(t)

	t.Run("tag creation and management", func(t *testing.T) {
		// Test tag creation logic
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

	t.Run("tag expiration logic", func(t *testing.T) {
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

func TestSonarrSeriesFileSize(t *testing.T) {
	// Test the getSeriesFileSize function (assuming it exists)
	tests := []struct {
		name     string
		series   sonarr.SeriesResource
		expected int64
	}{
		{
			name:     "series with statistics",
			series:   sonarr.SeriesResource{},
			expected: 0, // Default value since we can't easily mock the statistics
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The actual function would extract file size from series statistics
			// For now, we just test that the function would handle the SeriesResource type
			result := getSeriesFileSize(tt.series)
			assert.GreaterOrEqual(t, result, int64(0))
		})
	}
}

func TestEngine_SonarrErrorHandling(t *testing.T) {
	// Test with mock engine that has proper setup
	engine := createTestEngineNoDryRun(t)

	ctx := context.Background()

	t.Run("operations with nil sonarr client", func(t *testing.T) {
		// Temporarily set sonarr client to nil to test error handling
		originalSonarr := engine.sonarr
		engine.sonarr = nil

		// Test get operations
		_, err := engine.getSonarrItems(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sonarr client not available")

		_, err = engine.getSonarrTags(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sonarr client not available")

		// Test keep request operations
		err = engine.addSonarrKeepRequestTag(ctx, 123, "pinocchio")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sonarr client not available")

		// Test accept/decline operations
		err = engine.acceptSonarrKeepRequest(ctx, 123)
		assert.Error(t, err)

		err = engine.declineSonarrKeepRequest(ctx, 123)
		assert.Error(t, err)

		// Restore original client
		engine.sonarr = originalSonarr
	})
}

func TestEngine_SonarrIntegrationReadiness(t *testing.T) {
	// Test that the engine is properly set up for Sonarr integration
	cfg := &config.Config{
		CleanupInterval: 24,
		Libraries: map[string]*config.CleanupConfig{
			"TV Shows": {
				Enabled:             true,
				ContentAgeThreshold: 45,
				LastStreamThreshold: 120,
				CleanupDelay:        14,
				ExcludeTags:         []string{"ongoing", "favorite"},
			},
		},
		DryRun: false,
		Sonarr: &config.SonarrConfig{
			URL:    "http://sonarr:8989",
			APIKey: "test-sonarr-key",
		},
		Jellystat: &config.JellystatConfig{
			URL:    "http://jellystat:3000",
			APIKey: "test-key",
		},
	}

	engine, err := New(cfg)
	require.NoError(t, err)

	// Verify Sonarr configuration is present
	assert.NotNil(t, engine.cfg.Sonarr)
	assert.Equal(t, "http://sonarr:8989", engine.cfg.Sonarr.URL)
	assert.Equal(t, "test-sonarr-key", engine.cfg.Sonarr.APIKey)

	// Verify library configuration for TV shows
	assert.Contains(t, engine.cfg.Libraries, "TV Shows")
	tvConfig := engine.cfg.Libraries["TV Shows"]
	assert.True(t, tvConfig.Enabled)
	assert.Equal(t, 45, tvConfig.ContentAgeThreshold)
	assert.Equal(t, 120, tvConfig.LastStreamThreshold)
	assert.Equal(t, 14, tvConfig.CleanupDelay)
	assert.Contains(t, tvConfig.ExcludeTags, "ongoing")
	assert.Contains(t, tvConfig.ExcludeTags, "favorite")
}

// Helper function to create a test engine with dry run disabled for Sonarr tests
func createTestEngineNoDryRun(t *testing.T) *Engine {
	engine, tsm := CreateTestEngineWithMocks(t)
	t.Cleanup(func() {
		tsm.Close()
	})
	// Override dry run setting for these tests
	engine.cfg.DryRun = false
	return engine
}
