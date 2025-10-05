package cache

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/devopsarr/radarr-go/radarr"
	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/eko/gocache/lib/v4/cache"
	"github.com/eko/gocache/lib/v4/codec"
	"github.com/jon4hz/jellysweep/config"
)

type TagMap map[int32]string

// Cache key prefixes.
const (
	SonarrItemsCachePrefix = "sonarr-items-"
	SonarrTagsCachePrefix  = "sonarr-tags-"
	RadarrItemsCachePrefix = "radarr-items-"
	RadarrTagsCachePrefix  = "radarr-tags-"
)

type EngineCache struct {
	SonarrItemsCache *PrefixedCache[[]sonarr.SeriesResource]
	SonarrTagsCache  *PrefixedCache[TagMap]
	RadarrItemsCache *PrefixedCache[[]radarr.MovieResource]
	RadarrTagsCache  *PrefixedCache[TagMap]
	LibraryCache     *LibraryCache
}

func NewEngineCache(cfg *config.CacheConfig) (*EngineCache, error) {
	return &EngineCache{
		SonarrItemsCache: NewPrefixedCache[[]sonarr.SeriesResource](
			newCacheInstanceByType(cfg),
			cfg.Type,
			SonarrItemsCachePrefix,
		),
		SonarrTagsCache: NewPrefixedCache[TagMap](
			newCacheInstanceByType(cfg),
			cfg.Type,
			SonarrTagsCachePrefix,
		),
		RadarrItemsCache: NewPrefixedCache[[]radarr.MovieResource](
			newCacheInstanceByType(cfg),
			cfg.Type,
			RadarrItemsCachePrefix,
		),
		RadarrTagsCache: NewPrefixedCache[TagMap](
			newCacheInstanceByType(cfg),
			cfg.Type,
			RadarrTagsCachePrefix,
		),
		LibraryCache: NewLibraryCache(cfg),
	}, nil
}

func (e *EngineCache) ClearAll(ctx context.Context) {
	errs := []error{
		e.SonarrItemsCache.Clear(ctx),
		e.SonarrTagsCache.Clear(ctx),
		e.RadarrItemsCache.Clear(ctx),
		e.RadarrTagsCache.Clear(ctx),
		e.LibraryCache.Clear(ctx),
	}
	for _, err := range errs {
		if err != nil {
			log.Errorf("failed to clear cache: %v", err)
		}
	}
}

func newCacheInstanceByType(cfg *config.CacheConfig) *cache.Cache[any] {
	switch cfg.Type {
	case config.CacheTypeMemory:
		return newMemoryCache[any]()
	case config.CacheTypeRedis:
		return newRedisCache[any](cfg)
	default:
		return newMemoryCache[any]()
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
		{
			Stats:     e.LibraryCache.cache.GetStats(),
			CacheName: "library",
		},
	}
}
