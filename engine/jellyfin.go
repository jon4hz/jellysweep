package engine

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ccoveille/go-safecast"
	"github.com/charmbracelet/log"
	radarr "github.com/devopsarr/radarr-go/radarr"
	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/static"
	"github.com/jon4hz/jellysweep/version"
	jellyfin "github.com/sj14/jellyfin-go/api"
)

func newJellyfinClient(cfg *config.JellyfinConfig) *jellyfin.APIClient {
	clientConfig := jellyfin.NewConfiguration()
	clientConfig.Servers = jellyfin.ServerConfigurations{
		{
			URL:         cfg.URL,
			Description: "Jellyfin server",
		},
	}
	clientConfig.DefaultHeader = map[string]string{"Authorization": fmt.Sprintf(`MediaBrowser Token="%s"`, cfg.APIKey)}
	clientConfig.UserAgent = fmt.Sprintf("JellySweep/%s", version.Version)
	return jellyfin.NewAPIClient(clientConfig)
}

type jellyfinItem struct {
	jellyfin.BaseItemDto
	ParentLibraryID string `json:"parentLibraryId,omitempty"`
}

func (e *Engine) getJellyfinItems(ctx context.Context) ([]jellyfinItem, error) {
	var allItems []jellyfinItem

	// First, get all media folders (libraries)
	mediaFoldersResp, _, err := e.jellyfin.LibraryAPI.GetMediaFolders(ctx).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to get media folders: %w", err)
	}

	// Check if we have items in the response
	mediaFolders := mediaFoldersResp.GetItems()
	if len(mediaFolders) == 0 {
		return nil, fmt.Errorf("no media folders found")
	}

	// fetch virtual folders (required for the thresholds based on disk usage)
	virtualFolders, _, err := e.jellyfin.LibraryStructureAPI.GetVirtualFolders(ctx).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to get virtual folders: %w", err)
	}
	if len(virtualFolders) == 0 {
		log.Warn("No virtual folders found")
	}

	for _, folder := range virtualFolders {
		log.Debug("Found virtual folder", "name", folder.GetName())
		libraryName := folder.GetName()
		libraryConfig := e.cfg.GetLibraryConfig(libraryName)
		if libraryConfig == nil || !libraryConfig.Enabled {
			log.Debug("Skipping virtual folder for disabled library", "library", libraryName)
			continue
		}
		e.data.libraryFoldersMap[libraryName] = folder.GetLocations()
	}

	// Process each enabled library
	for _, folder := range mediaFolders {
		if folder.Id == nil || folder.GetName() == "" {
			continue
		}

		libraryName := folder.GetName()
		libraryID := folder.GetId()

		// Check if this library is enabled in the configuration
		libraryConfig := e.cfg.GetLibraryConfig(libraryName)
		if libraryConfig == nil || !libraryConfig.Enabled {
			log.Debug("Skipping disabled library", "library", libraryName)
			continue
		}

		e.data.libraryIDMap[libraryID] = libraryName
		log.Debug("Added library to ID map", "library", libraryName, "id", libraryID)

		log.Info("Processing library", "library", libraryName, "id", libraryID)

		// Get all items from this library
		libraryItems, err := e.getJellyfinItemsFromLibrary(ctx, libraryID, libraryName)
		if err != nil {
			log.Error("Failed to get items from library", "library", libraryName, "error", err)
			continue
		}

		// For some reason, the parentID returned from Jellyfin items dont match the library ID.
		// As a workaround, wrap the jellyfin item dto so it contains the library ID jellysweep expects.
		for _, item := range libraryItems {
			// Wrap the item to include the parent library ID
			wrappedItem := jellyfinItem{
				BaseItemDto:     item,
				ParentLibraryID: libraryID,
			}
			allItems = append(allItems, wrappedItem)
		}
	}

	return allItems, nil
}

func (e *Engine) getLibraryNameByID(libraryID string) string {
	if name, exists := e.data.libraryIDMap[libraryID]; exists {
		return name
	}
	log.Warn("Library ID not found in library ID map", "library", libraryID)
	return ""
}

func (e *Engine) getJellyfinItemsFromLibrary(ctx context.Context, libraryID, libraryName string) ([]jellyfin.BaseItemDto, error) {
	log.Debug("Getting items from library", "library", libraryName, "id", libraryID)

	var allItems []jellyfin.BaseItemDto

	// We'll paginate through all items in the library
	startIndex := int32(0)
	limit := int32(1000) // Get items in batches of 1000

	for {
		// Get items from this library
		itemsResp, _, err := e.jellyfin.ItemsAPI.GetItems(ctx).
			ParentId(libraryID).
			Recursive(true).
			StartIndex(startIndex).
			Limit(limit).
			Fields([]jellyfin.ItemFields{
				jellyfin.ITEMFIELDS_PATH,
				jellyfin.ITEMFIELDS_DATE_CREATED,
				jellyfin.ITEMFIELDS_TAGS,
				jellyfin.ITEMFIELDS_PARENT_ID,
				jellyfin.ITEMFIELDS_MEDIA_SOURCES,
			}).
			IncludeItemTypes([]jellyfin.BaseItemKind{
				jellyfin.BASEITEMKIND_MOVIE,
				jellyfin.BASEITEMKIND_SERIES,
			}).
			Execute()
		if err != nil {
			return nil, fmt.Errorf("failed to get items from library %s: %w", libraryName, err)
		}

		items := itemsResp.GetItems()
		if len(items) == 0 {
			log.Debug("No more items found in library", "library", libraryName)
			break
		}

		log.Debug("Retrieved items from library", "library", libraryName, "count", len(items))

		// Add items to our collection
		allItems = append(allItems, items...)

		// Log the items for debugging
		for _, item := range items {
			if item.GetName() != "" && item.Id != nil {
				log.Debug("Found item", "library", libraryName, "name", item.GetName(), "id", item.GetId(), "type", item.GetType())
			}
		}

		// Check if we've gotten all items
		totalRecordCount := itemsResp.GetTotalRecordCount()
		itemsLen, err := safecast.ToInt32(len(items))
		if err != nil {
			return nil, fmt.Errorf("failed to cast items length: %w", err)
		}
		if itemsLen > 0 && startIndex+itemsLen >= totalRecordCount {
			log.Debug("Retrieved all items from library", "library", libraryName, "total", totalRecordCount)
			break
		}

		// Move to next batch
		itemsCount, err := safecast.ToInt32(len(items))
		if err != nil {
			return nil, fmt.Errorf("failed to cast items count: %w", err)
		}
		startIndex += itemsCount
	}

	log.Info("Retrieved all items from library", "library", libraryName, "total", len(allItems))
	return allItems, nil
}

// createJellyfinLeavingLibrary adds all the items that are marked for deletion to the Jellyfin leaving library.
// There is a leaving-tv-shows library and a leaving-movies library.
// If the library does not exist, it will be created.
func (e *Engine) createJellyfinLeavingLibrary(ctx context.Context) error {
	// make sure we have up to date data
	e.cache.ClearAll(ctx)

	// Get ALL items currently marked for deletion (not just newly marked ones)
	currentlyLeavingMovies := make(map[string]bool)
	currentlyLeavingTVShows := make(map[string]bool)

	// make sure we have up to date data
	e.cache.ClearAll(ctx)

	// Get current marked items from APIs to see what actually has delete tags
	if err := e.populateCurrentlyMarkedItems(ctx, currentlyLeavingMovies, currentlyLeavingTVShows); err != nil {
		log.Warn("Failed to get currently marked items", "error", err)
		return fmt.Errorf("failed to get currently marked items: %w", err)
	}

	// Convert sets to slices for collection operations
	leavingMovies := []string{}
	for itemID := range currentlyLeavingMovies {
		leavingMovies = append(leavingMovies, itemID)
	}

	leavingTVShows := []string{}
	for itemID := range currentlyLeavingTVShows {
		leavingTVShows = append(leavingTVShows, itemID)
	}

	// Create/update leaving collections if we have items
	if len(leavingMovies) > 0 {
		if err := e.createOrUpdateLeavingCollection(ctx, "Leaving Movies", leavingMovies); err != nil {
			log.Error("Failed to create/update leaving movies collection", "error", err)
			return fmt.Errorf("failed to create/update leaving movies collection: %w", err)
		}
		log.Info("Updated leaving movies collection", "count", len(leavingMovies))
	}

	if len(leavingTVShows) > 0 {
		if err := e.createOrUpdateLeavingCollection(ctx, "Leaving TV Shows", leavingTVShows); err != nil {
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
	existingCollectionID, err := e.findCollectionByName(ctx, collectionName)
	if err != nil {
		return fmt.Errorf("failed to search for existing collection: %w", err)
	}

	if existingCollectionID != "" {
		// Collection exists, only add items that aren't already in it
		log.Debug("Found existing collection, adding new items", "collection", collectionName, "id", existingCollectionID)

		// Get current items in the collection
		currentItems, err := e.getCollectionItems(ctx, existingCollectionID)
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
			_, err = e.jellyfin.CollectionAPI.AddToCollection(ctx, existingCollectionID).
				Ids(itemsToAdd).
				Execute()
			if err != nil {
				return fmt.Errorf("failed to add items to existing collection %s: %w", collectionName, err)
			}
		} else {
			log.Debug("No new items to add to collection", "collection", collectionName)
		}

		// Set the jellysweep logo as the collection image (will only set if not already set)
		if err := e.setCollectionImage(ctx, existingCollectionID); err != nil {
			log.Warn("Failed to set collection image", "collection", collectionName, "error", err)
			// Don't fail the whole operation if image setting fails
		}
	} else {
		// Collection doesn't exist, create it
		log.Debug("Creating new collection", "collection", collectionName)

		createResp, _, err := e.jellyfin.CollectionAPI.CreateCollection(ctx).
			Name(collectionName).
			Ids(itemIDs).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to create collection %s: %w", collectionName, err)
		}

		// Set the jellysweep logo as the collection image
		if createResp != nil && createResp.Id != nil {
			if err := e.setCollectionImage(ctx, *createResp.Id); err != nil {
				log.Warn("Failed to set collection image", "collection", collectionName, "error", err)
				// Don't fail the whole operation if image setting fails
			}
		}
	}

	return nil
}

// findCollectionByName searches for a collection by name and returns its ID.
func (e *Engine) findCollectionByName(ctx context.Context, name string) (string, error) {
	// Get all collections
	result, _, err := e.jellyfin.ItemsAPI.GetItems(ctx).
		IncludeItemTypes([]jellyfin.BaseItemKind{jellyfin.BASEITEMKIND_BOX_SET}).
		Recursive(false).
		Execute()
	if err != nil {
		return "", fmt.Errorf("failed to get collections: %w", err)
	}

	// Search for collection by name
	items := result.GetItems()
	for _, item := range items {
		if item.GetName() == name {
			return item.GetId(), nil
		}
	}

	return "", nil // Collection not found
}

// getCollectionItems returns a map of item IDs currently in the collection.
func (e *Engine) getCollectionItems(ctx context.Context, collectionID string) (map[string]bool, error) {
	// Get items in the collection
	result, _, err := e.jellyfin.ItemsAPI.GetItems(ctx).
		ParentId(collectionID).
		Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to get collection items: %w", err)
	}

	// Create a set of current item IDs
	currentItems := make(map[string]bool)
	items := result.GetItems()
	for _, item := range items {
		currentItems[item.GetId()] = true
	}

	return currentItems, nil
}

// removeItemsFromLeavingLibraries removes items from the leaving libraries if they are no longer marked for deletion.
func (e *Engine) removeItemsFromLeavingLibraries(ctx context.Context) {
	// Find leaving collections
	moviesCollectionID, err := e.findCollectionByName(ctx, "Leaving Movies")
	if err != nil {
		log.Warn("Failed to find leaving movies collection", "error", err)
	}

	tvShowsCollectionID, err := e.findCollectionByName(ctx, "Leaving TV Shows")
	if err != nil {
		log.Warn("Failed to find leaving TV shows collection", "error", err)
	}

	// Get all items currently marked for deletion from Sonarr/Radarr
	currentlyLeavingMovies := make(map[string]bool)
	currentlyLeavingTVShows := make(map[string]bool)

	// Get current marked items from APIs to see what actually has delete tags
	if err := e.populateCurrentlyMarkedItems(ctx, currentlyLeavingMovies, currentlyLeavingTVShows); err != nil {
		log.Warn("Failed to get currently marked items", "error", err)
		return
	}

	// Remove items from leaving movies collection if they're no longer marked for deletion
	if moviesCollectionID != "" {
		if err := e.removeItemsNotInSet(ctx, moviesCollectionID, currentlyLeavingMovies, "Leaving Movies"); err != nil {
			log.Error("Failed to clean up leaving movies collection", "error", err)
		}
	}

	// Remove items from leaving TV shows collection if they're no longer marked for deletion
	if tvShowsCollectionID != "" {
		if err := e.removeItemsNotInSet(ctx, tvShowsCollectionID, currentlyLeavingTVShows, "Leaving TV Shows"); err != nil {
			log.Error("Failed to clean up leaving TV shows collection", "error", err)
		}
	}
}

// populateCurrentlyMarkedItems gets all media items that currently have jellysweep-delete tags.
func (e *Engine) populateCurrentlyMarkedItems(ctx context.Context, moviesSet, tvShowsSet map[string]bool) error {
	// We need to re-gather all items and check their current tags
	// This is necessary because e.data.mediaItems only contains freshly marked items
	jellyfinItems, err := e.getJellyfinItems(ctx)
	if err != nil {
		return fmt.Errorf("failed to get jellyfin items: %w", err)
	}

	// Get current Sonarr items and tags
	if e.sonarr != nil {
		sonarrItems, err := e.getSonarrItems(ctx, false) // Don't force cache refresh
		if err != nil {
			log.Warn("Failed to get sonarr items", "error", err)
		} else {
			sonarrTags, err := e.getSonarrTags(ctx, false)
			if err != nil {
				log.Warn("Failed to get sonarr tags", "error", err)
			} else {
				e.checkSonarrItemsForDeleteTags(jellyfinItems, sonarrItems, sonarrTags, tvShowsSet)
			}
		}
	}

	// Get current Radarr items and tags
	if e.radarr != nil {
		radarrItems, err := e.getRadarrItems(ctx, false) // Don't force cache refresh
		if err != nil {
			log.Warn("Failed to get radarr items", "error", err)
		} else {
			radarrTags, err := e.getRadarrTags(ctx, false)
			if err != nil {
				log.Warn("Failed to get radarr tags", "error", err)
			} else {
				e.checkRadarrItemsForDeleteTags(jellyfinItems, radarrItems, radarrTags, moviesSet)
			}
		}
	}

	return nil
}

// checkSonarrItemsForDeleteTags checks Sonarr items for jellysweep-delete tags and adds their Jellyfin IDs to the set.
func (e *Engine) checkSonarrItemsForDeleteTags(jellyfinItems []jellyfinItem, sonarrItems []sonarr.SeriesResource, sonarrTags map[int32]string, tvShowsSet map[string]bool) {
	for _, sonarrItem := range sonarrItems {
		// Check if this sonarr item has any jellysweep-delete tags
		hasDeleteTag := false
		for _, tagID := range sonarrItem.GetTags() {
			if tagName, exists := sonarrTags[tagID]; exists {
				if IsJellysweepDeleteTag(tagName) {
					hasDeleteTag = true
					break
				}
			}
		}

		if hasDeleteTag {
			// Find the corresponding Jellyfin item
			for _, jellyfinItem := range jellyfinItems {
				if jellyfinItem.GetType() == jellyfin.BASEITEMKIND_SERIES &&
					jellyfinItem.GetName() == sonarrItem.GetTitle() &&
					jellyfinItem.GetProductionYear() == sonarrItem.GetYear() {
					tvShowsSet[jellyfinItem.GetId()] = true
					break
				}
			}
		}
	}
}

// checkRadarrItemsForDeleteTags checks Radarr items for jellysweep-delete tags and adds their Jellyfin IDs to the set.
func (e *Engine) checkRadarrItemsForDeleteTags(jellyfinItems []jellyfinItem, radarrItems []radarr.MovieResource, radarrTags map[int32]string, moviesSet map[string]bool) {
	for _, radarrItem := range radarrItems {
		// Check if this radarr item has any jellysweep-delete tags
		hasDeleteTag := false
		for _, tagID := range radarrItem.GetTags() {
			if tagName, exists := radarrTags[tagID]; exists {
				if IsJellysweepDeleteTag(tagName) {
					hasDeleteTag = true
					break
				}
			}
		}

		if hasDeleteTag {
			// Find the corresponding Jellyfin item
			for _, jellyfinItem := range jellyfinItems {
				if jellyfinItem.GetType() == jellyfin.BASEITEMKIND_MOVIE &&
					jellyfinItem.GetName() == radarrItem.GetTitle() &&
					jellyfinItem.GetProductionYear() == radarrItem.GetYear() {
					moviesSet[jellyfinItem.GetId()] = true
					break
				}
			}
		}
	}
}

// removeItemsNotInSet removes items from a collection if they are not in the provided set.
func (e *Engine) removeItemsNotInSet(ctx context.Context, collectionID string, shouldKeepSet map[string]bool, collectionName string) error {
	// Get current items in the collection
	result, _, err := e.jellyfin.ItemsAPI.GetItems(ctx).
		ParentId(collectionID).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to get items in collection %s: %w", collectionName, err)
	}

	itemsToRemove := []string{}
	items := result.GetItems()

	for _, item := range items {
		itemID := item.GetId()
		if !shouldKeepSet[itemID] {
			itemsToRemove = append(itemsToRemove, itemID)
			log.Debug("Marking item for removal from collection", "collection", collectionName, "itemID", itemID, "itemName", item.GetName())
		}
	}

	// Remove items that should no longer be in the collection
	if len(itemsToRemove) > 0 {
		log.Info("Removing items from leaving collection", "collection", collectionName, "count", len(itemsToRemove))

		_, err = e.jellyfin.CollectionAPI.RemoveFromCollection(ctx, collectionID).
			Ids(itemsToRemove).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to remove items from collection %s: %w", collectionName, err)
		}
	}

	return nil
}

// setCollectionImage sets the jellysweep logo as the primary image for a collection.
// It checks if the collection already has a primary image and only sets it if it doesn't.
func (e *Engine) setCollectionImage(ctx context.Context, collectionID string) error {
	// First check if the collection already has a primary image
	hasImage, err := e.collectionHasPrimaryImage(ctx, collectionID)
	if err != nil {
		log.Warn("Failed to check if collection has primary image, attempting to set anyway", "collectionID", collectionID, "error", err)
		// Continue anyway and try to set the image
	} else if hasImage {
		log.Debug("Collection already has primary image, skipping", "collectionID", collectionID)
		return nil
	}

	// Get the jellysweep logo from embedded static files
	logoData, err := static.GetJellysweepLogo()
	if err != nil {
		return fmt.Errorf("failed to get jellysweep logo: %w", err)
	}

	// Create a temporary file for the logo data
	tempFile, err := os.CreateTemp("", "jellysweep-logo-*.png")
	if err != nil {
		return fmt.Errorf("failed to create temporary file for logo: %w", err)
	}
	defer func() {
		if closeErr := tempFile.Close(); closeErr != nil {
			log.Warn("Failed to close temporary logo file", "error", closeErr)
		}
		if removeErr := os.Remove(tempFile.Name()); removeErr != nil {
			log.Warn("Failed to remove temporary logo file", "file", tempFile.Name(), "error", removeErr)
		}
	}()

	// Write the logo data to the temporary file
	if _, err := tempFile.Write(logoData); err != nil {
		return fmt.Errorf("failed to write logo data to temporary file: %w", err)
	}

	// Seek back to the beginning of the file
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to beginning of temporary file: %w", err)
	}

	// Upload the image as the collection's primary image
	_, err = e.jellyfin.ImageAPI.SetItemImage(ctx, collectionID, jellyfin.IMAGETYPE_PRIMARY).
		Body(tempFile).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to set collection image: %w", err)
	}

	log.Debug("Successfully set jellysweep logo for collection", "collectionID", collectionID)
	return nil
}

// collectionHasPrimaryImage checks if a collection already has a primary image.
func (e *Engine) collectionHasPrimaryImage(ctx context.Context, collectionID string) (bool, error) {
	// Get item information to check if it has images
	item, _, err := e.jellyfin.ItemsAPI.GetItems(ctx).
		Ids([]string{collectionID}).
		Fields([]jellyfin.ItemFields{jellyfin.ITEMFIELDS_PRIMARY_IMAGE_ASPECT_RATIO}).
		Execute()
	if err != nil {
		return false, fmt.Errorf("failed to get collection info: %w", err)
	}

	items := item.GetItems()
	if len(items) == 0 {
		return false, fmt.Errorf("collection not found")
	}

	// Check if the item has a primary image aspect ratio (indicates image exists)
	aspectRatio := items[0].GetPrimaryImageAspectRatio()
	return aspectRatio > 0, nil
}
