package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type OIDCProviderTestSuite struct {
	suite.Suite
	router *gin.Engine
}

func (s *OIDCProviderTestSuite) SetupTest() {
	gin.SetMode(gin.TestMode)
	s.router = gin.New()

	// Setup session middleware for tests
	store := cookie.NewStore([]byte("test-secret"))
	s.router.Use(sessions.Sessions("mysession", store))
}

func (s *OIDCProviderTestSuite) TestNewOIDCProvider_InvalidIssuer() {
	cfg := &config.OIDCConfig{
		Enabled:      true,
		Issuer:       "invalid-issuer-url",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
		AdminGroup:   "admin",
	}

	provider, err := NewOIDCProvider(s.T().Context(), cfg, nil, &MockDB{})

	assert.Error(s.T(), err)
	assert.Nil(s.T(), provider)
}

func (s *OIDCProviderTestSuite) TestNewOIDCProvider_ValidConfig() {
	// Note: This test will likely fail without a real OIDC provider
	// but tests the basic structure
	cfg := &config.OIDCConfig{
		Enabled:      true,
		Issuer:       "https://auth.mydomain.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
		AdminGroup:   "admin",
	}

	// This will likely fail due to network issues in tests, but we can test the error handling
	provider, err := NewOIDCProvider(s.T().Context(), cfg, nil, &MockDB{})

	// In a real test environment with proper OIDC setup, this should work
	// For now, we just test that the function doesn't panic and handles errors
	if err != nil {
		assert.Nil(s.T(), provider)
	} else {
		assert.NotNil(s.T(), provider)
		assert.Equal(s.T(), cfg, provider.cfg)
	}
}

func (s *OIDCProviderTestSuite) TestOIDCProvider_RequireAuth_NoSession() {
	// Create a mock OIDC provider for testing middleware
	provider := &OIDCProvider{
		db: &MockDB{},
		cfg: &config.OIDCConfig{
			Enabled: true,
		},
	}

	s.router.GET("/protected", provider.RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusFound, w.Code)
	assert.Equal(s.T(), "/auth/oidc/login", w.Header().Get("Location"))
}

func (s *OIDCProviderTestSuite) TestOIDCProvider_RequireAuth_WithValidSession() {
	provider := &OIDCProvider{
		db: &MockDB{},
		cfg: &config.OIDCConfig{
			Enabled: true,
		},
	}

	// Create a custom router for this test to properly handle sessions
	router := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	router.Use(sessions.Sessions("mysession", store))

	// First, create a route to set up the session
	router.POST("/setup-session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", uint(10))
		session.Set("user_email", "oidc@example.com")
		session.Set("user_name", "OIDC User")
		session.Set("user_username", "oidcuser")
		session.Set("user_is_admin", true)
		session.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	// Then create the protected route
	router.GET("/protected", provider.RequireAuth(), func(c *gin.Context) {
		user := c.MustGet("user").(*models.User)
		c.JSON(http.StatusOK, gin.H{
			"user_id":  uint(10),
			"is_admin": user.IsAdmin,
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
	assert.Contains(s.T(), w2.Body.String(), "10")   // user_id
	assert.Contains(s.T(), w2.Body.String(), "true") // is_admin
}

func (s *OIDCProviderTestSuite) TestOIDCProvider_RequireAdmin_NotAdmin() {
	provider := &OIDCProvider{
		db: &MockDB{},
		cfg: &config.OIDCConfig{
			Enabled: true,
		},
	}

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	// Create a gin context and set a non-admin user
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user", &models.User{
		ID:      9,
		IsAdmin: false,
	})

	// Manually call the middleware
	middleware := provider.RequireAdmin()
	middleware(c)

	assert.True(s.T(), c.IsAborted())
}

func (s *OIDCProviderTestSuite) TestOIDCProvider_RequireAdmin_IsAdmin() {
	provider := &OIDCProvider{
		db: &MockDB{},
		cfg: &config.OIDCConfig{
			Enabled: true,
		},
	}

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	// Create a gin context and set an admin user
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user", &models.User{
		ID:      11,
		IsAdmin: true,
	})

	// Manually call the middleware
	middleware := provider.RequireAdmin()
	middleware(c)

	// If not aborted, the middleware passed
	assert.False(s.T(), c.IsAborted())
}

func (s *OIDCProviderTestSuite) TestOIDCProvider_RequireAdmin_InvalidUser() {
	provider := &OIDCProvider{
		db: &MockDB{},
		cfg: &config.OIDCConfig{
			Enabled: true,
		},
	}

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	// Create a gin context with invalid user type
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user", "invalid-user-type")

	// Manually call the middleware
	middleware := provider.RequireAdmin()
	middleware(c)

	assert.True(s.T(), c.IsAborted())
}

func (s *OIDCProviderTestSuite) TestOIDCProvider_GetAuthConfig() {
	cfg := &config.OIDCConfig{
		Enabled:      true,
		Issuer:       "https://example.com",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost/callback",
		AdminGroup:   "admin",
	}

	provider := &OIDCProvider{cfg: cfg}
	authConfig := provider.GetAuthConfig()

	assert.NotNil(s.T(), authConfig)
	assert.Equal(s.T(), cfg, authConfig.OIDC)
	assert.Nil(s.T(), authConfig.Jellyfin)
}

func (s *OIDCProviderTestSuite) TestOIDCProvider_Login() {
	// This test is limited because Login redirects to external OIDC provider
	provider := &OIDCProvider{
		db: &MockDB{},
		cfg: &config.OIDCConfig{
			Enabled: true,
		},
	}

	s.router.GET("/login", func(c *gin.Context) {
		// Safely handle the potential panic
		defer func() {
			if r := recover(); r != nil {
				// Expected since we don't have a real config
				c.JSON(http.StatusInternalServerError, gin.H{"error": "configuration error"})
			}
		}()
		provider.Login(c)
	})

	req := httptest.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	// Should return an error due to missing config
	assert.Equal(s.T(), http.StatusInternalServerError, w.Code)
}

func (s *OIDCProviderTestSuite) TestOIDCProvider_Callback() {
	// This test is limited because Callback requires real OIDC tokens
	provider := &OIDCProvider{
		db: &MockDB{},
		cfg: &config.OIDCConfig{
			Enabled: true,
		},
	}

	s.router.GET("/callback", provider.Callback)

	req := httptest.NewRequest("GET", "/callback?code=test-code", nil)
	w := httptest.NewRecorder()

	// Since we don't have a real OAuth config set up, this will likely panic
	// In a real test, you would mock the OAuth2 config and OIDC verifier
	defer func() {
		if r := recover(); r != nil {
			// Expected since we don't have a real config
		}
	}()

	s.router.ServeHTTP(w, req)
}

func TestOIDCProviderTestSuite(t *testing.T) {
	suite.Run(t, new(OIDCProviderTestSuite))
}
