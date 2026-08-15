package config

import (
	"os"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// validateConfig logs warnings (e.g. unreferenced groups); keep test output readable.
	log.SetLevel(log.FatalLevel)
	os.Exit(m.Run())
}

// validTestConfig returns the minimal config that passes validateConfig. Tests mutate a
// fresh copy per case. Map keys are lowercase, mirroring what viper delivers at runtime.
func validTestConfig() *Config {
	return &Config{
		CleanupSchedule: "0 */12 * * *",
		CleanupMode:     CleanupModeAll,
		SessionKey:      "test-session-key",
		Database:        &DatabaseConfig{Type: DatabaseTypeSQLite, Path: "./test.db"},
		Libraries: map[string]*CleanupConfig{
			"movies": {Enabled: true},
		},
		Auth:      &AuthConfig{Jellyfin: &JellyfinAuthConfig{Enabled: true}},
		Jellyfin:  &JellyfinConfig{URL: "http://localhost:8096", APIKey: "key"},
		Radarr:    &RadarrConfig{URL: "http://localhost:7878", APIKey: "key"},
		Jellystat: &JellystatConfig{URL: "http://localhost:3000", APIKey: "key"},
	}
}

func TestValidateConfigMinimalFixture(t *testing.T) {
	require.NoError(t, validateConfig(validTestConfig()))
}

func TestValidateQuotaGroups(t *testing.T) {
	withGroup := func(g *QuotaGroupConfig, ref string) *Config {
		c := validTestConfig()
		c.SweepUntilQuotaGroups = map[string]*QuotaGroupConfig{"media": g}
		c.Libraries["movies"].Filter.SweepUntilQuotaGroup = ref
		return c
	}

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string // empty = expect success
	}{
		{
			name: "percent_used only is valid",
			cfg:  withGroup(&QuotaGroupConfig{PercentUsed: 65}, "media"),
		},
		{
			name: "gb_free only is valid",
			cfg:  withGroup(&QuotaGroupConfig{GBFree: 500}, "media"),
		},
		{
			name: "both targets valid",
			cfg:  withGroup(&QuotaGroupConfig{PercentUsed: 65, GBFree: 500}, "media"),
		},
		{
			name: "unreferenced group is valid (warn only)",
			cfg:  withGroup(&QuotaGroupConfig{PercentUsed: 65}, ""),
		},
		{
			name:    "no target set",
			cfg:     withGroup(&QuotaGroupConfig{}, "media"),
			wantErr: "at least one of percent_used or gb_free",
		},
		{
			name:    "nil group config",
			cfg:     withGroup(nil, "media"),
			wantErr: "no configuration",
		},
		{
			name:    "percent_used at 100",
			cfg:     withGroup(&QuotaGroupConfig{PercentUsed: 100}, "media"),
			wantErr: "percent_used must be between",
		},
		{
			name:    "percent_used negative",
			cfg:     withGroup(&QuotaGroupConfig{PercentUsed: -5}, "media"),
			wantErr: "percent_used must be between",
		},
		{
			name:    "gb_free negative",
			cfg:     withGroup(&QuotaGroupConfig{GBFree: -1}, "media"),
			wantErr: "gb_free must be >= 0",
		},
		{
			name:    "invalid order value",
			cfg:     withGroup(&QuotaGroupConfig{PercentUsed: 65, Order: "biggest"}, "media"),
			wantErr: "order must be one of",
		},
		{
			name: "valid order values",
			cfg:  withGroup(&QuotaGroupConfig{PercentUsed: 65, Order: CleanupOrderLargestFirst}, "media"),
		},
		{
			name:    "library references undefined group",
			cfg:     withGroup(&QuotaGroupConfig{PercentUsed: 65}, "ghost"),
			wantErr: `undefined sweep_until_quota_group "ghost"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateQuotaGroupNamesAreCaseInsensitive(t *testing.T) {
	// Viper lowercases YAML map keys but not string values, so a group written as
	// `Media:` arrives as map key "media" while a reference `sweep_until_quota_group:
	// "Media"` keeps its case. Validation normalises both sides so any casing works,
	// and rewrites the reference to the canonical lowercase key.
	c := validTestConfig()
	c.SweepUntilQuotaGroups = map[string]*QuotaGroupConfig{"media": {PercentUsed: 65}}
	c.Libraries["movies"].Filter.SweepUntilQuotaGroup = "Media"
	require.NoError(t, validateConfig(c))
	assert.Equal(t, "media", c.Libraries["movies"].Filter.SweepUntilQuotaGroup)

	// Mixed-case group definitions (possible when the config is built directly rather
	// than through viper) are normalised too.
	c = validTestConfig()
	c.SweepUntilQuotaGroups = map[string]*QuotaGroupConfig{"Media": {PercentUsed: 65}}
	c.Libraries["movies"].Filter.SweepUntilQuotaGroup = "MEDIA"
	require.NoError(t, validateConfig(c))
	assert.Contains(t, c.SweepUntilQuotaGroups, "media")

	// Two groups whose names differ only by case collide after normalisation.
	c = validTestConfig()
	c.SweepUntilQuotaGroups = map[string]*QuotaGroupConfig{
		"Media": {PercentUsed: 65},
		"media": {PercentUsed: 70},
	}
	err := validateConfig(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different casing")
}

func TestQuotaGroupConfigGetOrder(t *testing.T) {
	assert.Equal(t, CleanupOrderDefault, (&QuotaGroupConfig{}).GetOrder())
	assert.Equal(t, CleanupOrderTitle, (&QuotaGroupConfig{Order: CleanupOrderTitle}).GetOrder())
	assert.Equal(t, CleanupOrderSmallestFirst, (&QuotaGroupConfig{Order: CleanupOrderSmallestFirst}).GetOrder())
}
