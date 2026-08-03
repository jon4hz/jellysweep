package engine

// Test doubles for the engine package.
//
// Conventions: dependencies are faked with small stateful hand-written fakes
// (no mock codegen), the database is a real in-memory sqlite via
// internal/database/databasetest. Lifecycle scenarios live in
// lifecycle_test.go; add a scenario there when changing the cleanup pipeline.

import (
	"context"
	"sync"
	"time"
)

// fakeStats implements stats.Statser.
type fakeStats struct {
	mu         sync.Mutex
	lastPlayed map[string]time.Time // jellyfinID -> last played; absent = never
	err        map[string]error     // jellyfinID -> error to return
}

func newFakeStats() *fakeStats {
	return &fakeStats{
		lastPlayed: make(map[string]time.Time),
		err:        make(map[string]error),
	}
}

func (f *fakeStats) GetItemLastPlayed(_ context.Context, jellyfinID string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.err[jellyfinID]; err != nil {
		return time.Time{}, err
	}
	return f.lastPlayed[jellyfinID], nil
}

func (f *fakeStats) setLastPlayed(jellyfinID string, when time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastPlayed[jellyfinID] = when
}
