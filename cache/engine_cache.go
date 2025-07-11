package cache

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
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
	SonarrTagsCache  *PrefixedCache[map[int32]string]
	RadarrItemsCache *PrefixedCache[[]radarr.MovieResource]
	RadarrTagsCache  *PrefixedCache[map[int32]string]
}

func NewEngineCache(cfg *config.CacheConfig) (*EngineCache, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("cache is not enabled")
	}

	return &EngineCache{
		SonarrItemsCache: NewPrefixedCache[[]sonarr.SeriesResource](newCacheInstanceByType(cfg), SonarrItemsCachePrefix),
		SonarrTagsCache:  NewPrefixedCache[map[int32]string](newCacheInstanceByType(cfg), SonarrTagsCachePrefix),
		RadarrItemsCache: NewPrefixedCache[[]radarr.MovieResource](newCacheInstanceByType(cfg), RadarrItemsCachePrefix),
		RadarrTagsCache:  NewPrefixedCache[map[int32]string](newCacheInstanceByType(cfg), RadarrTagsCachePrefix),
	}, nil
}

func (e *EngineCache) ClearAll(ctx context.Context) {
	errs := []error{
		e.SonarrItemsCache.Clear(ctx),
		e.SonarrTagsCache.Clear(ctx),
		e.RadarrItemsCache.Clear(ctx),
		e.RadarrTagsCache.Clear(ctx),
	}
	for _, err := range errs {
		if err != nil {
			log.Errorf("failed to clear cache: %v", err)
		}
	}
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
