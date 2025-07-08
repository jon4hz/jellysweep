package cache

import (
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/patrickmn/go-cache"
)

// Manager wraps the shared cache with convenience methods for API use.
type Manager struct {
	cache *cache.Cache
}

// NewManager creates a new cache manager that wraps the shared cache.
func NewManager(sharedCache *cache.Cache) *Manager {
	return &Manager{
		cache: sharedCache,
	}
}

// GetMediaItems retrieves cached media items for a user.
func (m *Manager) GetMediaItems(userID string) (map[string][]models.MediaItem, bool) {
	key := "media_items_" + userID
	if data, found := m.cache.Get(key); found {
		if mediaItems, ok := data.(map[string][]models.MediaItem); ok {
			log.Debug("Cache hit for media items", "userID", userID)
			return mediaItems, true
		}
	}
	log.Debug("Cache miss for media items", "userID", userID)
	return nil, false
}

// SetMediaItems stores media items in cache for a user.
func (m *Manager) SetMediaItems(userID string, data map[string][]models.MediaItem, ttl time.Duration) {
	key := "media_items_" + userID
	m.cache.Set(key, data, ttl)
	log.Debug("Cache set for media items", "userID", userID, "ttl", ttl)
}

// ClearMediaItems removes cached media items for a user.
func (m *Manager) ClearMediaItems(userID string) {
	key := "media_items_" + userID
	m.cache.Delete(key)
	log.Debug("Cache cleared for media items", "userID", userID)
}

// Get retrieves cached media items for a user (implements handler.CacheManager).
func (m *Manager) Get(userID string) (map[string][]models.MediaItem, bool) {
	return m.GetMediaItems(userID)
}

// Set stores media items in cache for a user (implements handler.CacheManager).
func (m *Manager) Set(userID string, data map[string][]models.MediaItem) {
	// Use a default TTL of 5 minutes for handler interface compatibility
	m.SetMediaItems(userID, data, 5*time.Minute)
}

// Clear removes cached media items for a user (implements handler.CacheManager).
func (m *Manager) Clear(userID string) {
	m.ClearMediaItems(userID)
}
