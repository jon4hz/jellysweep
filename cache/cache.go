package cache

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eko/gocache/lib/v4/cache"
	"github.com/eko/gocache/lib/v4/codec"
	"github.com/eko/gocache/lib/v4/store"
	go_store "github.com/eko/gocache/store/go_cache/v4"
	redis_store "github.com/eko/gocache/store/redis/v4"
	"github.com/jon4hz/jellysweep/config"
	gocache "github.com/patrickmn/go-cache"
	"github.com/redis/go-redis/v9"
)

// PrefixedCache wraps a cache.Cache and adds a prefix to all keys.
type PrefixedCache[T any] struct {
	cache  *cache.Cache[[]byte]
	prefix string
}

// NewPrefixedCache creates a new prefixed cache wrapper.
func NewPrefixedCache[T any](cache *cache.Cache[[]byte], prefix string) *PrefixedCache[T] {
	return &PrefixedCache[T]{
		cache:  cache,
		prefix: prefix,
	}
}

// Get retrieves a value from the cache with the prefixed key.
func (p *PrefixedCache[T]) Get(ctx context.Context, key any) (T, error) {
	prefixedKey := p.prefix + fmt.Sprintf("%v", key)
	data, err := p.cache.Get(ctx, prefixedKey)
	if err != nil {
		return *new(T), err
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return *new(T), err
	}
	return result, nil
}

// Set stores a value in the cache with the prefixed key.
func (p *PrefixedCache[T]) Set(ctx context.Context, key any, object T, options ...store.Option) error {
	prefixedKey := p.prefix + fmt.Sprintf("%v", key)
	data, err := json.Marshal(object)
	if err != nil {
		return err
	}
	return p.cache.Set(ctx, prefixedKey, data, options...)
}

// Delete removes a value from the cache with the prefixed key.
func (p *PrefixedCache[T]) Delete(ctx context.Context, key any) error {
	prefixedKey := p.prefix + fmt.Sprintf("%v", key)
	return p.cache.Delete(ctx, prefixedKey)
}

// Invalidate removes values from the cache matching the prefixed pattern.
func (p *PrefixedCache[T]) Invalidate(ctx context.Context, options ...store.InvalidateOption) error {
	return p.cache.Invalidate(ctx, options...)
}

// Clear removes all values from the cache.
func (p *PrefixedCache[T]) Clear(ctx context.Context) error {
	return p.cache.Clear(ctx)
}

// GetType returns the cache type.
func (p *PrefixedCache[T]) GetType() string {
	return p.cache.GetType()
}

// GetStats returns the cache statistics.
func (p *PrefixedCache[T]) GetStats() *codec.Stats {
	return p.cache.GetCodec().GetStats()
}

func newMemoryCache[T any]() *cache.Cache[T] {
	// never expire items in memory cache by ttl, we use the scheduler to handle expiration
	gocacheClient := gocache.New(gocache.NoExpiration, gocache.NoExpiration)
	gocacheStore := go_store.NewGoCache(gocacheClient)
	return cache.New[T](gocacheStore)
}

func newRedisCache[T any](cfg *config.CacheConfig) *cache.Cache[T] {
	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisURL,
	})
	redisStore := redis_store.NewRedis(redisClient)
	return cache.New[T](redisStore)
}
