package streamystats

import (
	"context"
	"time"

	"github.com/jon4hz/jellysweep/config"
	"github.com/jon4hz/jellysweep/engine/stats"
	"github.com/jon4hz/jellysweep/streamystats"
)

type streamystatsClient struct {
	client *streamystats.Client
}

func New(cfg *config.StreamystatsConfig, apiKey string) (stats.Statser, error) {
	client, err := streamystats.New(cfg, apiKey)
	if err != nil {
		return nil, err
	}
	return &streamystatsClient{
		client: client,
	}, nil
}

func (s *streamystatsClient) GetItemLastPlayed(ctx context.Context, jellyfinID string) (time.Time, error) {
	lastWatched, err := s.client.GetItemDetails(ctx, jellyfinID)
	if err != nil {
		return time.Time{}, err
	}
	if lastWatched == nil || lastWatched.LastWatched.IsZero() {
		return time.Time{}, nil // No playback history found
	}
	return lastWatched.LastWatched, nil
}
