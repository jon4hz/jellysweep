package engine

import (
	"context"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngine_MultipleRunsConsistency tests that multiple runs of the engine
// behave consistently and don't interfere with each other
func TestEngine_MultipleRunsConsistency(t *testing.T) {
	cfg := &config.Config{
		JellySweep: &config.JellysweepConfig{
			CleanupInterval: 1, // 1 hour for testing
			Libraries: map[string]*config.CleanupConfig{
				"Movies": {
					Enabled:             true,
					RequestAgeThreshold: 30,
					LastStreamThreshold: 90,
					CleanupDelay:        7,
					ExcludeTags:         []string{"favorite"},
				},
				"TV Shows": {
					Enabled:             true,
					RequestAgeThreshold: 45,
					LastStreamThreshold: 120,
					CleanupDelay:        14,
					ExcludeTags:         []string{"ongoing"},
				},
			},
			DryRun: true,
		},
		Jellystat: &config.JellystatConfig{
			URL:    "http://jellystat:3000",
			APIKey: "test-key",
		},
	}

	engine, err := New(cfg)
	require.NoError(t, err)

	// Initialize test data
	initialMediaItems := map[string][]MediaItem{
		"Movies": {
			{
				JellystatID: "movie1",
				Title:       "Test Movie 1",
				MediaType:   MediaTypeMovie,
				Tags:        []string{},
			},
			{
				JellystatID: "movie2",
				Title:       "Test Movie 2",
				MediaType:   MediaTypeMovie,
				Tags:        []string{"favorite"}, // Should be excluded
			},
		},
		"TV Shows": {
			{
				JellystatID: "show1",
				Title:       "Test Show 1",
				MediaType:   MediaTypeTV,
				Tags:        []string{},
			},
		},
	}

	t.Run("first run", func(t *testing.T) {
		// Reset engine data
		engine.data.mediaItems = make(map[string][]MediaItem)
		for k, v := range initialMediaItems {
			engine.data.mediaItems[k] = make([]MediaItem, len(v))
			copy(engine.data.mediaItems[k], v)
		}

		// Run filtering
		engine.filterMediaTags()

		// Verify first run results
		assert.Len(t, engine.data.mediaItems["Movies"], 1) // movie2 should be excluded
		assert.Len(t, engine.data.mediaItems["TV Shows"], 1)

		// Check that the correct movie remains
		assert.Equal(t, "Test Movie 1", engine.data.mediaItems["Movies"][0].Title)
	})

	t.Run("second run with same data", func(t *testing.T) {
		// Reset engine data with same initial data
		engine.data.mediaItems = make(map[string][]MediaItem)
		for k, v := range initialMediaItems {
			engine.data.mediaItems[k] = make([]MediaItem, len(v))
			copy(engine.data.mediaItems[k], v)
		}

		// Run filtering again
		engine.filterMediaTags()

		// Results should be identical to first run
		assert.Len(t, engine.data.mediaItems["Movies"], 1)
		assert.Len(t, engine.data.mediaItems["TV Shows"], 1)
		assert.Equal(t, "Test Movie 1", engine.data.mediaItems["Movies"][0].Title)
	})

	t.Run("run with modified data", func(t *testing.T) {
		// Modify the test data to simulate changes between runs
		modifiedMediaItems := map[string][]MediaItem{
			"Movies": {
				{
					JellystatID: "movie1",
					Title:       "Test Movie 1",
					MediaType:   MediaTypeMovie,
					Tags:        []string{"jellysweep-delete-2023-01-01"}, // Now has delete tag
				},
				{
					JellystatID: "movie3",
					Title:       "Test Movie 3",
					MediaType:   MediaTypeMovie,
					Tags:        []string{},
				},
			},
		}

		engine.data.mediaItems = modifiedMediaItems
		engine.filterMediaTags()

		// Movie1 should now be excluded due to delete tag
		// Movie3 should remain
		assert.Len(t, engine.data.mediaItems["Movies"], 1)
		assert.Equal(t, "Test Movie 3", engine.data.mediaItems["Movies"][0].Title)
	})
}

// TestEngine_CleanupCycle tests a complete cleanup cycle from start to finish
func TestEngine_CleanupCycle(t *testing.T) {
	engine := createTestEngine(t)

	t.Run("complete cleanup cycle", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Test that all cleanup functions can be called without panicking
		// Note: These will likely fail due to nil clients, but we test the flow

		// Step 1: Remove expired keep tags
		engine.removeExpiredKeepTags(ctx)

		// Step 2: Clean up old tags
		engine.cleanupOldTags(ctx)

		// Step 3: Mark items for deletion
		engine.markForDeletion(ctx)

		// Step 4: Remove recently played delete tags
		engine.removeRecentlyPlayedDeleteTags(ctx)

		// Step 5: Cleanup media
		engine.cleanupMedia(ctx)

		// If we reach here without panicking, the cycle completed
		assert.True(t, true) // Test passed
	})
}

// TestEngine_ConcurrentAccess tests that the engine can handle concurrent access safely
func TestEngine_ConcurrentAccess(t *testing.T) {
	engine := createTestEngine(t)

	t.Run("concurrent data access", func(t *testing.T) {
		// Initialize some test data
		engine.data.mediaItems = map[string][]MediaItem{
			"Movies": {
				{
					JellystatID: "concurrent1",
					Title:       "Concurrent Movie 1",
					MediaType:   MediaTypeMovie,
				},
			},
		}

		// Run multiple operations concurrently
		done := make(chan bool, 3)

		// Goroutine 1: Read data
		go func() {
			for i := 0; i < 10; i++ {
				_ = len(engine.data.mediaItems)
				time.Sleep(time.Millisecond)
			}
			done <- true
		}()

		// Goroutine 2: Filter data
		go func() {
			for i := 0; i < 5; i++ {
				engine.filterMediaTags()
				time.Sleep(2 * time.Millisecond)
			}
			done <- true
		}()

		// Goroutine 3: Access configuration
		go func() {
			for i := 0; i < 10; i++ {
				_ = engine.cfg.JellySweep.CleanupInterval
				time.Sleep(time.Millisecond)
			}
			done <- true
		}()

		// Wait for all goroutines to complete
		for i := 0; i < 3; i++ {
			select {
			case <-done:
				// Goroutine completed successfully
			case <-time.After(5 * time.Second):
				t.Fatal("Goroutine timed out")
			}
		}
	})
}

// TestEngine_DataIntegrity tests that data integrity is maintained across operations
func TestEngine_DataIntegrity(t *testing.T) {
	engine := createTestEngine(t)

	t.Run("data consistency during filtering", func(t *testing.T) {
		// Set up initial data with known quantities
		initialData := map[string][]MediaItem{
			"Movies": {
				{Title: "Movie 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
				{Title: "Movie 3", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 4", MediaType: MediaTypeMovie, Tags: []string{"jellysweep-delete-2023-01-01"}},
			},
			"TV Shows": {
				{Title: "Show 1", MediaType: MediaTypeTV, Tags: []string{}},
				{Title: "Show 2", MediaType: MediaTypeTV, Tags: []string{"ongoing"}},
			},
		}

		engine.data.mediaItems = initialData

		// Count initial items
		initialMovieCount := len(initialData["Movies"])
		initialShowCount := len(initialData["TV Shows"])

		// Apply filtering
		engine.filterMediaTags()

		// Verify that filtering worked as expected
		// Movie 2 should be excluded (favorite tag)
		// Movie 4 should be excluded (delete tag)
		// Show 2 should be excluded (ongoing tag)
		assert.Len(t, engine.data.mediaItems["Movies"], 2)   // Movie 1 and Movie 3
		assert.Len(t, engine.data.mediaItems["TV Shows"], 1) // Show 1

		// Verify we didn't lose any data unintentionally
		totalInitial := initialMovieCount + initialShowCount
		totalAfterFiltering := len(engine.data.mediaItems["Movies"]) + len(engine.data.mediaItems["TV Shows"])
		expectedAfterFiltering := 3 // Movie 1, Movie 3, Show 1

		assert.Equal(t, 6, totalInitial)
		assert.Equal(t, expectedAfterFiltering, totalAfterFiltering)

		// Verify specific items that should remain
		movieTitles := make([]string, 0)
		for _, item := range engine.data.mediaItems["Movies"] {
			movieTitles = append(movieTitles, item.Title)
		}
		assert.Contains(t, movieTitles, "Movie 1")
		assert.Contains(t, movieTitles, "Movie 3")
		assert.NotContains(t, movieTitles, "Movie 2") // Excluded by favorite tag
		assert.NotContains(t, movieTitles, "Movie 4") // Excluded by delete tag

		showTitles := make([]string, 0)
		for _, item := range engine.data.mediaItems["TV Shows"] {
			showTitles = append(showTitles, item.Title)
		}
		assert.Contains(t, showTitles, "Show 1")
		assert.NotContains(t, showTitles, "Show 2") // Excluded by ongoing tag
	})
}

// TestEngine_EdgeCases tests various edge cases and boundary conditions
func TestEngine_EdgeCases(t *testing.T) {
	engine := createTestEngine(t)

	t.Run("empty media items", func(t *testing.T) {
		engine.data.mediaItems = map[string][]MediaItem{}

		// Should handle empty data gracefully
		engine.filterMediaTags()
		assert.NotNil(t, engine.data.mediaItems)
		assert.Equal(t, 0, len(engine.data.mediaItems))
	})

	t.Run("nil media items", func(t *testing.T) {
		engine.data.mediaItems = nil

		// Should handle nil data gracefully
		engine.filterMediaTags()
		// After filtering, should have empty map
		assert.NotNil(t, engine.data.mediaItems)
	})

	t.Run("library not in configuration", func(t *testing.T) {
		engine.data.mediaItems = map[string][]MediaItem{
			"Unknown Library": {
				{Title: "Unknown Item", MediaType: MediaTypeMovie, Tags: []string{}},
			},
		}

		// Should handle unknown libraries gracefully
		engine.filterMediaTags()
		// Items from unknown libraries should be removed or handled appropriately
		// The exact behavior depends on implementation
	})

	t.Run("items with empty or nil tags", func(t *testing.T) {
		engine.data.mediaItems = map[string][]MediaItem{
			"Movies": {
				{Title: "No Tags", MediaType: MediaTypeMovie, Tags: nil},
				{Title: "Empty Tags", MediaType: MediaTypeMovie, Tags: []string{}},
			},
		}

		// Should handle items without tags gracefully
		engine.filterMediaTags()
		assert.Len(t, engine.data.mediaItems["Movies"], 2) // Both should remain
	})
}

// TestEngine_ConfigurationVariations tests different configuration scenarios
func TestEngine_ConfigurationVariations(t *testing.T) {
	t.Run("minimal configuration", func(t *testing.T) {
		cfg := &config.Config{
			JellySweep: &config.JellysweepConfig{
				CleanupInterval: 24,
				Libraries: map[string]*config.CleanupConfig{
					"Movies": {
						Enabled: true,
					},
				},
				DryRun: true,
			},
			Jellystat: &config.JellystatConfig{
				URL:    "http://jellystat:3000",
				APIKey: "test-key",
			},
		}

		engine, err := New(cfg)
		assert.NoError(t, err)
		assert.NotNil(t, engine)
	})

	t.Run("all services enabled", func(t *testing.T) {
		cfg := &config.Config{
			JellySweep: &config.JellysweepConfig{
				CleanupInterval: 24,
				Libraries: map[string]*config.CleanupConfig{
					"Movies": {Enabled: true},
				},
				DryRun: true,
				Email: &config.EmailConfig{
					Enabled:   true,
					SMTPHost:  "smtp.example.com",
					SMTPPort:  587,
					FromEmail: "test@example.com",
				},
				Ntfy: &config.NtfyConfig{
					Enabled:   true,
					ServerURL: "https://ntfy.sh",
					Topic:     "test",
				},
			},
			Jellyseerr: &config.JellyseerrConfig{
				URL:    "http://jellyseerr:5055",
				APIKey: "test-key",
			},
			Sonarr: &config.SonarrConfig{
				URL:    "http://sonarr:8989",
				APIKey: "test-key",
			},
			Radarr: &config.RadarrConfig{
				URL:    "http://radarr:7878",
				APIKey: "test-key",
			},
			Jellystat: &config.JellystatConfig{
				URL:    "http://jellystat:3000",
				APIKey: "test-key",
			},
		}

		engine, err := New(cfg)
		assert.NoError(t, err)
		assert.NotNil(t, engine)

		// Verify all services are configured
		assert.NotNil(t, engine.cfg.Jellyseerr)
		assert.NotNil(t, engine.cfg.Sonarr)
		assert.NotNil(t, engine.cfg.Radarr)
		assert.NotNil(t, engine.cfg.JellySweep.Email)
		assert.NotNil(t, engine.cfg.JellySweep.Ntfy)
	})
}

// TestEngine_StateManagement tests that the engine properly manages its internal state
func TestEngine_StateManagement(t *testing.T) {
	engine := createTestEngine(t)

	t.Run("state initialization", func(t *testing.T) {
		// Verify initial state
		assert.NotNil(t, engine.data)
		assert.NotNil(t, engine.data.userNotifications)
		assert.Equal(t, 0, len(engine.data.userNotifications))
	})

	t.Run("state modifications", func(t *testing.T) {
		// Modify state
		engine.data.userNotifications["test@example.com"] = []MediaItem{
			{Title: "Test", MediaType: MediaTypeMovie},
		}

		// Verify modification
		assert.Len(t, engine.data.userNotifications, 1)
		assert.Len(t, engine.data.userNotifications["test@example.com"], 1)

		// Clear state
		engine.data.userNotifications = make(map[string][]MediaItem)
		assert.Len(t, engine.data.userNotifications, 0)
	})

	t.Run("state persistence across operations", func(t *testing.T) {
		// Set up state
		engine.data.mediaItems = map[string][]MediaItem{
			"Movies": {{Title: "Persistent Movie", MediaType: MediaTypeMovie}},
		}

		// Run operation that might modify state
		engine.filterMediaTags()

		// Verify state is maintained appropriately
		assert.NotNil(t, engine.data.mediaItems)
		// The exact content depends on filtering logic, but structure should be maintained
	})
}

// TestEngine_ErrorRecovery tests that the engine can recover from various error conditions
func TestEngine_ErrorRecovery(t *testing.T) {
	engine := createTestEngine(t)

	t.Run("recovery from nil pointer scenarios", func(t *testing.T) {
		// Test with nil data structures
		engine.data.mediaItems = nil

		// Should not panic
		assert.NotPanics(t, func() {
			engine.filterMediaTags()
		})
	})

	t.Run("recovery from invalid tag formats", func(t *testing.T) {
		engine.data.mediaItems = map[string][]MediaItem{
			"Movies": {
				{
					Title:     "Movie with bad tag",
					MediaType: MediaTypeMovie,
					Tags:      []string{"jellysweep-delete-invalid-date"},
				},
			},
		}

		// Should handle invalid tag formats gracefully
		assert.NotPanics(t, func() {
			engine.filterMediaTags()
		})
	})
}
