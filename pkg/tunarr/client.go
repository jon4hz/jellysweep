package tunarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
)

// Client represents a Tunarr API client.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a new Tunarr API client.
func New(cfg *config.TunarrConfig) *Client {
	return &Client{
		baseURL:    cfg.URL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Channel represents a basic Tunarr channel.
type Channel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Number       int    `json:"number"`
	ProgramCount int    `json:"programCount"`
}

// ProgrammingResponse represents the response from the channel programming endpoint.
type ProgrammingResponse struct {
	Icon          ChannelIcon        `json:"icon"`
	Name          string             `json:"name"`
	Number        int                `json:"number"`
	TotalPrograms int                `json:"totalPrograms"`
	Programs      map[string]Program `json:"programs"` // Map of program ID to program
}

// ChannelIcon represents a channel icon configuration.
type ChannelIcon struct {
	Path     string  `json:"path"`
	Width    int     `json:"width"`
	Duration float64 `json:"duration"` // Duration can have decimals
	Position string  `json:"position"`
}

// Program represents a TV program in a channel.
type Program struct {
	Type               string       `json:"type"`
	Persisted          bool         `json:"persisted"`
	Duration           float64      `json:"duration"` // Duration in milliseconds, can have decimals
	Icon               string       `json:"icon,omitempty"`
	ID                 string       `json:"id"`
	Subtype            string       `json:"subtype"` // "movie", "episode", "track", etc.
	Summary            string       `json:"summary,omitempty"`
	Date               string       `json:"date,omitempty"`
	Year               int          `json:"year,omitempty"`
	Rating             string       `json:"rating,omitempty"`
	ServerFileKey      string       `json:"serverFileKey,omitempty"`
	ServerFilePath     string       `json:"serverFilePath,omitempty"`
	Title              string       `json:"title"`
	ShowID             string       `json:"showId,omitempty"`   // For episodes
	SeasonID           string       `json:"seasonId,omitempty"` // For episodes
	SeasonNumber       int          `json:"seasonNumber,omitempty"`
	EpisodeNumber      int          `json:"episodeNumber,omitempty"`
	AlbumID            string       `json:"albumId,omitempty"`  // For music
	ArtistID           string       `json:"artistId,omitempty"` // For music
	Index              int          `json:"index,omitempty"`
	Parent             *MediaParent `json:"parent,omitempty"`      // Parent media (season, album)
	Grandparent        *MediaParent `json:"grandparent,omitempty"` // Grandparent media (show, artist)
	ExternalSourceType string       `json:"externalSourceType"`
	ExternalSourceName string       `json:"externalSourceName"`
	ExternalSourceID   string       `json:"externalSourceId"`
	ExternalKey        string       `json:"externalKey"`
	UniqueID           string       `json:"uniqueId"`
	ExternalIDs        []ExternalID `json:"externalIds"`
}

// MediaParent represents a parent or grandparent media item.
type MediaParent struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Index       int          `json:"index,omitempty"`
	Guids       []string     `json:"guids,omitempty"`
	Year        int          `json:"year,omitempty"`
	ExternalKey string       `json:"externalKey,omitempty"`
	ExternalIDs []ExternalID `json:"externalIds"`
	Summary     string       `json:"summary,omitempty"`
	Type        string       `json:"type"` // "season", "album", "show", "artist"
}

// ExternalID represents an external ID mapping.
type ExternalID struct {
	Type     string `json:"type"`               // "single" or "multi"
	Source   string `json:"source"`             // "plex-guid", "imdb", "tmdb", "tvdb", or "plex", "jellyfin", "emby"
	SourceID string `json:"sourceId,omitempty"` // For multi type
	ID       string `json:"id"`
}

// doRequest performs an HTTP request to the Tunarr API.
func (c *Client) doRequest(ctx context.Context, method, endpoint string, queryParams url.Values) (*http.Response, error) {
	reqURL := c.baseURL + endpoint
	if len(queryParams) > 0 {
		reqURL += "?" + queryParams.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error performing request: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close() //nolint:errcheck
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return resp, nil
}

// GetChannels retrieves all channels from Tunarr.
func (c *Client) GetChannels(ctx context.Context) ([]Channel, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/channels", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	var channels []Channel
	if err := json.NewDecoder(resp.Body).Decode(&channels); err != nil {
		return nil, fmt.Errorf("error decoding channels response: %w", err)
	}

	return channels, nil
}

// GetChannelProgramming retrieves the programming for a specific channel.
// limit: maximum number of programs to retrieve (optional)
// offset: offset for pagination (optional)
func (c *Client) GetChannelProgramming(ctx context.Context, channelID string, limit, offset int) (*ProgrammingResponse, error) {
	queryParams := url.Values{}
	if limit > 0 {
		queryParams.Set("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		queryParams.Set("offset", fmt.Sprintf("%d", offset))
	}

	endpoint := fmt.Sprintf("/api/channels/%s/programming", channelID)
	resp, err := c.doRequest(ctx, "GET", endpoint, queryParams)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	var programming ProgrammingResponse
	if err := json.NewDecoder(resp.Body).Decode(&programming); err != nil {
		return nil, fmt.Errorf("error decoding programming response: %w", err)
	}

	return &programming, nil
}

// GetAllChannelPrograms retrieves all programs from a channel, handling pagination automatically.
func (c *Client) GetAllChannelPrograms(ctx context.Context, channelID string) ([]Program, error) {
	const batchSize = 100 // Fetch in batches
	var allPrograms []Program
	offset := 0

	for {
		programming, err := c.GetChannelProgramming(ctx, channelID, batchSize, offset)
		if err != nil {
			return nil, err
		}

		// sometimes tunarr can't return all programs for some reason.
		if len(programming.Programs) == 0 {
			break
		}

		// Extract programs from the map
		for _, program := range programming.Programs {
			allPrograms = append(allPrograms, program)
		}

		// Check if we've retrieved all programs
		if len(allPrograms) >= programming.TotalPrograms {
			break
		}

		offset += batchSize
	}

	return allPrograms, nil
}
