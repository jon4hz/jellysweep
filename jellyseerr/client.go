package jellyseerr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jon4hz/jellysweep/config"
)

// Client represents a Jellyseerr API client
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new Jellyseerr API client
func New(cfg *config.JellyseerrConfig) *Client {
	return &Client{
		baseURL:    cfg.URL,
		apiKey:     cfg.APIKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// MediaInfo represents media information from the API
type MediaInfo struct {
	ID       int            `json:"id"`
	TmdbID   int            `json:"tmdbId"`
	Requests []MediaRequest `json:"requests"`
}

// MediaRequest represents a media request
type MediaRequest struct {
	CreatedAt time.Time `json:"createdAt"`
}

// MovieDetails represents detailed movie information from /movie/{movieId}
type MovieDetails struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	ReleaseDate string    `json:"releaseDate"`
	MediaInfo   MediaInfo `json:"mediaInfo"`
}

// TvDetails represents detailed TV show information from /tv/{tvId}
type TvDetails struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	FirstAirDate string    `json:"firstAirDate"`
	MediaInfo    MediaInfo `json:"mediaInfo"`
}

// MediaItem represents a generic media item that can be either a movie or TV show
type MediaItem struct {
	ID           int            `json:"id"`
	TmdbID       int            `json:"tmdbId"`
	Title        string         `json:"title"` // For movies or TV shows
	MediaType    string         `json:"mediaType"`
	ReleaseDate  string         `json:"releaseDate,omitempty"`  // For movies
	FirstAirDate string         `json:"firstAirDate,omitempty"` // For TV shows
	MediaInfo    MediaInfo      `json:"mediaInfo"`
	Requests     []MediaRequest `json:"requests,omitempty"`
}

// doRequest performs an HTTP request to the Jellyseerr API
func (c *Client) doRequest(ctx context.Context, method, endpoint string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("error marshaling request body: %w", err)
		}
		reqBody = io.NopCloser(bytes.NewReader(jsonBody))
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, reqBody)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error performing request: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return resp, nil
}

// GetMovie retrieves movie details by TMDB ID
func (c *Client) GetMovie(ctx context.Context, tmdbID int32) (*MovieDetails, error) {
	endpoint := fmt.Sprintf("/api/v1/movie/%d", tmdbID)

	resp, err := c.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var movie MovieDetails
	if err := json.NewDecoder(resp.Body).Decode(&movie); err != nil {
		return nil, fmt.Errorf("error decoding movie response: %w", err)
	}

	return &movie, nil
}

// GetTvShow retrieves TV show details by TMDB ID
func (c *Client) GetTvShow(ctx context.Context, tmdbID int32) (*TvDetails, error) {
	endpoint := fmt.Sprintf("/api/v1/tv/%d", tmdbID)

	resp, err := c.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tvShow TvDetails
	if err := json.NewDecoder(resp.Body).Decode(&tvShow); err != nil {
		return nil, fmt.Errorf("error decoding TV show response: %w", err)
	}

	return &tvShow, nil
}

// GetMediaItem retrieves media details by TMDB ID, trying both movie and TV endpoints
func (c *Client) GetMediaItem(ctx context.Context, tmdbID int32, mediaType string) (*MediaItem, error) {
	var mediaItem MediaItem

	switch mediaType {
	case "movie":
		movie, err := c.GetMovie(ctx, tmdbID)
		if err != nil {
			return nil, err
		}
		mediaItem = MediaItem{
			ID:          movie.ID,
			TmdbID:      movie.MediaInfo.TmdbID,
			Title:       movie.Title,
			MediaType:   "movie",
			ReleaseDate: movie.ReleaseDate,
			MediaInfo:   movie.MediaInfo,
			Requests:    movie.MediaInfo.Requests,
		}
	case "tv":
		tvShow, err := c.GetTvShow(ctx, tmdbID)
		if err != nil {
			return nil, err
		}
		mediaItem = MediaItem{
			ID:           tvShow.ID,
			TmdbID:       tvShow.MediaInfo.TmdbID,
			Title:        tvShow.Name,
			MediaType:    "tv",
			FirstAirDate: tvShow.FirstAirDate,
			MediaInfo:    tvShow.MediaInfo,
			Requests:     tvShow.MediaInfo.Requests,
		}
	default:
		return nil, fmt.Errorf("unsupported media type: %s", mediaType)
	}

	return &mediaItem, nil
}

// GetRequestTime returns when a specific TV show or movie was requested
func (c *Client) GetRequestTime(ctx context.Context, tmdbID int32, mediaType string) (*time.Time, error) {
	mediaItem, err := c.GetMediaItem(ctx, tmdbID, mediaType)
	if err != nil {
		return nil, err
	}

	// Find the last (newest) request for this media
	if len(mediaItem.Requests) > 0 {
		var lastCreatedAt *time.Time
		for _, request := range mediaItem.Requests {
			if lastCreatedAt == nil || request.CreatedAt.After(*lastCreatedAt) {
				lastCreatedAt = &request.CreatedAt
			}
		}
		return lastCreatedAt, nil
	}

	return nil, nil
}
