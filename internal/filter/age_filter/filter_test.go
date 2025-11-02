package agefilter

import (
	"context"
	"testing"
	"time"

	"github.com/devopsarr/radarr-go/radarr"
	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	dbmock "github.com/jon4hz/jellysweep/internal/database/mock"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	arrmock "github.com/jon4hz/jellysweep/internal/engine/arr/mock"
	"github.com/samber/lo"
)

func TestFilter_Apply(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		mediaItems   []arr.MediaItem
		deletedMedia []database.Media
		addedDates   map[int32]*time.Time
		ageThreshold int
		want         int // number of items we expect to keep
		wantErr      bool
	}{
		{
			name: "movie newer than threshold - excluded",
			mediaItems: []arr.MediaItem{
				{
					Title:       "Recent Movie",
					MediaType:   models.MediaTypeMovie,
					TmdbId:      123,
					LibraryName: "Movies",
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(1)),
					},
				},
			},
			addedDates: map[int32]*time.Time{
				1: lo.ToPtr(now.AddDate(0, 0, -10)), // Added 10 days ago
			},
			ageThreshold: 30, // Threshold is 30 days
			want:         0,  // Should be excluded (too recent)
		},
		{
			name: "movie older than threshold - included",
			mediaItems: []arr.MediaItem{
				{
					Title:       "Old Movie",
					MediaType:   models.MediaTypeMovie,
					TmdbId:      124,
					LibraryName: "Movies",
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(2)),
					},
				},
			},
			addedDates: map[int32]*time.Time{
				2: lo.ToPtr(now.AddDate(0, 0, -60)), // Added 60 days ago
			},
			ageThreshold: 30, // Threshold is 30 days
			want:         1,  // Should be included (old enough)
		},
		{
			name: "tv show newer than threshold - excluded",
			mediaItems: []arr.MediaItem{
				{
					Title:       "Recent TV Show",
					MediaType:   models.MediaTypeTV,
					TvdbId:      456,
					LibraryName: "TV Shows",
					SeriesResource: sonarr.SeriesResource{
						Id: lo.ToPtr(int32(3)),
					},
				},
			},
			addedDates: map[int32]*time.Time{
				3: lo.ToPtr(now.AddDate(0, 0, -5)), // Added 5 days ago
			},
			ageThreshold: 30,
			want:         0, // Should be excluded
		},
		{
			name: "tv show older than threshold - included",
			mediaItems: []arr.MediaItem{
				{
					Title:       "Old TV Show",
					MediaType:   models.MediaTypeTV,
					TvdbId:      457,
					LibraryName: "TV Shows",
					SeriesResource: sonarr.SeriesResource{
						Id: lo.ToPtr(int32(4)),
					},
				},
			},
			addedDates: map[int32]*time.Time{
				4: lo.ToPtr(now.AddDate(0, 0, -45)), // Added 45 days ago
			},
			ageThreshold: 30,
			want:         1, // Should be included
		},
		{
			name: "no added date - included",
			mediaItems: []arr.MediaItem{
				{
					Title:       "Unknown Date Movie",
					MediaType:   models.MediaTypeMovie,
					TmdbId:      125,
					LibraryName: "Movies",
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(5)),
					},
				},
			},
			addedDates:   map[int32]*time.Time{},
			ageThreshold: 30,
			want:         1, // Should be included (no date found)
		},
		{
			name: "no library config - included",
			mediaItems: []arr.MediaItem{
				{
					Title:       "No Config Movie",
					MediaType:   models.MediaTypeMovie,
					TmdbId:      126,
					LibraryName: "Unknown Library",
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(6)),
					},
				},
			},
			addedDates: map[int32]*time.Time{
				6: lo.ToPtr(now.AddDate(0, 0, -10)),
			},
			ageThreshold: 30,
			want:         1, // Should be included (no config)
		},
		{
			name: "previously deleted media - uses deletion date as reference",
			mediaItems: []arr.MediaItem{
				{
					Title:       "Previously Deleted Movie",
					MediaType:   models.MediaTypeMovie,
					TmdbId:      127,
					LibraryName: "Movies",
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(7)),
					},
				},
			},
			deletedMedia: []database.Media{
				{
					TmdbId: lo.ToPtr(int32(127)),
				},
			},
			addedDates: map[int32]*time.Time{
				7: lo.ToPtr(now.AddDate(0, 0, -10)), // Re-added 10 days ago
			},
			ageThreshold: 30,
			want:         0, // Should be excluded (only 10 days since re-add)
		},
		{
			name: "mixed ages",
			mediaItems: []arr.MediaItem{
				{
					Title:       "Old Movie 1",
					MediaType:   models.MediaTypeMovie,
					TmdbId:      201,
					LibraryName: "Movies",
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(101)),
					},
				},
				{
					Title:       "Recent Movie 2",
					MediaType:   models.MediaTypeMovie,
					TmdbId:      202,
					LibraryName: "Movies",
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(102)),
					},
				},
				{
					Title:       "Old Movie 3",
					MediaType:   models.MediaTypeMovie,
					TmdbId:      203,
					LibraryName: "Movies",
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(103)),
					},
				},
			},
			addedDates: map[int32]*time.Time{
				101: lo.ToPtr(now.AddDate(0, 0, -60)),
				102: lo.ToPtr(now.AddDate(0, 0, -10)),
				103: lo.ToPtr(now.AddDate(0, 0, -40)),
			},
			ageThreshold: 30,
			want:         2, // Only 2 old enough
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			mockDB := dbmock.NewMockDB()
			mockSonarr := arrmock.NewMockArrer()
			mockRadarr := arrmock.NewMockArrer()

			// Add deleted media to mock DB
			for _, dm := range tt.deletedMedia {
				mockDB.AddDeletedMedia(dm, now.AddDate(0, 0, -50))
			}

			// Set up added dates in mocks
			for itemID, date := range tt.addedDates {
				mockSonarr.SetItemAddedDate(itemID, date)
				mockRadarr.SetItemAddedDate(itemID, date)
			}

			// Create config
			cfg := &config.Config{
				Libraries: map[string]*config.CleanupConfig{
					"Movies": {
						Filter: config.FilterConfig{
							ContentAgeThreshold: tt.ageThreshold,
						},
					},
					"TV Shows": {
						Filter: config.FilterConfig{
							ContentAgeThreshold: tt.ageThreshold,
						},
					},
				},
			}

			// Create filter
			f := New(cfg, mockDB, mockSonarr, mockRadarr)

			// Apply filter
			got, err := f.Apply(context.Background(), tt.mediaItems)
			if (err != nil) != tt.wantErr {
				t.Errorf("Apply() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(got) != tt.want {
				t.Errorf("Apply() returned %d items, want %d", len(got), tt.want)
			}
		})
	}
}

func TestFilter_String(t *testing.T) {
	f := New(nil, nil, nil, nil)
	if got := f.String(); got != "Age Filter" {
		t.Errorf("String() = %v, want %v", got, "Age Filter")
	}
}
