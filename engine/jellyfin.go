package engine

import (
	"context"
	"fmt"

	"github.com/ccoveille/go-safecast"
	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/config"
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
	clientConfig.UserAgent = fmt.Sprintf("Jellysweep/%s", version.Version)
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
