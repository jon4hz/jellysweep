package main

import (
	"context"

	"github.com/charmbracelet/log"

	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine"
)

func main() {
	cfg, err := config.Load("config.yml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	setLogLevel(cfg.Jellysweep.LogLevel)

	engine, err := engine.New(cfg)
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}

	if err := engine.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
	log.Info("jellysweep started successfully")
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
