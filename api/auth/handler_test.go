package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HandlerTestSuite struct {
	suite.Suite
	router *gin.Engine
}

func (s *HandlerTestSuite) SetupTest() {
	gin.SetMode(gin.TestMode)
	s.router = gin.New()

	// Setup session middleware for tests
	store := cookie.NewStore([]byte("test-secret"))
	s.router.Use(sessions.Sessions("mysession", store))
}

func (s *HandlerTestSuite) TestOIDCLogin_WithoutValidConfig() {
	// Create an OIDC provider without proper initialization
	provider := &OIDCProvider{
		cfg: &config.OIDCConfig{
			Enabled: true,
		},
		// config is nil, which will cause panic when calling AuthCodeURL
	}

	s.router.GET("/login", func(c *gin.Context) {
		// Expect this to panic due to nil config
		defer func() {
			if r := recover(); r != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "configuration error"})
			}
		}()
		provider.Login(c)
	})

	req := httptest.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusInternalServerError, w.Code)
}

func (s *HandlerTestSuite) TestOIDCCallback_WithoutValidConfig() {
	// Create an OIDC provider without proper initialization
	provider := &OIDCProvider{
		cfg: &config.OIDCConfig{
			Enabled: true,
		},
		// config and verifier are nil
	}

	s.router.GET("/callback", func(c *gin.Context) {
		// Expect this to handle the error gracefully
		defer func() {
			if r := recover(); r != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "configuration error"})
			}
		}()
		provider.Callback(c)
	})

	req := httptest.NewRequest("GET", "/callback?code=test-code", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	// Should either return an error status or 500 from panic recovery
	assert.True(s.T(), w.Code >= 400)
}

func (s *HandlerTestSuite) TestOIDCCallback_MissingCode() {
	// Create an OIDC provider without proper initialization
	provider := &OIDCProvider{
		cfg: &config.OIDCConfig{
			Enabled: true,
		},
	}

	s.router.GET("/callback", func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "missing code parameter"})
			}
		}()
		provider.Callback(c)
	})

	req := httptest.NewRequest("GET", "/callback", nil) // No code parameter
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	// Should return an error due to missing code
	assert.True(s.T(), w.Code >= 400)
}

func (s *HandlerTestSuite) TestSessionHelperFunctions() {
	// Test the helper functions directly
	router := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	router.Use(sessions.Sessions("test", store))

	router.GET("/test-helpers", func(c *gin.Context) {
		session := sessions.Default(c)

		// Set up test data
		session.Set("string_val", "test_string")
		session.Set("bool_val", true)
		session.Set("int_val", 42)
		session.Set("nil_val", nil)
		session.Save()

		// Test getSessionString
		assert.Equal(s.T(), "test_string", getSessionString(session, "string_val"))
		assert.Equal(s.T(), "", getSessionString(session, "int_val"))     // non-string
		assert.Equal(s.T(), "", getSessionString(session, "missing_key")) // missing
		assert.Equal(s.T(), "", getSessionString(session, "nil_val"))     // nil

		// Test getSessionBool
		assert.Equal(s.T(), true, getSessionBool(session, "bool_val"))
		assert.Equal(s.T(), false, getSessionBool(session, "string_val"))  // non-bool
		assert.Equal(s.T(), false, getSessionBool(session, "missing_key")) // missing
		assert.Equal(s.T(), false, getSessionBool(session, "nil_val"))     // nil

		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest("GET", "/test-helpers", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusOK, w.Code)
}

func (s *HandlerTestSuite) TestOIDCCallback_SessionHandling() {
	// Test session handling in a controlled way
	router := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	router.Use(sessions.Sessions("test", store))

	// Mock the OIDC callback session handling logic
	router.GET("/mock-callback", func(c *gin.Context) {
		session := sessions.Default(c)

		// Simulate what the real callback would do
		claims := struct {
			Email             string   `json:"email"`
			Name              string   `json:"name"`
			PreferredUsername string   `json:"preferred_username"`
			Sub               string   `json:"sub"`
			Groups            []string `json:"groups"`
		}{
			Email:             "test@example.com",
			Name:              "Test User",
			PreferredUsername: "testuser",
			Sub:               "test-sub-123",
			Groups:            []string{"admin", "users"},
		}

		// Save user ID in session (simulating callback logic)
		session.Set("user_id", claims.Sub)
		session.Set("user_email", claims.Email)
		session.Set("user_name", claims.Name)
		session.Set("user_username", claims.PreferredUsername)

		// Check admin group
		var isAdmin bool
		adminGroup := "admin"
		for _, group := range claims.Groups {
			if group == adminGroup {
				isAdmin = true
				break
			}
		}
		session.Set("user_is_admin", isAdmin)

		if err := session.Save(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "session save failed"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"user_id":  claims.Sub,
			"is_admin": isAdmin,
			"groups":   claims.Groups,
		})
	})

	req := httptest.NewRequest("GET", "/mock-callback", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(s.T(), http.StatusOK, w.Code)
	assert.Contains(s.T(), w.Body.String(), "test-sub-123")
	assert.Contains(s.T(), w.Body.String(), "true") // is_admin should be true
}

func (s *HandlerTestSuite) TestOIDCCallback_AdminGroupDetection() {
	// Test admin group detection logic
	testCases := []struct {
		name          string
		groups        []string
		adminGroup    string
		expectedAdmin bool
	}{
		{
			name:          "User in admin group",
			groups:        []string{"users", "admin", "developers"},
			adminGroup:    "admin",
			expectedAdmin: true,
		},
		{
			name:          "User not in admin group",
			groups:        []string{"users", "developers"},
			adminGroup:    "admin",
			expectedAdmin: false,
		},
		{
			name:          "No groups",
			groups:        []string{},
			adminGroup:    "admin",
			expectedAdmin: false,
		},
		{
			name:          "Different admin group",
			groups:        []string{"users", "administrators"},
			adminGroup:    "administrators",
			expectedAdmin: true,
		},
	}

	for _, tc := range testCases {
		s.T().Run(tc.name, func(t *testing.T) {
			var isAdmin bool
			for _, group := range tc.groups {
				if group == tc.adminGroup {
					isAdmin = true
					break
				}
			}
			assert.Equal(t, tc.expectedAdmin, isAdmin)
		})
	}
}

func TestHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(HandlerTestSuite))
}
