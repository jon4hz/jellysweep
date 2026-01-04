package jellyfin

import (
	"context"
	"fmt"

	"github.com/ccoveille/go-safecast"
	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/version"
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
				jellyfin.ITEMFIELDS_PROVIDER_IDS,
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
		itemsLen, err := safecast.Convert[int32](len(items))
		if err != nil {
			return nil, fmt.Errorf("failed to cast items length: %w", err)
		}
		if itemsLen > 0 && startIndex+itemsLen >= totalRecordCount {
			log.Debug("Retrieved all items from library", "library", libraryName, "total", totalRecordCount)
			break
		}

		// Move to next batch
		itemsCount, err := safecast.Convert[int32](len(items))
		if err != nil {
			return nil, fmt.Errorf("failed to cast items count: %w", err)
		}
		startIndex += itemsCount
	}

	log.Info("Retrieved all items from library", "library", libraryName, "total", len(allItems))
	return allItems, nil
}

// RemoveItem removes an item from Jellyfin by its ID.
func (c *Client) RemoveItem(ctx context.Context, itemID string) error {
	_, err := c.jellyfin.LibraryAPI.DeleteItem(ctx, itemID).Execute()
	if err != nil {
		return fmt.Errorf("failed to remove item %s: %w", itemID, err)
	}
	return nil
}

// GetEpisodes retrieves all episodes for a specific series from Jellyfin.
// It first fetches all seasons, then retrieves episodes from each season (skipping "Specials").
func (c *Client) GetEpisodes(ctx context.Context, seriesID string) ([]jellyfin.BaseItemDto, []string, error) {
	allEpisodes := make([]jellyfin.BaseItemDto, 0)
	seasonsWithoutEpisodes := make([]string, 0)

	// First, get all seasons for this series
	seasons, err := c.GetSeasons(ctx, seriesID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get seasons for series %s: %w", seriesID, err)
	}

	log.Debug("Fetching episodes from seasons", "seriesID", seriesID, "seasonCount", len(seasons))

	// Fetch episodes from each season
	for _, season := range seasons {
		if season.Id == nil {
			continue
		}

		seasonName := season.GetName()
		seasonID := season.GetId()

		// Skip "Specials" season
		if seasonName == "Specials" {
			log.Debug("Skipping Specials season", "seriesID", seriesID, "seasonID", seasonID)
			continue
		}

		log.Debug("Fetching episodes from season", "seriesID", seriesID, "seasonID", seasonID, "seasonName", seasonName)

		// Get episodes for this season with pagination
		startIndex := int32(0)
		limit := int32(1000)

		for {
			episodesResp, _, err := c.jellyfin.ItemsAPI.GetItems(ctx).
				ParentId(seasonID).
				StartIndex(startIndex).
				Limit(limit).
				Fields([]jellyfin.ItemFields{
					jellyfin.ITEMFIELDS_PATH,
					jellyfin.ITEMFIELDS_PARENT_ID,
					jellyfin.ITEMFIELDS_MEDIA_SOURCES,
				}).
				IncludeItemTypes([]jellyfin.BaseItemKind{
					jellyfin.BASEITEMKIND_EPISODE,
				}).
				Execute()
			if err != nil {
				log.Warnf("Failed to get episodes for season %s (%s): %v", seasonName, seasonID, err)
				break
			}

			episodes := episodesResp.GetItems()
			if len(episodes) == 0 {
				seasonsWithoutEpisodes = append(seasonsWithoutEpisodes, seasonID)
				log.Debug("No more episodes found for season", "seasonID", seasonID, "seasonName", seasonName)
				break
			}

			log.Debug("Retrieved episodes from season", "seasonID", seasonID, "seasonName", seasonName, "count", len(episodes))

			// Add episodes to our collection
			allEpisodes = append(allEpisodes, episodes...)

			// Check if we've gotten all episodes from this season
			totalRecordCount := episodesResp.GetTotalRecordCount()
			episodesCount, err := safecast.Convert[int32](len(episodes))
			if err != nil {
				return nil, nil, fmt.Errorf("failed to cast episodes length: %w", err)
			}
			if episodesCount > 0 && startIndex+episodesCount >= totalRecordCount {
				log.Debug("Retrieved all episodes from season", "seasonID", seasonID, "seasonName", seasonName, "total", totalRecordCount)
				break
			}
			startIndex += episodesCount
		}
	}

	log.Info("Retrieved all episodes for series", "seriesID", seriesID, "total", len(allEpisodes))
	return allEpisodes, seasonsWithoutEpisodes, nil
}

// GetSeasons retrieves all seasons for a specific series from Jellyfin.
func (c *Client) GetSeasons(ctx context.Context, seriesID string) ([]jellyfin.BaseItemDto, error) {
	var allSeasons []jellyfin.BaseItemDto

	// Get seasons from this series
	seasonsResp, _, err := c.jellyfin.ItemsAPI.GetItems(ctx).
		ParentId(seriesID).
		Fields([]jellyfin.ItemFields{
			jellyfin.ITEMFIELDS_PATH,
			jellyfin.ITEMFIELDS_PARENT_ID,
		}).
		IncludeItemTypes([]jellyfin.BaseItemKind{
			jellyfin.BASEITEMKIND_SEASON,
		}).
		Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to get seasons for series %s: %w", seriesID, err)
	}

	allSeasons = seasonsResp.GetItems()
	log.Info("Retrieved all seasons for series", "seriesID", seriesID, "total", len(allSeasons))
	return allSeasons, nil
}

// FindCollectionByName searches for a collection by name and returns its ID.
func (c *Client) FindCollectionByName(ctx context.Context, name string) (string, error) {
	// Get all collections
	result, _, err := c.jellyfin.ItemsAPI.GetItems(ctx).
		IncludeItemTypes([]jellyfin.BaseItemKind{jellyfin.BASEITEMKIND_BOX_SET}).
		Recursive(true).
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
		Recursive(true).
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
// Items are added in batches to avoid URL length limitations.
func (c *Client) CreateCollection(ctx context.Context, name string, itemIDs []string) error {
	const batchSize = 50

	// Create the collection with the first batch of items
	var initialItems []string
	if len(itemIDs) > 0 {
		if len(itemIDs) <= batchSize {
			initialItems = itemIDs
		} else {
			initialItems = itemIDs[:batchSize]
		}
	}

	collectionResp, _, err := c.jellyfin.CollectionAPI.CreateCollection(ctx).
		Name(name).
		Ids(initialItems).
		Execute()
	if err != nil {
		return fmt.Errorf("failed to create collection %s: %w", name, err)
	}

	// If there are more items, add them in batches
	if len(itemIDs) > batchSize {
		collectionID := collectionResp.GetId()
		for i := batchSize; i < len(itemIDs); i += batchSize {
			end := min(i+batchSize, len(itemIDs))
			batch := itemIDs[i:end]

			_, err := c.jellyfin.CollectionAPI.AddToCollection(ctx, collectionID).
				Ids(batch).
				Execute()
			if err != nil {
				return fmt.Errorf("failed to add items to collection %s (batch %d-%d): %w", name, i, end, err)
			}
		}
	}

	return nil
}

// AddItemsToCollection adds items to an existing collection.
// Items are added in batches to avoid URL length limitations.
func (c *Client) AddItemsToCollection(ctx context.Context, collectionID string, itemIDs []string) error {
	const batchSize = 50

	// Process items in batches
	for i := 0; i < len(itemIDs); i += batchSize {
		end := min(i+batchSize, len(itemIDs))
		batch := itemIDs[i:end]

		_, err := c.jellyfin.CollectionAPI.AddToCollection(ctx, collectionID).
			Ids(batch).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to add items to collection %s (batch %d-%d): %w", collectionID, i, end, err)
		}
	}

	return nil
}

// RemoveItemsFromCollection removes items from an existing collection.
// Items are removed in batches to avoid URL length limitations.
func (c *Client) RemoveItemsFromCollection(ctx context.Context, collectionID string, itemIDs []string) error {
	const batchSize = 50

	// Process items in batches
	for i := 0; i < len(itemIDs); i += batchSize {
		end := min(i+batchSize, len(itemIDs))
		batch := itemIDs[i:end]

		_, err := c.jellyfin.CollectionAPI.RemoveFromCollection(ctx, collectionID).
			Ids(batch).
			Execute()
		if err != nil {
			return fmt.Errorf("failed to remove items from collection %s (batch %d-%d): %w", collectionID, i, end, err)
		}
	}

	return nil
}
