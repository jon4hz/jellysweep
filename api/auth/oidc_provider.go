package auth

import (
	"context"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/gravatar"
	"golang.org/x/oauth2"
)

type OIDCProvider struct {
	provider    *oidc.Provider
	verifier    *oidc.IDTokenVerifier
	config      *oauth2.Config
	cfg         *config.OIDCConfig
	gravatarCfg *config.GravatarConfig
}

func NewOIDCProvider(ctx context.Context, cfg *config.OIDCConfig, gravatarCfg *config.GravatarConfig) (*OIDCProvider, error) {
	p := OIDCProvider{
		cfg:         cfg,
		gravatarCfg: gravatarCfg,
	}
	var err error
	p.provider, err = oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}

	p.config = &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     p.provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups"},
	}

	p.verifier = p.provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	return &p, nil
}

func (p *OIDCProvider) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		if userID == nil {
			c.Redirect(http.StatusFound, "/auth/oidc/login")
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

		// Generate Gravatar URL if enabled and email is available
		if p.gravatarCfg != nil && user.Email != "" {
			user.GravatarURL = gravatar.GenerateURL(user.Email, p.gravatarCfg)
		}

		c.Set("user_id", userID)
		c.Set("user", user)
		c.Next()
	}
}

func (p *OIDCProvider) RequireAdmin() gin.HandlerFunc {
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

// GetAuthConfig returns the OIDC configuration wrapped in AuthConfig.
func (p *OIDCProvider) GetAuthConfig() *config.AuthConfig {
	return &config.AuthConfig{
		OIDC: p.cfg,
	}
}
