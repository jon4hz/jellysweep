package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type IntegrationTestSuite struct {
	suite.Suite
	router *gin.Engine
}

func (s *IntegrationTestSuite) SetupTest() {
	gin.SetMode(gin.TestMode)
	s.router = gin.New()

	// Setup session middleware for tests
	store := cookie.NewStore([]byte("test-secret-key"))
	s.router.Use(sessions.Sessions("jellysweep-session", store))
}

func (s *IntegrationTestSuite) TestMultiProvider_Integration_JellyfinOnly() {
	// Test complete flow with Jellyfin-only configuration
	cfg := &config.AuthConfig{
		Jellyfin: &config.JellyfinConfig{
			Enabled: true,
			URL:     "http://localhost:8096",
		},
	}

	provider, err := NewProvider(context.Background(), cfg)
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), provider)

	// Setup routes
	s.router.POST("/login", provider.Login)
	s.router.GET("/callback", provider.Callback)
	s.router.GET("/protected", provider.RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "protected content"})
	})
	s.router.GET("/admin", provider.RequireAuth(), provider.RequireAdmin(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "admin content"})
	})

	// Test login with Jellyfin credentials (will fail without real server)
	form := url.Values{}
	form.Add("username", "testuser")
	form.Add("password", "testpass")

	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	// Should get unauthorized since no real Jellyfin server
	assert.Equal(s.T(), http.StatusUnauthorized, w.Code)

	// Test OAuth callback (should return not found)
	req2 := httptest.NewRequest("GET", "/callback", nil)
	w2 := httptest.NewRecorder()

	s.router.ServeHTTP(w2, req2)

	assert.Equal(s.T(), http.StatusNotFound, w2.Code)

	// Test protected route without authentication
	req3 := httptest.NewRequest("GET", "/protected", nil)
	w3 := httptest.NewRecorder()

	s.router.ServeHTTP(w3, req3)

	assert.Equal(s.T(), http.StatusFound, w3.Code)
	assert.Equal(s.T(), "/login", w3.Header().Get("Location"))
}

func (s *IntegrationTestSuite) TestMultiProvider_Integration_BothProviders() {
	// Test configuration with both providers enabled
	cfg := &config.AuthConfig{
		OIDC: &config.OIDCConfig{
			Enabled:      true,
			Issuer:       "https://auth.mydomain.com", // Will fail but tests structure
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
			RedirectURL:  "http://localhost:8080/callback",
			AdminGroup:   "admin",
		},
		Jellyfin: &config.JellyfinConfig{
			Enabled: true,
			URL:     "http://localhost:8096",
		},
	}

	// This will likely fail due to OIDC provider initialization
	provider, err := NewProvider(context.Background(), cfg)
	if err != nil {
		// Expected in test environment without real OIDC
		assert.Contains(s.T(), err.Error(), "failed to create OIDC provider")
		return
	}

	// If it succeeds (unlikely in test), verify both providers exist
	mp, ok := provider.(*MultiProvider)
	assert.True(s.T(), ok)
	assert.True(s.T(), mp.HasJellyfin())
	// OIDC might be nil due to network issues in test
}

func (s *IntegrationTestSuite) TestAuthProvider_Interface() {
	// Test that all providers implement the AuthProvider interface
	cfg := &config.AuthConfig{
		Jellyfin: &config.JellyfinConfig{
			Enabled: true,
			URL:     "http://localhost:8096",
		},
	}

	provider, err := NewProvider(context.Background(), cfg)
	assert.NoError(s.T(), err)

	// Test that provider implements all interface methods
	var _ AuthProvider = provider

	// Test each method exists and can be called
	assert.NotNil(s.T(), provider.GetAuthConfig())
	assert.NotNil(s.T(), provider.RequireAuth())
	assert.NotNil(s.T(), provider.RequireAdmin())

	// Test Login and Callback don't panic when called with valid context
	s.router.POST("/test-login", provider.Login)
	s.router.GET("/test-callback", provider.Callback)

	req1 := httptest.NewRequest("POST", "/test-login", nil)
	w1 := httptest.NewRecorder()
	s.router.ServeHTTP(w1, req1)
	// Should not panic, might return error due to missing credentials

	req2 := httptest.NewRequest("GET", "/test-callback", nil)
	w2 := httptest.NewRecorder()
	s.router.ServeHTTP(w2, req2)
	// Should not panic, will return not found for Jellyfin provider
	assert.Equal(s.T(), http.StatusNotFound, w2.Code)
}

func (s *IntegrationTestSuite) TestMiddleware_Chain() {
	// Test middleware chaining works correctly
	cfg := &config.AuthConfig{
		Jellyfin: &config.JellyfinConfig{
			Enabled: true,
			URL:     "http://localhost:8096",
		},
	}

	provider, err := NewProvider(context.Background(), cfg)
	assert.NoError(s.T(), err)

	callOrder := []string{}

	s.router.GET("/admin",
		func(c *gin.Context) {
			callOrder = append(callOrder, "middleware1")
			c.Next()
		},
		provider.RequireAuth(),
		func(c *gin.Context) {
			callOrder = append(callOrder, "middleware2")
			c.Next()
		},
		provider.RequireAdmin(),
		func(c *gin.Context) {
			callOrder = append(callOrder, "handler")
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		},
	)

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	// Should be redirected due to no authentication
	assert.Equal(s.T(), http.StatusFound, w.Code)
	assert.Contains(s.T(), callOrder, "middleware1")
	// Auth middleware should abort, so subsequent middleware shouldn't run
}

func (s *IntegrationTestSuite) TestSessionPersistence() {
	// Test that sessions work correctly across requests
	router := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	router.Use(sessions.Sessions("test", store))

	// Route to set session
	router.POST("/set-session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", "test-user")
		session.Set("user_name", "Test User")
		session.Set("user_is_admin", true)
		session.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	// Route to read session
	router.GET("/read-session", func(c *gin.Context) {
		session := sessions.Default(c)
		userID := getSessionString(session, "user_id")
		userName := getSessionString(session, "user_name")
		isAdmin := getSessionBool(session, "user_is_admin")

		c.JSON(http.StatusOK, gin.H{
			"user_id":   userID,
			"user_name": userName,
			"is_admin":  isAdmin,
		})
	})

	// Set session
	req1 := httptest.NewRequest("POST", "/set-session", nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(s.T(), http.StatusOK, w1.Code)

	// Get cookies
	cookies := w1.Result().Cookies()

	// Read session with cookies
	req2 := httptest.NewRequest("GET", "/read-session", nil)
	for _, cookie := range cookies {
		req2.AddCookie(cookie)
	}
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	assert.Equal(s.T(), http.StatusOK, w2.Code)
	assert.Contains(s.T(), w2.Body.String(), "test-user")
	assert.Contains(s.T(), w2.Body.String(), "Test User")
	assert.Contains(s.T(), w2.Body.String(), "true")
}

func (s *IntegrationTestSuite) TestConfigValidation() {
	// Test various configuration scenarios
	testCases := []struct {
		name        string
		config      *config.AuthConfig
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Nil config",
			config:      nil,
			expectError: true,
			errorMsg:    "auth config is required",
		},
		{
			name: "No providers enabled",
			config: &config.AuthConfig{
				OIDC:     &config.OIDCConfig{Enabled: false},
				Jellyfin: &config.JellyfinConfig{Enabled: false},
			},
			expectError: true,
			errorMsg:    "no authentication provider is enabled",
		},
		{
			name: "Only Jellyfin enabled",
			config: &config.AuthConfig{
				Jellyfin: &config.JellyfinConfig{
					Enabled: true,
					URL:     "http://localhost:8096",
				},
			},
			expectError: false,
		},
		{
			name: "Jellyfin nil but OIDC disabled",
			config: &config.AuthConfig{
				OIDC: &config.OIDCConfig{Enabled: false},
			},
			expectError: true,
			errorMsg:    "no authentication provider is enabled",
		},
	}

	for _, tc := range testCases {
		s.T().Run(tc.name, func(t *testing.T) {
			provider, err := NewProvider(context.Background(), tc.config)

			if tc.expectError {
				assert.Error(t, err)
				assert.Nil(t, provider)
				if tc.errorMsg != "" {
					assert.Contains(t, err.Error(), tc.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
			}
		})
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}
