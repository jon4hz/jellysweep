package jellystat

import (
	"context"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/stats"
	"github.com/jon4hz/jellysweep/pkg/jellystat"
)

type jellystatClient struct {
	client *jellystat.Client
}

func New(cfg *config.JellystatConfig) stats.Statser {
	return &jellystatClient{
		client: jellystat.New(cfg),
	}
}

func (s *jellystatClient) GetItemLastPlayed(ctx context.Context, jellyfinID string) (time.Time, error) {
	lastPlayed, err := s.client.GetLastPlayed(ctx, jellyfinID)
	if err != nil {
		return time.Time{}, err
	}
	if lastPlayed == nil || lastPlayed.LastPlayed == nil {
		return time.Time{}, nil // No playback history found
	}
	return *lastPlayed.LastPlayed, nil
}
