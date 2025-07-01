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
	engine *engine.Engine
}

func NewAdmin(eng *engine.Engine) *AdminHandler {
	return &AdminHandler{
		engine: eng,
	}
}

// AdminPanel shows the admin panel with keep requests
func (h *AdminHandler) AdminPanel(c *gin.Context) {
	user := c.MustGet("user").(*models.User)

	keepRequests, err := h.engine.GetKeepRequests(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to get keep requests: %v", err)
		return
	}

	// Get media items for the keep/delete tab
	mediaItemsMap, err := h.engine.GetMediaItemsMarkedForDeletion(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to get media items: %v", err)
		return
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

// AcceptKeepRequest accepts a keep request
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

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Keep request accepted successfully",
	})
}

// DeclineKeepRequest declines a keep request
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

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Keep request declined successfully",
	})
}
