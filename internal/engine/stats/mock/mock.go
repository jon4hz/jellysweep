package mock

import (
	"context"
	"sync"
	"time"
)

// MockStatser is a mock implementation of stats.Statser for testing.
type MockStatser struct {
	mu sync.RWMutex

	// Storage for item last played times
	itemLastPlayed map[string]time.Time

	// Error simulation
	GetItemLastPlayedError error
}

// NewMockStatser creates a new MockStatser instance.
func NewMockStatser() *MockStatser {
	return &MockStatser{
		itemLastPlayed: make(map[string]time.Time),
	}
}

// Reset clears all data and errors from the mock.
func (m *MockStatser) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.itemLastPlayed = make(map[string]time.Time)
	m.GetItemLastPlayedError = nil
}

// SetItemLastPlayed sets the last played time for a specific item.
func (m *MockStatser) SetItemLastPlayed(itemID string, lastPlayed time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.itemLastPlayed[itemID] = lastPlayed
}

// GetItemLastPlayed returns the mock last played time for a specific item.
func (m *MockStatser) GetItemLastPlayed(ctx context.Context, itemID string) (time.Time, error) {
	if m.GetItemLastPlayedError != nil {
		return time.Time{}, m.GetItemLastPlayedError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	lastPlayed, ok := m.itemLastPlayed[itemID]
	if !ok {
		return time.Time{}, nil
	}

	return lastPlayed, nil
}
