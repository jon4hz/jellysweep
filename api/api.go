package api

import (
	"context"
	"fmt"
	"io/fs"
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
	"github.com/jon4hz/jellysweep/static"
)

type Server struct {
	cfg          *config.Config
	ginEngine    *gin.Engine
	engine       *engine.Engine
	authProvider auth.AuthProvider
	cacheManager *cache.CacheManager
	imageCache   *cache.ImageCache
}

func New(ctx context.Context, cfg *config.Config, e *engine.Engine, debug bool) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	var authProvider auth.AuthProvider
	var err error

	// Only initialize auth provider if authentication is enabled
	if cfg.IsAuthenticationEnabled() {
		authProvider, err = auth.NewProvider(ctx, cfg.Auth, cfg.Gravatar)
		if err != nil {
			return nil, fmt.Errorf("failed to create auth provider: %w", err)
		}
	} else {
		// Create a no-op auth provider if auth is disabled
		authProvider = auth.NewNoOpProvider()
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

	if !debug {
		gin.SetMode(gin.ReleaseMode)
	}

	return &Server{
		cfg:          cfg,
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
		Path:   "/",
		MaxAge: s.cfg.SessionMaxAge,
		// TODO: make this secure in production
		HttpOnly: true,
		Secure:   false, // Set to true in production
		SameSite: http.SameSiteLaxMode,
	})
	s.ginEngine.Use(sessions.Sessions("jellysweep_session", store))
}

func (s *Server) setupRoutes() error {
	s.setupSession()

	h := handler.New(s.engine, s.cacheManager, s.imageCache, s.authProvider.GetAuthConfig())

	staticSub, err := fs.Sub(static.StaticFS, "static")
	if err != nil {
		return fmt.Errorf("failed to create static FS sub: %w", err)
	}
	s.ginEngine.StaticFS("/static", http.FS(staticSub))

	// Setup routes depending on authentication status
	if s.cfg.IsAuthenticationEnabled() {
		s.ginEngine.GET("/login", h.Login)

		// Auth routes
		s.ginEngine.POST("/auth/jellyfin/login", s.authProvider.Login)
		s.ginEngine.GET("/auth/oidc/callback", s.authProvider.Callback)
		s.ginEngine.GET("/auth/oidc/login", s.authProvider.Login)
	}

	protected := s.ginEngine.Group("/")
	protected.Use(s.authProvider.RequireAuth())

	protected.GET("/", h.Home)
	protected.GET("/logout", h.Logout)

	// API routes
	api := protected.Group("/api")
	api.GET("/me", h.Me)
	api.POST("/media/:id/request-keep", h.RequestKeepMedia)
	api.POST("/refresh", h.RefreshData)

	// Image cache route
	api.GET("/images/cache", h.ImageCache)

	// WebPush routes
	if s.cfg.WebPush != nil && s.cfg.WebPush.Enabled {
		webpushHandler := handler.NewWebPushHandler(s.engine.GetWebPushClient())
		api.GET("/webpush/vapid-key", webpushHandler.GetVAPIDKey)
		api.GET("/webpush/status", webpushHandler.GetSubscriptionStatus)
		api.POST("/webpush/subscribe", webpushHandler.Subscribe)
		api.POST("/webpush/unsubscribe", webpushHandler.UnsubscribeByEndpoint)
	}

	return nil
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
	adminAPI.POST("/media/:id/keep", h.MarkMediaAsKeep)
	adminAPI.POST("/media/:id/delete", h.MarkMediaForDeletion)
	adminAPI.POST("/media/:id/keep-forever", h.MarkMediaAsKeepForever)
}

func (s *Server) Run() error {
	s.ginEngine.Use(gin.Recovery())

	if err := s.setupRoutes(); err != nil {
		return fmt.Errorf("failed to setup routes: %w", err)
	}
	s.setupAdminRoutes()

	return s.ginEngine.Run(s.cfg.Listen)
}
