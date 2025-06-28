package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the configuration for the Jellysweep server and its dependencies.
type Config struct {
	// Jellysweep holds the configuration for the Jellysweep server.
	Jellysweep *JellysweepConfig `yaml:"jellysweep"`
	// Jellyseerr holds the configuration for the Jellyseerr server.
	Jellyseerr *JellyseerrConfig `yaml:"jellyseerr"`
	// Sonarr holds the configuration for the Sonarr server.
	Sonarr *SonarrConfig `yaml:"sonarr"`
	// Radarr holds the configuration for the Radarr server.
	Radarr *RadarrConfig `yaml:"radarr"`
	// Jellystat holds the configuration for the Jellystat server.
	Jellystat *JellystatConfig `yaml:"jellystat"`
}

// JellysweepConfig holds the configuration for the Jellysweep server.
type JellysweepConfig struct {
	// Listen is the address the Jellysweep server will listen on.
	Listen string `yaml:"listen"`
	// CleanupInterval is the interval in hours for the cleanup job.
	CleanupInterval int `yaml:"cleanup_interval"`
	// Libraries is a map of libraries to their cleanup configurations.
	Libraries map[string]*CleanupConfig `yaml:"libraries"`
	// DryRun indicates whether the cleanup job should run in dry-run mode.
	DryRun bool `yaml:"dry_run"`
}

type CleanupConfig struct {
	// Enabled indicates whether the cleanup job is enabled.
	Enabled bool `yaml:"enabled"`
	// RequestAgeThreshold is the minimum age in days for a request to be eligible for cleanup.
	RequestAgeThreshold int `yaml:"request_age_threshold"`
	// LastStreamThreshold is the minimum time in days since the last stream for content to be eligible for cleanup.
	LastStreamThreshold int `yaml:"last_stream_threshold"`
	// ExcludeTags is a list of tags to exclude from deletion.
	ExcludeTags []string `yaml:"exclude_tags"`
	// CleanupDelay is the delay in days before a media item is deleted after being marked for deletion.
	CleanupDelay int `yaml:"cleanup_delay"`
}

// JellyseerrConfig holds the configuration for the Jellyseerr server.
type JellyseerrConfig struct {
	// URL is the base URL of the Jellyseerr server.
	URL string `yaml:"url"`
	// APIKey is the API key for the Jellyseerr server.
	APIKey string `yaml:"api_key"`
}

// SonarrConfig holds the configuration for the Sonarr server.
type SonarrConfig struct {
	// URL is the base URL of the Sonarr server.
	URL string `yaml:"url"`
	// APIKey is the API key for the Sonarr server.
	APIKey string `yaml:"api_key"`
}

// RadarrConfig holds the configuration for the Radarr server.
type RadarrConfig struct {
	// URL is the base URL of the Radarr server.
	URL string `yaml:"url"`
	// APIKey is the API key for the Radarr server.
	APIKey string `yaml:"api_key"`
}

// JellystatConfig holds the configuration for the Jellystat server.
type JellystatConfig struct {
	// URL is the base URL of the Jellystat server.
	URL string `yaml:"url"`
	// APIKey is the API key for the Jellystat server.
	APIKey string `yaml:"api_key"`
}

// Load reads the configuration from the specified path and returns a Config struct.
func Load(path string) (*Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(data, &c)
	if err != nil {
		return nil, err
	}
	if c.Jellysweep == nil {
		return nil, fmt.Errorf("missing jellysweep config")
	}
	if c.Jellyseerr == nil {
		return nil, fmt.Errorf("missing jellyseerr config")
	}
	if c.Sonarr == nil {
		return nil, fmt.Errorf("missing sonarr config")
	}
	if c.Radarr == nil {
		return nil, fmt.Errorf("missing radarr config")
	}
	return &c, nil
}
