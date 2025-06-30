package auth

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/jellyfin"
)

type JellyfinProvider struct {
	client *jellyfin.Client
	cfg    *config.JellyfinConfig
}

func NewJellyfinProvider(cfg *config.JellyfinConfig) *JellyfinProvider {
	return &JellyfinProvider{
		client: jellyfin.New(cfg),
		cfg:    cfg,
	}
}

func (p *JellyfinProvider) Login(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	if username == "" || password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username and password are required"})
		return
	}

	ctx := c.Request.Context()
	authResp, err := p.client.AuthenticateByName(ctx, username, password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Save user info in session
	session := sessions.Default(c)
	session.Set("user_id", authResp.User.ID)
	session.Set("user_email", "") // Jellyfin doesn't provide email in auth response
	session.Set("user_name", authResp.User.Name)
	session.Set("user_username", authResp.User.Name)
	session.Set("user_is_admin", authResp.User.Configuration.IsAdministrator)

	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "redirect": "/"})
}

func (p *JellyfinProvider) Callback(c *gin.Context) {
	// Jellyfin doesn't use OAuth callback flow, this is a no-op
	c.JSON(http.StatusNotFound, gin.H{"error": "Not implemented"})
}

func (p *JellyfinProvider) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		if userID == nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		// create user model from session data
		user := &models.User{
			Sub:      userID.(string),
			Email:    getSessionString(session, "user_email"),
			Name:     getSessionString(session, "user_name"),
			Username: getSessionString(session, "user_username"),
			IsAdmin:  getSessionBool(session, "user_is_admin"),
		}

		c.Set("user_id", userID)
		c.Set("user", user)
		c.Next()
	}
}

func (p *JellyfinProvider) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := c.MustGet("user").(*models.User)
		if !ok || !user.IsAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// GetAuthConfig returns the Jellyfin configuration wrapped in AuthConfig
func (p *JellyfinProvider) GetAuthConfig() *config.AuthConfig {
	return &config.AuthConfig{
		Jellyfin: p.cfg,
	}
}
