package cmd

import (
	"context"
	"io"
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

var rootCmdPersistentFlags struct {
	LogFile    string
	ConfigFile string
	LogLevel   string
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rootCmdPersistentFlags.LogFile, "log-file", "", "File to write logs to")
	rootCmd.PersistentFlags().StringVarP(&rootCmdPersistentFlags.ConfigFile, "config", "c", "", "Path to config file (default: search for config.yml in current dir, ~/.jellysweep, /etc/jellysweep)")
	rootCmd.PersistentFlags().StringVar(&rootCmdPersistentFlags.LogLevel, "log-level", "", "Log level (debug, info, warn, error) - overrides config file setting")
}

var rootCmd = &cobra.Command{
	Use:   "jellysweep",
	Short: "JellySweep is a tool to manage media libraries with automatic deletion and user requests",
	Long:  `JellySweep helps you manage your media libraries by automatically deleting items that are no longer wanted, while allowing users to request to keep certain items.`,
	Example: `jellysweep --config config.yml
  jellysweep -c /path/to/config.yml --log-level debug
  jellysweep --log-level info  # searches for config in default locations`,
	CompletionOptions: cobra.CompletionOptions{
		HiddenDefaultCmd: true,
	},
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		setLogLevel(rootCmdPersistentFlags.LogLevel)
		logToFile()
	},
	Run: root,
}

func root(cmd *cobra.Command, _ []string) {
	cfg, err := config.Load(rootCmdPersistentFlags.ConfigFile)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	engine, err := engine.New(cfg)
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}

	server, err := api.New(ctx, cfg, engine, log.GetLevel() == log.DebugLevel)
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
