package engine

import (
	"context"
	"testing"

	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/stretchr/testify/require"
)

type args struct {
	eventType config.EventType
	message   string
}

type testTelegramClient struct {
	Calls []args
}

func (t *testTelegramClient) SendNotification(ctx context.Context, eventType config.EventType, message string) error {
	t.Calls = append(t.Calls, args{eventType, message})
	return nil
}

func (t *testTelegramClient) Called(tT *testing.T, eventType config.EventType, message string) {
	require.Contains(tT, t.Calls, args{eventType, message})
}

func TestNotification_sendTelegramDeletionCompletedNotification(t *testing.T) {
	telegramClient := &testTelegramClient{}
	cfg := &config.Config{
		Jellyfin: &config.JellyfinConfig{
			URL: "http://jellyfin:80",
		},
	}

	deletedItems := map[string][]arr.MediaItem{
		"library 1": []arr.MediaItem{
			{
				JellyfinID: "1",
				MediaType:  models.MediaTypeMovie,
				Title:      "movie 1",
			},
			{
				JellyfinID: "2",
				MediaType:  models.MediaTypeMovie,
				Title:      "movie 2",
			},
		},
		"library 2": []arr.MediaItem{
			{
				JellyfinID: "11",
				MediaType:  models.MediaTypeTV,
				Title:      "series 1",
			},
		},
		"library 3": []arr.MediaItem{
			{
				JellyfinID: "12",
				MediaType:  models.MediaTypeTV,
				Title:      "series 2",
			},
		},
	}

	engine := &Engine{cfg: cfg, telegram: telegramClient}

	engine.sendTelegramDeletionCompletedNotification(context.Background(), deletedItems)

	telegramClient.Called(t, config.EventTypeDeleted,
		`### Following media was deleted

#### Movies

- movie 1
- movie 2

#### TV Series

- series 1
- series 2

`)
}

func TestNotification_sendTelegramDeletionSummary(t *testing.T) {
	telegramClient := &testTelegramClient{}
	cfg := &config.Config{
		Jellyfin: &config.JellyfinConfig{
			URL: "http://jellyfin:80",
		},
	}

	items := []arr.MediaItem{
		{
			JellyfinID: "1",
			MediaType:  models.MediaTypeMovie,
			Title:      "movie 1",
		},
		{
			JellyfinID: "2",
			MediaType:  models.MediaTypeMovie,
			Title:      "movie 2",
		},
		{
			JellyfinID: "11",
			MediaType:  models.MediaTypeTV,
			Title:      "series 1",
		},
		{
			JellyfinID: "12",
			MediaType:  models.MediaTypeTV,
			Title:      "series 2",
		},
	}

	engine := &Engine{cfg: cfg, telegram: telegramClient}

	engine.sendTelegramDeletionSummary(context.Background(), items)

	telegramClient.Called(t, config.EventTypeMarkedForDeletion,
		`### Following media was marked for deletion

#### Movies

- [movie 1](http://jellyfin:80/web/#/details?id=1)
- [movie 2](http://jellyfin:80/web/#/details?id=2)

#### TV Series

- [series 1](http://jellyfin:80/web/#/details?id=11)
- [series 2](http://jellyfin:80/web/#/details?id=12)

`)
}
