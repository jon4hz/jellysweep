package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/api"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/engine"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Jellysweep server",
	Long:  `Start the Jellysweep server to handle media management requests and automatic deletions.`,
	Example: `jellysweep serve --config config.yml
jellysweep serve -c /path/to/config.yml --log-level debug
`,
	Run: startServer,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func startServer(cmd *cobra.Command, _ []string) {
	cfg, err := config.Load(rootCmdPersistentFlags.ConfigFile)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	exists, err := dbFileExists(cfg.Database.Path)
	if err != nil {
		log.Fatalf("failed to check database file: %v", err)
	}

	db, err := database.New(cfg.Database.Path)
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	engine, err := engine.New(cfg, db, !exists)
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}

	server, err := api.New(ctx, cfg, db, engine, log.GetLevel() == log.DebugLevel)
	if err != nil {
		log.Fatalf("failed to create API server: %v", err)
	}

	// Start the engine in a goroutine
	go func() {
		if err := engine.Run(ctx); err != nil {
			log.Error("engine error", "error", err)
		}
	}()

	// Start the API server in a goroutine
	go func() {
		log.Info("starting API server", "listen", cfg.Listen)
		if err := server.Run(); err != nil {
			log.Error("API server error", "error", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	log.Info("jellysweep started successfully")
	<-c
	log.Info("shutting down gracefully...")

	// Give time for graceful shutdown
	cancel()
	time.Sleep(2 * time.Second)
}

func dbFileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
