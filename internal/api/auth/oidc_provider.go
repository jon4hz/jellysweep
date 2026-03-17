package auth

import (
	"context"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"golang.org/x/oauth2"
)

type OIDCProvider struct {
	db          database.UserDB
	provider    *oidc.Provider
	verifier    *oidc.IDTokenVerifier
	config      *oauth2.Config
	cfg         *config.OIDCConfig
	gravatarCfg *config.GravatarConfig
}

func NewOIDCProvider(ctx context.Context, cfg *config.OIDCConfig, gravatarCfg *config.GravatarConfig, db database.UserDB) (*OIDCProvider, error) {
	p := OIDCProvider{
		cfg:         cfg,
		gravatarCfg: gravatarCfg,
		db:          db,
	}
	var err error
	httpClient := &http.Client{Timeout: config.TimeoutDuration(cfg.Timeout)}
	ctx = oidc.ClientContext(ctx, httpClient)
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

	p.verifier = p.provider.VerifierContext(ctx, &oidc.Config{ClientID: cfg.ClientID})
	return &p, nil
}

func (p *OIDCProvider) RequireAuth() gin.HandlerFunc {
	return requireAuth(p.gravatarCfg)
}

func (p *OIDCProvider) RequireAdmin() gin.HandlerFunc {
	return requireAdmin()
}
