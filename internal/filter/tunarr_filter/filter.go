package tunarrfilter

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/filter"
	"github.com/jon4hz/jellysweep/pkg/tunarr"
)

// Filter implements the filter.Filterer interface for Tunarr.
type Filter struct {
	client *tunarr.Client
	cfg    *config.Config
}

var _ filter.Filterer = (*Filter)(nil)

// New creates a new Tunarr Filter instance.
func New(cfg *config.Config) (*Filter, error) {
	if cfg.Tunarr == nil {
		return nil, fmt.Errorf("tunarr configuration is required")
	}

	return &Filter{
		client: tunarr.New(cfg.Tunarr),
		cfg:    cfg,
	}, nil
}

// String returns the name of the filter.
func (f *Filter) String() string { return "Tunarr Filter" }

// ChannelPrograms represents all programs across all channels, indexed for efficient lookup.
type ChannelPrograms struct {
	// Map of Jellyfin ID (externalKey) to whether it's in use
	jellyfinMovies map[string]bool
	// Map of Jellyfin Show ID to whether any episode from that show is in use
	jellyfinShows map[string]bool
}

// fetchAllChannelPrograms retrieves all programs from all channels and indexes them.
func (f *Filter) fetchAllChannelPrograms(ctx context.Context) (*ChannelPrograms, error) {
	log.Debug("Fetching all Tunarr channels")

	channels, err := f.client.GetChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get channels: %w", err)
	}

	log.Debugf("Found %d Tunarr channels", len(channels))

	cp := &ChannelPrograms{
		jellyfinMovies: make(map[string]bool),
		jellyfinShows:  make(map[string]bool),
	}

	// Fetch programs from all channels
	for _, channel := range channels {
		log.Debugf("Fetching programs for channel: %s (ID: %s)", channel.Name, channel.ID)

		programs, err := f.client.GetAllChannelPrograms(ctx, channel.ID)
		if err != nil {
			log.Warnf("Failed to get programs for channel %s: %v", channel.Name, err)
			continue
		}

		log.Debugf("Found %d programs in channel %s", len(programs), channel.Name)

		// Index programs by their Jellyfin IDs
		for _, program := range programs {
			// Only process content from Jellyfin
			if strings.ToLower(program.ExternalSourceType) != "jellyfin" { //nolint:goconst
				continue
			}

			// Process movies
			if program.Subtype == "movie" {
				// Use the externalKey (Jellyfin item ID) as the identifier
				if program.ExternalKey != "" {
					cp.jellyfinMovies[program.ExternalKey] = true
					// log.Debugf("Indexed movie: %s (Jellyfin ID: %s)", program.Title, program.ExternalKey)
				}
			}

			// Process episodes - we care about the show ID
			if program.Subtype == "episode" {
				// Get the show ID from grandparent or ShowID field
				var showID string
				if program.Grandparent != nil && program.Grandparent.ExternalKey != "" {
					showID = program.Grandparent.ExternalKey
				} else if program.ShowID != "" {
					showID = program.ShowID
				}

				// Also check external IDs for Jellyfin multi-type IDs
				if showID == "" {
					for _, extID := range program.ExternalIDs {
						if extID.Type == "multi" && strings.ToLower(extID.Source) == "jellyfin" {
							// For episodes, we want the show ID, which might be in the parent
							if program.Grandparent != nil {
								for _, parentExtID := range program.Grandparent.ExternalIDs {
									if parentExtID.Type == "multi" && strings.ToLower(parentExtID.Source) == "jellyfin" {
										showID = parentExtID.ID
										break
									}
								}
							}
							break
						}
					}
				}

				if showID != "" {
					cp.jellyfinShows[showID] = true
					// log.Debugf("Indexed episode from show: %s (Show Jellyfin ID: %s)", program.Title, showID)
				}
			}
		}
	}

	log.Infof("Indexed %d movies and %d TV shows from Tunarr channels",
		len(cp.jellyfinMovies), len(cp.jellyfinShows))

	return cp, nil
}

// Apply filters media items based on whether they're being used in Tunarr channels.
// For movies: checks if the movie's Jellyfin ID is in any channel.
// For TV shows: checks if any episode from the series is in any channel.
// Respects per-library tunarr_enabled setting in filter configuration.
func (f *Filter) Apply(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	// Fetch all channel programs once
	channelPrograms, err := f.fetchAllChannelPrograms(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch channel programs: %w", err)
	}

	filteredItems := make([]arr.MediaItem, 0)

	for _, item := range mediaItems {
		skip := false

		// Check if Tunarr filter is enabled for this library
		libraryConfig := f.cfg.GetLibraryConfig(item.LibraryName)
		tunarrEnabled := false
		if libraryConfig != nil {
			tunarrEnabled = libraryConfig.Filter.TunarrEnabled
		}

		// Only apply Tunarr filtering if enabled for this library
		if tunarrEnabled {
			switch item.MediaType {
			case models.MediaTypeMovie:
				// Check if this movie is in any Tunarr channel
				if item.JellyfinID != "" && channelPrograms.jellyfinMovies[item.JellyfinID] {
					log.Debug("Excluding movie due to tunarr usage", "movie", item.Title, "library", item.LibraryName, "jellyfinID", item.JellyfinID)
					skip = true
				}

			case models.MediaTypeTV:
				// Check if any episode from this series is in any Tunarr channel
				// For series, we need to check the series' Jellyfin ID
				if item.JellyfinID != "" && channelPrograms.jellyfinShows[item.JellyfinID] {
					log.Debug("Excluding series due to tunarr usage", "series", item.Title, "library", item.LibraryName, "jellyfinID", item.JellyfinID)
					skip = true
				}
			}
		}

		if !skip {
			filteredItems = append(filteredItems, item)
			if tunarrEnabled {
				log.Debug("Including item not used by tunarr", "item", item.Title, "library", item.LibraryName, "jellyfinID", item.JellyfinID)
			}
		}
	}

	return filteredItems, nil
}
