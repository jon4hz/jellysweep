package cache

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/eko/gocache/lib/v4/store"
	"github.com/jon4hz/jellysweep/config"
)

// LibraryResolver provides methods to resolve library names and IDs.
type LibraryResolver interface {
	GetLibraryNameByID(id string) string
	SetLibraryMapping(id, name string)
}

// LibraryCache implements LibraryResolver with a PrefixedCache for efficient library name/ID resolution.
type LibraryCache struct {
	cache *PrefixedCache[string]
}

// NewLibraryCache creates a new LibraryCache with the given cache configuration.
func NewLibraryCache(cfg *config.CacheConfig, options ...store.Option) *LibraryCache {
	return &LibraryCache{
		cache: NewPrefixedCache[string](
			newCacheInstanceByType(cfg),
			cfg.Type,
			"library:",
		),
	}
}

// GetLibraryNameByID returns the library name for the given library ID.
func (lc *LibraryCache) GetLibraryNameByID(id string) string {
	nameKey := fmt.Sprintf("id:%s", id)
	name, err := lc.cache.Get(context.Background(), nameKey)
	if err != nil {
		log.Warn("Failed to get library name from cache", "library_id", id, "error", err)
		return ""
	}
	return name
}

// SetLibraryMapping stores a bidirectional mapping between library ID and name.
func (lc *LibraryCache) SetLibraryMapping(id, name string) {
	ctx := context.Background()

	// Store both directions of the mapping
	nameKey := fmt.Sprintf("id:%s", id)

	if err := lc.cache.Set(ctx, nameKey, name); err != nil {
		log.Warn("Failed to set library name in cache", "library_id", id, "error", err)
	}
}
