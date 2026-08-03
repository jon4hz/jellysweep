package moviereleasefilter

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
	"github.com/devopsarr/radarr-go/radarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/jon4hz/jellysweep/internal/filter"
)

// Filter implements the filter.Filterer interface.
type Filter struct {
	cfg *config.Config
}

var _ filter.Filterer = (*Filter)(nil)

// New creates a new movie release Filter instance.
func New(cfg *config.Config) *Filter {
	return &Filter{
		cfg: cfg,
	}
}

// String returns the name of the filter.
func (f *Filter) String() string { return "Movie Release Filter" }

// Apply filters movie items based on their release date. Non-movie items pass through unchanged.
func (f *Filter) Apply(ctx context.Context, mediaItems []arr.MediaItem) ([]arr.MediaItem, error) {
	filteredItems := make([]arr.MediaItem, 0, len(mediaItems))

	for _, item := range mediaItems {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if item.MediaType != models.MediaTypeMovie {
			filteredItems = append(filteredItems, item)
			continue
		}

		libraryConfig := f.cfg.GetLibraryConfig(item.LibraryName)
		if libraryConfig == nil {
			filteredItems = append(filteredItems, item)
			log.Debug("no library config, including movie for deletion", "library", item.LibraryName, "title", item.Title)
			continue
		}

		releaseDateMax, err := libraryConfig.GetMovieReleaseDateMax()
		if err != nil {
			return nil, err
		}
		if releaseDateMax == nil {
			filteredItems = append(filteredItems, item)
			log.Debug("no movie release date maximum configured, including movie for deletion", "library", item.LibraryName, "title", item.Title)
			continue
		}

		releaseDate, ok := movieReleaseDate(item.MovieResource)
		if !ok {
			log.Debug("excluding movie without release date", "title", item.Title, "library", item.LibraryName)
			continue
		}

		if releaseDate.Before(*releaseDateMax) {
			filteredItems = append(filteredItems, item)
			log.Debug("including movie released before maximum date", "title", item.Title, "releaseDate", releaseDate.Format(time.RFC3339), "releaseDateMax", releaseDateMax.Format(time.RFC3339))
			continue
		}

		log.Debug("excluding movie released on or after maximum date", "title", item.Title, "releaseDate", releaseDate.Format(time.RFC3339), "releaseDateMax", releaseDateMax.Format(time.RFC3339))
	}

	return filteredItems, nil
}

func movieReleaseDate(movie radarr.MovieResource) (time.Time, bool) {
	var releaseDate time.Time

	for _, candidate := range []time.Time{
		movie.GetReleaseDate(),
		movie.GetInCinemas(),
		movie.GetDigitalRelease(),
		movie.GetPhysicalRelease(),
	} {
		if candidate.IsZero() {
			continue
		}
		if releaseDate.IsZero() || candidate.Before(releaseDate) {
			releaseDate = candidate
		}
	}

	if releaseDate.IsZero() {
		return time.Time{}, false
	}

	return releaseDate, true
}
