package engine

import (
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine_parseDeletionDateFromTag(t *testing.T) {
	engine := createTestEngine(t)

	tests := []struct {
		name     string
		tagName  string
		expected time.Time
		wantErr  bool
	}{
		{
			name:     "valid date tag",
			tagName:  "jellysweep-delete-2024-01-15",
			expected: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			wantErr:  false,
		},
		{
			name:     "valid date tag with trailing dash",
			tagName:  "jellysweep-delete-2024-12-31-",
			expected: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
			wantErr:  false,
		},
		{
			name:    "invalid date format",
			tagName: "jellysweep-delete-invalid-date",
			wantErr: true,
		},
		{
			name:    "empty tag",
			tagName: "jellysweep-delete-",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.parseDeletionDateFromTag(tt.tagName)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestEngine_triggerTagIDs(t *testing.T) {
	engine := createTestEngine(t)

	tests := []struct {
		name     string
		tags     map[int32]string
		expected []int32
	}{
		{
			name: "past deletion tags should trigger",
			tags: map[int32]string{
				1: "jellysweep-delete-2020-01-01",  // Past date
				2: "jellysweep-delete-2025-12-31",  // Future date
				3: "regular-tag",                   // Non-deletion tag
				4: "jellysweep-delete-2023-06-15-", // Past date with trailing dash
			},
			expected: []int32{1, 4},
		},
		{
			name: "no deletion tags",
			tags: map[int32]string{
				1: "regular-tag",
				2: "another-tag",
				3: "jellysweep-keep",
			},
			expected: []int32{},
		},
		{
			name: "only future deletion tags",
			tags: map[int32]string{
				1: "jellysweep-delete-2030-01-01",
				2: "jellysweep-delete-2029-12-31",
			},
			expected: []int32{},
		},
		{
			name: "invalid date formats should be skipped",
			tags: map[int32]string{
				1: "jellysweep-delete-invalid",
				2: "jellysweep-delete-2020-01-01", // Valid past date
				3: "jellysweep-delete-bad-date",
			},
			expected: []int32{2},
		},
		{
			name:     "empty tags map",
			tags:     map[int32]string{},
			expected: []int32{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.triggerTagIDs(tt.tags)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestEngine_filterMediaTags(t *testing.T) {
	engine := createTestEngine(t)

	// Set up test data
	engine.data.mediaItems = map[string][]MediaItem{
		"Movies": {
			{
				Title:     "Movie 1",
				Tags:      []string{"regular-tag"},
				MediaType: MediaTypeMovie,
			},
			{
				Title:     "Movie 2 - Excluded by tag",
				Tags:      []string{"favorite"}, // This is in exclude list
				MediaType: MediaTypeMovie,
			},
			{
				Title:     "Movie 3 - Has keep tag",
				Tags:      []string{"jellysweep-must-keep-2030-01-01"}, // Future keep tag
				MediaType: MediaTypeMovie,
			},
			{
				Title:     "Movie 4 - Expired keep tag",
				Tags:      []string{"jellysweep-must-keep-2020-01-01"}, // Expired keep tag
				MediaType: MediaTypeMovie,
			},
			{
				Title:     "Movie 5 - Must delete",
				Tags:      []string{"jellysweep-must-delete-for-sure"},
				MediaType: MediaTypeMovie,
			},
			{
				Title:     "Movie 6 - Already marked",
				Tags:      []string{"jellysweep-delete-2024-01-01"},
				MediaType: MediaTypeMovie,
			},
		},
		"TV Shows": {
			{
				Title:     "Show 1",
				Tags:      []string{"regular-tag"},
				MediaType: MediaTypeTV,
			},
			{
				Title:     "Show 2 - Excluded",
				Tags:      []string{"ongoing"}, // This is in exclude list for TV Shows
				MediaType: MediaTypeTV,
			},
		},
	}

	engine.filterMediaTags()

	// Check Movies library
	moviesItems := engine.data.mediaItems["Movies"]
	require.Len(t, moviesItems, 2) // Only Movie 1 and Movie 4 should remain

	movieTitles := make([]string, 0, len(moviesItems))
	for _, item := range moviesItems {
		movieTitles = append(movieTitles, item.Title)
	}
	assert.Contains(t, movieTitles, "Movie 1")
	assert.Contains(t, movieTitles, "Movie 4 - Expired keep tag")

	// Check TV Shows library
	tvItems := engine.data.mediaItems["TV Shows"]
	require.Len(t, tvItems, 1) // Only Show 1 should remain
	assert.Equal(t, "Show 1", tvItems[0].Title)
}

func TestEngine_filterMediaTags_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		mediaItems     map[string][]MediaItem
		expectedCounts map[string]int
	}{
		{
			name: "empty media items",
			mediaItems: map[string][]MediaItem{
				"Movies": {},
			},
			expectedCounts: map[string]int{
				"Movies": 0,
			},
		},
		{
			name:           "no libraries",
			mediaItems:     map[string][]MediaItem{},
			expectedCounts: map[string]int{},
		},
		{
			name: "items with multiple tags",
			mediaItems: map[string][]MediaItem{
				"Movies": {
					{
						Title:     "Movie with multiple tags",
						Tags:      []string{"regular-tag", "another-tag"},
						MediaType: MediaTypeMovie,
					},
					{
						Title:     "Movie with excluded tag among others",
						Tags:      []string{"regular-tag", "favorite", "another-tag"},
						MediaType: MediaTypeMovie,
					},
				},
			},
			expectedCounts: map[string]int{
				"Movies": 1, // Only the first movie should remain
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := createTestEngine(t)
			engine.data.mediaItems = tt.mediaItems

			engine.filterMediaTags()

			for library, expectedCount := range tt.expectedCounts {
				actualCount := len(engine.data.mediaItems[library])
				assert.Equal(t, expectedCount, actualCount, "Library %s should have %d items", library, expectedCount)
			}
		})
	}
}

func TestEngine_getCachedImageURL(t *testing.T) {
	tests := []struct {
		name     string
		imageURL string
		expected string
	}{
		{
			name:     "simple URL",
			imageURL: "http://example.com/image.jpg",
			expected: "/api/images/cache?url=http%3A%2F%2Fexample.com%2Fimage.jpg",
		},
		{
			name:     "URL with query parameters",
			imageURL: "http://example.com/image.jpg?size=large&format=webp",
			expected: "/api/images/cache?url=http%3A%2F%2Fexample.com%2Fimage.jpg%3Fsize%3Dlarge%26format%3Dwebp",
		},
		{
			name:     "empty URL",
			imageURL: "",
			expected: "",
		},
		{
			name:     "URL with special characters",
			imageURL: "http://example.com/images/movie title with spaces & symbols.jpg",
			expected: "/api/images/cache?url=http%3A%2F%2Fexample.com%2Fimages%2Fmovie+title+with+spaces+%26+symbols.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCachedImageURL(tt.imageURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test helper to create engine with specific library configurations
func createTestEngineWithLibraries(t *testing.T, libraries map[string]*config.CleanupConfig) *Engine {
	engine, tsm := CreateTestEngineWithMocks(t)
	t.Cleanup(func() {
		tsm.Close()
	})

	// Override the library configurations
	engine.cfg.Libraries = libraries

	return engine
}

func TestEngine_filterMediaTags_CustomExcludeTags(t *testing.T) {
	libraries := map[string]*config.CleanupConfig{
		"Custom Library": {
			Enabled:             true,
			ContentAgeThreshold: 30,
			LastStreamThreshold: 90,
			CleanupDelay:        7,
			ExcludeTags:         []string{"custom-exclude", "another-exclude"},
		},
	}

	engine := createTestEngineWithLibraries(t, libraries)
	engine.data.mediaItems = map[string][]MediaItem{
		"Custom Library": {
			{
				Title:     "Item 1 - Should remain",
				Tags:      []string{"regular-tag"},
				MediaType: MediaTypeMovie,
			},
			{
				Title:     "Item 2 - Custom excluded",
				Tags:      []string{"custom-exclude"},
				MediaType: MediaTypeMovie,
			},
			{
				Title:     "Item 3 - Another excluded",
				Tags:      []string{"another-exclude"},
				MediaType: MediaTypeMovie,
			},
		},
	}

	engine.filterMediaTags()

	items := engine.data.mediaItems["Custom Library"]
	require.Len(t, items, 1)
	assert.Equal(t, "Item 1 - Should remain", items[0].Title)
}
