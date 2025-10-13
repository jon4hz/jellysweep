package handler

import (
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/database"
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

type Handler struct {
	engine     *engine.Engine
	authConfig *config.AuthConfig
}

func New(eng *engine.Engine, authConfig *config.AuthConfig) *Handler {
	return &Handler{
		engine:     eng,
		authConfig: authConfig,
	}
}

func (h *Handler) Home(c *gin.Context) {
	user := c.MustGet("user").(*models.User)

	// Check if this is a forced refresh
	cacheControl := c.GetHeader("Cache-Control")
	pragma := c.GetHeader("Pragma")
	forceRefresh := c.Query("refresh") == RefreshParamTrue ||
		cacheControl == CacheControlNoCache ||
		cacheControl == CacheControlMaxAge0 ||
		pragma == PragmaNoCache

	mediaItems, err := h.engine.GetMediaItemsMarkedForDeletion(c.Request.Context())
	if err != nil {
		// Log error and fall back to empty data
		c.Header("Content-Type", "text/html")
		if user.IsAdmin {
			// Try to get pending requests for admin even on error
			if keepRequests, keepErr := h.engine.GetKeepRequests(c.Request.Context(), forceRefresh); keepErr == nil {
				if err := pages.DashboardWithPendingRequests(user, []database.Media{}, len(keepRequests)).Render(c.Request.Context(), c.Writer); err != nil {
					log.Error("Failed to render dashboard with pending requests", "error", err)
				}
			} else {
				if err := pages.Dashboard(user, []database.Media{}).Render(c.Request.Context(), c.Writer); err != nil {
					log.Error("Failed to render dashboard", "error", err)
				}
			}
		} else {
			if err := pages.Dashboard(user, []database.Media{}).Render(c.Request.Context(), c.Writer); err != nil {
				log.Error("Failed to render dashboard", "error", err)
			}
		}
		return
	}

	c.Header("Content-Type", "text/html")

	// If user is admin, get pending requests count for navbar indicator
	if user.IsAdmin {
		keepRequests, err := h.engine.GetKeepRequests(c.Request.Context(), false)
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
	userID := session.Get("user_id")
	isLoggedIn := userID != nil && userID != ""
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
	err := h.engine.GetImageCache().ServeImage(c.Request.Context(), imageURL, c.Writer, c.Request)
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
	mediaItems, err := h.engine.GetMediaItemsMarkedForDeletion(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get media items",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"mediaItems": mediaItems,
	})
}
