package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/database"
	"github.com/jon4hz/jellysweep/engine"
)

type PluginHandler struct {
	engine *engine.Engine
}

func NewPlugin(e *engine.Engine) *PluginHandler {
	return &PluginHandler{
		engine: e,
	}
}

// GetHealth checks the health of the plugin and returns a JSON response.
func (h *PluginHandler) GetHealth(c *gin.Context) {
	// This is a placeholder for plugin health check
	// In a real implementation, you would check the plugin's status
	c.JSON(200, gin.H{"status": "ok"})
}

// CheckMediaItemRequest represents the request structure for checking a media item.
type CheckMediaItemRequest struct {
	Name           string             `json:"name"`
	ProductionYear int                `json:"production_year"`
	MediaType      database.MediaType `json:"media_type"`
}

// CheckMediaItemResponse represents the response structure for checking a media item.
type CheckMediaItemResponse struct {
	DeletionDate time.Time `json:"deletion_date"`
}

// CheckMediaItem checks if a media item is marked for deletion and returns the deletion date.
func (h *PluginHandler) CheckMediaItem(c *gin.Context) {
	var request CheckMediaItemRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Validate media type
	if request.MediaType != database.MediaTypeMovie && request.MediaType != database.MediaTypeTV {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Media type must be 'movie' or 'tv'"})
		return
	}

	// Get all media items marked for deletion
	markedItems, err := h.engine.GetMediaItemsByMediaType(c.Request.Context(), request.MediaType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve marked media items"})
		return
	}

	// Search through all libraries for a matching item
	for _, item := range markedItems {
		// Match by type first
		if item.MediaType != request.MediaType {
			continue
		}

		// Match by title (case-insensitive)
		if !strings.EqualFold(item.Title, request.Name) {
			continue
		}

		// Match by year if provided
		if request.ProductionYear > 0 && int(item.Year) != request.ProductionYear {
			continue
		}

		// Found a match - return the deletion date
		response := CheckMediaItemResponse{
			DeletionDate: item.DefaultDeleteAt,
		}
		c.JSON(http.StatusOK, response)
		return
	}

	// No matching item found marked for deletion
	c.JSON(http.StatusNotFound, gin.H{"error": "Media item not found or not marked for deletion"})
}
