package handler

import (
	"net/http"
	"time"

	"github.com/ccoveille/go-safecast"
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
	user := c.MustGet("user").(*models.User)

	mediaIDVal := c.Param("id")
	mediaID, err := parseUintParam(mediaIDVal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid media ID",
		})
		return
	}

	err = h.engine.HandleKeepRequest(c.Request.Context(), user.ID, mediaID, true)
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
	user := c.MustGet("user").(*models.User)

	mediaIDVal := c.Param("id")
	mediaID, err := parseUintParam(mediaIDVal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid media ID",
		})
		return
	}

	err = h.engine.HandleKeepRequest(c.Request.Context(), user.ID, mediaID, false)
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
	user := c.MustGet("user").(*models.User)

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

	if err = h.engine.CreateAdminKeepEvent(c.Request.Context(), user.ID, media); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Failed to create admin keep event",
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
	user := c.MustGet("user").(*models.User)

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

	err = h.engine.CreateAdminUnkeepEvent(c.Request.Context(), user.ID, media)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Failed to create admin unkeep event",
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
	user := c.MustGet("user").(*models.User)

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

	media.DBDeleteReason = database.DBDeleteReasonKeepForever
	err = h.db.DeleteMediaItem(c.Request.Context(), media)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Database error",
		})
		return
	}

	err = h.engine.CreateKeepForeverEvent(c.Request.Context(), user.ID, media)
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

// HistoryPanel shows the deletion history panel.
func (h *AdminHandler) HistoryPanel(c *gin.Context) {
	user := c.MustGet("user").(*models.User)

	c.Header("Content-Type", "text/html")
	if err := pages.HistoryPanel(user).Render(c.Request.Context(), c.Writer); err != nil {
		log.Error("Failed to render history panel", "error", err)
	}
}

// GetHistory returns paginated history events.
func (h *AdminHandler) GetHistory(c *gin.Context) {
	page := 1
	pageSize := 50
	sortBy := c.DefaultQuery("sortBy", "event_time")
	sortOrder := c.DefaultQuery("sortOrder", "desc")
	jellyfinID := c.Query("jellyfinId")

	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := parseUintParam(pageStr); err == nil && p > 0 {
			page, err = safecast.ToInt(p)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   "Invalid page parameter",
				})
				return
			}
		}
	}

	if pageSizeStr := c.Query("pageSize"); pageSizeStr != "" {
		if ps, err := parseUintParam(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
			pageSize, err = safecast.ToInt(ps)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   "Invalid pageSize parameter",
				})
				return
			}
		}
	}

	var events []database.HistoryEvent
	var total int64
	var err error
	// If jellyfinId is provided, get history for that specific media
	if jellyfinID != "" {
		events, err = h.db.GetHistoryEventsByJellyfinID(c.Request.Context(), jellyfinID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "Failed to get history for media",
			})
			return
		}
		total = int64(len(events))

		// Apply manual pagination since GetHistoryEventsByJellyfinID doesn't paginate
		start := (page - 1) * pageSize
		end := start + pageSize
		if start > len(events) {
			events = []database.HistoryEvent{}
		} else {
			if end > len(events) {
				end = len(events)
			}
			events = events[start:end]
		}
	} else {
		// Get all history events
		events, total, err = h.db.GetHistoryEvents(c.Request.Context(), page, pageSize, sortBy, database.SortOrder(sortOrder))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "Failed to get history",
			})
			return
		}
	}

	// Convert to history event items
	items := models.ToHistoryEventItems(events)

	totalPages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		totalPages++
	}

	response := models.HistoryResponse{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    response,
	})
}

// GetHistoryByType returns paginated history events filtered by event type.
func (h *AdminHandler) GetHistoryByType(c *gin.Context) {
	eventType := c.Param("eventType")
	if eventType == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Event type is required",
		})
		return
	}

	page := 1
	pageSize := 50

	if pageStr := c.Query("page"); pageStr != "" {
		p, err := parseUintParam(pageStr)
		if err != nil || p == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "Invalid page parameter",
			})
			return
		}
		page, err = safecast.ToInt(p)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "Invalid page parameter",
			})
			return
		}
	}

	if pageSizeStr := c.Query("pageSize"); pageSizeStr != "" {
		ps, err := parseUintParam(pageSizeStr)
		if err != nil || ps == 0 || ps > 100 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "Invalid pageSize parameter",
			})
			return
		}
		pageSize, err = safecast.ToInt(ps)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "Invalid pageSize parameter",
			})
			return
		}
	}

	// Get history events by type
	events, total, err := h.db.GetHistoryEventsByEventType(
		c.Request.Context(),
		database.HistoryEventType(eventType),
		page,
		pageSize,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get history by type",
		})
		return
	}

	// Convert to history event items
	items := models.ToHistoryEventItems(events)

	totalPages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		totalPages++
	}

	response := models.HistoryResponse{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    response,
	})
}
