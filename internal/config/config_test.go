package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validConfig() *Config {
	return &Config{
		CleanupSchedule: "0 */12 * * *",
		CleanupMode:     CleanupModeAll,
		SessionKey:      "secret",
		Database:        &DatabaseConfig{Type: DatabaseTypeSQLite, Path: "./data/test.db"},
		Libraries:       map[string]*CleanupConfig{"Movies": {Enabled: true}},
		Auth:            &AuthConfig{Jellyfin: &JellyfinAuthConfig{Enabled: true}},
		Jellyfin:        &JellyfinConfig{URL: "http://jellyfin:8096", APIKey: "key"},
		Sonarr:          &SonarrConfig{URL: "http://sonarr:8989", APIKey: "key"},
		Radarr:          &RadarrConfig{URL: "http://radarr:7878", APIKey: "key"},
		Jellystat:       &JellystatConfig{URL: "http://jellystat:3000", APIKey: "key"},
	}
}

func TestValidateConfigAcceptsValidConfig(t *testing.T) {
	cfg := validConfig()
	require.NoError(t, validateConfig(cfg))
	require.Equal(t, CacheTypeMemory, cfg.Cache.Type, "cache defaults to memory")
}

func TestValidateConfigRejections(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"nil config", nil},
		{"missing schedule", func(c *Config) { c.CleanupSchedule = "" }},
		{"schedule with wrong field count", func(c *Config) { c.CleanupSchedule = "*/5 * * *" }},
		{"missing cleanup mode", func(c *Config) { c.CleanupMode = "" }},
		{"unknown cleanup mode", func(c *Config) { c.CleanupMode = "sometimes" }},
		{"keep_episodes without keep count", func(c *Config) { c.CleanupMode = CleanupModeKeepEpisodes; c.KeepCount = 0 }},
		{"keep_seasons without keep count", func(c *Config) { c.CleanupMode = CleanupModeKeepSeasons; c.KeepCount = 0 }},
		{"missing session key", func(c *Config) { c.SessionKey = "" }},
		{"missing database", func(c *Config) { c.Database = nil }},
		{"sqlite without path", func(c *Config) { c.Database = &DatabaseConfig{Type: DatabaseTypeSQLite} }},
		{"postgres without host", func(c *Config) {
			c.Database = &DatabaseConfig{Type: DatabaseTypePostgres, Name: "db", User: "u", Port: 5432}
		}},
		{"postgres with bad port", func(c *Config) {
			c.Database = &DatabaseConfig{Type: DatabaseTypePostgres, Host: "h", Name: "db", User: "u", Port: 70000}
		}},
		{"postgres with bad ssl mode", func(c *Config) {
			c.Database = &DatabaseConfig{Type: DatabaseTypePostgres, Host: "h", Name: "db", User: "u", Port: 5432, SSLMode: "maybe"}
		}},
		{"unknown database type", func(c *Config) { c.Database = &DatabaseConfig{Type: "oracle"} }},
		{"no libraries", func(c *Config) { c.Libraries = nil }},
		{"missing auth", func(c *Config) { c.Auth = nil }},
		{"oidc without issuer", func(c *Config) {
			c.Auth = &AuthConfig{OIDC: &OIDCConfig{Enabled: true, ClientID: "id", ClientSecret: "s", RedirectURL: "r", AdminGroup: "a"}}
		}},
		{"redis cache without url", func(c *Config) { c.Cache = &CacheConfig{Type: CacheTypeRedis} }},
		{"missing jellyfin", func(c *Config) { c.Jellyfin = nil }},
		{"jellyfin without api key", func(c *Config) { c.Jellyfin = &JellyfinConfig{URL: "http://jf"} }},
		{"no auth method enabled", func(c *Config) { c.Auth = &AuthConfig{Jellyfin: &JellyfinAuthConfig{Enabled: false}} }},
		{"neither sonarr nor radarr", func(c *Config) { c.Sonarr = nil; c.Radarr = nil }},
		{"sonarr without api key", func(c *Config) { c.Sonarr = &SonarrConfig{URL: "http://sonarr"} }},
		{"radarr without url", func(c *Config) { c.Radarr = &RadarrConfig{APIKey: "key"} }},
		{"no stats backend", func(c *Config) { c.Jellystat = nil }},
		{"both stats backends", func(c *Config) {
			c.Streamystats = &StreamystatsConfig{URL: "http://streamystats", ServerID: 1}
		}},
		{"jellystat without api key", func(c *Config) { c.Jellystat = &JellystatConfig{URL: "http://jellystat"} }},
		{"streamystats without server id", func(c *Config) {
			c.Jellystat = nil
			c.Streamystats = &StreamystatsConfig{URL: "http://streamystats"}
		}},
		{"jellyseerr without url", func(c *Config) { c.Jellyseerr = &JellyseerrConfig{APIKey: "key"} }},
		{"tunarr without url", func(c *Config) { c.Tunarr = &TunarrConfig{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg *Config
			if tt.mutate != nil {
				cfg = validConfig()
				tt.mutate(cfg)
			}
			require.Error(t, validateConfig(cfg))
		})
	}
}

func TestValidateConfigPostgresDefaultsSSLMode(t *testing.T) {
	cfg := validConfig()
	cfg.Database = &DatabaseConfig{Type: DatabaseTypePostgres, Host: "h", Name: "db", User: "u", Port: 5432}
	require.NoError(t, validateConfig(cfg))
}

func TestValidateConfigDefaultsDatabaseTypeToSQLite(t *testing.T) {
	cfg := validConfig()
	cfg.Database = &DatabaseConfig{Path: "./x.db"}
	require.NoError(t, validateConfig(cfg))
	require.Equal(t, DatabaseTypeSQLite, cfg.Database.Type)
}

func TestGetLibraryConfigIsCaseInsensitive(t *testing.T) {
	cfg := &Config{Libraries: map[string]*CleanupConfig{"movies": {CleanupDelay: 7}}}
	require.NotNil(t, cfg.GetLibraryConfig("Movies"))
	require.NotNil(t, cfg.GetLibraryConfig("MOVIES"))
	require.Nil(t, cfg.GetLibraryConfig("TV"))
	require.Nil(t, (&Config{}).GetLibraryConfig("Movies"))
}

func TestConfigDefaults(t *testing.T) {
	var nilCfg *Config
	require.Equal(t, CleanupMode("all"), nilCfg.GetCleanupMode())
	require.Equal(t, 1, nilCfg.GetKeepCount())
	require.Equal(t, CleanupModeKeepSeasons, (&Config{CleanupMode: CleanupModeKeepSeasons}).GetCleanupMode())
	require.Equal(t, 3, (&Config{KeepCount: 3}).GetKeepCount())
	require.Equal(t, 1, (&Config{KeepCount: -1}).GetKeepCount())
}

func TestCleanupConfigDefaultsAndDeprecatedFallbacks(t *testing.T) {
	empty := &CleanupConfig{}
	require.Equal(t, 30, empty.GetContentAgeThreshold())
	require.Equal(t, 30, empty.GetLastStreamThreshold())
	require.Equal(t, int64(0), empty.GetContentSizeThreshold())
	require.Equal(t, 30, empty.GetCleanupDelay())
	require.Equal(t, 90, empty.GetProtectionPeriod())
	require.Empty(t, empty.GetExcludeTags())

	deprecated := &CleanupConfig{
		ContentAgeThreshold:  10,
		LastStreamThreshold:  11,
		ContentSizeThreshold: 12,
		ExcludeTags:          []string{"old"},
	}
	require.Equal(t, 10, deprecated.GetContentAgeThreshold())
	require.Equal(t, 11, deprecated.GetLastStreamThreshold())
	require.Equal(t, int64(12), deprecated.GetContentSizeThreshold())
	require.Equal(t, []string{"old"}, deprecated.GetExcludeTags())

	both := &CleanupConfig{
		ContentAgeThreshold:  10,
		LastStreamThreshold:  11,
		ContentSizeThreshold: 12,
		ExcludeTags:          []string{"old"},
		CleanupDelay:         5,
		ProtectionPeriod:     14,
		Filter: FilterConfig{
			ContentAgeThreshold:  20,
			LastStreamThreshold:  21,
			ContentSizeThreshold: 22,
			ExcludeTags:          []string{"new"},
		},
	}
	require.Equal(t, 20, both.GetContentAgeThreshold(), "filter.* settings win over deprecated ones")
	require.Equal(t, 21, both.GetLastStreamThreshold())
	require.Equal(t, int64(22), both.GetContentSizeThreshold())
	require.Equal(t, []string{"new"}, both.GetExcludeTags())
	require.Equal(t, 5, both.GetCleanupDelay())
	require.Equal(t, 14, both.GetProtectionPeriod())
}

func TestGetMovieReleaseDateMax(t *testing.T) {
	disabled, err := (&CleanupConfig{}).GetMovieReleaseDateMax()
	require.NoError(t, err)
	require.Nil(t, disabled, "empty value disables the filter")

	got, err := (&CleanupConfig{Filter: FilterConfig{MovieReleaseDateMax: "2024-01-02"}}).GetMovieReleaseDateMax()
	require.NoError(t, err)
	require.Equal(t, time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC), *got)

	_, err = (&CleanupConfig{Filter: FilterConfig{MovieReleaseDateMax: "yesterday"}}).GetMovieReleaseDateMax()
	require.Error(t, err)
}

func TestParseConfigTimeLayouts(t *testing.T) {
	want := time.Date(2024, time.January, 2, 15, 4, 5, 0, time.UTC)
	for _, value := range []string{
		"2024-01-02T15:04:05Z",
		" 2024-01-02 15:04:05 ",
		"2024-01-02T15:04:05",
	} {
		got, err := parseConfigTime(value)
		require.NoError(t, err, value)
		require.Equal(t, want, got, value)
	}

	dateOnly, err := parseConfigTime("2024-01-02")
	require.NoError(t, err)
	require.Equal(t, time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC), dateOnly)

	_, err = parseConfigTime("02.01.2024")
	require.Error(t, err)
}

func TestURLSanitize(t *testing.T) {
	require.Equal(t, "http://host:1234", urlSanitize(" http://host:1234/ "))
	require.Equal(t, "http://host", urlSanitize("http://host"))
}
