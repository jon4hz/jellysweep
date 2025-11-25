package mock

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jon4hz/jellysweep/internal/engine/arr"
)

// MockArrer is a mock implementation of arr.Arrer for testing.
type MockArrer struct {
	mu sync.RWMutex

	// Storage for items and their added dates
	itemAddedDates map[int32]*time.Time

	// Error simulation
	GetItemsError         error
	DeleteMediaError      error
	ResetTagsError        error
	CleanupAllTagsError   error
	GetItemAddedDateError error
}

// NewMockArrer creates a new MockArrer instance.
func NewMockArrer() *MockArrer {
	return &MockArrer{
		itemAddedDates: make(map[int32]*time.Time),
	}
}

// Reset clears all data and errors from the mock.
func (m *MockArrer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.itemAddedDates = make(map[int32]*time.Time)
	m.GetItemsError = nil
	m.DeleteMediaError = nil
	m.ResetTagsError = nil
	m.CleanupAllTagsError = nil
	m.GetItemAddedDateError = nil
}

// SetItemAddedDate sets the added date for a specific item.
func (m *MockArrer) SetItemAddedDate(itemID int32, addedDate *time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.itemAddedDates[itemID] = addedDate
}

// GetItems is a mock implementation.
func (m *MockArrer) GetItems(ctx context.Context, jellyfinItems []arr.JellyfinItem) ([]arr.MediaItem, error) {
	if m.GetItemsError != nil {
		return nil, m.GetItemsError
	}
	return []arr.MediaItem{}, nil
}

// DeleteMedia is a mock implementation.
func (m *MockArrer) DeleteMedia(ctx context.Context, arrID int32, title string) error {
	if m.DeleteMediaError != nil {
		return m.DeleteMediaError
	}
	return nil
}

// ResetTags is a mock implementation.
func (m *MockArrer) ResetTags(ctx context.Context, additionalTags []string) error {
	if m.ResetTagsError != nil {
		return m.ResetTagsError
	}
	return nil
}

// CleanupAllTags is a mock implementation.
func (m *MockArrer) CleanupAllTags(ctx context.Context, additionalTags []string) error {
	if m.CleanupAllTagsError != nil {
		return m.CleanupAllTagsError
	}
	return nil
}

// ResetAllTagsAndAddIgnore is a mock implementation.
func (m *MockArrer) ResetAllTagsAndAddIgnore(ctx context.Context, id int32) error {
	return nil
}

// GetItemAddedDate returns the mock added date for a specific item.
func (m *MockArrer) GetItemAddedDate(ctx context.Context, itemID int32, since time.Time) (*time.Time, error) {
	if m.GetItemAddedDateError != nil {
		return nil, m.GetItemAddedDateError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	date, ok := m.itemAddedDates[itemID]
	if !ok {
		return nil, errors.New("item not found")
	}

	return date, nil
}
