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
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type FactoryTestSuite struct {
	suite.Suite
	router *gin.Engine
}

func (s *FactoryTestSuite) SetupTest() {
	gin.SetMode(gin.TestMode)
	s.router = gin.New()

	// Setup session middleware for tests
	store := cookie.NewStore([]byte("test-secret"))
	s.router.Use(sessions.Sessions("mysession", store))
}

func (s *FactoryTestSuite) TestNewProvider_NilConfig() {
	provider, err := NewProvider(context.Background(), nil, nil)
	assert.Nil(s.T(), provider)
	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(), "auth config is required")
}

func (s *FactoryTestSuite) TestNewProvider_NoProvidersEnabled() {
	cfg := &config.AuthConfig{
		OIDC: &config.OIDCConfig{
			Enabled: false,
		},
		Jellyfin: &config.JellyfinConfig{
			Enabled: false,
		},
	}

	provider, err := NewProvider(context.Background(), cfg, nil)
	assert.Nil(s.T(), provider)
	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(), "no authentication provider is enabled")
}

func (s *FactoryTestSuite) TestNewProvider_OnlyJellyfinEnabled() {
	cfg := &config.AuthConfig{
		Jellyfin: &config.JellyfinConfig{
			Enabled: true,
			URL:     "http://localhost:8096",
		},
	}

	provider, err := NewProvider(context.Background(), cfg, nil)
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), provider)

	mp, ok := provider.(*MultiProvider)
	assert.True(s.T(), ok)
	assert.NotNil(s.T(), mp.jellyfinProvider)
	assert.Nil(s.T(), mp.oidcProvider)
}

func (s *FactoryTestSuite) TestNewProvider_InvalidOIDCConfig() {
	cfg := &config.AuthConfig{
		OIDC: &config.OIDCConfig{
			Enabled:      true,
			Issuer:       "invalid-issuer",
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			RedirectURL:  "http://localhost/callback",
		},
	}

	provider, err := NewProvider(context.Background(), cfg, nil)
	assert.Error(s.T(), err)
	assert.Nil(s.T(), provider)
	assert.Contains(s.T(), err.Error(), "failed to create OIDC provider")
}

func (s *FactoryTestSuite) TestMultiProvider_Login_JellyfinPost() {
	cfg := &config.AuthConfig{
		Jellyfin: &config.JellyfinConfig{
			Enabled: true,
			URL:     "http://localhost:8096",
		},
	}

	provider, err := NewProvider(context.Background(), cfg, nil)
	assert.NoError(s.T(), err)

	// Setup test route
	s.router.POST("/login", provider.Login)

	// Test Jellyfin login (POST with username/password)
	form := url.Values{}
	form.Add("username", "testuser")
	form.Add("password", "testpass")

	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	// Since we don't have a real Jellyfin server, we expect an authentication error
	assert.Equal(s.T(), http.StatusUnauthorized, w.Code)
}

func (s *FactoryTestSuite) TestMultiProvider_Login_NoProviders() {
	mp := &MultiProvider{
		cfg:         &config.AuthConfig{},
		gravatarCfg: nil,
	}

	s.router.GET("/login", mp.Login)

	req := httptest.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusBadRequest, w.Code)
}

func (s *FactoryTestSuite) TestMultiProvider_Callback_NoOIDC() {
	mp := &MultiProvider{
		cfg:         &config.AuthConfig{},
		gravatarCfg: nil,
	}

	s.router.GET("/callback", mp.Callback)

	req := httptest.NewRequest("GET", "/callback", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusNotFound, w.Code)
}

func (s *FactoryTestSuite) TestMultiProvider_RequireAuth_NoSession() {
	mp := &MultiProvider{
		cfg:         &config.AuthConfig{},
		gravatarCfg: nil,
	}

	s.router.GET("/protected", mp.RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusFound, w.Code)
	assert.Equal(s.T(), "/login", w.Header().Get("Location"))
}

func (s *FactoryTestSuite) TestMultiProvider_RequireAuth_WithSession() {
	mp := &MultiProvider{
		cfg:         &config.AuthConfig{},
		gravatarCfg: nil,
	}

	// Create a custom router for this test to properly handle sessions
	router := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	router.Use(sessions.Sessions("mysession", store))

	// First, create a route to set up the session
	router.POST("/setup-session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", "test-user-id")
		session.Set("user_email", "test@example.com")
		session.Set("user_name", "Test User")
		session.Set("user_username", "testuser")
		session.Set("user_is_admin", false)
		session.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	// Then create the protected route
	router.GET("/protected", mp.RequireAuth(), func(c *gin.Context) {
		user := c.MustGet("user").(*models.User)
		c.JSON(http.StatusOK, gin.H{"user": user.Username})
	})

	// First request to set up session
	req1 := httptest.NewRequest("POST", "/setup-session", nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	// Extract cookies from the first response
	cookies := w1.Result().Cookies()

	// Second request to access protected route with session
	req2 := httptest.NewRequest("GET", "/protected", nil)
	for _, cookie := range cookies {
		req2.AddCookie(cookie)
	}
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	assert.Equal(s.T(), http.StatusOK, w2.Code)
	assert.Contains(s.T(), w2.Body.String(), "testuser")
}

func (s *FactoryTestSuite) TestMultiProvider_RequireAdmin_NotAdmin() {
	mp := &MultiProvider{
		cfg:         &config.AuthConfig{},
		gravatarCfg: nil,
	}

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	// Create a gin context and set a non-admin user
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user", &models.User{
		Sub:     "test-user",
		IsAdmin: false,
	})

	// Manually call the middleware
	middleware := mp.RequireAdmin()
	middleware(c)

	assert.True(s.T(), c.IsAborted())
}

func (s *FactoryTestSuite) TestMultiProvider_RequireAdmin_IsAdmin() {
	mp := &MultiProvider{
		cfg:         &config.AuthConfig{},
		gravatarCfg: nil,
	}

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	// Create a gin context and set an admin user
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user", &models.User{
		Sub:     "admin-user",
		IsAdmin: true,
	})

	// Manually call the middleware
	middleware := mp.RequireAdmin()
	middleware(c)

	// If not aborted, the middleware passed
	assert.False(s.T(), c.IsAborted())
}

func (s *FactoryTestSuite) TestMultiProvider_GetAuthConfig() {
	cfg := &config.AuthConfig{
		OIDC: &config.OIDCConfig{
			Enabled: true,
		},
	}

	mp := &MultiProvider{cfg: cfg, gravatarCfg: nil}
	result := mp.GetAuthConfig()

	assert.Equal(s.T(), cfg, result)
}

func (s *FactoryTestSuite) TestMultiProvider_HasOIDC() {
	mp := &MultiProvider{}

	// Test without OIDC
	assert.False(s.T(), mp.HasOIDC())

	// Test with OIDC
	mp.oidcProvider = &OIDCProvider{}
	assert.True(s.T(), mp.HasOIDC())
}

func (s *FactoryTestSuite) TestMultiProvider_HasJellyfin() {
	mp := &MultiProvider{}

	// Test without Jellyfin
	assert.False(s.T(), mp.HasJellyfin())

	// Test with Jellyfin
	mp.jellyfinProvider = &JellyfinProvider{}
	assert.True(s.T(), mp.HasJellyfin())
}

func (s *FactoryTestSuite) TestGetSessionString() {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	router.Use(sessions.Sessions("test", store))

	router.GET("/test", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("string_key", "test_value")
		session.Set("non_string_key", 123)
		session.Save()

		// Test existing string value
		result1 := getSessionString(session, "string_key")
		assert.Equal(s.T(), "test_value", result1)

		// Test non-string value
		result2 := getSessionString(session, "non_string_key")
		assert.Equal(s.T(), "", result2)

		// Test non-existent key
		result3 := getSessionString(session, "missing_key")
		assert.Equal(s.T(), "", result3)

		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
}

func (s *FactoryTestSuite) TestGetSessionBool() {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	router.Use(sessions.Sessions("test", store))

	router.GET("/test", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("bool_key", true)
		session.Set("non_bool_key", "not_bool")
		session.Save()

		// Test existing bool value
		result1 := getSessionBool(session, "bool_key")
		assert.True(s.T(), result1)

		// Test non-bool value
		result2 := getSessionBool(session, "non_bool_key")
		assert.False(s.T(), result2)

		// Test non-existent key
		result3 := getSessionBool(session, "missing_key")
		assert.False(s.T(), result3)

		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
}

func (s *FactoryTestSuite) TestMultiProvider_RequireAuth_WithGravatar() {
	gravatarCfg := &config.GravatarConfig{
		Enabled:      true,
		DefaultImage: "mp",
		Rating:       "g",
		Size:         80,
	}

	mp := &MultiProvider{
		cfg:         &config.AuthConfig{},
		gravatarCfg: gravatarCfg,
	}

	// Create a custom router for this test to properly handle sessions
	router := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	router.Use(sessions.Sessions("mysession", store))

	// First, create a route to set up the session with email
	router.POST("/setup-session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", "test-user-id")
		session.Set("user_email", "test@example.com")
		session.Set("user_name", "Test User")
		session.Set("user_username", "testuser")
		session.Set("user_is_admin", false)
		session.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	// Then create the protected route
	router.GET("/protected", mp.RequireAuth(), func(c *gin.Context) {
		user := c.MustGet("user").(*models.User)
		c.JSON(http.StatusOK, gin.H{
			"user":        user.Username,
			"gravatarURL": user.GravatarURL,
		})
	})

	// First request to set up session
	req1 := httptest.NewRequest("POST", "/setup-session", nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	// Extract cookies from the first response
	cookies := w1.Result().Cookies()

	// Second request to access protected route with session
	req2 := httptest.NewRequest("GET", "/protected", nil)
	for _, cookie := range cookies {
		req2.AddCookie(cookie)
	}
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	assert.Equal(s.T(), http.StatusOK, w2.Code)
	assert.Contains(s.T(), w2.Body.String(), "testuser")
	// Check that Gravatar URL is generated (MD5 of test@example.com)
	assert.Contains(s.T(), w2.Body.String(), "55502f40dc8b7c769880b10874abc9d0")
}

func (s *FactoryTestSuite) TestMultiProvider_RequireAuth_WithoutEmail() {
	gravatarCfg := &config.GravatarConfig{
		Enabled: true,
	}

	mp := &MultiProvider{
		cfg:         &config.AuthConfig{},
		gravatarCfg: gravatarCfg,
	}

	// Create a custom router for this test to properly handle sessions
	router := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	router.Use(sessions.Sessions("mysession", store))

	// First, create a route to set up the session without email
	router.POST("/setup-session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", "test-user-id")
		session.Set("user_email", "") // No email
		session.Set("user_name", "Test User")
		session.Set("user_username", "testuser")
		session.Set("user_is_admin", false)
		session.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	// Then create the protected route
	router.GET("/protected", mp.RequireAuth(), func(c *gin.Context) {
		user := c.MustGet("user").(*models.User)
		c.JSON(http.StatusOK, gin.H{
			"user":        user.Username,
			"gravatarURL": user.GravatarURL,
		})
	})

	// First request to set up session
	req1 := httptest.NewRequest("POST", "/setup-session", nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	// Extract cookies from the first response
	cookies := w1.Result().Cookies()

	// Second request to access protected route with session
	req2 := httptest.NewRequest("GET", "/protected", nil)
	for _, cookie := range cookies {
		req2.AddCookie(cookie)
	}
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	assert.Equal(s.T(), http.StatusOK, w2.Code)
	assert.Contains(s.T(), w2.Body.String(), "testuser")
	// Check that no Gravatar URL is generated when email is empty
	assert.Contains(s.T(), w2.Body.String(), `"gravatarURL":""`)
}

func TestFactoryTestSuite(t *testing.T) {
	suite.Run(t, new(FactoryTestSuite))
}
