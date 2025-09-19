package streamystats

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jon4hz/jellysweep/config"
)

type Client struct {
	cfg         *config.StreamystatsConfig
	jellyfinCfg *config.JellyfinConfig
	httpClient  *http.Client
	baseURL     *url.URL
}

// CustomTime wraps time.Time to handle custom JSON unmarshaling.
type CustomTime struct {
	time.Time
}

// UnmarshalJSON implements json.Unmarshaler for CustomTime.
func (ct *CustomTime) UnmarshalJSON(data []byte) error {
	// Remove quotes from JSON string
	str := strings.Trim(string(data), `"`)
	if str == "null" || str == "" {
		return nil
	}

	// Parse with the expected format
	t, err := time.Parse("2006-01-02 15:04:05.999-07", str)
	if err != nil {
		return err
	}

	// Convert to UTC to ensure consistent timezone handling
	ct.Time = t.UTC()
	return nil
}

// MarshalJSON implements json.Marshaler for CustomTime.
func (ct CustomTime) MarshalJSON() ([]byte, error) {
	if ct.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + ct.Format("2006-01-02 15:04:05.999-07") + `"`), nil
}

type ItemDetails struct {
	LastWatched CustomTime `json:"lastWatched"`
}

func New(cfg *config.StreamystatsConfig, jellyfinCfg *config.JellyfinConfig) (*Client, error) {
	baseURL, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid streamystats URL: %w", err)
	}

	return &Client{
		cfg:         cfg,
		jellyfinCfg: jellyfinCfg,
		baseURL:     baseURL,
		httpClient:  &http.Client{},
	}, nil
}

var ErrItemNotFound = fmt.Errorf("item not found")

func (c *Client) GetItemDetails(ctx context.Context, itemID string) (*ItemDetails, error) {
	itemURL := fmt.Sprintf("%s/api/get-item-details/%s?serverId=%d", c.baseURL.String(), itemID, c.cfg.ServerID)

	req, err := http.NewRequestWithContext(ctx, "GET", itemURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create item details request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.jellyfinCfg.APIKey))

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
