package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/version"
)

// Client represents a Jellyfin API client.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a new Jellyfin client.
func New(cfg *config.JellyfinConfig) *Client {
	return &Client{
		baseURL:    cfg.URL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// AuthResponse represents the response from the authentication endpoint.
type AuthResponse struct {
	User        User `json:"User"`
	SessionInfo struct {
		AccessToken string `json:"AccessToken"`
	} `json:"SessionInfo"`
	AccessToken string `json:"AccessToken"`
}

// User represents a Jellyfin user.
type User struct {
	ID                        string `json:"Id"`
	Name                      string `json:"Name"`
	ServerID                  string `json:"ServerId"`
	HasPassword               bool   `json:"HasPassword"`
	HasConfiguredPassword     bool   `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool   `json:"HasConfiguredEasyPassword"`
	EnableAutoLogin           bool   `json:"EnableAutoLogin"`
	LastLoginDate             string `json:"LastLoginDate"`
	LastActivityDate          string `json:"LastActivityDate"`
	Configuration             struct {
		IsAdministrator bool `json:"IsAdministrator"`
		IsHidden        bool `json:"IsHidden"`
		IsDisabled      bool `json:"IsDisabled"`
	} `json:"Configuration"`
}

// AuthenticateByName authenticates a user by username and password.
func (c *Client) AuthenticateByName(ctx context.Context, username, password string) (*AuthResponse, error) {
	authURL, err := url.JoinPath(c.baseURL, "/Users/AuthenticateByName")
	if err != nil {
		return nil, fmt.Errorf("failed to build auth URL: %w", err)
	}

	authData := map[string]interface{}{
		"Username": username,
		"Pw":       password,
	}

	jsonData, err := json.Marshal(authData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal auth data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", authURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Authorization",
		fmt.Sprintf(`MediaBrowser Client="JellySweep", Device="JellySweep", DeviceId="jellysweep-auth", Version="%s",`, version.Version))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authentication failed with status %d: %s", resp.StatusCode, string(body))
	}

	var authResp AuthResponse
	if err := json.Unmarshal(body, &authResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &authResp, nil
}
