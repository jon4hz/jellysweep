package moviereleasefilter

import (
	"context"
	"testing"
	"time"

	"github.com/devopsarr/radarr-go/radarr"
	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
)

func TestApplyFiltersMoviesByReleaseDateMax(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {
				Filter: config.FilterConfig{
					MovieReleaseDateMax: "2024-01-01",
				},
			},
		},
	}

	acceptedMovieDate := time.Date(2023, time.December, 31, 0, 0, 0, 0, time.UTC)
	rejectedMovieDate := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	acceptedMovie := radarr.MovieResource{}
	acceptedMovie.SetReleaseDate(acceptedMovieDate)
	rejectedMovie := radarr.MovieResource{}
	rejectedMovie.SetReleaseDate(rejectedMovieDate)

	items := []arr.MediaItem{
		{
			Title:         "Accepted Movie",
			LibraryName:   "Movies",
			MediaType:     models.MediaTypeMovie,
			MovieResource: acceptedMovie,
		},
		{
			Title:         "Rejected Movie",
			LibraryName:   "Movies",
			MediaType:     models.MediaTypeMovie,
			MovieResource: rejectedMovie,
		},
		{
			Title:          "TV Show",
			LibraryName:    "Movies",
			MediaType:      models.MediaTypeTV,
			SeriesResource: sonarr.SeriesResource{},
		},
	}

	filtered, err := New(cfg).Apply(context.Background(), items)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered items, got %d", len(filtered))
	}
	if filtered[0].Title != "Accepted Movie" {
		t.Fatalf("expected movie before maximum date to pass filter, got %q", filtered[0].Title)
	}
	if filtered[1].Title != "TV Show" {
		t.Fatalf("expected TV item to pass filter, got %q", filtered[1].Title)
	}
}

func TestApplyIncludesMoviesWhenThresholdDisabled(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {},
		},
	}

	item := arr.MediaItem{
		Title:       "No Release Date",
		LibraryName: "Movies",
		MediaType:   models.MediaTypeMovie,
	}

	filtered, err := New(cfg).Apply(context.Background(), []arr.MediaItem{item})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if len(filtered) != 1 {
		t.Fatalf("expected movie to pass when maximum release date is disabled, got %d items", len(filtered))
	}
}

func TestMovieReleaseDateUsesEarliestKnownReleaseDate(t *testing.T) {
	movie := radarr.MovieResource{}
	releaseDate := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	inCinemas := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	digitalRelease := time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC)

	movie.SetReleaseDate(releaseDate)
	movie.SetInCinemas(inCinemas)
	movie.SetDigitalRelease(digitalRelease)

	got, ok := movieReleaseDate(movie)
	if !ok {
		t.Fatal("expected release date to be found")
	}
	if !got.Equal(inCinemas) {
		t.Fatalf("expected earliest release date %s, got %s", inCinemas, got)
	}
}
