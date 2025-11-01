package cache

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/eko/gocache/lib/v4/cache"
	"github.com/eko/gocache/lib/v4/codec"
	"github.com/jon4hz/jellysweep/internal/config"
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
	SonarrTagsCache *PrefixedCache[TagMap]
	RadarrTagsCache *PrefixedCache[TagMap]
}

func NewEngineCache(cfg *config.CacheConfig) (*EngineCache, error) {
	return &EngineCache{
		SonarrTagsCache: NewPrefixedCache[TagMap](
			newCacheInstanceByType(cfg),
			cfg.Type,
			SonarrTagsCachePrefix,
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
		e.SonarrTagsCache.Clear(ctx),
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
			Stats:     e.SonarrTagsCache.GetStats(),
			CacheName: "sonarr-tags",
		},
		{
			Stats:     e.RadarrTagsCache.GetStats(),
			CacheName: "radarr-tags",
		},
	}
}
