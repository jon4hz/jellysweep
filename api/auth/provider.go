package auth

import (
	"context"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jon4hz/jellysweep/config"
	"golang.org/x/oauth2"
)

type Provider struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	config   *oauth2.Config
	cfg      *config.OIDCConfig
}

func New(ctx context.Context, cfg *config.OIDCConfig) (*Provider, error) {
	p := Provider{
		cfg: cfg,
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
