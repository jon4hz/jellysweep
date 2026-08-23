package engine

import (
	"context"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/engine/jellyfin"
	jellyfinAPI "github.com/sj14/jellyfin-go/api"
)

// jellyfinClient is the subset of *jellyfin.Client used by the engine.
type jellyfinClient interface {
	GetJellyfinItems(ctx context.Context) ([]arr.JellyfinItem, error)
	RemoveItemWithCleanupMode(ctx context.Context, itemID, title string, itemType jellyfinAPI.BaseItemKind, cleanupMode config.CleanupMode, keepCount int) error
	FindCollectionByName(ctx context.Context, name string) (string, error)
	GetCollectionItems(ctx context.Context, collectionID string) (map[string]bool, error)
	CreateCollection(ctx context.Context, name string, itemIDs []string) error
	AddItemsToCollection(ctx context.Context, collectionID string, itemIDs []string) error
	RemoveItemsFromCollection(ctx context.Context, collectionID string, itemIDs []string) error
}

var _ jellyfinClient = (*jellyfin.Client)(nil)
