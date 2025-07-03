package auth

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (p *OIDCProvider) Login(c *gin.Context) {
	state := uuid.New().String()
	url := p.config.AuthCodeURL(state)
	c.Redirect(http.StatusFound, url)
}

func (p *OIDCProvider) Callback(c *gin.Context) {
	ctx := c.Request.Context()
	code := c.Query("code")

	oauth2Token, err := p.config.Exchange(ctx, code)
	if err != nil {
		c.AbortWithError(http.StatusUnauthorized, err) //nolint:errcheck
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		c.AbortWithError(http.StatusInternalServerError, err) //nolint:errcheck
		return
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		c.AbortWithError(http.StatusUnauthorized, err) //nolint:errcheck
		return
	}

	var claims struct {
		Email             string   `json:"email"`
		Name              string   `json:"name"`
		PreferredUsername string   `json:"preferred_username"`
		Sub               string   `json:"sub"`
		Groups            []string `json:"groups"`
	}
	if err := idToken.Claims(&claims); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err) //nolint:errcheck
		return
	}

	// Save user ID in session
	session := sessions.Default(c)
	session.Set("user_id", claims.Sub)
	session.Set("user_email", claims.Email)
	session.Set("user_name", claims.Name)
	session.Set("user_username", claims.PreferredUsername)

	var isAdmin bool
	for _, group := range claims.Groups {
		if group == p.cfg.AdminGroup {
			isAdmin = true
			break
		}
	}
	session.Set("user_is_admin", isAdmin)

	if err := session.Save(); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err) //nolint:errcheck
		return
	}
	c.Redirect(http.StatusFound, "/")
}
