package jellystat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jon4hz/jellysweep/config"
)

const (
	ItemTypeSeries = "Series"
	ItemTypeMovie  = "Movie"
)

// Client represents a Jellystat API client
type Client struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// New creates a new Jellystat client
func New(cfg *config.JellystatConfig) *Client {
	return &Client{
		baseURL: cfg.URL,
		apiKey:  cfg.APIKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ItemHistoryRequest represents the request body for getItemHistory
type ItemHistoryRequest struct {
	ItemID string `json:"itemid"`
}

// ItemHistoryParams represents query parameters for getItemHistory
type ItemHistoryParams struct {
	Size    int    `json:"size,omitempty"`
	Page    int    `json:"page,omitempty"`
	Search  string `json:"search,omitempty"`
	Sort    string `json:"sort,omitempty"`
	Desc    bool   `json:"desc,omitempty"`
	Filters string `json:"filters,omitempty"`
}

// PlaybackHistory represents a single playback history entry
type PlaybackHistory struct {
	UserName             string    `json:"UserName"`
	NowPlayingItemName   string    `json:"NowPlayingItemName"`
	PlaybackDuration     int64     `json:"PlaybackDuration"`
	ActivityDateInserted time.Time `json:"ActivityDateInserted"`
	FullName             string    `json:"FullName,omitempty"`
}

// ItemHistoryResponse represents the response from getItemHistory
type ItemHistoryResponse struct {
	Results []PlaybackHistory `json:"results"`
}

// LastPlayedInfo contains information about when an item was last played
type LastPlayedInfo struct {
	ItemID       string
	ItemName     string
	ItemType     string
	LastPlayed   *time.Time
	LastUser     string
	PlayCount    int
	TotalRuntime int64
}

// LibraryMetadata represents metadata for a library
type LibraryMetadata struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

// LibraryItemsRequest represents the request body for getLibraryItems
type LibraryItemsRequest struct {
	LibraryID string `json:"libraryid"`
}

// LibraryItem represents a single item in a library
type LibraryItem struct {
	ID       string `json:"Id"`
	Name     string `json:"Name"`
	ParentID string `json:"ParentId,omitempty"`
	Type     string `json:"Type"`
}

// GetItemHistory retrieves the playback history for a specific item
func (c *Client) GetItemHistory(ctx context.Context, itemID string, params *ItemHistoryParams) (*ItemHistoryResponse, error) {
	if params == nil {
		params = &ItemHistoryParams{}
	}

	// Prepare request body
	reqBody := ItemHistoryRequest{
		ItemID: itemID,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Build URL with query parameters
	u, err := url.Parse(c.baseURL + "/api/getItemHistory")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	query := u.Query()
	if params.Size > 0 {
		query.Set("size", strconv.Itoa(params.Size))
	}
	if params.Page > 0 {
		query.Set("page", strconv.Itoa(params.Page))
	}
	if params.Search != "" {
		query.Set("search", params.Search)
	}
	if params.Sort != "" {
		query.Set("sort", params.Sort)
	}
	if params.Desc {
		query.Set("desc", "true")
	}
	if params.Filters != "" {
		query.Set("filters", params.Filters)
	}
	u.RawQuery = query.Encode()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-token", c.apiKey)

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result ItemHistoryResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &result, nil
}

// GetLastPlayed retrieves information about when an item was last played
func (c *Client) GetLastPlayed(ctx context.Context, itemID string) (*LastPlayedInfo, error) {
	// Get history sorted by date (most recent first)
	params := &ItemHistoryParams{
		Size: 100, // Get enough results to calculate stats
		Page: 1,
		Sort: "ActivityDateInserted",
		Desc: true,
	}

	history, err := c.GetItemHistory(ctx, itemID, params)
	if err != nil {
		return nil, err
	}

	info := &LastPlayedInfo{
		ItemID:    itemID,
		PlayCount: len(history.Results), // Use length of results instead of Count field
	}

	if len(history.Results) > 0 {
		// Get info from the most recent entry
		recent := history.Results[0]
		info.LastPlayed = &recent.ActivityDateInserted
		info.LastUser = recent.UserName
		info.ItemName = recent.NowPlayingItemName
		info.ItemType = recent.FullName // Use FullName as a substitute for ItemType

		// Calculate total runtime from all plays
		for _, play := range history.Results {
			info.TotalRuntime += play.PlaybackDuration
		}
	}

	return info, nil
}

// GetItemsLastPlayedBefore returns items that were last played before the specified time
// This would typically require a different API endpoint that lists all items,
// but for now we'll return an error indicating this functionality needs implementation
func (c *Client) GetItemsLastPlayedBefore(ctx context.Context, before time.Time) ([]LastPlayedInfo, error) {
	return nil, fmt.Errorf("GetItemsLastPlayedBefore requires iteration over all items - not implemented yet")
}

// GetLibraryMetadata retrieves metadata for all libraries
func (c *Client) GetLibraryMetadata(ctx context.Context) ([]LibraryMetadata, error) {
	// Build URL
	u, err := url.Parse(c.baseURL + "/stats/getLibraryMetadata")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("x-api-token", c.apiKey)

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result []LibraryMetadata
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return result, nil
}

// GetLibraryItems retrieves items from a specific library
func (c *Client) GetLibraryItems(ctx context.Context, libraryID string) ([]LibraryItem, error) {
	// Prepare request body
	reqBody := LibraryItemsRequest{
		LibraryID: libraryID,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Build URL with query parameters
	u, err := url.Parse(c.baseURL + "/api/getLibraryItems")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-token", c.apiKey)

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result []LibraryItem
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return result, nil
}
