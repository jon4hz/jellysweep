package auth

import (
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

type JellyfinProviderTestSuite struct {
	suite.Suite
	provider *JellyfinProvider
	router   *gin.Engine
}

func (s *JellyfinProviderTestSuite) SetupTest() {
	gin.SetMode(gin.TestMode)
	s.router = gin.New()

	// Setup session middleware for tests
	store := cookie.NewStore([]byte("test-secret"))
	s.router.Use(sessions.Sessions("mysession", store))

	jellyfinCfg := &config.JellyfinConfig{
		URL:    "http://localhost:8096",
		APIKey: "test-api-key",
	}
	authCfg := &config.JellyfinAuthConfig{
		Enabled: true,
	}
	s.provider = NewJellyfinProvider(jellyfinCfg, &MockDB{}, authCfg, nil)
}

func (s *JellyfinProviderTestSuite) TestNewJellyfinProvider() {
	jellyfinCfg := &config.JellyfinConfig{
		URL:    "http://localhost:8096",
		APIKey: "test-api-key",
	}
	authCfg := &config.JellyfinAuthConfig{
		Enabled: true,
	}

	provider := NewJellyfinProvider(jellyfinCfg, &MockDB{}, authCfg, nil)

	assert.NotNil(s.T(), provider)
	assert.Equal(s.T(), authCfg, provider.cfg)
	assert.NotNil(s.T(), provider.client)
}

func (s *JellyfinProviderTestSuite) TestLogin_MissingCredentials() {
	s.router.POST("/login", s.provider.Login)

	// Test with missing username
	form := url.Values{}
	form.Add("password", "testpass")

	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusBadRequest, w.Code)
	assert.Contains(s.T(), w.Body.String(), "Username and password are required")
}

func (s *JellyfinProviderTestSuite) TestLogin_EmptyCredentials() {
	s.router.POST("/login", s.provider.Login)

	// Test with empty credentials
	form := url.Values{}
	form.Add("username", "")
	form.Add("password", "")

	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusBadRequest, w.Code)
	assert.Contains(s.T(), w.Body.String(), "Username and password are required")
}

func (s *JellyfinProviderTestSuite) TestLogin_InvalidCredentials() {
	s.router.POST("/login", s.provider.Login)

	// Test with invalid credentials (will fail since no real Jellyfin server)
	form := url.Values{}
	form.Add("username", "testuser")
	form.Add("password", "wrongpass")

	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusUnauthorized, w.Code)
	assert.Contains(s.T(), w.Body.String(), "Invalid credentials")
}

func (s *JellyfinProviderTestSuite) TestCallback() {
	s.router.GET("/callback", s.provider.Callback)

	req := httptest.NewRequest("GET", "/callback", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusNotFound, w.Code)
	assert.Contains(s.T(), w.Body.String(), "Not implemented")
}

func (s *JellyfinProviderTestSuite) TestRequireAuth_NoSession() {
	s.router.GET("/protected", s.provider.RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusFound, w.Code)
	assert.Equal(s.T(), "/login", w.Header().Get("Location"))
}

func (s *JellyfinProviderTestSuite) TestRequireAuth_WithValidSession() {
	// Create a custom router for this test to properly handle sessions
	router := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	router.Use(sessions.Sessions("mysession", store))

	// First, create a route to set up the session
	router.POST("/setup-session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", uint(10))
		session.Set("user_email", "test@example.com")
		session.Set("user_name", "Test User")
		session.Set("user_username", "testuser")
		session.Set("user_is_admin", false)
		session.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	// Then create the protected route
	router.GET("/protected", s.provider.RequireAuth(), func(c *gin.Context) {
		user := c.MustGet("user").(*models.User)
		c.JSON(http.StatusOK, gin.H{"user_id": user.ID})
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
	assert.Contains(s.T(), w2.Body.String(), "10")
}

func (s *JellyfinProviderTestSuite) TestRequireAdmin_NotAdmin() {
	s.router.GET("/admin", s.provider.RequireAdmin(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	// Create a gin context and set a non-admin user
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user", &models.User{
		IsAdmin: false,
	})

	// Manually call the middleware
	middleware := s.provider.RequireAdmin()
	middleware(c)

	assert.True(s.T(), c.IsAborted())
}

func (s *JellyfinProviderTestSuite) TestRequireAdmin_IsAdmin() {
	s.router.GET("/admin", s.provider.RequireAdmin(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	// Create a gin context and set an admin user
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user", &models.User{
		IsAdmin: true,
	})

	// Manually call the middleware and next handler
	middleware := s.provider.RequireAdmin()
	middleware(c)

	// If not aborted, the middleware passed
	assert.False(s.T(), c.IsAborted())
}

func (s *JellyfinProviderTestSuite) TestRequireAdmin_InvalidUser() {
	s.router.GET("/admin", s.provider.RequireAdmin(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	// Create a gin context with invalid user type
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user", "invalid-user-type")

	// Manually call the middleware
	middleware := s.provider.RequireAdmin()
	middleware(c)

	assert.True(s.T(), c.IsAborted())
}

func (s *JellyfinProviderTestSuite) TestGetAuthConfig() {
	jellyfinCfg := &config.JellyfinConfig{
		URL:    "http://localhost:8096",
		APIKey: "test-api-key",
	}
	authCfg := &config.JellyfinAuthConfig{
		Enabled: true,
	}
	provider := NewJellyfinProvider(jellyfinCfg, &MockDB{}, authCfg, nil)

	authConfig := provider.GetAuthConfig()

	assert.NotNil(s.T(), authConfig)
	assert.Equal(s.T(), authCfg, authConfig.Jellyfin)
	assert.Nil(s.T(), authConfig.OIDC)
}

func TestJellyfinProviderTestSuite(t *testing.T) {
	suite.Run(t, new(JellyfinProviderTestSuite))
}
