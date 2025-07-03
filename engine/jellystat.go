package engine

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/jellystat"
	"github.com/samber/lo"
)

func (e *Engine) getJellystatItems(ctx context.Context) ([]jellystat.LibraryItem, error) {
	enabledLibraryIDs, err := e.getJellystatEnabledLibraryIDs(ctx)
	if err != nil {
		return nil, err
	}

	var allItems []jellystat.LibraryItem
	for _, libraryID := range enabledLibraryIDs {
		items, err := e.getJellystatLibraryItems(ctx, libraryID)
		if err != nil {
			return nil, err
		}
		allItems = append(allItems, items...)
	}

	return allItems, nil
}

func (e *Engine) getJellystatEnabledLibraryIDs(ctx context.Context) ([]string, error) {
	if e.data.libraryIDMap == nil {
		e.data.libraryIDMap = make(map[string]string)
	}
	libraries, err := e.jellystat.GetLibraryMetadata(ctx)
	if err != nil {
		return nil, err
	}
	var enabledLibraryIDs []string
	for _, library := range libraries {
		libraryName := strings.ToLower(library.Name)
		if slices.Contains(lo.Keys(e.cfg.Libraries), libraryName) {
			enabledLibraryIDs = append(enabledLibraryIDs, library.ID)
			e.data.libraryIDMap[library.ID] = library.Name // maybe we need to change that to the lower case version? Not sure yet.
		}
	}
	return enabledLibraryIDs, nil
}

// getJellystatLibraryItems retrieves all items from a specific Jellystat library and filters out archived items.
func (e *Engine) getJellystatLibraryItems(ctx context.Context, libraryID string) ([]jellystat.LibraryItem, error) {
	items, err := e.jellystat.GetLibraryItems(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	// Filter out archived items
	var filteredItems []jellystat.LibraryItem
	for _, item := range items {
		if !item.Archived {
			filteredItems = append(filteredItems, item)
		}
	}
	return filteredItems, nil
}

func (e *Engine) getMediaItemLastStreamed(ctx context.Context, m MediaItem) (time.Time, error) {
	lastPlayed, err := e.jellystat.GetLastPlayed(ctx, m.JellystatID)
	if err != nil {
		return time.Time{}, err
	}
	if lastPlayed == nil || lastPlayed.LastPlayed == nil {
		return time.Time{}, nil // No playback history found
	}
	return *lastPlayed.LastPlayed, nil
}

// filterLastStreamThreshold filters out media items that have been streamed within the configured threshold.
func (e *Engine) filterLastStreamThreshold(ctx context.Context) error {
	filteredItems := make(map[string][]MediaItem, 0)
	for lib, items := range e.data.mediaItems {
		for _, item := range items {
			lastStreamed, err := e.getMediaItemLastStreamed(ctx, item)
			if err != nil {
				return err
			}
			if lastStreamed.IsZero() {
				filteredItems[lib] = append(filteredItems[lib], item) // No last streamed time, mark for deletion
				continue
			}
			// Check if the last streamed time is older than the configured threshold
			libraryConfig := e.cfg.GetLibraryConfig(lib)
			if libraryConfig != nil && time.Since(lastStreamed) > time.Duration(libraryConfig.LastStreamThreshold)*24*time.Hour {
				filteredItems[lib] = append(filteredItems[lib], item)
				continue
			}
			log.Debugf("Excluding item %s due to recent stream: %s", item.Title, lastStreamed.Format(time.RFC3339))
		}
	}
	e.data.mediaItems = filteredItems
	return nil
}

func (e *Engine) getLibraryNameByID(libraryID string) string {
	if name, exists := e.data.libraryIDMap[libraryID]; exists {
		return name
	}
	log.Warn("Library ID not found in library ID map", "library", libraryID)
	return ""
}
