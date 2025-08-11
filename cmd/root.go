package cmd

import (
	"io"
	"os"

	"github.com/charmbracelet/log"
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
		setLogLevel(rootCmdPersistentFlags.LogLevel)
		logToFile()
	},
	RunE: root,
}

func root(cmd *cobra.Command, _ []string) error {
	return cmd.Help()
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

func logToFile() {
	if rootCmdPersistentFlags.LogFile == "" {
		log.Info("no log file specified, logging to console only")
		return
	}
	file, err := os.OpenFile(rootCmdPersistentFlags.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec
	if err != nil {
		log.Errorf("failed to open log file: %v", err)
		return
	}

	// Create a multi-writer that writes to both console and file
	multiWriter := io.MultiWriter(os.Stdout, file)
	log.SetOutput(multiWriter)
	log.Info("logging to both console and file", "file", rootCmdPersistentFlags.LogFile)
}

func Execute() error {
	return rootCmd.Execute()
}
