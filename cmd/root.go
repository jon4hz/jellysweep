package cmd

import (
	"context"

	"github.com/charmbracelet/fang"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/logging"
	"github.com/spf13/cobra"
)

var rootCmdPersistentFlags struct {
	LogFile    string
	ConfigFile string
	LogLevel   string
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rootCmdPersistentFlags.LogFile, "log-file", "", "File to write logs to")
	rootCmd.PersistentFlags().StringVarP(&rootCmdPersistentFlags.ConfigFile, "config", "c", "", "Path to config file (default: search for config.yml in current dir, ~/.jellysweep, /etc/jellysweep)")
	rootCmd.PersistentFlags().StringVar(&rootCmdPersistentFlags.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	_ = config.BindPFlag("log_level", rootCmd.PersistentFlags().Lookup("log-level"))
}

var rootCmd = &cobra.Command{
	Use:   "jellysweep",
	Short: "Jellysweep is a tool to manage media libraries with automatic deletion and user requests",
	Long:  `Jellysweep helps you manage your media libraries by automatically deleting items that are no longer wanted, while allowing users to request to keep certain items.`,
	Example: `jellysweep serve --config config.yml
  jellysweep serve -c /path/to/config.yml --log-level debug
  jellysweep serve --log-level info  # searches for config in default locations`,
	CompletionOptions: cobra.CompletionOptions{
		HiddenDefaultCmd: true,
	},
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		logging.SetLevel(rootCmdPersistentFlags.LogLevel)
		logging.SetOutputFile(rootCmdPersistentFlags.LogFile)
	},
	RunE: root,
}

func root(cmd *cobra.Command, _ []string) error {
	return cmd.Help()
}

func Execute() error {
	return fang.Execute(context.Background(), rootCmd)
}
