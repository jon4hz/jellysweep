package engine

// Test doubles for the engine package.
//
// Conventions: dependencies are faked with small stateful hand-written fakes
// (no mock codegen), the database is a real in-memory sqlite via
// internal/database/databasetest. Lifecycle scenarios live in
// lifecycle_test.go; add a scenario there when changing the cleanup pipeline.

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	jellyfinAPI "github.com/sj14/jellyfin-go/api"
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

// fakeArr implements arr.Arrer with a stateful in-memory library.
type fakeArr struct {
	mu          sync.Mutex
	items       map[int32]arr.MediaItem // current arr library, keyed by arr ID
	deleted     []int32                 // DeleteMedia calls in order
	unmonitored []int32
	ignored     []int32 // ResetAllTagsAndAddIgnore calls
	tagResets   int     // ResetTags + CleanupAllTags calls
	deleteErr   map[int32]error
	getItemsErr error
	addedDates  map[int32]*time.Time

	rootFolderUsage      map[string]float64 // GetRootFolderUsage result
	rootFolderUsageErr   error
	rootFolderUsageCalls int
}

var _ arr.Arrer = (*fakeArr)(nil)

func newFakeArr() *fakeArr {
	return &fakeArr{
		items:      make(map[int32]arr.MediaItem),
		deleteErr:  make(map[int32]error),
		addedDates: make(map[int32]*time.Time),
	}
}

// GetItems returns library items present in the given jellyfin items.
func (f *fakeArr) GetItems(_ context.Context, jellyfinItems []arr.JellyfinItem) ([]arr.MediaItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getItemsErr != nil {
		return nil, f.getItemsErr
	}
	jellyfinIDs := make(map[string]struct{}, len(jellyfinItems))
	for _, item := range jellyfinItems {
		jellyfinIDs[item.GetId()] = struct{}{}
	}
	items := make([]arr.MediaItem, 0, len(f.items))
	for _, item := range f.items {
		if _, ok := jellyfinIDs[item.JellyfinID]; ok {
			items = append(items, item)
		}
	}
	return items, nil
}

func (f *fakeArr) DeleteMedia(_ context.Context, arrID int32, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.deleteErr[arrID]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, arrID)
	delete(f.items, arrID)
	return nil
}

func (f *fakeArr) UnmonitorMedia(_ context.Context, arrID int32, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unmonitored = append(f.unmonitored, arrID)
	return nil
}

func (f *fakeArr) ResetTags(context.Context, []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tagResets++
	return nil
}

func (f *fakeArr) CleanupAllTags(context.Context, []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tagResets++
	return nil
}

func (f *fakeArr) ResetAllTagsAndAddIgnore(_ context.Context, id int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ignored = append(f.ignored, id)
	return nil
}

func (f *fakeArr) GetItemAddedDate(_ context.Context, itemID int32, _ time.Time) (*time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addedDates[itemID], nil
}

func (f *fakeArr) GetRootFolderUsage(context.Context) (map[string]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rootFolderUsageCalls++
	if f.rootFolderUsageErr != nil {
		return nil, f.rootFolderUsageErr
	}
	return f.rootFolderUsage, nil
}

func (f *fakeArr) hasDeleted(arrID int32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.deleted, arrID)
}

// fakeJellyfin implements the jellyfinClient interface.
type fakeJellyfin struct {
	mu        sync.Mutex
	items     []arr.JellyfinItem
	removed   []string // RemoveItemWithCleanupMode calls
	removeErr map[string]error
	getErr    error
	// collections, keyed by name: itemID -> present
	collections map[string]map[string]bool
}

var _ jellyfinClient = (*fakeJellyfin)(nil)

func newFakeJellyfin() *fakeJellyfin {
	return &fakeJellyfin{
		removeErr:   make(map[string]error),
		collections: make(map[string]map[string]bool),
	}
}

func (f *fakeJellyfin) GetJellyfinItems(context.Context) ([]arr.JellyfinItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	return slices.Clone(f.items), nil
}

func (f *fakeJellyfin) RemoveItemWithCleanupMode(_ context.Context, itemID, _ string, _ jellyfinAPI.BaseItemKind, _ config.CleanupMode, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.removeErr[itemID]; err != nil {
		return err
	}
	f.removed = append(f.removed, itemID)
	for i, item := range f.items {
		if item.GetId() == itemID {
			f.items = slices.Delete(f.items, i, i+1)
			break
		}
	}
	return nil
}

func (f *fakeJellyfin) FindCollectionByName(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.collections[name]; ok {
		return name, nil // collection ID == name in the fake
	}
	return "", nil
}

func (f *fakeJellyfin) GetCollectionItems(_ context.Context, collectionID string) (map[string]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make(map[string]bool, len(f.collections[collectionID]))
	for id, present := range f.collections[collectionID] {
		items[id] = present
	}
	return items, nil
}

func (f *fakeJellyfin) CreateCollection(_ context.Context, name string, itemIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	collection := make(map[string]bool, len(itemIDs))
	for _, id := range itemIDs {
		collection[id] = true
	}
	f.collections[name] = collection
	return nil
}

func (f *fakeJellyfin) AddItemsToCollection(_ context.Context, collectionID string, itemIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.collections[collectionID] == nil {
		f.collections[collectionID] = make(map[string]bool)
	}
	for _, id := range itemIDs {
		f.collections[collectionID][id] = true
	}
	return nil
}

func (f *fakeJellyfin) RemoveItemsFromCollection(_ context.Context, collectionID string, itemIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range itemIDs {
		delete(f.collections[collectionID], id)
	}
	return nil
}

func (f *fakeJellyfin) hasRemoved(itemID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.removed, itemID)
}
