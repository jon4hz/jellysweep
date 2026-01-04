package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"slices"

	"github.com/charmbracelet/log"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

func (p *OIDCProvider) Login(c *gin.Context) {
	state := uuid.New().String()
	session := sessions.Default(c)

	session.Set("oauth_state", state)

	var url string
	if p.cfg.UsePKCE {
		// Generate PKCE code verifier and challenge
		codeVerifier, err := generateCodeVerifier()
		if err != nil {
			log.Error("Failed to generate PKCE code verifier", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Authentication initialization failed"})
			c.Abort()
			return
		}

		// Store the code verifier in session for later use in callback
		session.Set("pkce_verifier", codeVerifier)

		url = p.config.AuthCodeURL(state,
			oauth2.S256ChallengeOption(codeVerifier),
		)
	} else {
		url = p.config.AuthCodeURL(state)
	}

	if err := session.Save(); err != nil {
		log.Error("Failed to save session during login", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Authentication initialization failed"})
		c.Abort()
		return
	}

	c.Redirect(http.StatusFound, url)
}

func (p *OIDCProvider) Callback(c *gin.Context) {
	ctx := c.Request.Context()
	code := c.Query("code")
	state := c.Query("state")
	session := sessions.Default(c)

	storedState := session.Get("oauth_state")
	if storedState == nil || storedState.(string) != state {
		log.Warn("OAuth state validation failed", "expected", storedState, "received", state)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		c.Abort()
		return
	}

	session.Delete("oauth_state")

	var oauth2Token *oauth2.Token
	var err error

	if p.cfg.UsePKCE {
		codeVerifier := session.Get("pkce_verifier")
		if codeVerifier == nil {
			log.Error("PKCE code verifier not found in session")
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authentication failed"})
			c.Abort()
			return
		}

		oauth2Token, err = p.config.Exchange(ctx, code,
			oauth2.SetAuthURLParam("code_verifier", codeVerifier.(string)),
		)

		session.Delete("pkce_verifier")
		if err := session.Save(); err != nil {
			log.Error("Failed to save session after PKCE cleanup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Authentication failed"})
			c.Abort()
			return
		}
	} else {
		oauth2Token, err = p.config.Exchange(ctx, code)
	}

	if err != nil {
		log.Error("Failed to exchange authorization code for token", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		c.Abort()
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		log.Error("ID token not found in OAuth2 token response")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Authentication failed"})
		c.Abort()
		return
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		log.Error("Failed to verify ID token", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		c.Abort()
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
		log.Error("Failed to parse ID token claims", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Authentication failed"})
		c.Abort()
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
		log.Error("Failed to get or create user", "error", err, "username", claims.PreferredUsername)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Authentication failed"})
		c.Abort()
		return
	}
	session.Set("user_id", user.ID)

	// Update auto-approval permission based on OIDC group membership
	// Only update if auto_approve_group is configured
	if p.cfg.AutoApproveGroup != "" {
		hasAutoApprove := slices.Contains(claims.Groups, p.cfg.AutoApproveGroup)
		if err := p.db.UpdateUserAutoApproval(c.Request.Context(), user.ID, hasAutoApprove); err != nil {
			log.Error("Failed to update user auto-approval permission", "error", err, "user_id", user.ID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Authentication failed"})
			c.Abort()
			return
		}
	}

	if err := session.Save(); err != nil {
		log.Error("Failed to save session after authentication", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Authentication failed"})
		c.Abort()
		return
	}

	c.Redirect(http.StatusFound, "/")
}

// generateCodeVerifier creates a random code verifier for PKCE.
func generateCodeVerifier() (string, error) {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
