package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/gravatar"
)

// AuthProvider defines the interface for authentication providers.
type AuthProvider interface {
	// Login handles the login process for the provider
	Login(c *gin.Context)

	// Callback handles the authentication callback (if applicable)
	Callback(c *gin.Context)

	// RequireAuth returns middleware that requires authentication
	RequireAuth() gin.HandlerFunc

	// RequireAdmin returns middleware that requires admin privileges
	RequireAdmin() gin.HandlerFunc
}

// MultiProvider wraps multiple auth providers.
type MultiProvider struct {
	db               database.UserDB
	oidcProvider     *OIDCProvider
	jellyfinProvider *JellyfinProvider
	cfg              *config.AuthConfig
	gravatarCfg      *config.GravatarConfig
}

// NewProvider creates a multi-provider that supports both OIDC and Jellyfin authentication.
func NewProvider(ctx context.Context, cfg *config.Config, gravatarCfg *config.GravatarConfig, db database.UserDB) (AuthProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("auth config is required")
	}

	mp := &MultiProvider{cfg: cfg.Auth, gravatarCfg: gravatarCfg, db: db}

	// Initialize OIDC provider if enabled
	if cfg.Auth.OIDC != nil && cfg.Auth.OIDC.Enabled {
		oidcProvider, err := NewOIDCProvider(ctx, cfg.Auth.OIDC, gravatarCfg, db)
		if err != nil {
			return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
		}
		mp.oidcProvider = oidcProvider
	}

	// Initialize Jellyfin provider if enabled
	if cfg.Jellyfin != nil && cfg.Auth.Jellyfin.Enabled {
		mp.jellyfinProvider = NewJellyfinProvider(cfg.Jellyfin, db, cfg.Auth.Jellyfin, gravatarCfg)
	}

	// At least one provider must be enabled
	if mp.oidcProvider == nil && mp.jellyfinProvider == nil {
		return nil, fmt.Errorf("no authentication provider is enabled")
	}

	return mp, nil
}

// Login handles login for the appropriate provider.
func (mp *MultiProvider) Login(c *gin.Context) {
	// Check if this is a Jellyfin login request (has username/password form data)
	if c.Request.Method == "POST" && (c.PostForm("username") != "" || c.PostForm("password") != "") {
		if mp.jellyfinProvider != nil {
			mp.jellyfinProvider.Login(c)
			return
		}
	}

	// Default to OIDC login
	if mp.oidcProvider != nil {
		mp.oidcProvider.Login(c)
		return
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": "No authentication method available"})
}

// Callback handles OAuth callbacks (OIDC only).
func (mp *MultiProvider) Callback(c *gin.Context) {
	if mp.oidcProvider != nil {
		mp.oidcProvider.Callback(c)
		return
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "OAuth callback not supported"})
}

// RequireAuth returns middleware that works with both providers.
func (mp *MultiProvider) RequireAuth() gin.HandlerFunc {
	return requireAuth(mp.gravatarCfg)
}

// RequireAdmin returns middleware that checks for admin privileges.
func (mp *MultiProvider) RequireAdmin() gin.HandlerFunc {
	return requireAdmin()
}

// Helper methods for the MultiProvider.
func (mp *MultiProvider) HasOIDC() bool {
	return mp.oidcProvider != nil
}

func (mp *MultiProvider) HasJellyfin() bool {
	return mp.jellyfinProvider != nil
}

// requireAuth is the shared implementation for RequireAuth middleware.
func requireAuth(gravatarCfg *config.GravatarConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		if userID == nil {
			abortUnauthenticated(c)
			return
		}

		userIDUint, ok := userID.(uint)
		if !ok {
			abortUnauthenticated(c)
			return
		}

		// create user model from session data
		user := &models.User{
			ID:       userIDUint,
			Email:    getSessionString(session, "user_email"),
			Name:     getSessionString(session, "user_name"),
			Username: getSessionString(session, "user_username"),
			IsAdmin:  getSessionBool(session, "user_is_admin"),
		}

		// Generate Gravatar URL if enabled and email is available
		if gravatarCfg != nil && user.Email != "" {
			user.GravatarURL = gravatar.GenerateURL(user.Email, gravatarCfg)
		}

		c.Set("user_id", userID)
		c.Set("user", user)
		c.Next()
	}
}

// requireAdmin is the shared implementation for RequireAdmin middleware.
func requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := c.Get("user")
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			c.Abort()
			return
		}
		u, ok := user.(*models.User)
		if !ok || !u.IsAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// Helper functions to safely get session values.
func getSessionString(session sessions.Session, key string) string {
	if val := session.Get(key); val != nil {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getSessionBool(session sessions.Session, key string) bool {
	if val := session.Get(key); val != nil {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

// abortUnauthenticated returns 401 JSON for API requests or redirects to /login for page requests.
func abortUnauthenticated(c *gin.Context) {
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/admin/api/") {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	} else {
		c.Redirect(http.StatusFound, "/login")
	}
	c.Abort()
}
