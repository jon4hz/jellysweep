package databasefilter

import (
	"context"
	"testing"
	"time"

	"github.com/devopsarr/radarr-go/radarr"
	"github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/database"
	dbmock "github.com/jon4hz/jellysweep/internal/database/mock"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/samber/lo"
)

func TestFilter_Apply(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		mediaItems []arr.MediaItem
		dbItems    []database.Media
		want       int // number of items we expect after filtering
		wantErr    bool
	}{
		{
			name: "item already in database - excluded",
			mediaItems: []arr.MediaItem{
				{
					Title:     "Movie 1",
					MediaType: models.MediaTypeMovie,
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(1)),
					},
				},
			},
			dbItems: []database.Media{
				{
					ArrID:     1,
					MediaType: database.MediaTypeMovie,
				},
			},
			want: 0, // Should be excluded
		},
		{
			name: "item not in database - included",
			mediaItems: []arr.MediaItem{
				{
					Title:     "Movie 2",
					MediaType: models.MediaTypeMovie,
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(2)),
					},
				},
			},
			dbItems: []database.Media{},
			want:    1, // Should be included
		},
		{
			name: "tv show already in database - excluded",
			mediaItems: []arr.MediaItem{
				{
					Title:     "TV Show 1",
					MediaType: models.MediaTypeTV,
					SeriesResource: sonarr.SeriesResource{
						Id: lo.ToPtr(int32(10)),
					},
				},
			},
			dbItems: []database.Media{
				{
					ArrID:     10,
					MediaType: database.MediaTypeTV,
				},
			},
			want: 0, // Should be excluded
		},
		{
			name: "tv show not in database - included",
			mediaItems: []arr.MediaItem{
				{
					Title:     "TV Show 2",
					MediaType: models.MediaTypeTV,
					SeriesResource: sonarr.SeriesResource{
						Id: lo.ToPtr(int32(11)),
					},
				},
			},
			dbItems: []database.Media{},
			want:    1, // Should be included
		},
		{
			name: "mixed - some in database, some not",
			mediaItems: []arr.MediaItem{
				{
					Title:     "Movie 1",
					MediaType: models.MediaTypeMovie,
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(1)),
					},
				},
				{
					Title:     "Movie 2",
					MediaType: models.MediaTypeMovie,
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(2)),
					},
				},
				{
					Title:     "TV Show 1",
					MediaType: models.MediaTypeTV,
					SeriesResource: sonarr.SeriesResource{
						Id: lo.ToPtr(int32(10)),
					},
				},
			},
			dbItems: []database.Media{
				{
					ArrID:     1,
					MediaType: database.MediaTypeMovie,
				},
				{
					ArrID:     10,
					MediaType: database.MediaTypeTV,
				},
			},
			want: 1, // Only Movie 2 should be included
		},
		{
			name: "different media types with same arr id - not excluded",
			mediaItems: []arr.MediaItem{
				{
					Title:     "Movie 1",
					MediaType: models.MediaTypeMovie,
					MovieResource: radarr.MovieResource{
						Id: lo.ToPtr(int32(1)),
					},
				},
			},
			dbItems: []database.Media{
				{
					ArrID:     1,
					MediaType: database.MediaTypeTV, // Different media type
				},
			},
			want: 1, // Should be included (different media type)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock
			mockDB := dbmock.NewMockDB()

			// Add items to mock database
			for _, item := range tt.dbItems {
				item.DefaultDeleteAt = now
				_ = mockDB.CreateMediaItems(context.Background(), []database.Media{item})
			}

			// Create filter
			f := New(mockDB)

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
	f := New(nil)
	if got := f.String(); got != "Database Filter" {
		t.Errorf("String() = %v, want %v", got, "Database Filter")
	}
}
