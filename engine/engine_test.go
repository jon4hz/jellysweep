package engine

import (
	"context"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.Config
		wantErr bool
	}{
		{
			name: "valid config with all services",
			config: &config.Config{
				CleanupInterval: 24,
				Libraries: map[string]*config.CleanupConfig{
					"Movies": {
						Enabled:             true,
						ContentAgeThreshold: 30,
						LastStreamThreshold: 90,
						CleanupDelay:        7,
						ExcludeTags:         []string{"favorite"},
					},
				},
				DryRun: true,
				Jellyseerr: &config.JellyseerrConfig{
					URL:    "http://jellyseerr:5055",
					APIKey: "test-api-key",
				},
				Sonarr: &config.SonarrConfig{
					URL:    "http://sonarr:8989",
					APIKey: "test-sonarr-key",
				},
				Radarr: &config.RadarrConfig{
					URL:    "http://radarr:7878",
					APIKey: "test-radarr-key",
				},
				Jellystat: &config.JellystatConfig{
					URL:    "http://jellystat:3000",
					APIKey: "test-jellystat-key",
				},
			},
			wantErr: false,
		},
		{
			name: "valid config without optional services",
			config: &config.Config{
				CleanupInterval: 24,
				Libraries: map[string]*config.CleanupConfig{
					"Movies": {
						Enabled:             true,
						ContentAgeThreshold: 30,
						LastStreamThreshold: 90,
						CleanupDelay:        7,
					},
				},
				DryRun: true,
				Jellystat: &config.JellystatConfig{
					URL:    "http://jellystat:3000",
					APIKey: "test-jellystat-key",
				},
			},
			wantErr: false,
		},
		{
			name: "config with email notifications",
			config: &config.Config{
				CleanupInterval: 24,
				Libraries: map[string]*config.CleanupConfig{
					"Movies": {
						Enabled:             true,
						ContentAgeThreshold: 30,
						LastStreamThreshold: 90,
						CleanupDelay:        7,
					},
				},
				DryRun: true,
				Email: &config.EmailConfig{
					Enabled:   true,
					SMTPHost:  "smtp.example.com",
					SMTPPort:  587,
					Username:  "test@example.com",
					Password:  "password",
					FromEmail: "noreply@example.com",
					FromName:  "JellySweep",
					UseTLS:    true,
				},
				Jellystat: &config.JellystatConfig{
					URL:    "http://jellystat:3000",
					APIKey: "test-jellystat-key",
				},
			},
			wantErr: false,
		},
		{
			name: "config with ntfy notifications",
			config: &config.Config{
				CleanupInterval: 24,
				Libraries: map[string]*config.CleanupConfig{
					"Movies": {
						Enabled:             true,
						ContentAgeThreshold: 30,
						LastStreamThreshold: 90,
						CleanupDelay:        7,
					},
				},
				DryRun: true,
				Ntfy: &config.NtfyConfig{
					Enabled:   true,
					ServerURL: "https://ntfy.sh",
					Topic:     "jellysweep-test",
				},
				Jellystat: &config.JellystatConfig{
					URL:    "http://jellystat:3000",
					APIKey: "test-jellystat-key",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := New(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, engine)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, engine)
				assert.Equal(t, tt.config, engine.cfg)
				assert.NotNil(t, engine.data)
				assert.NotNil(t, engine.data.userNotifications)
			}
		})
	}
}

func TestEngine_Run(t *testing.T) {
	cfg := &config.Config{
		CleanupInterval: 1, // 1 hour for testing
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {
				Enabled:             true,
				ContentAgeThreshold: 30,
				LastStreamThreshold: 90,
				CleanupDelay:        7,
			},
		},
		DryRun: true,
		Jellystat: &config.JellystatConfig{
			URL:    "http://jellystat:3000",
			APIKey: "test-key",
		},
	}

	engine, err := New(cfg)
	require.NoError(t, err)

	t.Run("run with context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := engine.Run(ctx)
		assert.NoError(t, err)
	})
}

func TestEngine_Close(t *testing.T) {
	cfg := &config.Config{
		CleanupInterval: 24,
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {
				Enabled:             true,
				ContentAgeThreshold: 30,
				LastStreamThreshold: 90,
				CleanupDelay:        7,
			},
		},
		DryRun: true,
		Jellystat: &config.JellystatConfig{
			URL:    "http://jellystat:3000",
			APIKey: "test-key",
		},
	}

	engine, err := New(cfg)
	require.NoError(t, err)

	err = engine.Close()
	assert.NoError(t, err)
}

func TestMediaType_Constants(t *testing.T) {
	assert.Equal(t, MediaType("tv"), MediaTypeTV)
	assert.Equal(t, MediaType("movie"), MediaTypeMovie)
}

func TestMediaItem_Structure(t *testing.T) {
	item := MediaItem{
		JellystatID: "test-id",
		Title:       "Test Movie",
		TmdbId:      12345,
		Year:        2023,
		Tags:        []string{"test-tag"},
		MediaType:   MediaTypeMovie,
		RequestedBy: "user@example.com",
		RequestDate: time.Now(),
	}

	assert.Equal(t, "test-id", item.JellystatID)
	assert.Equal(t, "Test Movie", item.Title)
	assert.Equal(t, int32(12345), item.TmdbId)
	assert.Equal(t, int32(2023), item.Year)
	assert.Equal(t, []string{"test-tag"}, item.Tags)
	assert.Equal(t, MediaTypeMovie, item.MediaType)
	assert.Equal(t, "user@example.com", item.RequestedBy)
	assert.False(t, item.RequestDate.IsZero())
}

func TestEngine_DataStructure(t *testing.T) {
	engine := createTestEngine(t)

	assert.NotNil(t, engine.data)
	assert.NotNil(t, engine.data.userNotifications)
	assert.Equal(t, 0, len(engine.data.userNotifications))
	assert.Nil(t, engine.data.jellystatItems)
	assert.Nil(t, engine.data.sonarrItems)
	assert.Nil(t, engine.data.radarrItems)
	assert.Nil(t, engine.data.sonarrTags)
	assert.Nil(t, engine.data.radarrTags)
	assert.Nil(t, engine.data.libraryIDMap)
	assert.Nil(t, engine.data.mediaItems)
}
