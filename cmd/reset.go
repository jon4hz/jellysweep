package cmd

import (
	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/database"
	"github.com/jon4hz/jellysweep/engine"
	"github.com/spf13/cobra"
)

var resetCmdFlags struct {
	IncludeIgnore bool
	IncludeTags   []string
}

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset all tags in sonarr and radarr",
	Long:  `This command resets all jellysweep tags in Sonarr and Radarr, removing any custom tags that were added by Jellysweep.`,
	Run:   reset,
}

func init() {
	resetCmd.Flags().BoolVar(&resetCmdFlags.IncludeIgnore, "include-ignore", false, "Also reset jellysweep-ignore tags")
	resetCmd.Flags().StringSliceVar(&resetCmdFlags.IncludeTags, "include-tags", nil, "Additional tags to include in the reset (e.g., my-custom-tag1,my-custom-tag2)")

	rootCmd.AddCommand(resetCmd)
}

func reset(cmd *cobra.Command, _ []string) {
	cfg, err := config.Load(rootCmdPersistentFlags.ConfigFile)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := database.New(cfg.Database.Path)
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}

	engine, err := engine.New(cfg, db, false)
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}
	defer engine.Close() //nolint:errcheck

	log.Info("Starting reset of all jellysweep tags...")

	additionalTags := make([]string, 0, len(resetCmdFlags.IncludeTags))
	additionalTags = append(additionalTags, resetCmdFlags.IncludeTags...)
	if resetCmdFlags.IncludeIgnore {
		additionalTags = append(additionalTags, "jellysweep-ignore")
	}

	if err := engine.ResetAllTags(cmd.Context(), additionalTags); err != nil {
		log.Fatalf("failed to reset tags: %v", err)
	}

	log.Info("Successfully reset all jellysweep tags!")
}
