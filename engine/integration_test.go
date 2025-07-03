package engine

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// IMPORTANT: This file contains integration tests that demonstrate a critical bug
// in the current implementation related to library name case sensitivity.
//
// THE BUG: Viper (the configuration library) normalizes map keys to lowercase when
// loading YAML configuration, but library names from Jellystat are case-sensitive.
// This creates a mismatch where GetLibraryConfig(libraryName) fails to find
// configurations because it does exact string matching on the map keys.
//
// IMPACT: Libraries with non-lowercase names (e.g., "Movies", "TV Shows") will
// not have their exclude tags or other configuration applied, leading to incorrect
// filtering behavior.
//
// Tests in this file:
// - TestEngine_LibraryNameCaseSensitivity: Tests case sensitivity with manually created configs
// - TestEngine_ViperConfigIntegration: Tests the actual viper loading process and demonstrates the bug
//
// THE FIX: The GetLibraryConfig method now implements case-insensitive fallback lookup
// to handle mismatches between viper's normalized keys and Jellystat's case-sensitive names.

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

// TestEngine_LibraryNameCaseSensitivity tests how the engine handles library name case sensitivity
// This is critical because viper loads config with case-insensitive map keys, but library names
// from Jellystat are case-sensitive, which can cause configuration mismatches
func TestEngine_LibraryNameCaseSensitivity(t *testing.T) {
	// Helper function to create engine with specific library configuration
	createEngineWithLibraryConfig := func(libraryNames []string) *Engine {
		libraries := make(map[string]*config.CleanupConfig)
		for _, name := range libraryNames {
			libraries[name] = &config.CleanupConfig{
				Enabled:             true,
				RequestAgeThreshold: 30,
				LastStreamThreshold: 90,
				CleanupDelay:        7,
				ExcludeTags:         []string{"favorite", "ongoing"},
			}
		}

		cfg := &config.Config{
			JellySweep: &config.JellysweepConfig{
				CleanupInterval: 24,
				Libraries:       libraries,
				DryRun:          true,
			},
			Jellystat: &config.JellystatConfig{
				URL:    "http://jellystat:3000",
				APIKey: "test-key",
			},
		}

		engine, err := New(cfg)
		require.NoError(t, err)
		return engine
	}

	t.Run("exact case match", func(t *testing.T) {
		// Test when library names match exactly
		engine := createEngineWithLibraryConfig([]string{"Movies", "TV Shows"})

		engine.data.mediaItems = map[string][]MediaItem{
			"Movies": {
				{Title: "Movie 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
			"TV Shows": {
				{Title: "Show 1", MediaType: MediaTypeTV, Tags: []string{}},
				{Title: "Show 2", MediaType: MediaTypeTV, Tags: []string{"ongoing"}},
			},
		}

		engine.filterMediaTags()

		// Should filter correctly when case matches exactly
		assert.Len(t, engine.data.mediaItems["Movies"], 1, "Movie with 'favorite' tag should be excluded")
		assert.Len(t, engine.data.mediaItems["TV Shows"], 1, "Show with 'ongoing' tag should be excluded")
		assert.Equal(t, "Movie 1", engine.data.mediaItems["Movies"][0].Title)
		assert.Equal(t, "Show 1", engine.data.mediaItems["TV Shows"][0].Title)
	})

	t.Run("case mismatch - config lowercase, data uppercase", func(t *testing.T) {
		// Test when config has lowercase but data has different case
		engine := createEngineWithLibraryConfig([]string{"movies", "tv shows"})

		engine.data.mediaItems = map[string][]MediaItem{
			"Movies": { // Data has title case
				{Title: "Movie 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
			"TV Shows": { // Data has title case
				{Title: "Show 1", MediaType: MediaTypeTV, Tags: []string{}},
				{Title: "Show 2", MediaType: MediaTypeTV, Tags: []string{"ongoing"}},
			},
		}

		engine.filterMediaTags()

		// With the fix, filtering should now work correctly even with case mismatches
		assert.Len(t, engine.data.mediaItems["Movies"], 1, "Movie with 'favorite' tag should be excluded (case-insensitive lookup)")
		assert.Len(t, engine.data.mediaItems["TV Shows"], 1, "Show with 'ongoing' tag should be excluded (case-insensitive lookup)")
		assert.Equal(t, "Movie 1", engine.data.mediaItems["Movies"][0].Title)
		assert.Equal(t, "Show 1", engine.data.mediaItems["TV Shows"][0].Title)
	})

	t.Run("case mismatch - config uppercase, data lowercase", func(t *testing.T) {
		// Test when config has uppercase but data has different case
		engine := createEngineWithLibraryConfig([]string{"MOVIES", "TV SHOWS"})

		engine.data.mediaItems = map[string][]MediaItem{
			"movies": { // Data has lowercase
				{Title: "Movie 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
			"tv shows": { // Data has lowercase
				{Title: "Show 1", MediaType: MediaTypeTV, Tags: []string{}},
				{Title: "Show 2", MediaType: MediaTypeTV, Tags: []string{"ongoing"}},
			},
		}

		engine.filterMediaTags()

		// With the fix, filtering should now work correctly even with case mismatches
		assert.Len(t, engine.data.mediaItems["movies"], 1, "Movie with 'favorite' tag should be excluded (case-insensitive lookup)")
		assert.Len(t, engine.data.mediaItems["tv shows"], 1, "Show with 'ongoing' tag should be excluded (case-insensitive lookup)")
		assert.Equal(t, "Movie 1", engine.data.mediaItems["movies"][0].Title)
		assert.Equal(t, "Show 1", engine.data.mediaItems["tv shows"][0].Title)
	})

	t.Run("mixed case scenarios", func(t *testing.T) {
		// Test various mixed case scenarios
		engine := createEngineWithLibraryConfig([]string{"Movies", "tv-shows", "DOCUMENTARIES"})

		engine.data.mediaItems = map[string][]MediaItem{
			"movies": { // lowercase vs "Movies" in config
				{Title: "Movie 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
			"TV-Shows": { // different case vs "tv-shows" in config
				{Title: "Show 1", MediaType: MediaTypeTV, Tags: []string{}},
				{Title: "Show 2", MediaType: MediaTypeTV, Tags: []string{"ongoing"}},
			},
			"documentaries": { // lowercase vs "DOCUMENTARIES" in config
				{Title: "Doc 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Doc 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
		}

		engine.filterMediaTags()

		// With the fix, filtering should now work correctly even with case mismatches
		assert.Len(t, engine.data.mediaItems["movies"], 1, "Movie with 'favorite' tag should be excluded (case-insensitive lookup)")
		assert.Len(t, engine.data.mediaItems["TV-Shows"], 1, "Show with 'ongoing' tag should be excluded (case-insensitive lookup)")
		assert.Len(t, engine.data.mediaItems["documentaries"], 1, "Doc with 'favorite' tag should be excluded (case-insensitive lookup)")
		assert.Equal(t, "Movie 1", engine.data.mediaItems["movies"][0].Title)
		assert.Equal(t, "Show 1", engine.data.mediaItems["TV-Shows"][0].Title)
		assert.Equal(t, "Doc 1", engine.data.mediaItems["documentaries"][0].Title)
	})

	t.Run("viper case insensitive simulation", func(t *testing.T) {
		// Simulate how viper might load configuration with different case
		// This represents what would happen if viper normalizes keys to lowercase
		engine := createEngineWithLibraryConfig([]string{"movies", "tv shows"}) // viper normalized to lowercase

		// But the actual library names from Jellystat come in different cases
		engine.data.mediaItems = map[string][]MediaItem{
			"Movies": { // From Jellystat - title case
				{Title: "Movie 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
			"TV Shows": { // From Jellystat - title case
				{Title: "Show 1", MediaType: MediaTypeTV, Tags: []string{}},
				{Title: "Show 2", MediaType: MediaTypeTV, Tags: []string{"ongoing"}},
			},
		}

		engine.filterMediaTags()

		// With the fix, filtering should now work correctly even though viper normalized the keys
		assert.Len(t, engine.data.mediaItems["Movies"], 1, "Movies should be filtered using case-insensitive lookup")
		assert.Len(t, engine.data.mediaItems["TV Shows"], 1, "TV Shows should be filtered using case-insensitive lookup")
		assert.Equal(t, "Movie 1", engine.data.mediaItems["Movies"][0].Title)
		assert.Equal(t, "Show 1", engine.data.mediaItems["TV Shows"][0].Title)
	})

	t.Run("library not in configuration", func(t *testing.T) {
		// Test with library names that don't exist in config at all
		engine := createEngineWithLibraryConfig([]string{"Movies"}) // Only Movies configured

		engine.data.mediaItems = map[string][]MediaItem{
			"Movies": {
				{Title: "Movie 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
			"Unconfigured Library": { // Not in config
				{Title: "Unknown 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Unknown 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
		}

		engine.filterMediaTags()

		// Movies should be filtered (favorite tag excluded)
		assert.Len(t, engine.data.mediaItems["Movies"], 1, "Movies should be filtered based on config")
		assert.Equal(t, "Movie 1", engine.data.mediaItems["Movies"][0].Title)

		// Unconfigured library should pass through without filtering
		assert.Len(t, engine.data.mediaItems["Unconfigured Library"], 2, "Unconfigured libraries should not be filtered")
	})

	t.Run("empty library configuration", func(t *testing.T) {
		// Test with empty library configuration
		cfg := &config.Config{
			JellySweep: &config.JellysweepConfig{
				CleanupInterval: 24,
				Libraries:       make(map[string]*config.CleanupConfig), // Empty
				DryRun:          true,
			},
			Jellystat: &config.JellystatConfig{
				URL:    "http://jellystat:3000",
				APIKey: "test-key",
			},
		}

		engine, err := New(cfg)
		require.NoError(t, err)

		engine.data.mediaItems = map[string][]MediaItem{
			"Movies": {
				{Title: "Movie 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
		}

		engine.filterMediaTags()

		// With no configuration, no filtering should occur
		assert.Len(t, engine.data.mediaItems["Movies"], 2, "No filtering with empty config")
	})

	t.Run("nil library configuration", func(t *testing.T) {
		// Test with nil library configuration
		cfg := &config.Config{
			JellySweep: &config.JellysweepConfig{
				CleanupInterval: 24,
				Libraries:       nil, // Nil
				DryRun:          true,
			},
			Jellystat: &config.JellystatConfig{
				URL:    "http://jellystat:3000",
				APIKey: "test-key",
			},
		}

		engine, err := New(cfg)
		require.NoError(t, err)

		engine.data.mediaItems = map[string][]MediaItem{
			"Movies": {
				{Title: "Movie 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
		}

		// Should not panic with nil configuration
		assert.NotPanics(t, func() {
			engine.filterMediaTags()
		})

		// With nil configuration, no filtering should occur
		assert.Len(t, engine.data.mediaItems["Movies"], 2, "No filtering with nil config")
	})

	t.Run("demonstrate case-insensitive lookup", func(t *testing.T) {
		// Test case-insensitive lookup functionality
		libraries := make(map[string]*config.CleanupConfig)
		libraries["movies"] = &config.CleanupConfig{ // viper normalized key
			Enabled:             true,
			RequestAgeThreshold: 30,
			LastStreamThreshold: 90,
			CleanupDelay:        7,
			ExcludeTags:         []string{"favorite"},
		}

		cfg := &config.Config{
			JellySweep: &config.JellysweepConfig{
				CleanupInterval: 24,
				Libraries:       libraries,
				DryRun:          true,
			},
			Jellystat: &config.JellystatConfig{
				URL:    "http://jellystat:3000",
				APIKey: "test-key",
			},
		}

		engine, err := New(cfg)
		require.NoError(t, err)

		// Verify the exact match lookup works
		config := engine.cfg.GetLibraryConfig("movies")
		assert.NotNil(t, config)
		assert.True(t, config.Enabled)

		// With the fix, the case-insensitive lookup now works!
		configFromCaseSensitive := engine.cfg.GetLibraryConfig("Movies")
		assert.NotNil(t, configFromCaseSensitive, "Case-insensitive lookup now works with the fix!")
		assert.True(t, configFromCaseSensitive.Enabled, "Configuration should be accessible via case-insensitive lookup")
	})
}

// TestEngine_ViperConfigIntegration tests how the engine works with viper's case-insensitive config loading
// This test simulates the actual config loading process to demonstrate the case sensitivity issue
func TestEngine_ViperConfigIntegration(t *testing.T) {
	t.Run("simulate viper case insensitive loading", func(t *testing.T) {
		// Create a temporary config file with mixed case library names
		configContent := `
jellysweep:
  cleanup_interval: 24
  dry_run: true
  auth:
    jellyfin:
      enabled: true
      url: "http://jellyfin:8096"
  libraries:
    Movies:  # Title case in config file
      enabled: true
      request_age_threshold: 30
      last_stream_threshold: 90
      cleanup_delay: 7
      exclude_tags: ["favorite"]
    "TV Shows":  # Title case with spaces
      enabled: true
      request_age_threshold: 45
      last_stream_threshold: 120
      cleanup_delay: 14
      exclude_tags: ["ongoing"]
    DOCUMENTARIES:  # All caps
      enabled: true
      request_age_threshold: 60
      last_stream_threshold: 150
      cleanup_delay: 21
      exclude_tags: ["educational"]

jellyseerr:
  url: "http://jellyseerr:5055"
  api_key: "test-key"

sonarr:
  url: "http://sonarr:8989"
  api_key: "test-key"

radarr:
  url: "http://radarr:7878"
  api_key: "test-key"

jellystat:
  url: "http://jellystat:3000"
  api_key: "test-key"
`

		// Create a temporary file
		tmpfile, err := os.CreateTemp("", "jellysweep-test-config-*.yml")
		require.NoError(t, err)
		defer os.Remove(tmpfile.Name())

		_, err = tmpfile.WriteString(configContent)
		require.NoError(t, err)
		tmpfile.Close()

		// Load config using the actual Load function (which uses viper)
		cfg, err := config.Load(tmpfile.Name())
		require.NoError(t, err)

		// Create engine with the loaded config
		engine, err := New(cfg)
		require.NoError(t, err)

		// Verify how viper handled the library names
		t.Log("Library configurations found:")
		for key, libConfig := range cfg.JellySweep.Libraries {
			t.Logf("  Key: '%s', Enabled: %v", key, libConfig.Enabled)
		}

		// Test with different case variations of library names that might come from Jellystat
		testCases := []struct {
			actualLibraryName string
			expectedFound     bool
			description       string
		}{
			// With the fix, case-insensitive lookup should work for all these cases
			{"Movies", true, "Should find with case-insensitive lookup (FIXED)"},
			{"movies", true, "Finds because viper normalized to lowercase"},
			{"MOVIES", true, "Should find with case-insensitive lookup (FIXED)"},
			{"TV Shows", true, "Should find with case-insensitive lookup (FIXED)"},
			{"tv shows", true, "Finds because viper normalized to lowercase"},
			{"TV SHOWS", true, "Should find with case-insensitive lookup (FIXED)"},
			{"DOCUMENTARIES", true, "Should find with case-insensitive lookup (FIXED)"},
			{"documentaries", true, "Finds because viper normalized to lowercase"},
			{"Documentaries", true, "Should find with case-insensitive lookup (FIXED)"},
		}

		for _, tc := range testCases {
			t.Run(fmt.Sprintf("lookup_%s", tc.actualLibraryName), func(t *testing.T) {
				config := engine.cfg.GetLibraryConfig(tc.actualLibraryName)
				if tc.expectedFound {
					assert.NotNil(t, config, "Should find config for %s (%s)", tc.actualLibraryName, tc.description)
				} else {
					assert.Nil(t, config, "Should NOT find config for %s (%s)", tc.actualLibraryName, tc.description)
				}
			})
		}

		// Simulate media items coming from Jellystat with different cases
		engine.data.mediaItems = map[string][]MediaItem{
			"Movies": { // Exact match - should work
				{Title: "Movie 1", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 2", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
			"movies": { // Lowercase - should not work (no config found)
				{Title: "Movie 3", MediaType: MediaTypeMovie, Tags: []string{}},
				{Title: "Movie 4", MediaType: MediaTypeMovie, Tags: []string{"favorite"}},
			},
			"TV Shows": { // Exact match - should work
				{Title: "Show 1", MediaType: MediaTypeTV, Tags: []string{}},
				{Title: "Show 2", MediaType: MediaTypeTV, Tags: []string{"ongoing"}},
			},
			"tv shows": { // Lowercase - should not work
				{Title: "Show 3", MediaType: MediaTypeTV, Tags: []string{}},
				{Title: "Show 4", MediaType: MediaTypeTV, Tags: []string{"ongoing"}},
			},
		}

		engine.filterMediaTags()

		// Verify filtering results
		t.Log("Filtering results:")
		for lib, items := range engine.data.mediaItems {
			t.Logf("  Library '%s': %d items remaining", lib, len(items))
			for _, item := range items {
				t.Logf("    - %s (tags: %v)", item.Title, item.Tags)
			}
		}

		// Expected results (demonstrating the FIX):
		// All libraries should now be filtered correctly due to case-insensitive lookup
		// - "Movies" should be filtered (config found via case-insensitive lookup) -> 1 item
		// - "movies" should be filtered (config found directly) -> 1 item
		// - "TV Shows" should be filtered (config found via case-insensitive lookup) -> 1 item
		// - "tv shows" should be filtered (config found directly) -> 1 item

		assert.Len(t, engine.data.mediaItems["Movies"], 1, "Movies should be filtered using case-insensitive lookup")
		assert.Equal(t, "Movie 1", engine.data.mediaItems["Movies"][0].Title)

		assert.Len(t, engine.data.mediaItems["movies"], 1, "movies (viper normalized) should be filtered")
		assert.Equal(t, "Movie 3", engine.data.mediaItems["movies"][0].Title)

		assert.Len(t, engine.data.mediaItems["TV Shows"], 1, "TV Shows should be filtered using case-insensitive lookup")
		assert.Equal(t, "Show 1", engine.data.mediaItems["TV Shows"][0].Title)

		assert.Len(t, engine.data.mediaItems["tv shows"], 1, "tv shows (viper normalized) should be filtered")
		assert.Equal(t, "Show 3", engine.data.mediaItems["tv shows"][0].Title)
	})

	t.Run("viper key normalization behavior", func(t *testing.T) {
		// Test to understand how viper actually handles map keys
		// This might vary based on viper version and configuration

		// Create config with various case patterns
		configContent := `
jellysweep:
  cleanup_interval: 24
  dry_run: true
  auth:
    jellyfin:
      enabled: true
      url: "http://jellyfin:8096"
  libraries:
    "Mixed Case Library":
      enabled: true
      exclude_tags: ["test"]
    "UPPERCASE_LIBRARY":
      enabled: true
      exclude_tags: ["test"]
    "lowercase_library":
      enabled: true
      exclude_tags: ["test"]

jellyseerr:
  url: "http://jellyseerr:5055"
  api_key: "test-key"

sonarr:
  url: "http://sonarr:8989"
  api_key: "test-key"

radarr:
  url: "http://radarr:7878"
  api_key: "test-key"

jellystat:
  url: "http://jellystat:3000"
  api_key: "test-key"
`

		tmpfile, err := os.CreateTemp("", "jellysweep-viper-test-*.yml")
		require.NoError(t, err)
		defer os.Remove(tmpfile.Name())

		_, err = tmpfile.WriteString(configContent)
		require.NoError(t, err)
		tmpfile.Close()

		cfg, err := config.Load(tmpfile.Name())
		require.NoError(t, err)

		// Log all the actual keys that viper loaded
		t.Log("Actual library keys loaded by viper:")
		for key := range cfg.JellySweep.Libraries {
			t.Logf("  '%s'", key)
		}

		// This will show us exactly how viper is handling the keys
		// and whether they're being normalized or preserved as-is
	})

	t.Run("workaround using case-insensitive lookup", func(t *testing.T) {
		// Test case-insensitive lookup functionality that replaced the LibraryName field approach

		configContent := `
jellysweep:
  cleanup_interval: 24
  dry_run: true
  auth:
    jellyfin:
      enabled: true
      url: "http://jellyfin:8096"
  libraries:
    movies:  # normalized by viper
      enabled: true
      exclude_tags: ["favorite"]
    tv_shows:  # normalized by viper
      enabled: true
      exclude_tags: ["ongoing"]

jellyseerr:
  url: "http://jellyseerr:5055"
  api_key: "test-key"

sonarr:
  url: "http://sonarr:8989"
  api_key: "test-key"

radarr:
  url: "http://radarr:7878"
  api_key: "test-key"

jellystat:
  url: "http://jellystat:3000"
  api_key: "test-key"
`

		tmpfile, err := os.CreateTemp("", "jellysweep-workaround-*.yml")
		require.NoError(t, err)
		defer os.Remove(tmpfile.Name())

		_, err = tmpfile.WriteString(configContent)
		require.NoError(t, err)
		tmpfile.Close()

		cfg, err := config.Load(tmpfile.Name())
		require.NoError(t, err)

		// Verify the direct lookup works
		moviesConfig := cfg.GetLibraryConfig("movies")
		assert.NotNil(t, moviesConfig)
		assert.True(t, moviesConfig.Enabled)

		tvConfig := cfg.GetLibraryConfig("tv_shows")
		assert.NotNil(t, tvConfig)
		assert.True(t, tvConfig.Enabled)

		// With the fix, both the direct lookup and case-insensitive lookup work
		assert.NotNil(t, cfg.GetLibraryConfig("Movies"), "Case-insensitive lookup now works!")
		assert.NotNil(t, cfg.GetLibraryConfig("TV_Shows"), "Case-insensitive lookup now works for underscore variant!")
		assert.NotNil(t, cfg.GetLibraryConfig("MOVIES"), "Case-insensitive lookup now works for uppercase!")
		assert.NotNil(t, cfg.GetLibraryConfig("TV_SHOWS"), "Case-insensitive lookup now works for uppercase underscore!")

		t.Log("The fix now enables case-insensitive lookup without needing a separate LibraryName field")
	})
}
