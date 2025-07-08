package handler

import (
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/cache"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine"
	"github.com/jon4hz/jellysweep/web/templates/pages"
)

const (
	// Cache control constants.
	CacheControlNoCache = "no-cache"
	CacheControlMaxAge0 = "max-age=0"
	PragmaNoCache       = "no-cache"
	RefreshParamTrue    = "true"
)

// CacheManager interface for managing user-specific caches.
type CacheManager interface {
	Get(userID string) (map[string][]models.MediaItem, bool)
	Set(userID string, data map[string][]models.MediaItem)
	Clear(userID string)
}

type Handler struct {
	engine       *engine.Engine
	cacheManager CacheManager
	imageCache   *cache.ImageCache
	authConfig   *config.AuthConfig
}

func New(eng *engine.Engine, cm CacheManager, im *cache.ImageCache, authConfig *config.AuthConfig) *Handler {
	return &Handler{
		engine:       eng,
		cacheManager: cm,
		imageCache:   im,
		authConfig:   authConfig,
	}
}

func (h *Handler) Home(c *gin.Context) {
	user := c.MustGet("user").(*models.User)

	// Create cache key based on user
	userID := user.Sub // Use the subject from OIDC token as user ID

	// Check if this is a forced refresh
	cacheControl := c.GetHeader("Cache-Control")
	pragma := c.GetHeader("Pragma")
	forceRefresh := c.Query("refresh") == RefreshParamTrue ||
		cacheControl == CacheControlNoCache ||
		cacheControl == CacheControlMaxAge0 ||
		pragma == PragmaNoCache

	var mediaItemsMap map[string][]models.MediaItem
	var err error

	// Try to get from cache first (if not forced refresh)
	if !forceRefresh {
		if cachedData, found := h.cacheManager.Get(userID); found {
			mediaItemsMap = cachedData
		}
	}

	// If not in cache or forced refresh, get fresh data
	if mediaItemsMap == nil || forceRefresh {
		mediaItemsMap, err = h.engine.GetMediaItemsMarkedForDeletion(c.Request.Context())
		if err != nil {
			// Log error and fall back to empty data
			c.Header("Content-Type", "text/html")
			if user.IsAdmin {
				// Try to get pending requests for admin even on error
				if keepRequests, keepErr := h.engine.GetKeepRequests(c.Request.Context()); keepErr == nil {
					if err := pages.DashboardWithPendingRequests(user, []models.MediaItem{}, len(keepRequests)).Render(c.Request.Context(), c.Writer); err != nil {
						log.Error("Failed to render dashboard with pending requests", "error", err)
					}
				} else {
					if err := pages.Dashboard(user, []models.MediaItem{}).Render(c.Request.Context(), c.Writer); err != nil {
						log.Error("Failed to render dashboard", "error", err)
					}
				}
			} else {
				if err := pages.Dashboard(user, []models.MediaItem{}).Render(c.Request.Context(), c.Writer); err != nil {
					log.Error("Failed to render dashboard", "error", err)
				}
			}
			return
		}

		// Store in cache
		h.cacheManager.Set(userID, mediaItemsMap)
	}

	// Convert to flat list for the dashboard
	var mediaItems []models.MediaItem
	for _, libraryItems := range mediaItemsMap {
		mediaItems = append(mediaItems, libraryItems...)
	}

	c.Header("Content-Type", "text/html")

	// If user is admin, get pending requests count for navbar indicator
	if user.IsAdmin {
		keepRequests, err := h.engine.GetKeepRequests(c.Request.Context())
		if err != nil {
			// Log error but continue without pending count
			if err := pages.Dashboard(user, mediaItems).Render(c.Request.Context(), c.Writer); err != nil {
				log.Error("Failed to render dashboard", "error", err)
			}
			return
		}
		if err := pages.DashboardWithPendingRequests(user, mediaItems, len(keepRequests)).Render(c.Request.Context(), c.Writer); err != nil {
			log.Error("Failed to render dashboard with pending requests", "error", err)
		}
	} else {
		if err := pages.Dashboard(user, mediaItems).Render(c.Request.Context(), c.Writer); err != nil {
			log.Error("Failed to render dashboard", "error", err)
		}
	}
}

func (h *Handler) Login(c *gin.Context) {
	session := sessions.Default(c)
	sessionID := session.Get("user_id")
	isLoggedIn := sessionID != nil && sessionID != ""
	if isLoggedIn {
		c.Redirect(http.StatusFound, "/")
		return
	}

	c.Header("Content-Type", "text/html")
	if err := pages.Login(h.authConfig).Render(c.Request.Context(), c.Writer); err != nil {
		log.Error("Failed to render login page", "error", err)
	}
}

func (h *Handler) Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	if err := session.Save(); err != nil {
		if err := c.AbortWithError(http.StatusInternalServerError, err); err != nil {
			log.Error("Failed to abort with error", "error", err)
		}
		return
	}
	c.Redirect(http.StatusFound, "/login")
}

// API endpoint for requesting to keep media.
func (h *Handler) RequestKeepMedia(c *gin.Context) {
	mediaID := c.Param("id")
	user := c.MustGet("user").(*models.User)

	err := h.engine.RequestKeepMedia(c.Request.Context(), mediaID, user.Username)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Clear the user's cache since media status has changed
	userID := user.Sub
	h.cacheManager.Clear(userID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Keep request submitted successfully",
	})
}

// ImageCache serves cached images or downloads them if not cached.
func (h *Handler) ImageCache(c *gin.Context) {
	imageURL := c.Query("url")
	if imageURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url parameter is required"})
		return
	}

	// Serve the cached image
	err := h.imageCache.ServeImage(c.Request.Context(), imageURL, c.Writer, c.Request)
	if err != nil {
		// Error is already handled in ServeImage
		return
	}
}

// Me returns the current user's information.
func (h *Handler) Me(c *gin.Context) {
	user := c.MustGet("user").(*models.User)

	c.JSON(http.StatusOK, gin.H{
		"username": user.Username,
		"isAdmin":  user.IsAdmin,
	})
}

// GetMediaItems returns the current user's media items as JSON.
func (h *Handler) GetMediaItems(c *gin.Context) {
	user := c.MustGet("user").(*models.User)
	userID := user.Sub

	// Check if this is a forced refresh
	cacheControl := c.GetHeader("Cache-Control")
	pragma := c.GetHeader("Pragma")
	forceRefresh := c.Query("refresh") == RefreshParamTrue ||
		cacheControl == CacheControlNoCache ||
		cacheControl == CacheControlMaxAge0 ||
		pragma == PragmaNoCache

	var mediaItemsMap map[string][]models.MediaItem
	var err error

	// Try to get from cache first (if not forced refresh)
	if !forceRefresh {
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

		// Store in cache
		h.cacheManager.Set(userID, mediaItemsMap)
	}

	// Convert to flat list for the dashboard
	var mediaItems []models.MediaItem
	for _, libraryItems := range mediaItemsMap {
		mediaItems = append(mediaItems, libraryItems...)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"mediaItems": mediaItems,
	})
}
