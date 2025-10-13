package jellyfin

import (
	"context"
	"fmt"

	"github.com/ccoveille/go-safecast"
	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/cache"
	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine/arr"
	"github.com/jon4hz/jellysweep/version"
	jellyfin "github.com/sj14/jellyfin-go/api"
)

// Client provides a high-level interface for interacting with Jellyfin.
type Client struct {
	jellyfin   *jellyfin.APIClient
	cfg        *config.Config
	itemsCache *cache.PrefixedCache[cache.JellyfinItemsData]
}

// New creates a new Jellyfin client with the given configuration and cache.
func New(cfg *config.Config, itemsCache *cache.PrefixedCache[cache.JellyfinItemsData]) *Client {
	return &Client{
		jellyfin:   newJellyfinClient(cfg.Jellyfin),
		cfg:        cfg,
		itemsCache: itemsCache,
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
func (c *Client) GetJellyfinItems(ctx context.Context, forceRefresh bool) ([]arr.JellyfinItem, map[string][]string, error) {
	if forceRefresh {
		if err := c.itemsCache.Clear(ctx); err != nil {
			log.Debug("Failed to clear jellyfin items cache, fetching from API", "error", err)
		}
	}

	cachedData, err := c.itemsCache.Get(ctx, "all")
	if err != nil {
		log.Debug("Failed to get Jellyfin items from cache, fetching from API", "error", err)
	}
	if len(cachedData.Items) != 0 && !forceRefresh {
		// Convert cached JellyfinItems to arr.JellyfinItems
		arrItems := make([]arr.JellyfinItem, len(cachedData.Items))
		for i, item := range cachedData.Items {
			arrItems[i] = arr.JellyfinItem{
				BaseItemDto:       item.BaseItemDto,
				ParentLibraryName: item.ParentLibraryName,
			}
		}
		return arrItems, cachedData.LibraryFoldersMap, nil
	}

	allItems, err := c.fetchJellyfinItems(ctx)
	if err != nil {
		return nil, nil, err
	}

	libraryFoldersMap, err := c.GetLibraryFoldersMap(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Convert to cache format and store
	cacheItems := make([]cache.JellyfinItem, len(allItems))
	for i, item := range allItems {
		cacheItems[i] = cache.JellyfinItem{
			BaseItemDto:       item.BaseItemDto,
			ParentLibraryName: item.ParentLibraryName,
		}
	}

	cacheData := cache.JellyfinItemsData{
		Items:             cacheItems,
		LibraryFoldersMap: libraryFoldersMap,
	}

	if err := c.itemsCache.Set(ctx, "all", cacheData); err != nil {
		log.Warnf("Failed to cache Jellyfin items: %v", err)
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
