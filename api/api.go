package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/auth"
	"github.com/jon4hz/jellysweep/api/cache"
	"github.com/jon4hz/jellysweep/api/handler"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine"
)

type Server struct {
	cfg          *config.JellysweepConfig
	ginEngine    *gin.Engine
	engine       *engine.Engine
	authProvider auth.AuthProvider
	cacheManager *cache.CacheManager
	imageCache   *cache.ImageCache
}

func New(ctx context.Context, cfg *config.Config, e *engine.Engine) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	authProvider, err := auth.NewProvider(ctx, cfg.JellySweep.Auth)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth provider: %w", err)
	}

	// Create cache manager with 5 minute TTL
	cacheManager := cache.NewCacheManager(5 * time.Minute)

	// Create image cache in ./cache/images directory
	imageCache := cache.NewImageCache("./data/cache/images")

	// Start cleanup goroutine for expired cache entries
	go func() {
		ticker := time.NewTicker(10 * time.Minute) // Cleanup every 10 minutes
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cacheManager.CleanupExpired()
				// Also cleanup old images (older than 7 days)
				if err := imageCache.CleanupOldImages(7 * 24 * time.Hour); err != nil {
					log.Errorf("Failed to cleanup old images: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return &Server{
		cfg:          cfg.JellySweep,
		ginEngine:    gin.Default(),
		authProvider: authProvider,
		engine:       e,
		cacheManager: cacheManager,
		imageCache:   imageCache,
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

	h := handler.New(s.engine, s.cacheManager, s.imageCache, s.authProvider.GetAuthConfig())

	s.ginEngine.Static("/static", "./static") // TODO: make this an embedded file system
	s.ginEngine.GET("/login", h.Login)

	// Auth routes
	s.ginEngine.POST("/auth/jellyfin/login", s.authProvider.Login)
	s.ginEngine.GET("//auth/oidc/callback", s.authProvider.Callback)
	s.ginEngine.GET("/auth/oidc/login", s.authProvider.Login)

	protected := s.ginEngine.Group("/")
	protected.Use(s.authProvider.RequireAuth())

	protected.GET("/", h.Home)
	protected.GET("/logout", h.Logout)

	// API routes
	api := protected.Group("/api")
	api.POST("/media/:id/request-keep", h.RequestKeepMedia)
	api.POST("/refresh", h.RefreshData)

	// Image cache route
	api.GET("/images/cache", h.ImageCache)
}

func (s *Server) setupAdminRoutes() {
	adminGroup := s.ginEngine.Group("/admin")
	adminGroup.Use(s.authProvider.RequireAuth(), s.authProvider.RequireAdmin())

	h := handler.NewAdmin(s.engine)

	// Admin panel page
	adminGroup.GET("", h.AdminPanel)
	adminGroup.GET("/", h.AdminPanel)

	// Admin API routes
	adminAPI := adminGroup.Group("/api")
	adminAPI.POST("/keep-requests/:id/accept", h.AcceptKeepRequest)
	adminAPI.POST("/keep-requests/:id/decline", h.DeclineKeepRequest)
}

func (s *Server) Run() error {
	s.setupRoutes()
	s.setupAdminRoutes()

	return s.ginEngine.Run(s.cfg.Listen)
}
