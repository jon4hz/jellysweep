package cache

import (
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/api/models"
)

// CacheEntry represents a cached item with its expiration time
type CacheEntry struct {
	Data      map[string][]models.MediaItem
	ExpiresAt time.Time
}

// CacheManager manages user-specific caches
type CacheManager struct {
	cache map[string]*CacheEntry // key is user ID
	mutex sync.RWMutex
	ttl   time.Duration
}

// NewCacheManager creates a new cache manager
func NewCacheManager(ttl time.Duration) *CacheManager {
	return &CacheManager{
		cache: make(map[string]*CacheEntry),
		mutex: sync.RWMutex{},
		ttl:   ttl,
	}
}

// Get retrieves cached data for a user
func (cm *CacheManager) Get(userID string) (map[string][]models.MediaItem, bool) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	entry, exists := cm.cache[userID]
	if !exists || time.Now().After(entry.ExpiresAt) {
		log.Debug("Cache miss for user", "userID", userID)
		return nil, false
	}
	log.Debug("Cache hit for user", "userID", userID)
	return entry.Data, true
}

// Set stores data in cache for a user
func (cm *CacheManager) Set(userID string, data map[string][]models.MediaItem) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	cm.cache[userID] = &CacheEntry{
		Data:      data,
		ExpiresAt: time.Now().Add(cm.ttl),
	}
	log.Debug("Cache set for user", "userID", userID, "expiresAt", cm.cache[userID].ExpiresAt)
}

// Clear removes cached data for a user
func (cm *CacheManager) Clear(userID string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	delete(cm.cache, userID)
	log.Debug("Cache cleared for user", "userID", userID)
}

// CleanupExpired removes expired cache entries
func (cm *CacheManager) CleanupExpired() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	now := time.Now()
	for userID, entry := range cm.cache {
		if now.After(entry.ExpiresAt) {
			delete(cm.cache, userID)
			log.Debug("Removed expired cache entry", "userID", userID, "expiresAt", entry.ExpiresAt)
		}
	}
}
