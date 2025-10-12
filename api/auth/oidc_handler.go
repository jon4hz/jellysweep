package auth

import (
	"net/http"
	"slices"

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
	session.Set("user_email", claims.Email) // required for gravatar
	session.Set("user_name", claims.Name)
	session.Set("user_username", claims.PreferredUsername)

	isAdmin := slices.Contains(claims.Groups, p.cfg.AdminGroup)
	session.Set("user_is_admin", isAdmin)

	// Get or create user in database
	user, err := p.db.GetOrCreateUser(c.Request.Context(), claims.PreferredUsername)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err) //nolint:errcheck
		return
	}
	session.Set("user_id", user.ID)

	if err := session.Save(); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err) //nolint:errcheck
		return
	}

	c.Redirect(http.StatusFound, "/")
}
