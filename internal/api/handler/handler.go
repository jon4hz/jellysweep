package handler

import (
	"net/http"
	"strconv"

	"github.com/charmbracelet/log"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/engine"
	"github.com/jon4hz/jellysweep/web/templates/pages"
)

type Handler struct {
	engine *engine.Engine
	config *config.Config
}

func New(eng *engine.Engine, cfg *config.Config) *Handler {
	return &Handler{
		engine: eng,
		config: cfg,
	}
}

func (h *Handler) Home(c *gin.Context) {
	user := c.MustGet("user").(*models.User)

	mediaItems, err := h.engine.GetMediaItems(c.Request.Context(), false)
	if err != nil {
		// Log error and fall back to empty data
		log.Error("Failed to get media items", "error", err)
		mediaItems = []database.Media{}
	}

	// Convert to user-safe media items (excludes sensitive fields like RequestedBy)
	userMediaItems := models.ToUserMediaItems(mediaItems, h.config)

	c.Header("Content-Type", "text/html")

	// If user is admin, get pending requests count for navbar indicator
	if user.IsAdmin {
		requests, err := h.engine.GetMediaWithPendingRequest(c.Request.Context())
		if err != nil {
			// Log error but continue without pending count
			if err := pages.Dashboard(user, userMediaItems).Render(c.Request.Context(), c.Writer); err != nil {
				log.Error("Failed to render dashboard", "error", err)
			}
			return
		}
		if err := pages.DashboardWithPendingRequests(user, userMediaItems, len(requests)).Render(c.Request.Context(), c.Writer); err != nil {
			log.Error("Failed to render dashboard with pending requests", "error", err)
		}
	} else {
		if err := pages.Dashboard(user, userMediaItems).Render(c.Request.Context(), c.Writer); err != nil {
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
	if err := pages.Login(h.config.Auth).Render(c.Request.Context(), c.Writer); err != nil {
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

func parseUintParam(param string) (uint, error) {
	var id uint64
	var err error
	if id, err = strconv.ParseUint(param, 10, 0); err != nil {
		return 0, err
	}
	return uint(id), nil
}

// API endpoint for requesting to keep media.
func (h *Handler) RequestKeepMedia(c *gin.Context) {
	mediaIDVal := c.Param("id")
	user := c.MustGet("user").(*models.User)

	// Convert mediaID to uint
	mediaID, err := parseUintParam(mediaIDVal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid media ID",
		})
		return
	}

	err = h.engine.RequestKeepMedia(c.Request.Context(), mediaID, user.ID, user.Username)
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
	mediaItems, err := h.engine.GetMediaItems(c.Request.Context(), false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get media items",
		})
		return
	}

	// Convert to user-safe media items (excludes sensitive fields like RequestedBy)
	userMediaItems := models.ToUserMediaItems(mediaItems, h.config)

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"mediaItems": userMediaItems,
	})
}
