package api

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/jon4hz/jellysweep/api/auth"
	"github.com/jon4hz/jellysweep/api/handler"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/database"
	"github.com/jon4hz/jellysweep/engine"
	"github.com/jon4hz/jellysweep/static"
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

	return &Server{
		cfg:          cfg,
		db:           db,
		ginEngine:    gin.Default(),
		authProvider: authProvider,
		engine:       e,
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

	h := handler.New(s.engine, s.db, s.cfg)

	staticSub, err := fs.Sub(static.StaticFS, "static")
	if err != nil {
		return fmt.Errorf("failed to create static FS sub: %w", err)
	}
	s.ginEngine.StaticFS("/static", http.FS(staticSub))

	// Serve robots.txt from root
	s.ginEngine.GET("/robots.txt", func(c *gin.Context) {
		data, err := static.StaticFS.ReadFile("static/robots.txt")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/plain", data)
	})

	s.ginEngine.GET("/login", h.Login)

	// Auth routes
	s.ginEngine.POST("/auth/jellyfin/login", s.authProvider.Login)
	s.ginEngine.GET("/auth/oidc/callback", s.authProvider.Callback)
	s.ginEngine.GET("/auth/oidc/login", s.authProvider.Login)

	protected := s.ginEngine.Group("/")
	protected.Use(s.authProvider.RequireAuth())

	protected.GET("/", h.Home)
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

	return nil
}

func (s *Server) setupAdminRoutes() {
	adminGroup := s.ginEngine.Group("/admin")
	adminGroup.Use(s.authProvider.RequireAuth(), s.authProvider.RequireAdmin())

	h := handler.NewAdmin(s.engine, s.db, s.cfg)

	// Admin panel page
	adminGroup.GET("", h.AdminPanel)
	adminGroup.GET("/", h.AdminPanel)

	// Scheduler panel page
	adminGroup.GET("/scheduler", h.SchedulerPanel)

	// History panel page
	adminGroup.GET("/history", h.HistoryPanel)

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
	adminAPI.GET("/history", h.GetDeletedMedia)
}

func (s *Server) setupPluginRoutes() error {
	if s.cfg.APIKey == "" {
		return fmt.Errorf("API key is required for plugin routes")
	}
	pluginAPI := s.ginEngine.Group("/plugin")

	tokenAuth := auth.NewAPIKeyProvider(s.cfg.APIKey)
	pluginAPI.Use(tokenAuth.RequireAuth())

	h := handler.NewPlugin(s.db)

	pluginAPI.GET("/health", h.GetHealth)
	pluginAPI.POST("/check", h.CheckMediaItem)

	return nil
}

func (s *Server) Run() error {
	s.ginEngine.Use(gin.Recovery())
	s.ginEngine.Use(gzip.Gzip(gzip.DefaultCompression))

	if err := s.setupRoutes(); err != nil {
		return fmt.Errorf("failed to setup routes: %w", err)
	}
	if err := s.setupPluginRoutes(); err != nil {
		log.Warn("Plugin routes not enabled", "error", err)
	}
	s.setupAdminRoutes()

	return s.ginEngine.Run(s.cfg.Listen)
}
