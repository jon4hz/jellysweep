package handler

import (
	"net/http"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/web/templates/pages"
)

type Handler struct {
}

func New() *Handler {
	return &Handler{}
}

func (h *Handler) Home(c *gin.Context) {
	user := c.MustGet("user").(*models.User)

	// Mock data for now - replace with actual data from your engine
	now := time.Now()
	mockMediaItems := []pages.MediaItem{
		{
			ID:           "1",
			Title:        "The Matrix",
			Type:         "movie",
			Year:         1999,
			Library:      "Movies",
			DeletionDate: now.AddDate(0, 0, 7), // in 7 days
			PosterURL:    "",
			CanRequest:   true,
			HasRequested: false,
		},
		{
			ID:           "2",
			Title:        "Breaking Bad",
			Type:         "tv",
			Year:         2008,
			Library:      "TV Shows",
			DeletionDate: now.AddDate(0, 0, 3), // in 3 days
			PosterURL:    "",
			CanRequest:   true,
			HasRequested: true,
		},
		{
			ID:           "3",
			Title:        "Inception",
			Type:         "movie",
			Year:         2010,
			Library:      "Movies",
			DeletionDate: now.AddDate(0, 0, 14), // in 14 days
			PosterURL:    "",
			CanRequest:   true,
			HasRequested: false,
		},
	}

	c.Header("Content-Type", "text/html")
	pages.Dashboard(user, mockMediaItems).Render(c.Request.Context(), c.Writer)
}

func (h *Handler) Login(c *gin.Context) {
	session := sessions.Default(c)
	sessionID := session.Get("user_id")
	isLoggedIn := sessionID != nil && sessionID != ""
	if isLoggedIn {
		c.Redirect(http.StatusFound, "/")
		return
	}

	c.Header("Content-Type", "text/html")
	pages.Login().Render(c.Request.Context(), c.Writer)
}

func (h *Handler) Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	if err := session.Save(); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, "/login")
}

// API endpoint for requesting to keep media
func (h *Handler) RequestKeepMedia(c *gin.Context) {
	mediaID := c.Param("id")
	user := c.MustGet("user").(*models.User)

	// TODO: Implement actual logic to store the request
	// For now, just return success
	_ = mediaID
	_ = user

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Request submitted successfully",
	})
}
