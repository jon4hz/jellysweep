package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/database"
)

// DatabaseHandler handles database-related API endpoints.
type DatabaseHandler struct {
	db database.DatabaseInterface
}

// NewDatabaseHandler creates a new database handler.
func NewDatabaseHandler(db database.DatabaseInterface) *DatabaseHandler {
	return &DatabaseHandler{db: db}
}

// GetCleanupHistory returns the cleanup run history.
func (h *DatabaseHandler) GetCleanupHistory(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 10
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	history, err := h.db.GetCleanupRunHistory(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get cleanup history"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"history": history})
}

// GetCleanupRun returns details for a specific cleanup run.
func (h *DatabaseHandler) GetCleanupRun(c *gin.Context) {
	runIDStr := c.Param("id")
	runID, err := strconv.ParseInt(runIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid run ID"})
		return
	}

	run, err := h.db.GetCleanupRun(c.Request.Context(), runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get cleanup run"})
		return
	}

	if run == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Cleanup run not found"})
		return
	}

	// Get steps for this run
	steps, err := h.db.GetCleanupRunSteps(c.Request.Context(), runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get cleanup steps"})
		return
	}

	// Get media items for this run
	mediaItems, err := h.db.GetMediaItemsForRun(c.Request.Context(), runID, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get media items"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"run":         run,
		"steps":       steps,
		"media_items": mediaItems,
	})
}

// GetCleanupStats returns overall cleanup statistics.
func (h *DatabaseHandler) GetCleanupStats(c *gin.Context) {
	stats, err := h.db.GetCleanupStats(c.Request.Context(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get cleanup stats"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"stats": stats})
}

// GetMediaHistory returns the action history for a specific media item.
func (h *DatabaseHandler) GetMediaHistory(c *gin.Context) {
	mediaID := c.Param("media_id")
	if mediaID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Media ID is required"})
		return
	}

	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 50 {
		limit = 10
	}

	history, err := h.db.GetMediaItemHistory(c.Request.Context(), mediaID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get media history"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"history": history})
}

// GetActiveCleanupRun returns the currently active cleanup run, if any.
func (h *DatabaseHandler) GetActiveCleanupRun(c *gin.Context) {
	run, err := h.db.GetActiveCleanupRun(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get active cleanup run"})
		return
	}

	if run == nil {
		c.JSON(http.StatusOK, gin.H{"active_run": nil})
		return
	}

	// Get steps for the active run
	steps, err := h.db.GetCleanupRunSteps(c.Request.Context(), run.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get cleanup steps"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"active_run": run,
		"steps":      steps,
	})
}
