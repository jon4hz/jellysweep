package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
)

// NoOpProvider provides a no-op implementation of the AuthProvider interface
// for when authentication is disabled in the configuration.
type NoOpProvider struct{}

// NewNoOpProvider creates a new no-op authentication provider.
func NewNoOpProvider() AuthProvider {
	return &NoOpProvider{}
}

// Login is a no-op implementation.
func (np *NoOpProvider) Login(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "authentication disabled"})
}

// Callback is a no-op implementation.
func (np *NoOpProvider) Callback(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "authentication disabled"})
}

// RequireAuth returns a middleware that always passes through when authentication is disabled.
func (np *NoOpProvider) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Set a default user
		c.Set("user_id", uint(0))
		c.Set("user", &models.User{
			Name:     "Anonymous User",
			Username: "anonymous",
			IsAdmin:  true, // Grant admin rights since there's no auth
		})
		c.Next()
	}
}

// RequireAdmin returns a middleware that always passes through when authentication is disabled.
func (np *NoOpProvider) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

// GetAuthConfig returns a minimal auth config.
func (np *NoOpProvider) GetAuthConfig() *config.AuthConfig {
	return &config.AuthConfig{
		// Return empty config with no providers enabled
		OIDC:     &config.OIDCConfig{Enabled: false},
		Jellyfin: &config.JellyfinAuthConfig{Enabled: false},
	}
}
