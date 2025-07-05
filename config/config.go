package config

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/spf13/viper"
)

// Config holds the configuration for the JellySweep server and its dependencies.
type Config struct {
	// Listen is the address the JellySweep server will listen on.
	Listen string `yaml:"listen" mapstructure:"listen"`
	// CleanupInterval is the interval in hours for the cleanup job.
	CleanupInterval int `yaml:"cleanup_interval" mapstructure:"cleanup_interval"`
	// Libraries is a map of libraries to their cleanup configurations.
	Libraries map[string]*CleanupConfig `yaml:"libraries" mapstructure:"libraries"`
	// DryRun indicates whether the cleanup job should run in dry-run mode.
	DryRun bool `yaml:"dry_run" mapstructure:"dry_run"`
	// Auth holds the authentication configuration for the JellySweep server.
	Auth *AuthConfig `yaml:"auth" mapstructure:"auth"`
	// SessionKey is the key used to encrypt session data.
	SessionKey string `yaml:"session_key" mapstructure:"session_key"`
	// SessionMaxAge is the maximum age of a session in seconds.
	SessionMaxAge int `yaml:"session_max_age" mapstructure:"session_max_age"`
	// Email holds the email notification configuration.
	Email *EmailConfig `yaml:"email" mapstructure:"email"`
	// Ntfy holds the ntfy notification configuration.
	Ntfy *NtfyConfig `yaml:"ntfy" mapstructure:"ntfy"`
	// WebPush holds the webpush notification configuration.
	WebPush *WebPushConfig `yaml:"webpush" mapstructure:"webpush"`
	// ServerURL is the base URL of the JellySweep server.
	ServerURL string `yaml:"server_url" mapstructure:"server_url"`

	// Jellyseerr holds the configuration for the Jellyseerr server.
	Jellyseerr *JellyseerrConfig `yaml:"jellyseerr" mapstructure:"jellyseerr"`
	// Sonarr holds the configuration for the Sonarr server.
	Sonarr *SonarrConfig `yaml:"sonarr" mapstructure:"sonarr"`
	// Radarr holds the configuration for the Radarr server.
	Radarr *RadarrConfig `yaml:"radarr" mapstructure:"radarr"`
	// Jellystat holds the configuration for the Jellystat server.
	Jellystat *JellystatConfig `yaml:"jellystat" mapstructure:"jellystat"`
	// Gravatar holds the configuration for Gravatar profile pictures.
	Gravatar *GravatarConfig `yaml:"gravatar" mapstructure:"gravatar"`
}

// AuthConfig holds the authentication configuration for the JellySweep server.
type AuthConfig struct {
	// OIDC holds the OpenID Connect configuration.
	OIDC *OIDCConfig `yaml:"oidc" mapstructure:"oidc"`
	// Jellyfin holds the Jellyfin authentication configuration.
	Jellyfin *JellyfinConfig `yaml:"jellyfin" mapstructure:"jellyfin"`
}

// OIDCConfig holds the OpenID Connect configuration for the JellySweep server.
type OIDCConfig struct {
	// Enabled indicates whether OIDC authentication is enabled.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// Name is the display name for the OIDC provider.
	Name string `yaml:"name" mapstructure:"name"`
	// Issuer is the OIDC issuer URL.
	Issuer string `yaml:"issuer" mapstructure:"issuer"`
	// ClientID is the OIDC client ID.
	ClientID string `yaml:"client_id" mapstructure:"client_id"`
	// ClientSecret is the OIDC client secret.
	ClientSecret string `yaml:"client_secret" mapstructure:"client_secret"`
	// RedirectURL is the redirect URL for the oidc flow.
	RedirectURL string `yaml:"redirect_url" mapstructure:"redirect_url"`
	// AdminGroup is the group that has admin privileges.
	AdminGroup string `yaml:"admin_group" mapstructure:"admin_group"`
}

// JellyfinConfig holds the Jellyfin authentication configuration for the JellySweep server.
type JellyfinConfig struct {
	// Enabled indicates whether Jellyfin authentication is enabled.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// URL is the Jellyfin server URL.
	URL string `yaml:"url" mapstructure:"url"`
}

// EmailConfig holds the email notification configuration.
type EmailConfig struct {
	// Enabled indicates whether email notifications are enabled.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// SMTPHost is the SMTP server host.
	SMTPHost string `yaml:"smtp_host" mapstructure:"smtp_host"`
	// SMTPPort is the SMTP server port.
	SMTPPort int `yaml:"smtp_port" mapstructure:"smtp_port"`
	// Username is the SMTP username.
	Username string `yaml:"username" mapstructure:"username"`
	// Password is the SMTP password.
	Password string `yaml:"password" mapstructure:"password"`
	// FromEmail is the email address from which notifications are sent.
	FromEmail string `yaml:"from_email" mapstructure:"from_email"`
	// FromName is the name from which notifications are sent.
	FromName string `yaml:"from_name" mapstructure:"from_name"`
	// UseTLS indicates whether to use TLS for the SMTP connection.
	UseTLS bool `yaml:"use_tls" mapstructure:"use_tls"`
	// UseSSL indicates whether to use SSL for the SMTP connection.
	UseSSL bool `yaml:"use_ssl" mapstructure:"use_ssl"`
	// InsecureSkipVerify indicates whether to skip TLS certificate verification.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify" mapstructure:"insecure_skip_verify"`
}

// NtfyConfig holds the ntfy notification configuration.
type NtfyConfig struct {
	// Enabled indicates whether ntfy notifications are enabled.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// ServerURL is the URL of the ntfy server.
	ServerURL string `yaml:"server_url" mapstructure:"server_url"`
	// Topic is the ntfy topic to publish notifications to.
	Topic string `yaml:"topic" mapstructure:"topic"`
	// Username is the ntfy username for authentication.
	Username string `yaml:"username" mapstructure:"username"`
	// Password is the ntfy password for authentication.
	Password string `yaml:"password" mapstructure:"password"`
	// Token is the ntfy token for authentication.
	Token string `yaml:"token" mapstructure:"token"`
}

// WebPushConfig holds the webpush notification configuration.
type WebPushConfig struct {
	// Enabled indicates whether webpush notifications are enabled.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// VAPIDEmail is the email associated with the VAPID keys.
	VAPIDEmail string `yaml:"vapid_email" mapstructure:"vapid_email"`
	// PublicKey is the VAPID public key.
	PublicKey string `yaml:"public_key" mapstructure:"public_key"`
	// PrivateKey is the VAPID private key.
	PrivateKey string `yaml:"private_key" mapstructure:"private_key"`
}

type CleanupConfig struct {
	// Enabled indicates whether the cleanup job is enabled.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// RequestAgeThreshold is the minimum age in days for a request to be eligible for cleanup.
	RequestAgeThreshold int `yaml:"request_age_threshold" mapstructure:"request_age_threshold"`
	// LastStreamThreshold is the minimum time in days since the last stream for content to be eligible for cleanup.
	LastStreamThreshold int `yaml:"last_stream_threshold" mapstructure:"last_stream_threshold"`
	// ExcludeTags is a list of tags to exclude from deletion.
	ExcludeTags []string `yaml:"exclude_tags" mapstructure:"exclude_tags"`
	// CleanupDelay is the delay in days before a media item is deleted after being marked for deletion.
	CleanupDelay int `yaml:"cleanup_delay" mapstructure:"cleanup_delay"`
}

// JellyseerrConfig holds the configuration for the Jellyseerr server.
type JellyseerrConfig struct {
	// URL is the base URL of the Jellyseerr server.
	URL string `yaml:"url" mapstructure:"url"`
	// APIKey is the API key for the Jellyseerr server.
	APIKey string `yaml:"api_key" mapstructure:"api_key"`
}

// SonarrConfig holds the configuration for the Sonarr server.
type SonarrConfig struct {
	// URL is the base URL of the Sonarr server.
	URL string `yaml:"url" mapstructure:"url"`
	// APIKey is the API key for the Sonarr server.
	APIKey string `yaml:"api_key" mapstructure:"api_key"`
}

// RadarrConfig holds the configuration for the Radarr server.
type RadarrConfig struct {
	// URL is the base URL of the Radarr server.
	URL string `yaml:"url" mapstructure:"url"`
	// APIKey is the API key for the Radarr server.
	APIKey string `yaml:"api_key" mapstructure:"api_key"`
}

// JellystatConfig holds the configuration for the Jellystat server.
type JellystatConfig struct {
	// URL is the base URL of the Jellystat server.
	URL string `yaml:"url" mapstructure:"url"`
	// APIKey is the API key for the Jellystat server.
	APIKey string `yaml:"api_key" mapstructure:"api_key"`
}

// GravatarConfig holds the configuration for Gravatar profile pictures.
type GravatarConfig struct {
	// Enabled indicates whether Gravatar support is enabled.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// DefaultImage is the default image to use when no Gravatar is found.
	// Valid values: "404", "mp", "identicon", "monsterid", "wavatar", "retro", "robohash", "blank"
	DefaultImage string `yaml:"default_image" mapstructure:"default_image"`
	// Rating is the maximum rating for Gravatar images.
	// Valid values: "g", "pg", "r", "x"
	Rating string `yaml:"rating" mapstructure:"rating"`
	// Size is the size of the Gravatar image in pixels (1-2048).
	Size int `yaml:"size" mapstructure:"size"`
}

// Load reads the configuration from the specified path and returns a Config struct.
// If path is empty, it will use default search paths for config files.
// If no config file is found, it will generate a default one in the current directory.
func Load(path string) (*Config, error) {
	v := viper.New()

	// Set default values
	setDefaults(v)

	// Configure Viper
	v.SetConfigType("yaml")
	v.SetEnvPrefix("JELLYSWEEP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var configFileFound bool
	if path != "" {
		// Use specific config file
		v.SetConfigFile(path)
	} else {
		// Search for config in common locations
		v.SetConfigName("config")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.jellysweep")
		v.AddConfigPath("/etc/jellysweep")
	}

	// Read the config file
	if err := v.ReadInConfig(); err != nil {
		// If no config file is found, use defaults
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	} else {
		configFileFound = true
	}

	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Print info about config file usage
	if configFileFound {
		log.Debug("Using config file", "file", v.ConfigFileUsed())
		log.Debug("Environment variables with JELLYSWEEP_ prefix will override config file values")
	}

	// Validate required configs
	if err := validateConfig(&c); err != nil {
		return nil, err
	}

	return &c, nil
}

// setDefaults sets default values for the configuration.
func setDefaults(v *viper.Viper) {
	// JellySweep defaults
	v.SetDefault("jellysweep.listen", "0.0.0.0:3002")
	v.SetDefault("jellysweep.cleanup_interval", 12)
	v.SetDefault("jellysweep.dry_run", false)
	v.SetDefault("jellysweep.server_url", "http://localhost:3002")
	v.SetDefault("jellysweep.session_max_age", 172800) // 48 hour

	// Auth defaults
	v.SetDefault("jellysweep.auth.oidc.enabled", false)
	v.SetDefault("jellysweep.auth.oidc.name", "OIDC")
	v.SetDefault("jellysweep.auth.oidc.issuer", "")
	v.SetDefault("jellysweep.auth.oidc.client_id", "")
	v.SetDefault("jellysweep.auth.oidc.client_secret", "")
	v.SetDefault("jellysweep.auth.oidc.redirect_url", "")
	v.SetDefault("jellysweep.auth.oidc.admin_group", "")
	v.SetDefault("jellysweep.auth.jellyfin.enabled", true)

	// Email defaults
	v.SetDefault("jellysweep.email.enabled", false)
	v.SetDefault("jellysweep.email.smtp_port", 587)
	v.SetDefault("jellysweep.email.from_name", "JellySweep")
	v.SetDefault("jellysweep.email.use_tls", true)
	v.SetDefault("jellysweep.email.use_ssl", false)
	v.SetDefault("jellysweep.email.insecure_skip_verify", false)

	// Ntfy defaults
	v.SetDefault("jellysweep.ntfy.enabled", false)
	v.SetDefault("jellysweep.ntfy.server_url", "https://ntfy.sh")

	// Gravatar defaults
	v.SetDefault("jellysweep.gravatar.enabled", false)
	v.SetDefault("jellysweep.gravatar.default_image", "robohash")
	v.SetDefault("jellysweep.gravatar.rating", "g")
	v.SetDefault("jellysweep.gravatar.size", 80)

	// WebPush defaults
	v.SetDefault("jellysweep.webpush.enabled", false)
	v.SetDefault("jellysweep.webpush.ttl", 60)
}

// validateConfig validates the configuration.
func validateConfig(c *Config) error {
	if c == nil {
		return fmt.Errorf("missing jellysweep config")
	}

	// Validate auth configuration
	if c.Auth == nil {
		return fmt.Errorf("missing auth config")
	}

	authEnabled := false
	if c.Auth.OIDC != nil && c.Auth.OIDC.Enabled {
		authEnabled = true
		if c.Auth.OIDC.Issuer == "" {
			return fmt.Errorf("OIDC issuer is required when OIDC is enabled")
		}
		if c.Auth.OIDC.ClientID == "" {
			return fmt.Errorf("OIDC client ID is required when OIDC is enabled")
		}
		if c.Auth.OIDC.ClientSecret == "" {
			return fmt.Errorf("OIDC client secret is required when OIDC is enabled")
		}
		if c.Auth.OIDC.RedirectURL == "" {
			return fmt.Errorf("OIDC redirect URL is required when OIDC is enabled")
		}
		if c.Auth.OIDC.AdminGroup == "" {
			return fmt.Errorf("OIDC admin group is required when OIDC is enabled")
		}
	}

	if c.Auth.Jellyfin != nil && c.Auth.Jellyfin.Enabled {
		authEnabled = true
		if c.Auth.Jellyfin.URL == "" {
			return fmt.Errorf("Jellyfin URL is required when Jellyfin auth is enabled") //nolint:staticcheck
		}
	}

	if !authEnabled {
		log.Warn("No authentication methods enabled, JellySweep will run without authentication")
	}
	// Validate external services
	if c.Jellyseerr == nil {
		return fmt.Errorf("missing jellyseerr config")
	}
	if c.Jellyseerr.URL == "" {
		return fmt.Errorf("jellyseerr URL is required")
	}
	if c.Jellyseerr.APIKey == "" {
		return fmt.Errorf("jellyseerr API key is required")
	}

	if c.Sonarr == nil && c.Radarr == nil {
		return fmt.Errorf("either sonarr or radarr config must be provided")
	}

	if c.Sonarr != nil {
		if c.Sonarr.URL == "" {
			return fmt.Errorf("sonarr URL is required when sonarr is configured")
		}
		if c.Sonarr.APIKey == "" {
			return fmt.Errorf("sonarr API key is required when sonarr is configured")
		}
	}

	if c.Radarr != nil {
		if c.Radarr.URL == "" {
			return fmt.Errorf("radarr URL is required when radarr is configured")
		}
		if c.Radarr.APIKey == "" {
			return fmt.Errorf("radarr API key is required when radarr is configured")
		}
	}

	return nil
}

// GetLibraryConfig returns the cleanup configuration for a specific library.
// This function handles the case-sensitivity issue where viper normalizes map keys
// to lowercase, but library names from Jellystat are case-sensitive.
func (c *Config) GetLibraryConfig(libraryName string) *CleanupConfig {
	if c.Libraries == nil {
		return nil
	}

	// First try exact match (for backward compatibility)
	if config, exists := c.Libraries[libraryName]; exists {
		return config
	}

	libraryNameLower := strings.ToLower(libraryName)
	for key, config := range c.Libraries {
		if strings.ToLower(key) == libraryNameLower {
			return config
		}
	}

	return nil
}

// IsAuthenticationEnabled returns true if at least one authentication method is enabled.
func (c *Config) IsAuthenticationEnabled() bool {
	if c.Auth == nil {
		return false
	}

	if c.Auth.OIDC != nil && c.Auth.OIDC.Enabled {
		return true
	}

	if c.Auth.Jellyfin != nil && c.Auth.Jellyfin.Enabled {
		return true
	}

	return false
}
