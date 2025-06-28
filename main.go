package main

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
)

func main() {
	cfg, err := config.Load("config.yml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	setLogLevel(cfg.Jellysweep.LogLevel)

	ctx, cancel := context.WithCancel(context.Background())
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
		log.Info("starting API server", "listen", cfg.Jellysweep.Listen)
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
