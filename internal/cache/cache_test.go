package cache

import (
	"testing"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/stretchr/testify/require"
)

func newMemoryPrefixedCache(t *testing.T, prefix string) *PrefixedCache[TagMap] {
	t.Helper()
	return NewPrefixedCache[TagMap](newMemoryCache[any](), config.CacheTypeMemory, prefix)
}

func TestPrefixedCacheRoundTrip(t *testing.T) {
	c := newMemoryPrefixedCache(t, "tags-")
	want := TagMap{1: "jellysweep-ignore", 2: "4k"}

	require.NoError(t, c.Set(t.Context(), "all", want))

	got, err := c.Get(t.Context(), "all")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestPrefixedCacheMissReturnsError(t *testing.T) {
	c := newMemoryPrefixedCache(t, "tags-")
	_, err := c.Get(t.Context(), "missing")
	require.Error(t, err)
}

func TestPrefixedCacheDeleteAndClear(t *testing.T) {
	c := newMemoryPrefixedCache(t, "tags-")
	require.NoError(t, c.Set(t.Context(), "a", TagMap{1: "a"}))
	require.NoError(t, c.Set(t.Context(), "b", TagMap{2: "b"}))

	require.NoError(t, c.Delete(t.Context(), "a"))
	_, err := c.Get(t.Context(), "a")
	require.Error(t, err)
	_, err = c.Get(t.Context(), "b")
	require.NoError(t, err)

	require.NoError(t, c.Clear(t.Context()))
	_, err = c.Get(t.Context(), "b")
	require.Error(t, err)
}

func TestPrefixedCacheIsolatesPrefixesOnSharedStore(t *testing.T) {
	store := newMemoryCache[any]()
	sonarr := NewPrefixedCache[TagMap](store, config.CacheTypeMemory, SonarrTagsCachePrefix)
	radarr := NewPrefixedCache[TagMap](store, config.CacheTypeMemory, RadarrTagsCachePrefix)

	require.NoError(t, sonarr.Set(t.Context(), "all", TagMap{1: "sonarr"}))
	require.NoError(t, radarr.Set(t.Context(), "all", TagMap{1: "radarr"}))

	got, err := sonarr.Get(t.Context(), "all")
	require.NoError(t, err)
	require.Equal(t, TagMap{1: "sonarr"}, got, "same key under a different prefix must not collide")
}

func TestEngineCacheClearAll(t *testing.T) {
	engineCache, err := NewEngineCache(&config.CacheConfig{Type: config.CacheTypeMemory})
	require.NoError(t, err)

	require.NoError(t, engineCache.SonarrTagsCache.Set(t.Context(), "all", TagMap{1: "a"}))
	require.NoError(t, engineCache.RadarrTagsCache.Set(t.Context(), "all", TagMap{1: "b"}))

	engineCache.ClearAll(t.Context())

	_, err = engineCache.SonarrTagsCache.Get(t.Context(), "all")
	require.Error(t, err)
	_, err = engineCache.RadarrTagsCache.Get(t.Context(), "all")
	require.Error(t, err)
}
