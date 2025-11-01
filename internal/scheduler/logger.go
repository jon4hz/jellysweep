package scheduler

import "github.com/charmbracelet/log"

type logger struct {
	log *log.Logger
}

func newLogger() *logger {
	return &logger{
		log: log.Default().WithPrefix("scheduler"),
	}
}

func (l *logger) Debug(msg string, args ...any) {
	l.log.Debug(msg, args...)
}

func (l *logger) Error(msg string, args ...any) {
	l.log.Error(msg, args...)
}

func (l *logger) Info(msg string, args ...any) {
	l.log.Info(msg, args...)
}

func (l *logger) Warn(msg string, args ...any) {
	l.log.Warn(msg, args...)
}
