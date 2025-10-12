package auth

import (
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"
	jellyfin "github.com/sj14/jellyfin-go/api"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/database"
	"github.com/jon4hz/jellysweep/gravatar"
	"github.com/jon4hz/jellysweep/version"
)

type JellyfinProvider struct {
	db          database.DB
	client      *jellyfin.APIClient
	cfg         *config.JellyfinAuthConfig
	gravatarCfg *config.GravatarConfig
}

func NewJellyfinProvider(cfg *config.JellyfinConfig, db database.DB, authCfg *config.JellyfinAuthConfig, gravatarCfg *config.GravatarConfig) *JellyfinProvider {
	clientConfig := jellyfin.NewConfiguration()
	clientConfig.Servers = jellyfin.ServerConfigurations{
		{
			URL:         cfg.URL,
			Description: "Jellyfin auth server",
		},
	}
	clientConfig.UserAgent = fmt.Sprintf("Jellysweep-Auth/%s", version.Version)
	clientConfig.DefaultHeader = map[string]string{
		"X-Emby-Authorization": fmt.Sprintf(
			`MediaBrowser Client="Jellysweep-Auth", Device="Jellysweep", DeviceId="jellysweep-auth", Version="%s",`,
			version.Version,
		),
	}
	return &JellyfinProvider{
		db:          db,
		client:      jellyfin.NewAPIClient(clientConfig),
		cfg:         authCfg,
		gravatarCfg: gravatarCfg,
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
	auth := *jellyfin.NewAuthenticateUserByName()
	auth.SetUsername(username)
	auth.SetPw(password)
	authResp, r, err := p.client.UserAPI.AuthenticateUserByName(ctx).AuthenticateUserByName(auth).Execute()
	if err != nil {
		if r != nil {
			log.Error("Failed to authenticate user", "error", err, "status", r.StatusCode)
		} else {
			log.Error("Failed to authenticate user", "error", err)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}
	defer r.Body.Close() //nolint:errcheck

	jellyfinUsername := authResp.GetUser().Name.Get()
	if jellyfinUsername == nil || *jellyfinUsername == "" {
		log.Error("Jellyfin user has no username")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user information"})
		return
	}

	// Save user info in session
	session := sessions.Default(c)
	session.Set("user_id", authResp.GetUser().Id)
	session.Set("user_email", "") // Jellyfin doesn't provide email in auth response
	session.Set("user_name", *jellyfinUsername)
	session.Set("user_username", *jellyfinUsername)
	session.Set("user_is_admin", *authResp.GetUser().Policy.Get().IsAdministrator)

	// Get or create user in database
	user, err := p.db.GetOrCreateUser(c.Request.Context(), *jellyfinUsername)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err) //nolint:errcheck
		return
	}
	session.Set("user_id", user.ID)

	if err := session.Save(); err != nil {
		log.Error("Failed to save session", "error", err)
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
			ID:       userID.(uint), // Jellyfin user ID is an integer
			Email:    getSessionString(session, "user_email"),
			Name:     getSessionString(session, "user_name"),
			Username: getSessionString(session, "user_username"),
			IsAdmin:  getSessionBool(session, "user_is_admin"),
		}

		// Generate Gravatar URL if enabled and email is available
		if p.gravatarCfg != nil && user.Email != "" {
			user.GravatarURL = gravatar.GenerateURL(user.Email, p.gravatarCfg)
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

// GetAuthConfig returns the Jellyfin configuration wrapped in AuthConfig.
func (p *JellyfinProvider) GetAuthConfig() *config.AuthConfig {
	return &config.AuthConfig{
		Jellyfin: p.cfg,
	}
}
