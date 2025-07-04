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

// AdminPanel shows the admin panel with keep requests.
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

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Media protected forever successfully",
	})
}
