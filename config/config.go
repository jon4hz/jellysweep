package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the configuration for the JellySweep server and its dependencies.
type Config struct {
	// JellySweep holds the configuration for the JellySweep server.
	JellySweep *JellysweepConfig `yaml:"jellysweep"`
	// Jellyseerr holds the configuration for the Jellyseerr server.
	Jellyseerr *JellyseerrConfig `yaml:"jellyseerr"`
	// Sonarr holds the configuration for the Sonarr server.
	Sonarr *SonarrConfig `yaml:"sonarr"`
	// Radarr holds the configuration for the Radarr server.
	Radarr *RadarrConfig `yaml:"radarr"`
	// Jellystat holds the configuration for the Jellystat server.
	Jellystat *JellystatConfig `yaml:"jellystat"`
}

// JellysweepConfig holds the configuration for the JellySweep server.
type JellysweepConfig struct {
	// Listen is the address the JellySweep server will listen on.
	Listen string `yaml:"listen"`
	// CleanupInterval is the interval in hours for the cleanup job.
	CleanupInterval int `yaml:"cleanup_interval"`
	// Libraries is a map of libraries to their cleanup configurations.
	Libraries map[string]*CleanupConfig `yaml:"libraries"`
	// DryRun indicates whether the cleanup job should run in dry-run mode.
	DryRun bool `yaml:"dry_run"`
	// LogLevel is the logging level for the JellySweep server.
	LogLevel string `yaml:"log_level"`
	// Auth holds the authentication configuration for the JellySweep server.
	Auth *AuthConfig `yaml:"auth"`
	// SessionKey is the key used to encrypt session data.
	SessionKey string `yaml:"session_key"`
	// Email holds the email notification configuration.
	Email *EmailConfig `yaml:"email"`
	// Ntfy holds the ntfy notification configuration.
	Ntfy *NtfyConfig `yaml:"ntfy"`
}

// AuthConfig holds the authentication configuration for the JellySweep server.
type AuthConfig struct {
	// OIDC holds the OpenID Connect configuration.
	OIDC *OIDCConfig `yaml:"oidc"`
}

// OIDCConfig holds the OpenID Connect configuration for the JellySweep server.
type OIDCConfig struct {
	// Enabled indicates whether OIDC authentication is enabled.
	Enabled bool `yaml:"enabled"`
	// Issuer is the OIDC issuer URL.
	Issuer string `yaml:"issuer"`
	// ClientID is the OIDC client ID.
	ClientID string `yaml:"client_id"`
	// ClientSecret is the OIDC client secret.
	ClientSecret string `yaml:"client_secret"`
	// RedirectURL is the redirect URL for the oidc flow.
	RedirectURL string `yaml:"redirect_url"`
	// AdminGroup is the group that has admin privileges.
	AdminGroup string `yaml:"admin_group"`
}

// EmailConfig holds the email notification configuration.
type EmailConfig struct {
	// Enabled indicates whether email notifications are enabled.
	Enabled bool `yaml:"enabled"`
	// SMTPHost is the SMTP server host.
	SMTPHost string `yaml:"smtp_host"`
	// SMTPPort is the SMTP server port.
	SMTPPort int `yaml:"smtp_port"`
	// Username is the SMTP username.
	Username string `yaml:"username"`
	// Password is the SMTP password.
	Password string `yaml:"password"`
	// FromEmail is the email address from which notifications are sent.
	FromEmail string `yaml:"from_email"`
	// FromName is the name from which notifications are sent.
	FromName string `yaml:"from_name"`
	// UseTLS indicates whether to use TLS for the SMTP connection.
	UseTLS bool `yaml:"use_tls"`
	// UseSSL indicates whether to use SSL for the SMTP connection.
	UseSSL bool `yaml:"use_ssl"`
	// InsecureSkipVerify indicates whether to skip TLS certificate verification.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
}

// NtfyConfig holds the ntfy notification configuration.
type NtfyConfig struct {
	// Enabled indicates whether ntfy notifications are enabled.
	Enabled bool `yaml:"enabled"`
	// ServerURL is the URL of the ntfy server.
	ServerURL string `yaml:"server_url"`
	// Topic is the ntfy topic to publish notifications to.
	Topic string `yaml:"topic"`
	// Username is the ntfy username for authentication.
	Username string `yaml:"username"`
	// Password is the ntfy password for authentication.
	Password string `yaml:"password"`
	// Token is the ntfy token for authentication.
	Token string `yaml:"token"`
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
	if c.JellySweep == nil {
		return nil, fmt.Errorf("missing jellysweep config")
	}
	if c.Jellyseerr == nil {
		return nil, fmt.Errorf("missing jellyseerr config")
	}
	if c.Sonarr == nil && c.Radarr == nil {
		return nil, fmt.Errorf("either sonarr or radarr config must be provided")
	}
	return &c, nil
}
