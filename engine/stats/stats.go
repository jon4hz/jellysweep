package stats

import (
	"context"
	"time"
)

type Statser interface {
	GetItemLastPlayed(ctx context.Context, itemID string) (time.Time, error)
}
