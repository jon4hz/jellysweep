package tracearr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
)

const (
	// watchedMediaPageSize is the most /watched-media returns per page.
	watchedMediaPageSize = "1000"
	// maxAttempts bounds how often a rate limited request is sent. The limit is
	// per minute, so waiting out a few windows covers normal contention.
	maxAttempts = 5
	// defaultRetryAfter is used when a rate limited response has no usable Retry-After.
	defaultRetryAfter = time.Minute
	// maxRetryAfter is the longest wait accepted from a rate limited response.
	maxRetryAfter = 5 * time.Minute
)

// Client is a client for the Tracearr public API v2. /watched-media needs
// Tracearr 2.3.0 or later.
type Client struct {
	cfg        *config.TracearrConfig
	httpClient *http.Client
}

// New creates a new Tracearr client.
func New(cfg *config.TracearrConfig) *Client {
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: config.TimeoutDuration(cfg.Timeout)},
	}
}

// MediaType selects which titles /watched-media lists.
type MediaType string

const (
	MediaTypeMovie MediaType = "movie"
	// MediaTypeShow lists series, with plays of their episodes rolled up.
	MediaTypeShow MediaType = "show"
)

// Title is a movie or series with at least one counted play. Tracearr counts a
// play once it runs two minutes or reaches the watched threshold.
type Title struct {
	// TmdbID and TvdbID are zero when Tracearr has no such ID for the title.
	TmdbID int32
	TvdbID int32
	// LastWatchedDay is the UTC day of the latest counted play, at midnight UTC.
	LastWatchedDay time.Time
}

type titleRow struct {
	TmdbID         *int32  `json:"tmdb_id"`
	TvdbID         *int32  `json:"tvdb_id"`
	LastWatchedDay *string `json:"last_watched_day"`
}

type watchedMediaResponse struct {
	Data *[]titleRow `json:"data"`
	Meta struct {
		NextCursor *string `json:"nextCursor"`
	} `json:"meta"`
}

// GetWatchedTitles returns the titles of a media type last played on or after
// the UTC day of since, newest first. A title missing from the result has no
// counted play in that time.
func (c *Client) GetWatchedTitles(ctx context.Context, mediaType MediaType, since time.Time) ([]Title, error) {
	sinceDay := since.UTC().Truncate(24 * time.Hour)

	query := url.Values{}
	query.Set("media_type", string(mediaType))
	// The default leaves out movies that weren't finished and series with any
	// episode left unwatched.
	query.Set("min_state", "partial")
	query.Set("pageSize", watchedMediaPageSize)

	var titles []Title
	for {
		var page watchedMediaResponse
		if err := c.get(ctx, "/api/v2/public/watched-media", query, &page); err != nil {
			return nil, err
		}
		pageTitles, err := toTitles(page)
		if err != nil {
			return nil, err
		}
		// Titles come newest first, so the first older one ends the walk.
		for _, title := range pageTitles {
			if title.LastWatchedDay.Before(sinceDay) {
				return titles, nil
			}
			titles = append(titles, title)
		}

		if page.Meta.NextCursor == nil || *page.Meta.NextCursor == "" {
			return titles, nil
		}
		if *page.Meta.NextCursor == query.Get("cursor") {
			return nil, errors.New("tracearr watched media returned the same cursor twice")
		}
		query.Set("cursor", *page.Meta.NextCursor)
	}
}

// toTitles validates a response, because a title or a whole page going missing
// would read as nothing having been played.
func toTitles(page watchedMediaResponse) ([]Title, error) {
	if page.Data == nil {
		return nil, errors.New("tracearr response has no data")
	}

	titles := make([]Title, 0, len(*page.Data))
	for _, row := range *page.Data {
		if row.LastWatchedDay == nil {
			return nil, errors.New("tracearr response has a title without last_watched_day")
		}
		lastWatchedDay, err := time.Parse(time.DateOnly, *row.LastWatchedDay)
		if err != nil {
			return nil, fmt.Errorf("tracearr response has an invalid last_watched_day: %w", err)
		}

		title := Title{LastWatchedDay: lastWatchedDay}
		if row.TmdbID != nil {
			title.TmdbID = *row.TmdbID
		}
		if row.TvdbID != nil {
			title.TvdbID = *row.TvdbID
		}
		titles = append(titles, title)
	}
	return titles, nil
}

// get sends a request and decodes the response, waiting out rate limits.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	requestURL := c.cfg.URL + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}

	for attempt := 1; ; attempt++ {
		retryAfter, err := c.fetch(ctx, requestURL, out)
		if err != nil || retryAfter == nil {
			return err
		}
		if attempt == maxAttempts {
			return fmt.Errorf("tracearr rate limit still exceeded after %d attempts", maxAttempts)
		}
		if *retryAfter > maxRetryAfter {
			return fmt.Errorf("tracearr rate limit asks to wait %s, longer than the allowed %s", *retryAfter, maxRetryAfter)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(*retryAfter):
		}
	}
}

// fetch sends one request. A non-nil duration means the request was rate limited
// and should be retried after that long.
func (c *Client) fetch(ctx context.Context, requestURL string, out any) (*time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create tracearr request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.cfg.APIKey))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute tracearr request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return &retryAfter, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tracearr request to %s failed with status %d", req.URL.Path, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, fmt.Errorf("failed to decode tracearr response: %w", err)
	}
	return nil, nil
}

func parseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return defaultRetryAfter
	}
	return time.Duration(seconds) * time.Second
}
