package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
)

// APIKeyProvider provides an implementation of the AuthProvider interface
// for API key authentication.
type APIKeyProvider struct {
	apiKey string
}

// NewAPIKeyProvider creates a new API key authentication provider.
func NewAPIKeyProvider(apiKey string) AuthProvider {
	return &APIKeyProvider{
		apiKey: apiKey,
	}
}

// Login is a no-op implementation.
func (ap *APIKeyProvider) Login(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "Not implemented"})
}

// Callback is a no-op implementation.
func (ap *APIKeyProvider) Callback(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "Not implemented"})
}

// RequireAuth returns a middleware that always passes through when authentication is disabled.
func (ap *APIKeyProvider) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("X-API-Key") != ap.apiKey {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
			c.Abort()
			return
		}

		c.Set("user_id", uint(0))
		c.Set("user", &models.User{
			Name:     "api_key",
			Username: "api_key",
			IsAdmin:  true,
		})
		c.Next()
	}
}

// RequireAdmin returns a middleware that always passes through when authentication is disabled.
func (ap *APIKeyProvider) RequireAdmin() gin.HandlerFunc {
	return ap.RequireAuth() // Admin check is the same as auth check for API key
}

// GetAuthConfig returns a minimal auth config.
func (ap *APIKeyProvider) GetAuthConfig() *config.AuthConfig {
	return &config.AuthConfig{
		// Return empty config with no providers enabled
		OIDC:     &config.OIDCConfig{Enabled: false},
		Jellyfin: &config.JellyfinAuthConfig{Enabled: false},
	}
}
