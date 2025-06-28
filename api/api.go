package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/auth"
	"github.com/jon4hz/jellysweep/api/handler"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine"
)

type Server struct {
	cfg          *config.JellysweepConfig
	ginEngine    *gin.Engine
	engine       *engine.Engine
	authProvider *auth.Provider
}

func New(ctx context.Context, cfg *config.Config, e *engine.Engine) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	// TODO: make oidc optional and add jellyfin auth
	authProvider, err := auth.New(ctx, cfg.Jellysweep.Auth.OIDC)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth provider: %w", err)
	}
	return &Server{
		cfg:          cfg.Jellysweep,
		ginEngine:    gin.Default(),
		authProvider: authProvider,
		engine:       e,
	}, nil
}

func (s *Server) setupSession() {
	store := cookie.NewStore([]byte(s.cfg.SessionKey))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
		Secure:   false, // Set to true in production
		SameSite: http.SameSiteLaxMode,
	})
	s.ginEngine.Use(sessions.Sessions("jellysweep_session", store))
}

func (s *Server) setupRoutes() {
	s.setupSession()

	h := handler.New()

	s.ginEngine.Static("/static", "./static")
	s.ginEngine.GET("/login", h.Login)
	s.ginEngine.GET("/oauth/login", s.authProvider.Login)
	s.ginEngine.GET("/oauth/callback", s.authProvider.Callback)

	protected := s.ginEngine.Group("/")
	protected.Use(s.authProvider.RequireAuth())

	protected.GET("/", h.Home)
	protected.GET("/logout", h.Logout)

	// API routes
	api := protected.Group("/api")
	api.POST("/media/:id/request-keep", h.RequestKeepMedia)
}

func (s *Server) setupAdminRoutes() {
	adminGroup := s.ginEngine.Group("/admin")
	h := handler.NewAdmin()
	adminGroup.Use(s.authProvider.RequireAuth(), s.authProvider.RequireAdmin())

	_ = h

}

func (s *Server) Run() error {
	s.setupRoutes()
	s.setupAdminRoutes()

	return s.ginEngine.Run(s.cfg.Listen)
}
