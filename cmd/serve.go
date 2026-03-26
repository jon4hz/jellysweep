package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

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
		log.Fatal("failed to load config", "error", err)
	}

	// Explicit --log-level flag overrides config.
	if !cmd.Flags().Changed("log-level") {
		setLogLevel(cfg.LogLevel)
	}

	exists, err := dbFileExists(cfg.Database.Path)
	if err != nil {
		log.Fatal("failed to check database file", "error", err)
	}

	db, err := database.New(cfg.Database.Path)
	if err != nil {
		log.Fatal("failed to initialize database", "error", err)
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	engine, err := engine.New(cfg, db, !exists)
	if err != nil {
		log.Fatal("failed to create engine", "error", err)
	}

	server, err := api.New(ctx, cfg, db, engine, log.GetLevel() == log.DebugLevel)
	if err != nil {
		log.Fatal("failed to create API server", "error", err)
	}

	// Start the engine in a goroutine
	go func() {
		if err := engine.Run(ctx); err != nil {
			log.Error("engine error", "error", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Start the API server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		log.Info("starting API server", "listen", cfg.Listen)
		serverErr <- server.Run(ctx)
	}()

	log.Info("jellysweep started successfully")

	// Wait for signal or server error
	select {
	case <-c:
		log.Info("shutting down gracefully...")
		cancel()
	case err := <-serverErr:
		if err != nil {
			log.Error("API server error", "error", err)
		}
		cancel()
	}

	// Wait for the server to finish shutting down
	if err := <-serverErr; err != nil {
		log.Error("API server shutdown error", "error", err)
	}

	if err := engine.Close(); err != nil {
		log.Error("engine shutdown error", "error", err)
	}
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
