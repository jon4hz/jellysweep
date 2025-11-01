package engine

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	jellyfin "github.com/sj14/jellyfin-go/api"
)

func (e *Engine) cleanupMedia(ctx context.Context) error {
	deletedItems := make(map[string][]arr.MediaItem)

	mediaItems, err := e.db.GetMediaItems(ctx, false)
	if err != nil {
		log.Errorf("failed to get media items from database: %v", err)
		return err
	}

	for _, item := range mediaItems {
		// since the deletion policies were already set during the scaning phase, we can just use the existing policy engine.
		if ok, err := e.policy.ShouldTriggerDeletion(ctx, item); err != nil {
			log.Errorf("failed to check deletion policy for media item %s: %v", item.Title, err)
			continue
		} else if !ok {
			log.Infof("Skipping deletion for media item %s, no policies triggered", item.Title)
			continue
		}

		if e.cfg.DryRun {
			log.Info("[Dry Run] Would delete media item", "title", item.Title, "library", item.LibraryName)
			continue
		}

		switch item.MediaType {
		case database.MediaTypeTV:
			if e.sonarr == nil {
				log.Warnf("Sonarr client not configured, cannot delete TV show %s", item.Title)
				continue
			}
			if err := e.sonarr.DeleteMedia(ctx, item.ArrID, item.Title); err != nil {
				log.Errorf("failed to delete Sonarr media %s: %v", item.Title, err)
				continue
			}

			// Also remove from Jellyfin according to cleanup mode
			if err := e.removeJellyfinItem(ctx, item); err != nil {
				log.Errorf("failed to remove Jellyfin item %s: %v", item.Title, err)
				// Continue even if Jellyfin removal fails, as Sonarr deletion succeeded
			}

			deletedItems["TV Shows"] = append(deletedItems["TV Shows"], arr.MediaItem{
				Title:     item.Title,
				Year:      item.Year,
				MediaType: models.MediaTypeTV,
			})

		case database.MediaTypeMovie:
			if e.radarr == nil {
				log.Warnf("Radarr client not configured, cannot delete movie %s", item.Title)
				continue
			}
			if err := e.radarr.DeleteMedia(ctx, item.ArrID, item.Title); err != nil {
				log.Errorf("failed to delete Radarr media %s: %v", item.Title, err)
				continue
			}

			// Also remove from Jellyfin (always entire movie)
			if err := e.removeJellyfinItem(ctx, item); err != nil {
				log.Errorf("failed to remove Jellyfin item %s: %v", item.Title, err)
				// Continue even if Jellyfin removal fails, as Radarr deletion succeeded
			}

			deletedItems["Movies"] = append(deletedItems["Movies"], arr.MediaItem{
				Title:     item.Title,
				Year:      item.Year,
				MediaType: models.MediaTypeMovie,
			})

		default:
			log.Errorf("unsupported media type for deletion: %s", item.MediaType)
			continue
		}
		item.DBDeleteReason = database.DBDeleteReasonDefault

		if err := e.db.DeleteMediaItem(ctx, &item); err != nil {
			log.Errorf("failed to delete media item %s from database: %v", item.Title, err)
			continue
		}

		if err := e.CreateDeletedEvent(ctx, &item); err != nil {
			log.Errorf("failed to create deletion event for %s: %v", item.Title, err)
		}
	}

	// Send completion notification if any items were deleted
	if len(deletedItems) > 0 {
		if err := e.sendNtfyDeletionCompletedNotification(ctx, deletedItems); err != nil {
			log.Errorf("failed to send deletion completed notification: %v", err)
		}
	}

	return nil
}

func (e *Engine) removeJellyfinItem(ctx context.Context, item database.Media) error {
	// Determine the Jellyfin item type based on media type
	var itemType jellyfin.BaseItemKind
	switch item.MediaType {
	case database.MediaTypeMovie:
		itemType = jellyfin.BASEITEMKIND_MOVIE
	case database.MediaTypeTV:
		itemType = jellyfin.BASEITEMKIND_SERIES
	default:
		log.Warnf("Unknown media type for Jellyfin cleanup: %s", item.MediaType)
		return nil
	}

	// Use the new cleanup engine that respects cleanup modes
	cleanupMode := e.cfg.GetCleanupMode()
	keepCount := e.cfg.GetKeepCount()

	if err := e.jellyfin.RemoveItemWithCleanupMode(ctx, item.JellyfinID, item.Title, itemType, cleanupMode, keepCount); err != nil {
		log.Error("failed to remove jellyfin item", "jellyfinID", item.JellyfinID, "error", err)
		return err
	}

	return nil
}
