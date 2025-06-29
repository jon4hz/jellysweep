package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/api"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(resetCmd)
}

var rootCmd = &cobra.Command{
	Use:     "jellysweep",
	Short:   "JellySweep is a tool to manage media libraries with automatic deletion and user requests",
	Long:    `JellySweep helps you manage your media libraries by automatically deleting items that are no longer wanted, while allowing users to request to keep certain items.`,
	Example: `jellysweep --config config.yml`,
	CompletionOptions: cobra.CompletionOptions{
		HiddenDefaultCmd: true,
	},

	Run: root,
}

func root(cmd *cobra.Command, _ []string) {
	cfg, err := config.Load("config.yml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	setLogLevel(cfg.JellySweep.LogLevel)

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	engine, err := engine.New(cfg)
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}

	server, err := api.New(ctx, cfg, engine)
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
		log.Info("starting API server", "listen", cfg.JellySweep.Listen)
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

func setLogLevel(level string) {
	switch level {
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "warn":
		log.SetLevel(log.WarnLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	default:
		log.Warnf("unknown log level %s, defaulting to info", level)
		log.SetLevel(log.InfoLevel)
	}
}

func Execute() error {
	return rootCmd.Execute()
}
