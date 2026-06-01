package logging

import (
	"io"
	"os"

	"github.com/charmbracelet/log"
)

var currentLogFile *os.File

// SetLevel sets the global log level. Valid values: debug, info, warn, error.
// Defaults to info.
func SetLevel(level string) {
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
		log.Warn("unknown log level, defaulting to info", "level", level)
		log.SetLevel(log.InfoLevel)
	}
}

// SetOutputFile sets logs destination file. If path is non-empty, logs to
// both stdout and file; otherwise stdout only.
func SetOutputFile(path string) {
	log.SetOutput(os.Stdout)
	if currentLogFile != nil {
		if err := currentLogFile.Close(); err != nil {
			log.Error("failed to close previous log file", "error", err)
		}
		currentLogFile = nil
	}
	if path == "" {
		log.Info("no log file specified, logging to console only")
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec
	if err != nil {
		log.Error("failed to open log file", "error", err)
		return
	}
	currentLogFile = file
	log.SetOutput(io.MultiWriter(os.Stdout, file))
	log.Info("logging to both console and file", "file", path)
}
