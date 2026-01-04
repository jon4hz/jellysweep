package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"slices"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

func (p *OIDCProvider) Login(c *gin.Context) {
	state := uuid.New().String()
	session := sessions.Default(c)

	var url string
	if p.cfg.UsePKCE {
		// Generate PKCE code verifier and challenge
		codeVerifier, err := generateCodeVerifier()
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err) //nolint:errcheck
			return
		}

		// Store the code verifier in session for later use in callback
		session.Set("pkce_verifier", codeVerifier)
		if err := session.Save(); err != nil {
			c.AbortWithError(http.StatusInternalServerError, err) //nolint:errcheck
			return
		}

		url = p.config.AuthCodeURL(state,
			oauth2.S256ChallengeOption(codeVerifier),
		)
	} else {
		url = p.config.AuthCodeURL(state)
	}

	c.Redirect(http.StatusFound, url)
}

func (p *OIDCProvider) Callback(c *gin.Context) {
	ctx := c.Request.Context()
	code := c.Query("code")
	session := sessions.Default(c)

	var oauth2Token *oauth2.Token
	var err error

	if p.cfg.UsePKCE {
		codeVerifier := session.Get("pkce_verifier")
		if codeVerifier == nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		oauth2Token, err = p.config.Exchange(ctx, code,
			oauth2.SetAuthURLParam("code_verifier", codeVerifier.(string)),
		)

		session.Delete("pkce_verifier")
		if err := session.Save(); err != nil {
			c.AbortWithError(http.StatusInternalServerError, err) //nolint:errcheck
			return
		}
	} else {
		oauth2Token, err = p.config.Exchange(ctx, code)
	}

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

	// Update auto-approval permission based on OIDC group membership
	// Only update if auto_approve_group is configured
	if p.cfg.AutoApproveGroup != "" {
		hasAutoApprove := slices.Contains(claims.Groups, p.cfg.AutoApproveGroup)
		if err := p.db.UpdateUserAutoApproval(c.Request.Context(), user.ID, hasAutoApprove); err != nil {
			c.AbortWithError(http.StatusInternalServerError, err) //nolint:errcheck
			return
		}
	}

	if err := session.Save(); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err) //nolint:errcheck
		return
	}

	c.Redirect(http.StatusFound, "/")
}

// generateCodeVerifier creates a random code verifier for PKCE.
func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
