package main

import (
	"context"

	"github.com/charmbracelet/log"

	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine"
)

func main() {

	log.SetLevel(log.DebugLevel)

	cfg, err := config.Load("config.yml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	engine, err := engine.New(cfg)
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}

	if err := engine.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
	log.Info("jellysweep started successfully")
}
