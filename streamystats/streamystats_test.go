package streamystats

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.StreamystatsConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: &config.StreamystatsConfig{
				URL:      "http://localhost:3000",
				ServerID: 1,
			},
			wantErr: false,
		},
		{
			name: "invalid URL",
			cfg: &config.StreamystatsConfig{
				URL:      "://invalid-url",
				ServerID: 1,
			},
			wantErr: true,
		},
	}

	jellyfinCfg := &config.JellyfinConfig{
		APIKey: "test-api-key",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.cfg, jellyfinCfg)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
				assert.NotNil(t, client.httpClient)
			}
		})
	}
}

func TestClient_GetItemDetails(t *testing.T) {
	lastWatchedTime, err := time.Parse("2006-01-02 15:04:05.999-07", "2025-07-24 11:39:07.635+00")
	lastWatchedTime = lastWatchedTime.UTC() // Ensure it's in UTC for consistency

	require.NoError(t, err)

	tests := []struct {
		name           string
		itemID         string
		serverID       int
		apiKey         string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		wantErr        bool
		expectedItem   *ItemDetails
	}{
		{
			name:     "successful item details",
			itemID:   "test-item-123",
			serverID: 1,
			apiKey:   "test-api-key",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/api/get-item-details/test-item-123", r.URL.Path)
				assert.Equal(t, "1", r.URL.Query().Get("serverId"))
				assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))

				w.Header().Set("Content-Type", "application/json")
				item := ItemDetails{
					LastWatched: CustomTime{lastWatchedTime},
				}
				json.NewEncoder(w).Encode(item)
			},
			wantErr: false,
			expectedItem: &ItemDetails{
				LastWatched: CustomTime{lastWatchedTime},
			},
		},
		{
			name:     "item not found",
			itemID:   "nonexistent",
			serverID: 1,
			apiKey:   "test-api-key",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "Item not found"})
			},
			wantErr:      true,
			expectedItem: nil,
		},
		{
			name:     "server error",
			itemID:   "error-item",
			serverID: 1,
			apiKey:   "test-api-key",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "Internal server error"})
			},
			wantErr:      true,
			expectedItem: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			cfg := &config.StreamystatsConfig{
				URL:      server.URL,
				ServerID: tt.serverID,
			}

			jellyfinCfg := &config.JellyfinConfig{
				APIKey: tt.apiKey,
			}

			client, err := New(cfg, jellyfinCfg)
			require.NoError(t, err)

			item, err := client.GetItemDetails(context.Background(), tt.itemID)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, item)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedItem, item)
			}
		})
	}
}

func TestCustomTime_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		jsonData    string
		expectError bool
		expectedStr string
	}{
		{
			name:        "valid time format",
			jsonData:    `"2025-07-24 11:39:07.635+00"`,
			expectError: false,
			expectedStr: "2025-07-24 11:39:07.635 +0000 UTC",
		},
		{
			name:        "null value",
			jsonData:    `null`,
			expectError: false,
			expectedStr: "0001-01-01 00:00:00 +0000 UTC",
		},
		{
			name:        "empty string",
			jsonData:    `""`,
			expectError: false,
			expectedStr: "0001-01-01 00:00:00 +0000 UTC",
		},
		{
			name:        "invalid format",
			jsonData:    `"2025-07-24T11:39:07Z"`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ct CustomTime
			err := json.Unmarshal([]byte(tt.jsonData), &ct)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedStr, ct.String())
			}
		})
	}
}
