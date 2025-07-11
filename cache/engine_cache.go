package cache

import (
	"fmt"

	"github.com/devopsarr/radarr-go/radarr"
	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/eko/gocache/lib/v4/cache"
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

	var cacheInstance *cache.Cache[[]byte]
	switch cfg.Type {
	case config.CacheTypeMemory:
		cacheInstance = newMemoryCache[[]byte]()
	case config.CacheTypeRedis:
		cacheInstance = newRedisCache[[]byte](cfg)
	default:
		return nil, fmt.Errorf("unsupported cache type: %s", cfg.Type)
	}

	return &EngineCache{
		SonarrItemsCache: NewPrefixedCache[[]sonarr.SeriesResource](cacheInstance, SonarrItemsCachePrefix),
		SonarrTagsCache:  NewPrefixedCache[[]sonarr.TagResource](cacheInstance, SonarrTagsCachePrefix),
		RadarrItemsCache: NewPrefixedCache[[]radarr.MovieResource](cacheInstance, RadarrItemsCachePrefix),
		RadarrTagsCache:  NewPrefixedCache[[]radarr.TagResource](cacheInstance, RadarrTagsCachePrefix),
	}, nil
}
