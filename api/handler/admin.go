package handler

import (
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/engine"
	"github.com/jon4hz/jellysweep/web/templates/pages"
)

type AdminHandler struct {
	engine       *engine.Engine
	cacheManager CacheManager
}

func NewAdmin(eng *engine.Engine) *AdminHandler {
	return &AdminHandler{
		engine: eng,
	}
}

// SetCacheManager allows setting the cache manager for admin operations.
func (h *AdminHandler) SetCacheManager(cm CacheManager) {
	h.cacheManager = cm
}

// AdminPanel shows the admin panel with keep requests.
func (h *AdminHandler) AdminPanel(c *gin.Context) {
	user := c.MustGet("user").(*models.User)

	// Create cache key based on user (admin cache)
	userID := user.Sub + "_admin"

	// Check if this is a forced refresh
	cacheControl := c.GetHeader("Cache-Control")
	pragma := c.GetHeader("Pragma")
	forceRefresh := c.Query("refresh") == RefreshParamTrue ||
		cacheControl == CacheControlNoCache ||
		cacheControl == CacheControlMaxAge0 ||
		pragma == PragmaNoCache

	keepRequests, err := h.engine.GetKeepRequests(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to get keep requests: %v", err)
		return
	}

	var mediaItemsMap map[string][]models.MediaItem

	// Try to get from cache first (if not forced refresh and cache manager available)
	if !forceRefresh && h.cacheManager != nil {
		if cachedData, found := h.cacheManager.Get(userID); found {
			mediaItemsMap = cachedData
		}
	}

	// If not in cache or forced refresh, get fresh data
	if mediaItemsMap == nil || forceRefresh {
		mediaItemsMap, err = h.engine.GetMediaItemsMarkedForDeletion(c.Request.Context())
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to get media items: %v", err)
			return
		}

		// Store in cache if cache manager is available
		if h.cacheManager != nil {
			h.cacheManager.Set(userID, mediaItemsMap)
		}
	}

	// Flatten the map to a slice
	var mediaItems []models.MediaItem
	for _, libraryItems := range mediaItemsMap {
		mediaItems = append(mediaItems, libraryItems...)
	}

	c.Header("Content-Type", "text/html")
	if err := pages.AdminPanel(user, keepRequests, mediaItems).Render(c.Request.Context(), c.Writer); err != nil {
		log.Error("Failed to render admin panel", "error", err)
	}
}

// AcceptKeepRequest accepts a keep request.
func (h *AdminHandler) AcceptKeepRequest(c *gin.Context) {
	mediaID := c.Param("id")

	err := h.engine.AcceptKeepRequest(c.Request.Context(), mediaID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Clear admin cache since data has changed
	if h.cacheManager != nil {
		user := c.MustGet("user").(*models.User)
		userID := user.Sub + "_admin"
		h.cacheManager.Clear(userID)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Keep request accepted successfully",
	})
}

// DeclineKeepRequest declines a keep request.
func (h *AdminHandler) DeclineKeepRequest(c *gin.Context) {
	mediaID := c.Param("id")

	err := h.engine.DeclineKeepRequest(c.Request.Context(), mediaID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Clear admin cache since data has changed
	if h.cacheManager != nil {
		user := c.MustGet("user").(*models.User)
		userID := user.Sub + "_admin"
		h.cacheManager.Clear(userID)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Keep request declined successfully",
	})
}

// MarkMediaAsKeep adds the jellysweep-keep tag to a media item.
func (h *AdminHandler) MarkMediaAsKeep(c *gin.Context) {
	mediaID := c.Param("id")

	err := h.engine.AddTagToMedia(c.Request.Context(), mediaID, engine.TagKeep)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Clear admin cache since data has changed
	if h.cacheManager != nil {
		user := c.MustGet("user").(*models.User)
		userID := user.Sub + "_admin"
		h.cacheManager.Clear(userID)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Media marked as keep successfully",
	})
}

// MarkMediaForDeletion adds the must-delete tag to a media item.
func (h *AdminHandler) MarkMediaForDeletion(c *gin.Context) {
	mediaID := c.Param("id")

	err := h.engine.AddTagToMedia(c.Request.Context(), mediaID, engine.TagMustDelete)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Clear admin cache since data has changed
	if h.cacheManager != nil {
		user := c.MustGet("user").(*models.User)
		userID := user.Sub + "_admin"
		h.cacheManager.Clear(userID)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Media marked for deletion successfully",
	})
}

// MarkMediaAsKeepForever removes all jellysweep tags and adds the jellysweep-ignore tag to permanently protect media.
func (h *AdminHandler) MarkMediaAsKeepForever(c *gin.Context) {
	mediaID := c.Param("id")

	err := h.engine.AddTagToMedia(c.Request.Context(), mediaID, engine.TagIgnore)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Clear admin cache since data has changed
	if h.cacheManager != nil {
		user := c.MustGet("user").(*models.User)
		userID := user.Sub + "_admin"
		h.cacheManager.Clear(userID)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Media protected forever successfully",
	})
}

// GetKeepRequests returns keep requests as JSON with caching support.
func (h *AdminHandler) GetKeepRequests(c *gin.Context) {
	keepRequests, err := h.engine.GetKeepRequests(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get keep requests",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"keepRequests": keepRequests,
	})
}

// GetAdminMediaItems returns media items for admin with caching support.
func (h *AdminHandler) GetAdminMediaItems(c *gin.Context) {
	user := c.MustGet("user").(*models.User)
	userID := user.Sub + "_admin"

	// Check if this is a forced refresh
	cacheControl := c.GetHeader("Cache-Control")
	pragma := c.GetHeader("Pragma")
	forceRefresh := c.Query("refresh") == RefreshParamTrue ||
		cacheControl == CacheControlNoCache ||
		cacheControl == CacheControlMaxAge0 ||
		pragma == PragmaNoCache

	var mediaItemsMap map[string][]models.MediaItem
	var err error

	// Try to get from cache first (if not forced refresh and cache manager available)
	if !forceRefresh && h.cacheManager != nil {
		if cachedData, found := h.cacheManager.Get(userID); found {
			mediaItemsMap = cachedData
		}
	}

	// If not in cache or forced refresh, get fresh data
	if mediaItemsMap == nil || forceRefresh {
		mediaItemsMap, err = h.engine.GetMediaItemsMarkedForDeletion(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "Failed to get media items",
			})
			return
		}

		// Store in cache if cache manager is available
		if h.cacheManager != nil {
			h.cacheManager.Set(userID, mediaItemsMap)
		}
	}

	// Convert to flat list
	var mediaItems []models.MediaItem
	for _, libraryItems := range mediaItemsMap {
		mediaItems = append(mediaItems, libraryItems...)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"mediaItems": mediaItems,
	})
}
