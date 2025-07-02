package cmd

import (
	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine"
	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset all tags in sonarr and radarr",
	Long:  `This command resets all jellysweep tags in Sonarr and Radarr, removing any custom tags that were added by JellySweep.`,
	Run:   reset,
}

func reset(cmd *cobra.Command, _ []string) {
	cfg, err := config.Load("config.yml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	setLogLevel(cfg.JellySweep.LogLevel)

	engine, err := engine.New(cfg)
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}
	defer engine.Close() //nolint:errcheck

	log.Info("Starting reset of all jellysweep tags...")

	if err := engine.ResetAllTags(cmd.Context()); err != nil {
		log.Fatalf("failed to reset tags: %v", err)
	}

	log.Info("Successfully reset all jellysweep tags!")
}
