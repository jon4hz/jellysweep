package jellyfin

import (
	"context"
	"fmt"

	"github.com/ccoveille/go-safecast"
	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine/arr"
	"github.com/jon4hz/jellysweep/version"
	jellyfin "github.com/sj14/jellyfin-go/api"
)

// Client provides a high-level interface for interacting with Jellyfin.
type Client struct {
	jellyfin *jellyfin.APIClient
	cfg      *config.Config
}

// New creates a new Jellyfin client with the given configuration and cache.
func New(cfg *config.Config) *Client {
	return &Client{
		jellyfin: newJellyfinClient(cfg.Jellyfin),
		cfg:      cfg,
	}
}

// newJellyfinClient creates a new low-level Jellyfin API client.
func newJellyfinClient(cfg *config.JellyfinConfig) *jellyfin.APIClient {
	clientConfig := jellyfin.NewConfiguration()
	clientConfig.Servers = jellyfin.ServerConfigurations{
		{
			URL:         cfg.URL,
			Description: "Jellyfin server",
		},
	}
	clientConfig.DefaultHeader = map[string]string{"Authorization": fmt.Sprintf(`MediaBrowser Token="%s"`, cfg.APIKey)}
	clientConfig.UserAgent = fmt.Sprintf("Jellysweep/%s", version.Version)
	return jellyfin.NewAPIClient(clientConfig)
}

// GetJellyfinItems retrieves all media items from enabled Jellyfin libraries.
// It returns JellyfinItem objects that include the library name for easier processing.
func (c *Client) GetJellyfinItems(ctx context.Context) ([]arr.JellyfinItem, map[string][]string, error) {
	allItems, err := c.fetchJellyfinItems(ctx)
	if err != nil {
		return nil, nil, err
	}

	libraryFoldersMap, err := c.GetLibraryFoldersMap(ctx)
	if err != nil {
		return nil, nil, err
	}

	return allItems, libraryFoldersMap, nil
}

// GetLibraryFoldersMap retrieves the mapping of library names to their folder paths.
func (c *Client) GetLibraryFoldersMap(ctx context.Context) (map[string][]string, error) {
	libraryFoldersMap := make(map[string][]string)

	// fetch virtual folders (required for the thresholds based on disk usage)
	virtualFolders, _, err := c.jellyfin.LibraryStructureAPI.GetVirtualFolders(ctx).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to get virtual folders: %w", err)
	}
	if len(virtualFolders) == 0 {
		log.Warn("No virtual folders found")
	}

	// Build library folders map
	for _, folder := range virtualFolders {
		log.Debug("Found virtual folder", "name", folder.GetName())
		libraryName := folder.GetName()
		libraryConfig := c.cfg.GetLibraryConfig(libraryName)
		if libraryConfig == nil || !libraryConfig.Enabled {
			log.Debug("Skipping virtual folder for disabled library", "library", libraryName)
			continue
		}
		libraryFoldersMap[libraryName] = folder.GetLocations()
	}

	return libraryFoldersMap, nil
}

// fetchJellyfinItems fetches items from the Jellyfin API (extracted from original GetJellyfinItems).
func (c *Client) fetchJellyfinItems(ctx context.Context) ([]arr.JellyfinItem, error) {
	var allItems []arr.JellyfinItem

	// First, get all media folders (libraries)
	mediaFoldersResp, _, err := c.jellyfin.LibraryAPI.GetMediaFolders(ctx).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to get media folders: %w", err)
	}

	// Check if we have items in the response
	mediaFolders := mediaFoldersResp.GetItems()
	if len(mediaFolders) == 0 {
		return nil, fmt.Errorf("no media folders found")
	}

	// Process each enabled library
	for _, folder := range mediaFolders {
		if folder.Id == nil || folder.GetName() == "" {
			continue
		}

		libraryName := folder.GetName()
		libraryID := folder.GetId()

		// Check if this library is enabled in the configuration
		libraryConfig := c.cfg.GetLibraryConfig(libraryName)
		if libraryConfig == nil || !libraryConfig.Enabled {
			log.Debug("Skipping disabled library", "library", libraryName)
			continue
		}

		log.Info("Processing library", "library", libraryName, "id", libraryID)

		// Get all items from this library
		libraryItems, err := c.getJellyfinItemsFromLibrary(ctx, libraryID, libraryName)
		if err != nil {
			log.Error("Failed to get items from library", "library", libraryName, "error", err)
			continue
		}

		// Wrap the jellyfin item dto to include the library name instead of ID
		for _, item := range libraryItems {
			wrappedItem := arr.JellyfinItem{
				BaseItemDto:       item,
				ParentLibraryName: libraryName, // Store library name directly
			}
			allItems = append(allItems, wrappedItem)
		}
	}

	return allItems, nil
}

// getJellyfinItemsFromLibrary retrieves all items from a specific Jellyfin library.
func (c *Client) getJellyfinItemsFromLibrary(ctx context.Context, libraryID, libraryName string) ([]jellyfin.BaseItemDto, error) {
	log.Debug("Getting items from library", "library", libraryName, "id", libraryID)

	var allItems []jellyfin.BaseItemDto

	// We'll paginate through all items in the library
	startIndex := int32(0)
	limit := int32(1000) // Get items in batches of 1000

	for {
		// Get items from this library
		itemsResp, _, err := c.jellyfin.ItemsAPI.GetItems(ctx).
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

// FindCollectionByName searches for a collection by name and returns its ID.
func (c *Client) FindCollectionByName(ctx context.Context, name string) (string, error) {
	// Get all collections
	result, _, err := c.jellyfin.ItemsAPI.GetItems(ctx).
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

// GetCollectionItems returns a map of item IDs currently in the collection.
func (c *Client) GetCollectionItems(ctx context.Context, collectionID string) (map[string]bool, error) {
	// Get items in the collection
	result, _, err := c.jellyfin.ItemsAPI.GetItems(ctx).
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

// CreateCollection creates a new collection with the given name and item IDs.
func (c *Client) CreateCollection(ctx context.Context, name string, itemIDs []string) error {
	_, _, err := c.jellyfin.CollectionAPI.CreateCollection(ctx).
		Name(name).
		Ids(itemIDs).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to create collection %s: %w", name, err)
	}
	return nil
}

// AddItemsToCollection adds items to an existing collection.
func (c *Client) AddItemsToCollection(ctx context.Context, collectionID string, itemIDs []string) error {
	_, err := c.jellyfin.CollectionAPI.AddToCollection(ctx, collectionID).
		Ids(itemIDs).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to add items to collection %s: %w", collectionID, err)
	}
	return nil
}

// RemoveItemsFromCollection removes items from an existing collection.
func (c *Client) RemoveItemsFromCollection(ctx context.Context, collectionID string, itemIDs []string) error {
	_, err := c.jellyfin.CollectionAPI.RemoveFromCollection(ctx, collectionID).
		Ids(itemIDs).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to remove items from collection %s: %w", collectionID, err)
	}
	return nil
}
