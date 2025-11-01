package jellyseerr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
)

func TestGetMovie(t *testing.T) {
	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/movie/12345" {
			w.Header().Set("Content-Type", "application/json")
			response := `{
				"id": 12345,
				"title": "Test Movie",
				"releaseDate": "2023-01-01",
				"mediaInfo": {
					"id": 1,
					"tmdbId": 12345,
					"status": 5,
					"requests": [
						{
							"id": 1,
							"status": 2,
							"createdAt": "2023-01-01T00:00:00.000Z",
							"updatedAt": "2023-01-01T00:00:00.000Z",
							"is4k": false
						}
					],
					"createdAt": "2023-01-01T00:00:00.000Z",
					"updatedAt": "2023-01-01T00:00:00.000Z"
				}
			}`
			fmt.Fprint(w, response)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Create client with test server URL
	client := New(&config.JellyseerrConfig{
		URL:    server.URL,
		APIKey: "test-api-key",
	})

	// Test GetMovie
	movie, err := client.GetMovie(context.Background(), 12345)
	if err != nil {
		t.Fatalf("GetMovie failed: %v", err)
	}

	if movie.ID != 12345 {
		t.Errorf("Expected movie ID 12345, got %d", movie.ID)
	}
	if movie.Title != "Test Movie" {
		t.Errorf("Expected movie title 'Test Movie', got %s", movie.Title)
	}
	if movie.MediaInfo.TmdbID != 12345 {
		t.Errorf("Expected TMDB ID 12345, got %d", movie.MediaInfo.TmdbID)
	}
	if len(movie.MediaInfo.Requests) != 1 {
		t.Errorf("Expected 1 request, got %d", len(movie.MediaInfo.Requests))
	}
}

func TestGetTvShow(t *testing.T) {
	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/tv/67890" {
			w.Header().Set("Content-Type", "application/json")
			response := `{
				"id": 67890,
				"name": "Test TV Show",
				"firstAirDate": "2023-01-01",
				"mediaInfo": {
					"id": 2,
					"tmdbId": 67890,
					"status": 5,
					"requests": [
						{
							"id": 2,
							"status": 2,
							"createdAt": "2023-01-02T00:00:00.000Z",
							"updatedAt": "2023-01-02T00:00:00.000Z",
							"is4k": false
						}
					],
					"createdAt": "2023-01-02T00:00:00.000Z",
					"updatedAt": "2023-01-02T00:00:00.000Z"
				}
			}`
			fmt.Fprint(w, response)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Create client with test server URL
	client := New(&config.JellyseerrConfig{
		URL:    server.URL,
		APIKey: "test-api-key",
	})

	// Test GetTvShow
	tvShow, err := client.GetTvShow(context.Background(), 67890)
	if err != nil {
		t.Fatalf("GetTvShow failed: %v", err)
	}

	if tvShow.ID != 67890 {
		t.Errorf("Expected TV show ID 67890, got %d", tvShow.ID)
	}
	if tvShow.Name != "Test TV Show" {
		t.Errorf("Expected TV show name 'Test TV Show', got %s", tvShow.Name)
	}
	if tvShow.MediaInfo.TmdbID != 67890 {
		t.Errorf("Expected TMDB ID 67890, got %d", tvShow.MediaInfo.TmdbID)
	}
	if len(tvShow.MediaInfo.Requests) != 1 {
		t.Errorf("Expected 1 request, got %d", len(tvShow.MediaInfo.Requests))
	}
}

func TestGetRequestTime(t *testing.T) {
	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/movie/12345" {
			w.Header().Set("Content-Type", "application/json")
			response := `{
				"id": 12345,
				"title": "Test Movie",
				"releaseDate": "2023-01-01",
				"mediaInfo": {
					"id": 1,
					"tmdbId": 12345,
					"status": 5,
					"requests": [
						{
							"id": 1,
							"status": 2,
							"createdAt": "2023-01-01T10:30:00.000Z",
							"updatedAt": "2023-01-01T10:30:00.000Z",
							"is4k": false
						}
					],
					"createdAt": "2023-01-01T00:00:00.000Z",
					"updatedAt": "2023-01-01T00:00:00.000Z"
				}
			}`
			fmt.Fprint(w, response)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Create client with test server URL
	client := New(&config.JellyseerrConfig{
		URL:    server.URL,
		APIKey: "test-api-key",
	})

	// Test GetRequestTime
	requestTime, err := client.GetRequestTime(context.Background(), 12345, "movie")
	if err != nil {
		t.Fatalf("GetRequestTime failed: %v", err)
	}

	expectedTime := time.Date(2023, 1, 1, 10, 30, 0, 0, time.UTC)
	if !requestTime.Equal(expectedTime) {
		t.Errorf("Expected request time %v, got %v", expectedTime, *requestTime)
	}
}

func TestGetRequestTimeNoRequests(t *testing.T) {
	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/movie/12345" {
			w.Header().Set("Content-Type", "application/json")
			response := `{
				"id": 12345,
				"title": "Test Movie",
				"releaseDate": "2023-01-01",
				"mediaInfo": {
					"id": 1,
					"tmdbId": 12345,
					"status": 5,
					"requests": [],
					"createdAt": "2023-01-01T00:00:00.000Z",
					"updatedAt": "2023-01-01T00:00:00.000Z"
				}
			}`
			fmt.Fprint(w, response)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Create client with test server URL
	client := New(&config.JellyseerrConfig{
		URL:    server.URL,
		APIKey: "test-api-key",
	})

	// Test GetRequestTime with no requests - should return no error but nil time
	requestTime, err := client.GetRequestTime(context.Background(), 12345, "movie")
	if err != nil {
		t.Fatalf("GetRequestTime failed: %v", err)
	}

	if requestTime != nil {
		t.Errorf("Expected nil request time, got %v", *requestTime)
	}
}
