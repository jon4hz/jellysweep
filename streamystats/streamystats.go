package streamystats

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/jon4hz/jellysweep/config"
)

type Client struct {
	cfg        *config.StreamystatsConfig
	apiKey     string
	httpClient *http.Client
	baseURL    *url.URL
}

type ItemDetails struct {
	LastWatched time.Time `json:"lastWatched"`
}

func New(cfg *config.StreamystatsConfig, apiKey string) (*Client, error) {
	baseURL, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid streamystats URL: %w", err)
	}

	return &Client{
		cfg:        cfg,
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}, nil
}

var ErrItemNotFound = fmt.Errorf("item not found")

func (c *Client) GetItemDetails(ctx context.Context, itemID string) (*ItemDetails, error) {
	itemURL := fmt.Sprintf("%s/api/get-item-details/%s?serverId=%d", c.baseURL.String(), itemID, c.cfg.ServerID)

	req, err := http.NewRequestWithContext(ctx, "GET", itemURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create item details request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute item details request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrItemNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get item details with status %d", resp.StatusCode)
	}

	var itemDetails ItemDetails
	if err := json.NewDecoder(resp.Body).Decode(&itemDetails); err != nil {
		return nil, fmt.Errorf("failed to decode item details response: %w", err)
	}

	return &itemDetails, nil
}
