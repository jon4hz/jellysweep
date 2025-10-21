package handler

import (
	"net/http"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/cache"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/database"
	"github.com/jon4hz/jellysweep/engine"
	"github.com/jon4hz/jellysweep/web/templates/pages"
	"golang.org/x/sync/errgroup"
)

type AdminHandler struct {
	engine *engine.Engine
	db     database.DB
	config *config.Config
}

func NewAdmin(eng *engine.Engine, db database.DB, cfg *config.Config) *AdminHandler {
	return &AdminHandler{
		engine: eng,
		db:     db,
		config: cfg,
	}
}

// AdminPanel shows the admin panel with keep requests.
func (h *AdminHandler) AdminPanel(c *gin.Context) {
	user := c.MustGet("user").(*models.User)

	requests, err := h.db.GetMediaWithPendingRequest(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to get keep requests: %v", err)
		return
	}

	mediaItems, err := h.db.GetMediaItems(c.Request.Context(), false)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to get media items: %v", err)
		return
	}

	// Convert to admin media items (includes all fields including RequestedBy)
	adminRequests := models.ToAdminMediaItems(requests, h.config)
	adminMediaItems := models.ToAdminMediaItems(mediaItems, h.config)

	c.Header("Content-Type", "text/html")
	if err := pages.AdminPanel(user, adminRequests, adminMediaItems).Render(c.Request.Context(), c.Writer); err != nil {
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

// MarkMediaAsProtected marks a media item as protected for a set duration.
func (h *AdminHandler) MarkMediaAsProtected(c *gin.Context) {
	mediaIDVal := c.Param("id")
	mediaID, err := parseUintParam(mediaIDVal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid media ID",
		})
		return
	}

	media, err := h.db.GetMediaItemByID(c.Request.Context(), mediaID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Database error",
		})
		return
	}

	libraryConfig := h.config.GetLibraryConfig(media.LibraryName)
	if libraryConfig == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "No library configuration found",
		})
		return
	}

	protectedUntil := time.Now().Add(time.Hour * 24 * time.Duration(libraryConfig.GetProtectionPeriod()))
	err = h.db.SetMediaProtectedUntil(c.Request.Context(), media.ID, &protectedUntil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Failed to set media protected until",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Media protected successfully",
	})
}

// MarkMediaAsUnkeepable marks a media item as unkeepable and deny all keep requests.
func (h *AdminHandler) MarkMediaAsUnkeepable(c *gin.Context) {
	mediaIDVal := c.Param("id")
	mediaID, err := parseUintParam(mediaIDVal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid media ID",
		})
		return
	}

	media, err := h.db.GetMediaItemByID(c.Request.Context(), mediaID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Database error",
		})
		return
	}

	err = h.db.MarkMediaAsUnkeepable(c.Request.Context(), media.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Failed to mark media as unkeepable",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Media marked for deletion successfully",
	})
}

// MarkMediaAsKeepForever removes the media item from the database.
// It also adds a "jellysweep-ignore" tag to the media item in Sonarr/Radarr to prevent it from being re-added.
func (h *AdminHandler) MarkMediaAsKeepForever(c *gin.Context) {
	mediaIDVal := c.Param("id")
	mediaID, err := parseUintParam(mediaIDVal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid media ID",
		})
		return
	}

	media, err := h.db.GetMediaItemByID(c.Request.Context(), mediaID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Database error",
		})
		return
	}

	err = h.engine.AddIgnoreTag(c.Request.Context(), media)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Engine error",
		})
		return
	}

	err = h.db.DeleteMediaItem(c.Request.Context(), media.ID, database.DBDeleteReasonKeepForever)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Database error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Media protected forever",
	})
}

// GetKeepRequests returns keep requests as JSON.
func (h *AdminHandler) GetKeepRequests(c *gin.Context) {
	requests, err := h.db.GetMediaWithPendingRequest(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get keep requests",
		})
		return
	}

	// Convert to admin media items (includes all fields including RequestedBy)
	adminRequests := models.ToAdminMediaItems(requests, h.config)

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"keepRequests": adminRequests,
	})
}

// GetAdminMediaItems returns media items for admin with caching support.
func (h *AdminHandler) GetAdminMediaItems(c *gin.Context) {
	mediaItems, err := h.db.GetMediaItems(c.Request.Context(), false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get media items",
		})
		return
	}

	// Convert to admin media items (includes all fields including RequestedBy)
	adminMediaItems := models.ToAdminMediaItems(mediaItems, h.config)

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"mediaItems": adminMediaItems,
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
		if err := engineCache.SonarrTagsCache.Clear(c.Request.Context()); err != nil {
			log.Error("Failed to clear Sonarr tags cache", "error", err)
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
