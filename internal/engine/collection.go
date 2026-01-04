package engine

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/database"
)

// createJellyfinLeavingCollections creates or updates "Leaving Soon" collections in Jellyfin.
// These collections show users which media items are scheduled for deletion.
// There are separate collections for movies and TV shows.
func (e *Engine) createJellyfinLeavingCollections(ctx context.Context) error {
	if !e.cfg.EnableLeavingCollections {
		log.Debug("Leaving collections feature is disabled, skipping")
		return nil
	}

	log.Info("Creating/updating Jellyfin leaving collections")

	// Get all media items currently marked for deletion from the database
	mediaItems, err := e.db.GetMediaItems(ctx, false) // Don't include protected items
	if err != nil {
		log.Error("Failed to get media items from database", "error", err)
		return fmt.Errorf("failed to get media items from database: %w", err)
	}

	if len(mediaItems) == 0 {
		log.Debug("No media items marked for deletion, skipping collection creation")
		return nil
	}

	// Separate items by media type and collect their Jellyfin IDs
	leavingMovies := []string{}
	leavingTVShows := []string{}

	for _, item := range mediaItems {
		switch item.MediaType {
		case database.MediaTypeMovie:
			leavingMovies = append(leavingMovies, item.JellyfinID)
		case database.MediaTypeTV:
			leavingTVShows = append(leavingTVShows, item.JellyfinID)
		default:
			log.Warn("Unknown media type", "type", item.MediaType, "title", item.Title)
		}
	}

	// Create/update leaving collections if we have items
	if len(leavingMovies) > 0 {
		if err := e.createOrUpdateLeavingCollection(ctx, e.cfg.LeavingCollectionsMovieName, leavingMovies); err != nil {
			log.Error("Failed to create/update leaving movies collection", "error", err)
			return fmt.Errorf("failed to create/update leaving movies collection: %w", err)
		}
		log.Info("Updated leaving movies collection", "count", len(leavingMovies))
	}

	if len(leavingTVShows) > 0 {
		if err := e.createOrUpdateLeavingCollection(ctx, e.cfg.LeavingCollectionsTVName, leavingTVShows); err != nil {
			log.Error("Failed to create/update leaving TV shows collection", "error", err)
			return fmt.Errorf("failed to create/update leaving TV shows collection: %w", err)
		}
		log.Info("Updated leaving TV shows collection", "count", len(leavingTVShows))
	}

	return nil
}

// createOrUpdateLeavingCollection creates or updates a collection for items leaving the system.
func (e *Engine) createOrUpdateLeavingCollection(ctx context.Context, collectionName string, itemIDs []string) error {
	// Check if collection already exists
	existingCollectionID, err := e.jellyfin.FindCollectionByName(ctx, collectionName)
	if err != nil {
		return fmt.Errorf("failed to search for existing collection: %w", err)
	}

	if existingCollectionID != "" {
		// Collection exists, update it to match the current item list
		log.Debug("Found existing collection, updating items", "collection", collectionName, "id", existingCollectionID)

		// Get current items in the collection
		currentItems, err := e.jellyfin.GetCollectionItems(ctx, existingCollectionID)
		if err != nil {
			log.Warn("Failed to get current collection items", "collection", collectionName, "error", err)
			// Continue anyway, try to add all items
			currentItems = make(map[string]bool)
		}

		// Find items that need to be added (not already in collection)
		itemsToAdd := []string{}
		for _, itemID := range itemIDs {
			if !currentItems[itemID] {
				itemsToAdd = append(itemsToAdd, itemID)
			}
		}

		// Add new items to the collection
		if len(itemsToAdd) > 0 {
			log.Debug("Adding new items to collection", "collection", collectionName, "count", len(itemsToAdd))
			if err = e.jellyfin.AddItemsToCollection(ctx, existingCollectionID, itemsToAdd); err != nil {
				return fmt.Errorf("failed to add items to existing collection %s: %w", collectionName, err)
			}
		} else {
			log.Debug("No new items to add to collection", "collection", collectionName)
		}
	} else {
		// Collection doesn't exist, create it
		log.Debug("Creating new collection", "collection", collectionName)

		if err := e.jellyfin.CreateCollection(ctx, collectionName, itemIDs); err != nil {
			return fmt.Errorf("failed to create collection %s: %w", collectionName, err)
		}
	}

	return nil
}

// removeItemsFromLeavingCollections removes items from the leaving collections if they are no longer marked for deletion.
func (e *Engine) removeItemsFromLeavingCollections(ctx context.Context) {
	if !e.cfg.EnableLeavingCollections {
		log.Debug("Leaving collections feature is disabled, skipping cleanup")
		return
	}

	log.Info("Cleaning up leaving collections")

	// Find leaving collections
	moviesCollectionID, err := e.jellyfin.FindCollectionByName(ctx, e.cfg.LeavingCollectionsMovieName)
	if err != nil {
		log.Warn("Failed to find leaving movies collection", "error", err)
	}

	tvShowsCollectionID, err := e.jellyfin.FindCollectionByName(ctx, e.cfg.LeavingCollectionsTVName)
	if err != nil {
		log.Warn("Failed to find leaving TV shows collection", "error", err)
	}

	// Get all items currently marked for deletion from database
	mediaItems, err := e.db.GetMediaItems(ctx, false) // Don't include protected items
	if err != nil {
		log.Warn("Failed to get currently marked items from database", "error", err)
		return
	}

	// Build sets of items that should be in collections
	currentlyLeavingMovies := make(map[string]bool)
	currentlyLeavingTVShows := make(map[string]bool)

	for _, item := range mediaItems {
		switch item.MediaType {
		case database.MediaTypeMovie:
			currentlyLeavingMovies[item.JellyfinID] = true
		case database.MediaTypeTV:
			currentlyLeavingTVShows[item.JellyfinID] = true
		}
	}

	// Remove items from leaving movies collection if they're no longer marked for deletion
	if moviesCollectionID != "" {
		if err := e.removeItemsNotInSet(ctx, moviesCollectionID, currentlyLeavingMovies, e.cfg.LeavingCollectionsMovieName); err != nil {
			log.Error("Failed to clean up leaving movies collection", "error", err)
		}
	}

	// Remove items from leaving TV shows collection if they're no longer marked for deletion
	if tvShowsCollectionID != "" {
		if err := e.removeItemsNotInSet(ctx, tvShowsCollectionID, currentlyLeavingTVShows, e.cfg.LeavingCollectionsTVName); err != nil {
			log.Error("Failed to clean up leaving TV shows collection", "error", err)
		}
	}
}

// removeItemsNotInSet removes items from a collection if they are not in the provided set.
func (e *Engine) removeItemsNotInSet(ctx context.Context, collectionID string, shouldKeepSet map[string]bool, collectionName string) error {
	// Get current items in the collection
	currentItems, err := e.jellyfin.GetCollectionItems(ctx, collectionID)
	if err != nil {
		return fmt.Errorf("failed to get items in collection %s: %w", collectionName, err)
	}

	itemsToRemove := []string{}
	for itemID := range currentItems {
		if !shouldKeepSet[itemID] {
			itemsToRemove = append(itemsToRemove, itemID)
			log.Debug("Marking item for removal from collection", "collection", collectionName, "itemID", itemID)
		}
	}

	// Remove items that should no longer be in the collection
	if len(itemsToRemove) > 0 {
		log.Info("Removing items from leaving collection", "collection", collectionName, "count", len(itemsToRemove))

		if err := e.jellyfin.RemoveItemsFromCollection(ctx, collectionID, itemsToRemove); err != nil {
			return fmt.Errorf("failed to remove items from collection %s: %w", collectionName, err)
		}
	}

	return nil
}
