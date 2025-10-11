package cache

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/devopsarr/radarr-go/radarr"
	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/eko/gocache/lib/v4/cache"
	"github.com/eko/gocache/lib/v4/codec"
	"github.com/jon4hz/jellysweep/config"
	jellyfin "github.com/sj14/jellyfin-go/api"
)

type TagMap map[int32]string

// JellyfinItem represents a Jellyfin media item with its library context.
type JellyfinItem struct {
	jellyfin.BaseItemDto
	ParentLibraryName string `json:"parentLibraryName,omitempty"`
}

// JellyfinItemsData contains cached Jellyfin items and library folder mappings.
type JellyfinItemsData struct {
	Items             []JellyfinItem      `json:"items"`
	LibraryFoldersMap map[string][]string `json:"libraryFoldersMap"`
}

// Cache key prefixes.
const (
	JellyfinItemsCachePrefix = "jellyfin-items-"
	SonarrItemsCachePrefix   = "sonarr-items-"
	SonarrTagsCachePrefix    = "sonarr-tags-"
	RadarrItemsCachePrefix   = "radarr-items-"
	RadarrTagsCachePrefix    = "radarr-tags-"
)

type EngineCache struct {
	JellyfinItemsCache *PrefixedCache[JellyfinItemsData]
	SonarrItemsCache   *PrefixedCache[[]sonarr.SeriesResource]
	SonarrTagsCache    *PrefixedCache[TagMap]
	RadarrItemsCache   *PrefixedCache[[]radarr.MovieResource]
	RadarrTagsCache    *PrefixedCache[TagMap]
}

func NewEngineCache(cfg *config.CacheConfig) (*EngineCache, error) {
	return &EngineCache{
		JellyfinItemsCache: NewPrefixedCache[JellyfinItemsData](
			newCacheInstanceByType(cfg),
			cfg.Type,
			JellyfinItemsCachePrefix,
		),
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
	}, nil
}

func (e *EngineCache) ClearAll(ctx context.Context) {
	errs := []error{
		e.JellyfinItemsCache.Clear(ctx),
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
			Stats:     e.JellyfinItemsCache.GetStats(),
			CacheName: "jellyfin-items",
		},
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
