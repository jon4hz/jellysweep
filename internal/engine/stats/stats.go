package stats

import (
	"context"
	"time"

	"github.com/jon4hz/jellysweep/internal/api/models"
)

// Item identifies a media item to a stats backend. Each backend uses whichever
// ID it can look items up by. A zero TmdbID or TvdbID means the ID is unknown.
type Item struct {
	JellyfinID string
	MediaType  models.MediaType
	TmdbID     int32
	TvdbID     int32
}

type Statser interface {
	GetItemLastPlayed(ctx context.Context, item Item) (time.Time, error)
}
