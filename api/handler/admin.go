package handler

import (
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/cache"
	"github.com/jon4hz/jellysweep/database"
	"github.com/jon4hz/jellysweep/engine"
	"github.com/jon4hz/jellysweep/tags"
	"github.com/jon4hz/jellysweep/web/templates/pages"
	"golang.org/x/sync/errgroup"
)

type AdminHandler struct {
	engine *engine.Engine
	db     database.DB
}

func NewAdmin(eng *engine.Engine, db database.DB) *AdminHandler {
	return &AdminHandler{
		engine: eng,
		db:     db,
	}
}

// AdminPanel shows the admin panel with keep requests.
func (h *AdminHandler) AdminPanel(c *gin.Context) {
	user := c.MustGet("user").(*models.User)

	requests, err := h.db.GetMediaWithRequest(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to get keep requests: %v", err)
		return
	}

	mediaItems, err := h.db.GetMediaItems(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to get media items: %v", err)
		return
	}

	c.Header("Content-Type", "text/html")
	if err := pages.AdminPanel(user, requests, mediaItems).Render(c.Request.Context(), c.Writer); err != nil {
		log.Error("Failed to render admin panel", "error", err)
	}
}

// AcceptKeepRequest accepts a keep request.
func (h *AdminHandler) AcceptKeepRequest(c *gin.Context) {
	mediaIDVal := c.Param("id")
	mediaID, err := parseUintParam(mediaIDVal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid media ID",
		})
		return
	}

	err = h.engine.HandleKeepRequest(c.Request.Context(), mediaID, true)
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
	mediaIDVal := c.Param("id")
	mediaID, err := parseUintParam(mediaIDVal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid media ID",
		})
		return
	}

	err = h.engine.HandleKeepRequest(c.Request.Context(), mediaID, false)
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

	err := h.engine.AddTagToMedia(c.Request.Context(), mediaID, tags.JellysweepKeepPrefix)
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

	err := h.engine.AddTagToMedia(c.Request.Context(), mediaID, tags.JellysweepDeleteForSureTag)
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

	err := h.engine.AddTagToMedia(c.Request.Context(), mediaID, tags.JellysweepIgnoreTag)
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
	requests, err := h.db.GetMediaWithRequest(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get keep requests",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"keepRequests": requests,
	})
}

// GetAdminMediaItems returns media items for admin with caching support.
func (h *AdminHandler) GetAdminMediaItems(c *gin.Context) {
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
	engineCache := h.engine.GetEngineCache()
	if engineCache == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Cache cleared successfully",
		})
		return
	}

	// Use error group to clear all caches concurrently and collect any errors
	var g errgroup.Group
	g.Go(func() error {
		if err := engineCache.SonarrItemsCache.Clear(c.Request.Context()); err != nil {
			log.Error("Failed to clear Sonarr items cache", "error", err)
			return err
		}
		return nil
	})

	g.Go(func() error {
		if err := engineCache.SonarrTagsCache.Clear(c.Request.Context()); err != nil {
			log.Error("Failed to clear Sonarr tags cache", "error", err)
			return err
		}
		return nil
	})

	g.Go(func() error {
		if err := engineCache.RadarrItemsCache.Clear(c.Request.Context()); err != nil {
			log.Error("Failed to clear Radarr items cache", "error", err)
			return err
		}
		return nil
	})

	g.Go(func() error {
		if err := engineCache.RadarrTagsCache.Clear(c.Request.Context()); err != nil {
			log.Error("Failed to clear Radarr tags cache", "error", err)
			return err
		}
		return nil
	})

	// Wait for all cache clearing operations to complete
	if err := g.Wait(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to clear one or more caches",
		})
		return
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
