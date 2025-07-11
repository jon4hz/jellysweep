package cache

import (
	"fmt"

	"github.com/devopsarr/radarr-go/radarr"
	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/eko/gocache/lib/v4/cache"
	"github.com/eko/gocache/lib/v4/codec"
	"github.com/jon4hz/jellysweep/config"
)

// Cache key prefixes.
const (
	SonarrItemsCachePrefix = "sonarr-items-"
	SonarrTagsCachePrefix  = "sonarr-tags-"
	RadarrItemsCachePrefix = "radarr-items-"
	RadarrTagsCachePrefix  = "radarr-tags-"
)

type EngineCache struct {
	SonarrItemsCache *PrefixedCache[[]sonarr.SeriesResource]
	SonarrTagsCache  *PrefixedCache[[]sonarr.TagResource]
	RadarrItemsCache *PrefixedCache[[]radarr.MovieResource]
	RadarrTagsCache  *PrefixedCache[[]radarr.TagResource]
}

func NewEngineCache(cfg *config.CacheConfig) (*EngineCache, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("cache is not enabled")
	}

	return &EngineCache{
		SonarrItemsCache: NewPrefixedCache[[]sonarr.SeriesResource](newCacheInstanceByType(cfg), SonarrItemsCachePrefix),
		SonarrTagsCache:  NewPrefixedCache[[]sonarr.TagResource](newCacheInstanceByType(cfg), SonarrTagsCachePrefix),
		RadarrItemsCache: NewPrefixedCache[[]radarr.MovieResource](newCacheInstanceByType(cfg), RadarrItemsCachePrefix),
		RadarrTagsCache:  NewPrefixedCache[[]radarr.TagResource](newCacheInstanceByType(cfg), RadarrTagsCachePrefix),
	}, nil
}

func newCacheInstanceByType(cfg *config.CacheConfig) *cache.Cache[[]byte] {
	switch cfg.Type {
	case config.CacheTypeMemory:
		return newMemoryCache[[]byte]()
	case config.CacheTypeRedis:
		return newRedisCache[[]byte](cfg)
	default:
		return newMemoryCache[[]byte]()
	}
}

type Stats struct {
	*codec.Stats
	CacheName string `json:"cacheName"`
}

func (e *EngineCache) GetStats() []*Stats {
	return []*Stats{
		{
			Stats:     e.SonarrItemsCache.GetStats(),
			CacheName: "sonarr-items",
		},
		{
			Stats:     e.SonarrTagsCache.GetStats(),
			CacheName: "sonarr-tags",
		},
		{
			Stats:     e.RadarrItemsCache.GetStats(),
			CacheName: "radarr-items",
		},
		{
			Stats:     e.RadarrTagsCache.GetStats(),
			CacheName: "radarr-tags",
		},
	}
}
