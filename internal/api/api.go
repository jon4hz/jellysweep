package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/jon4hz/jellysweep/internal/api/auth"
	"github.com/jon4hz/jellysweep/internal/api/handler"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/engine"
	"github.com/jon4hz/jellysweep/internal/static"

	"github.com/jon4hz/jellysweep/frontend/spa"
)

type Server struct {
	cfg          *config.Config
	db           database.DB
	ginEngine    *gin.Engine
	engine       *engine.Engine
	authProvider auth.AuthProvider
}

func New(ctx context.Context, cfg *config.Config, db database.DB, e *engine.Engine, debug bool) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	authProvider, err := auth.NewProvider(ctx, cfg, cfg.Gravatar, db)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth provider: %w", err)
	}

	if !debug {
		gin.SetMode(gin.ReleaseMode)
	}

	ginEngine := gin.Default()
	if cfg.TrustedProxies != nil {
		if err := ginEngine.SetTrustedProxies(cfg.TrustedProxies); err != nil {
			return nil, fmt.Errorf("failed to set trusted proxies: %w", err)
		}
	}

	return &Server{
		cfg:          cfg,
		db:           db,
		ginEngine:    ginEngine,
		authProvider: authProvider,
		engine:       e,
	}, nil
}

func (s *Server) setupSession() {
	store := cookie.NewStore([]byte(s.cfg.SessionKey))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   s.cfg.SessionMaxAge,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	s.ginEngine.Use(sessions.Sessions("jellysweep_session", store))
}

func (s *Server) setupRoutes() error {
	s.setupSession()

	h := handler.New(s.engine, s.cfg)

	staticSub, err := fs.Sub(static.StaticFS, "static")
	if err != nil {
		return fmt.Errorf("failed to create static FS sub: %w", err)
	}
	s.ginEngine.StaticFS("/static", http.FS(staticSub))

	s.ginEngine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Serve robots.txt from root
	s.ginEngine.GET("/robots.txt", func(c *gin.Context) {
		data, err := static.StaticFS.ReadFile("static/robots.txt")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/plain", data)
	})

	// Public API routes (no auth required)
	s.ginEngine.GET("/api/auth/config", h.GetAuthConfig)

	// Auth routes
	s.ginEngine.POST("/auth/jellyfin/login", s.authProvider.Login)
	s.ginEngine.GET("/auth/oidc/callback", s.authProvider.Callback)
	s.ginEngine.GET("/auth/oidc/login", s.authProvider.Login)

	protected := s.ginEngine.Group("/")
	protected.Use(s.authProvider.RequireAuth())

	protected.GET("/logout", h.Logout)

	// API routes
	api := protected.Group("/api")
	api.GET("/me", h.Me)
	api.GET("/media", h.GetMediaItems)
	api.POST("/media/:id/request-keep", h.RequestKeepMedia)

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

	// SPA catch-all for react slop
	if err := spa.SetupSPA(s.ginEngine); err != nil {
		log.Warn("SPA not available (frontend not built?)", "error", err)
	}

	return nil
}

func (s *Server) setupAdminRoutes() {
	adminGroup := s.ginEngine.Group("/admin")
	adminGroup.Use(s.authProvider.RequireAuth(), s.authProvider.RequireAdmin())

	h := handler.NewAdmin(s.engine, s.cfg)

	// Admin API routes
	adminAPI := adminGroup.Group("/api")
	adminAPI.POST("/keep-requests/:id/accept", h.AcceptKeepRequest)
	adminAPI.POST("/keep-requests/:id/decline", h.DeclineKeepRequest)
	adminAPI.POST("/media/:id/keep", h.MarkMediaAsProtected)
	adminAPI.POST("/media/:id/delete", h.MarkMediaAsUnkeepable)
	adminAPI.POST("/media/:id/keep-forever", h.MarkMediaAsKeepForever)

	adminAPI.GET("/keep-requests", h.GetKeepRequests)
	adminAPI.GET("/media", h.GetAdminMediaItems)

	// Scheduler management endpoints
	adminAPI.GET("/scheduler/jobs", h.GetSchedulerJobs)
	adminAPI.POST("/scheduler/jobs/:id/run", h.RunSchedulerJob)
	adminAPI.POST("/scheduler/jobs/:id/enable", h.EnableSchedulerJob)
	adminAPI.POST("/scheduler/jobs/:id/disable", h.DisableSchedulerJob)
	adminAPI.GET("/scheduler/cache/stats", h.GetSchedulerCacheStats)
	adminAPI.POST("/scheduler/cache/clear", h.ClearSchedulerCache)

	// History endpoints
	adminAPI.GET("/history", h.GetHistory)

	// User management endpoints
	adminAPI.GET("/users", h.GetAllUsers)
	adminAPI.PUT("/users/:id/permissions", h.UpdateUserPermissions)
}

func (s *Server) setupPluginRoutes() error {
	if s.cfg.APIKey == "" {
		return fmt.Errorf("API key is required for plugin routes")
	}
	pluginAPI := s.ginEngine.Group("/plugin")

	tokenAuth := auth.NewAPIKeyProvider(s.cfg.APIKey)
	pluginAPI.Use(tokenAuth.RequireAuth())

	h := handler.NewPlugin(s.engine)

	pluginAPI.GET("/health", h.GetHealth)
	pluginAPI.POST("/check", h.CheckMediaItem)

	return nil
}

func (s *Server) Run(ctx context.Context) error {
	s.ginEngine.Use(gin.Recovery())
	s.ginEngine.Use(gzip.Gzip(gzip.DefaultCompression))

	if err := s.setupRoutes(); err != nil {
		return fmt.Errorf("failed to setup routes: %w", err)
	}
	if err := s.setupPluginRoutes(); err != nil {
		log.Warn("Plugin routes not enabled", "error", err)
	}
	s.setupAdminRoutes()

	srv := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.ginEngine,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Shutdown gracefully when context is cancelled
	go func() {
		<-ctx.Done()
		log.Info("shutting down API server...")
		// Use a fresh timeout context since ctx is already cancelled.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("API server forced to shutdown", "error", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("API server error: %w", err)
	}
	return nil
}
