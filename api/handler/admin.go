package handler

import (
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/cache"
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

	// Check if this is a forced refresh
	cacheControl := c.GetHeader("Cache-Control")
	pragma := c.GetHeader("Pragma")
	forceRefresh := c.Query("refresh") == RefreshParamTrue ||
		cacheControl == CacheControlNoCache ||
		cacheControl == CacheControlMaxAge0 ||
		pragma == PragmaNoCache

	keepRequests, err := h.engine.GetKeepRequests(c.Request.Context(), forceRefresh)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to get keep requests: %v", err)
		return
	}

	var mediaItemsMap map[string][]models.MediaItem
	// If not in cache or forced refresh, get fresh data
	if mediaItemsMap == nil || forceRefresh {
		mediaItemsMap, err = h.engine.GetMediaItemsMarkedForDeletion(c.Request.Context())
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to get media items: %v", err)
			return
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

	err := h.engine.AddTagToMedia(c.Request.Context(), mediaID, engine.JellysweepKeepPrefix)
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

	err := h.engine.AddTagToMedia(c.Request.Context(), mediaID, engine.JellysweepDeleteForSureTag)
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

	err := h.engine.AddTagToMedia(c.Request.Context(), mediaID, engine.JellysweepIgnoreTag)
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

// GetKeepRequests returns keep requests as JSON.
func (h *AdminHandler) GetKeepRequests(c *gin.Context) {
	keepRequests, err := h.engine.GetKeepRequests(c.Request.Context(), false)
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
	// Check if this is a forced refresh
	cacheControl := c.GetHeader("Cache-Control")
	pragma := c.GetHeader("Pragma")
	forceRefresh := c.Query("refresh") == RefreshParamTrue ||
		cacheControl == CacheControlNoCache ||
		cacheControl == CacheControlMaxAge0 ||
		pragma == PragmaNoCache

	var mediaItemsMap map[string][]models.MediaItem
	var err error
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

// GetSchedulerJobs returns all scheduler jobs as JSON.
func (h *AdminHandler) GetSchedulerJobs(c *gin.Context) {
	jobs := h.engine.GetScheduler().GetJobs()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"jobs":    jobs,
	})
}

// RunSchedulerJob manually triggers a scheduler job.
func (h *AdminHandler) RunSchedulerJob(c *gin.Context) {
	jobID := c.Param("id")

	err := h.engine.GetScheduler().RunJobNow(jobID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Job triggered successfully",
	})
}

// EnableSchedulerJob enables a scheduler job.
func (h *AdminHandler) EnableSchedulerJob(c *gin.Context) {
	jobID := c.Param("id")

	err := h.engine.GetScheduler().EnableJob(jobID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Job enabled successfully",
	})
}

// DisableSchedulerJob disables a scheduler job.
func (h *AdminHandler) DisableSchedulerJob(c *gin.Context) {
	jobID := c.Param("id")

	err := h.engine.GetScheduler().DisableJob(jobID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Job disabled successfully",
	})
}

// GetSchedulerCacheStats returns cache statistics.
func (h *AdminHandler) GetSchedulerCacheStats(c *gin.Context) {
	stats := h.engine.GetEngineCache().GetStats()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"stats":   stats,
	})
}

// ClearSchedulerCache clears the engine cache.
func (h *AdminHandler) ClearSchedulerCache(c *gin.Context) {
	if engineCache := h.engine.GetEngineCache(); engineCache != nil {
		// Clear each cache in the engine cache
		if err := engineCache.SonarrItemsCache.Clear(c.Request.Context()); err != nil {
			log.Error("Failed to clear Sonarr items cache", "error", err)
		}
		if err := engineCache.SonarrTagsCache.Clear(c.Request.Context()); err != nil {
			log.Error("Failed to clear Sonarr tags cache", "error", err)
		}
		if err := engineCache.RadarrItemsCache.Clear(c.Request.Context()); err != nil {
			log.Error("Failed to clear Radarr items cache", "error", err)
		}
		if err := engineCache.RadarrTagsCache.Clear(c.Request.Context()); err != nil {
			log.Error("Failed to clear Radarr tags cache", "error", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Cache cleared successfully",
	})
}

// SchedulerPanel shows the scheduler management panel.
func (h *AdminHandler) SchedulerPanel(c *gin.Context) {
	user := c.MustGet("user").(*models.User)

	// Get scheduler jobs
	jobs := h.engine.GetScheduler().GetJobs()

	// Get cache stats from engine cache
	var cacheStats []*cache.Stats
	if engineCache := h.engine.GetEngineCache(); engineCache != nil {
		cacheStats = engineCache.GetStats()
	}

	c.Header("Content-Type", "text/html")
	if err := pages.SchedulerPanel(user, jobs, cacheStats).Render(c.Request.Context(), c.Writer); err != nil {
		log.Error("Failed to render scheduler panel", "error", err)
	}
}
