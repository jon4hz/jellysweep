package engine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	radarr "github.com/devopsarr/radarr-go/radarr"
	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/jellyseerr"
	"github.com/jon4hz/jellysweep/jellystat"
	"github.com/jon4hz/jellysweep/notify/email"
	"github.com/stretchr/testify/assert"
)

// TestServerManager manages all mock test servers
type TestServerManager struct {
	JellystatServer  *httptest.Server
	JellyseerrServer *httptest.Server
	SonarrServer     *httptest.Server
	RadarrServer     *httptest.Server
	NtfyServer       *httptest.Server
}

// NewTestServerManager creates and starts all mock servers
func NewTestServerManager() *TestServerManager {
	tsm := &TestServerManager{}

	// Create Jellystat mock server
	tsm.JellystatServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/libraries"):
			// Mock library data
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[
				{
					"Id": "lib1",
					"Name": "Movies",
					"Type": "movies"
				},
				{
					"Id": "lib2", 
					"Name": "TV Shows",
					"Type": "tvshows"
				}
			]`))
		case strings.Contains(r.URL.Path, "/api/library"):
			// Mock library items
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[
				{
					"Id": "item1",
					"Name": "Test Movie",
					"Type": "Movie",
					"UserId": "user1",
					"LastSeen": "2024-01-01T00:00:00Z",
					"PlayCount": 5,
					"TotalPlayCount": 10,
					"ProviderId": "123"
				},
				{
					"Id": "item2",
					"Name": "Test Series",
					"Type": "Series", 
					"UserId": "user2",
					"LastSeen": "2024-06-01T00:00:00Z",
					"PlayCount": 2,
					"TotalPlayCount": 15,
					"ProviderId": "456"
				}
			]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	// Create Jellyseerr mock server
	tsm.JellyseerrServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/v1/request"):
			// Mock request data
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"results": [
					{
						"id": 1,
						"status": 2,
						"media": {
							"id": 123,
							"mediaType": "movie",
							"tmdbId": 12345,
							"tvdbId": null,
							"imdbId": "tt1234567",
							"status": 5
						},
						"requestedBy": {
							"id": 1,
							"email": "user1@example.com",
							"displayName": "User 1"
						},
						"createdAt": "2024-01-01T00:00:00.000Z",
						"updatedAt": "2024-01-05T00:00:00.000Z"
					},
					{
						"id": 2,
						"status": 2,
						"media": {
							"id": 456,
							"mediaType": "tv", 
							"tmdbId": 67890,
							"tvdbId": 98765,
							"imdbId": "tt7654321",
							"status": 5
						},
						"requestedBy": {
							"id": 2,
							"email": "user2@example.com",
							"displayName": "User 2"
						},
						"createdAt": "2024-02-01T00:00:00.000Z",
						"updatedAt": "2024-02-05T00:00:00.000Z"
					}
				],
				"pageInfo": {
					"pages": 1,
					"pageSize": 20,
					"results": 2,
					"page": 1
				}
			}`))
		case strings.Contains(r.URL.Path, "/api/v1/user"):
			// Mock user data
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"results": [
					{
						"id": 1,
						"email": "user1@example.com",
						"displayName": "User 1",
						"createdAt": "2024-01-01T00:00:00.000Z"
					},
					{
						"id": 2,
						"email": "user2@example.com",
						"displayName": "User 2",
						"createdAt": "2024-01-01T00:00:00.000Z"
					}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	// Create Sonarr mock server
	tsm.SonarrServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/v3/series") && !strings.Contains(r.URL.Path, "/api/v3/series/"):
			if r.Method == "GET" {
				// Mock series data
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`[
					{
						"id": 123,
						"title": "Test Series 1",
						"path": "/tv/Test Series 1",
						"tvdbId": 456789,
						"imdbId": "tt1234567",
						"tags": [1, 2],
						"monitored": true,
						"status": "continuing",
						"year": 2020,
						"images": [
							{
								"coverType": "poster",
								"url": "/MediaCover/123/poster.jpg",
								"remoteUrl": "http://example.com/poster123.jpg"
							}
						],
						"statistics": {
							"sizeOnDisk": 5368709120,
							"episodeFileCount": 10,
							"episodeCount": 10,
							"totalEpisodeCount": 10
						}
					},
					{
						"id": 456,
						"title": "Test Series 2", 
						"path": "/tv/Test Series 2",
						"tvdbId": 789012,
						"imdbId": "tt7654321",
						"tags": [2, 3],
						"monitored": true,
						"status": "ended",
						"year": 2021,
						"images": [
							{
								"coverType": "poster",
								"url": "/MediaCover/456/poster.jpg",
								"remoteUrl": "http://example.com/poster456.jpg"
							}
						],
						"statistics": {
							"sizeOnDisk": 10737418240,
							"episodeFileCount": 20,
							"episodeCount": 20,
							"totalEpisodeCount": 20
						}
					}
				]`))
			} else if r.Method == "PUT" {
				// Mock series update
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"id": 123, "title": "Test Series 1", "tags": [1, 2, 4]}`))
			}
		case strings.Contains(r.URL.Path, "/api/v3/series/"):
			if r.Method == "GET" {
				// Mock individual series data
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"id": 123,
					"title": "Test Series 1",
					"path": "/tv/Test Series 1",
					"tvdbId": 456789,
					"imdbId": "tt1234567",
					"tags": [1, 2],
					"monitored": true,
					"status": "continuing",
					"year": 2020,
					"images": [
						{
							"coverType": "poster",
							"url": "/MediaCover/123/poster.jpg",
							"remoteUrl": "http://example.com/poster123.jpg"
						}
					],
					"statistics": {
						"sizeOnDisk": 5368709120,
						"episodeFileCount": 10,
						"episodeCount": 10,
						"totalEpisodeCount": 10
					}
				}`))
			} else if r.Method == "PUT" {
				// Mock series update
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"id": 123, "title": "Test Series 1", "tags": [1, 2, 4]}`))
			} else if r.Method == "DELETE" {
				// Mock series deletion
				w.WriteHeader(http.StatusOK)
			}
		case strings.Contains(r.URL.Path, "/api/v3/tag/detail"):
			if r.Method == "GET" {
				// Mock tag detail data
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`[
					{"id": 1, "label": "favorite", "delayProfileIds": [], "notificationIds": [], "restrictionIds": [], "seriesIds": [123]},
					{"id": 2, "label": "ongoing", "delayProfileIds": [], "notificationIds": [], "restrictionIds": [], "seriesIds": [123, 456]},
					{"id": 3, "label": "jellysweep-must-keep-2025-12-31", "delayProfileIds": [], "notificationIds": [], "restrictionIds": [], "seriesIds": []},
					{"id": 4, "label": "jellysweep-must-delete-for-sure", "delayProfileIds": [], "notificationIds": [], "restrictionIds": [], "seriesIds": []},
					{"id": 5, "label": "jellysweep-delete-2024-07-02", "delayProfileIds": [], "notificationIds": [], "restrictionIds": [], "seriesIds": []}
				]`))
			}
		case strings.Contains(r.URL.Path, "/api/v3/tag"):
			if r.Method == "GET" {
				// Mock tag data
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`[
					{"id": 1, "label": "favorite"},
					{"id": 2, "label": "ongoing"},
					{"id": 3, "label": "jellysweep-must-keep-2025-12-31"},
					{"id": 4, "label": "jellysweep-must-delete-for-sure"},
					{"id": 5, "label": "jellysweep-delete-2024-07-02"}
				]`))
			} else if r.Method == "POST" {
				// Mock tag creation
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte(`{"id": 6, "label": "new-tag"}`))
			} else if r.Method == "DELETE" {
				// Mock tag deletion
				w.WriteHeader(http.StatusOK)
			}
		case strings.Contains(r.URL.Path, "/api/v3/system/status"):
			// Mock system status
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"version": "3.0.0", "buildTime": "2024-01-01T00:00:00Z"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	// Create Radarr mock server
	tsm.RadarrServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/v3/movie"):
			if r.Method == "GET" {
				// Mock movie data
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`[
					{
						"id": 789,
						"title": "Test Movie 1",
						"path": "/movies/Test Movie 1",
						"tmdbId": 12345,
						"imdbId": "tt1234567",
						"tags": [1, 2],
						"monitored": true,
						"status": "released"
					},
					{
						"id": 012,
						"title": "Test Movie 2",
						"path": "/movies/Test Movie 2", 
						"tmdbId": 67890,
						"imdbId": "tt7654321",
						"tags": [2, 3],
						"monitored": true,
						"status": "released"
					}
				]`))
			} else if r.Method == "PUT" {
				// Mock movie update
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"id": 789, "title": "Test Movie 1", "tags": [1, 2, 4]}`))
			}
		case strings.Contains(r.URL.Path, "/api/v3/tag"):
			if r.Method == "GET" {
				// Mock tag data
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`[
					{"id": 1, "label": "favorite"},
					{"id": 2, "label": "new-release"},
					{"id": 3, "label": "jellysweep-must-keep-2025-12-31"},
					{"id": 4, "label": "jellysweep-must-delete-for-sure"},
					{"id": 5, "label": "jellysweep-delete-2024-07-02"}
				]`))
			} else if r.Method == "POST" {
				// Mock tag creation
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte(`{"id": 6, "label": "new-tag"}`))
			} else if r.Method == "DELETE" {
				// Mock tag deletion
				w.WriteHeader(http.StatusOK)
			}
		case strings.Contains(r.URL.Path, "/api/v3/system/status"):
			// Mock system status
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"version": "3.0.0", "buildTime": "2024-01-01T00:00:00Z"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	// Create Ntfy mock server
	tsm.NtfyServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock ntfy notification endpoint
		w.WriteHeader(http.StatusOK)
	}))

	return tsm
}

// Close shuts down all mock servers
func (tsm *TestServerManager) Close() {
	if tsm.JellystatServer != nil {
		tsm.JellystatServer.Close()
	}
	if tsm.JellyseerrServer != nil {
		tsm.JellyseerrServer.Close()
	}
	if tsm.SonarrServer != nil {
		tsm.SonarrServer.Close()
	}
	if tsm.RadarrServer != nil {
		tsm.RadarrServer.Close()
	}
	if tsm.NtfyServer != nil {
		tsm.NtfyServer.Close()
	}
}

// CreateTestEngineWithMocks creates an engine instance with mock backends
func CreateTestEngineWithMocks(t *testing.T) (*Engine, *TestServerManager) {
	tsm := NewTestServerManager()

	cfg := &config.Config{
		JellySweep: &config.JellysweepConfig{
			CleanupInterval: 24,
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
			Email: &config.EmailConfig{
				Enabled:   true,
				SMTPHost:  "localhost",
				SMTPPort:  587,
				Username:  "test@example.com",
				Password:  "password",
				FromEmail: "test@example.com",
				FromName:  "JellySweep Test",
				UseTLS:    true,
			},
			Ntfy: &config.NtfyConfig{
				Enabled:   true,
				ServerURL: tsm.NtfyServer.URL,
				Topic:     "jellysweep-test",
			},
		},
		Jellystat: &config.JellystatConfig{
			URL:    tsm.JellystatServer.URL,
			APIKey: "test-jellystat-key",
		},
		Jellyseerr: &config.JellyseerrConfig{
			URL:    tsm.JellyseerrServer.URL,
			APIKey: "test-jellyseerr-key",
		},
		Sonarr: &config.SonarrConfig{
			URL:    tsm.SonarrServer.URL,
			APIKey: "test-sonarr-key",
		},
		Radarr: &config.RadarrConfig{
			URL:    tsm.RadarrServer.URL,
			APIKey: "test-radarr-key",
		},
	}

	engine, err := New(cfg)
	assert.NoError(t, err)
	assert.NotNil(t, engine)

	return engine, tsm
}

// Mock implementations for testing specific functions

// MockJellystatClient creates a mock jellystat client with test data
func MockJellystatClient(serverURL string) *jellystat.Client {
	client := jellystat.New(&config.JellystatConfig{
		URL:    serverURL,
		APIKey: "test-key",
	})
	return client
}

// MockJellyseerrClient creates a mock jellyseerr client with test data
func MockJellyseerrClient(serverURL string) *jellyseerr.Client {
	client := jellyseerr.New(&config.JellyseerrConfig{
		URL:    serverURL,
		APIKey: "test-key",
	})
	return client
}

// MockSonarrClient creates a mock sonarr client configuration
func MockSonarrClient(serverURL string) (*sonarr.APIClient, error) {
	cfg := sonarr.NewConfiguration()
	cfg.Servers[0].URL = serverURL
	return sonarr.NewAPIClient(cfg), nil
}

// MockRadarrClient creates a mock radarr client configuration
func MockRadarrClient(serverURL string) (*radarr.APIClient, error) {
	cfg := radarr.NewConfiguration()
	cfg.Servers[0].URL = serverURL
	return radarr.NewAPIClient(cfg), nil
}

// MockEmailService creates a mock email notification service
func MockEmailService(cfg *config.EmailConfig) *email.NotificationService {
	return email.New(cfg)
}

// Test data structures for mock responses

// MockMediaItem represents test media item data
type MockMediaItem struct {
	ID          string
	Title       string
	MediaType   MediaType
	RequestedBy string
	RequestDate time.Time
	LastSeen    time.Time
	PlayCount   int
}

// GetMockMediaItems returns sample media items for testing
func GetMockMediaItems() []MockMediaItem {
	return []MockMediaItem{
		{
			ID:          "sonarr:123",
			Title:       "Test Series 1",
			MediaType:   MediaTypeTV,
			RequestedBy: "user1@example.com",
			RequestDate: time.Now().AddDate(0, 0, -40),
			LastSeen:    time.Now().AddDate(0, 0, -20),
			PlayCount:   5,
		},
		{
			ID:          "radarr:789",
			Title:       "Test Movie 1",
			MediaType:   MediaTypeMovie,
			RequestedBy: "user2@example.com",
			RequestDate: time.Now().AddDate(0, 0, -35),
			LastSeen:    time.Now().AddDate(0, 0, -10),
			PlayCount:   3,
		},
		{
			ID:          "sonarr:456",
			Title:       "Test Series 2",
			MediaType:   MediaTypeTV,
			RequestedBy: "user1@example.com",
			RequestDate: time.Now().AddDate(0, 0, -50),
			LastSeen:    time.Now().AddDate(0, 0, -5),
			PlayCount:   8,
		},
		{
			ID:          "radarr:012",
			Title:       "Test Movie 2",
			MediaType:   MediaTypeMovie,
			RequestedBy: "user3@example.com",
			RequestDate: time.Now().AddDate(0, 0, -60),
			LastSeen:    time.Now().AddDate(0, 0, -100),
			PlayCount:   1,
		},
	}
}

// GetMockKeepRequests returns sample keep requests for testing
func GetMockKeepRequests() []models.KeepRequest {
	return []models.KeepRequest{
		{
			MediaID:      "sonarr:123",
			RequestedBy:  "user1@example.com",
			RequestDate:  time.Now().AddDate(0, 0, -5),
			Title:        "Test Series 1",
			Type:         "tv",
			Year:         2020,
			Library:      "TV Shows",
			DeletionDate: time.Now().AddDate(0, 0, 7),
		},
		{
			MediaID:      "radarr:789",
			RequestedBy:  "user2@example.com",
			RequestDate:  time.Now().AddDate(0, 0, -3),
			Title:        "Test Movie 1",
			Type:         "movie",
			Year:         2021,
			Library:      "Movies",
			DeletionDate: time.Now().AddDate(0, 0, 7),
		},
	}
}

// GetMockLibraryConfig returns sample library configuration for testing
func GetMockLibraryConfig() map[string]*config.CleanupConfig {
	return map[string]*config.CleanupConfig{
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
	}
}
