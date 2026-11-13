package tracearr

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/stats"
	"github.com/jon4hz/jellysweep/pkg/tracearr"
)

const (
	// snapshotTTL keeps one cleanup run on one snapshot without carrying plays
	// over into the next run.
	snapshotTTL = 5 * time.Minute
	// windowMargin widens the window past the longest threshold.
	windowMargin = 24 * time.Hour
)

type tracearrClient struct {
	client *tracearr.Client
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	watched   *watched
	fetchedAt time.Time
}

// watched holds the last day each title was played, by the IDs Radarr and
// Sonarr use. TMDB numbers movies and series separately, so they're kept apart.
type watched struct {
	movies     map[int32]time.Time // TMDB ID -> last watched day
	seriesTvdb map[int32]time.Time // TVDB ID -> last watched day
	seriesTmdb map[int32]time.Time // TMDB ID -> last watched day
}

// New creates a Statser backed by Tracearr.
//
// Tracearr doesn't know items by Jellyfin ID, so this matches them on their
// TMDB and TVDB IDs instead, which also counts plays on every server Tracearr
// tracks. It reads Tracearr's list of played titles once per run, only as far
// back as the longest last stream threshold: a title last played before that is
// reported as never played, as either way it's outside the threshold.
func New(cfg *config.Config) stats.Statser {
	return &tracearrClient{
		client: tracearr.New(cfg.Tracearr),
		window: window(cfg),
		now:    time.Now,
	}
}

func window(cfg *config.Config) time.Duration {
	days := (&config.CleanupConfig{}).GetLastStreamThreshold()
	for _, library := range cfg.Libraries {
		if library != nil {
			days = max(days, library.GetLastStreamThreshold())
		}
	}
	return time.Duration(days)*24*time.Hour + windowMargin
}

func (s *tracearrClient) GetItemLastPlayed(ctx context.Context, item stats.Item) (time.Time, error) {
	if err := checkMatchable(item); err != nil {
		return time.Time{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if s.watched == nil || now.Sub(s.fetchedAt) >= snapshotTTL {
		w, err := s.fetchWatched(ctx, now)
		if err != nil {
			return time.Time{}, err
		}
		s.watched = w
		s.fetchedAt = now
	}

	var day time.Time
	if item.MediaType == models.MediaTypeMovie {
		day = s.watched.movies[item.TmdbID]
	} else {
		day = latest(s.watched.seriesTvdb[item.TvdbID], s.watched.seriesTmdb[item.TmdbID])
	}
	if day.IsZero() {
		return time.Time{}, nil
	}
	// Tracearr only gives the day, so count the play as late in it as possible
	// rather than risk deleting an item up to a day early.
	return latestBefore(day.Add(24*time.Hour), now), nil
}

// checkMatchable rejects items Tracearr can't be asked about, since reading
// them as never played would make them deletion candidates.
func checkMatchable(item stats.Item) error {
	switch item.MediaType {
	case models.MediaTypeMovie:
		if item.TmdbID == 0 {
			return fmt.Errorf("cannot look up plays of movie %s without a tmdb id", item.JellyfinID)
		}
	case models.MediaTypeTV:
		if item.TmdbID == 0 && item.TvdbID == 0 {
			return fmt.Errorf("cannot look up plays of series %s without a tmdb or tvdb id", item.JellyfinID)
		}
	default:
		return fmt.Errorf("cannot look up plays of %s with media type %q", item.JellyfinID, item.MediaType)
	}
	return nil
}

func (s *tracearrClient) fetchWatched(ctx context.Context, now time.Time) (*watched, error) {
	since := now.Add(-s.window)
	movies, err := s.client.GetWatchedTitles(ctx, tracearr.MediaTypeMovie, since)
	if err != nil {
		return nil, fmt.Errorf("failed to get watched movies from tracearr: %w", err)
	}
	series, err := s.client.GetWatchedTitles(ctx, tracearr.MediaTypeShow, since)
	if err != nil {
		return nil, fmt.Errorf("failed to get watched series from tracearr: %w", err)
	}

	w := &watched{
		movies:     make(map[int32]time.Time),
		seriesTvdb: make(map[int32]time.Time),
		seriesTmdb: make(map[int32]time.Time),
	}
	for _, title := range movies {
		record(w.movies, title.TmdbID, title.LastWatchedDay)
	}
	for _, title := range series {
		record(w.seriesTvdb, title.TvdbID, title.LastWatchedDay)
		record(w.seriesTmdb, title.TmdbID, title.LastWatchedDay)
	}

	// Wrong URLs and keys already fail on their status, so no titles means an
	// idle server. Titles that all lack IDs mean the API changed instead.
	titles := len(movies) + len(series)
	matchable := len(w.movies) + len(w.seriesTvdb) + len(w.seriesTmdb)
	if titles == 0 {
		log.Warn("Tracearr has no plays in the window, treating every item as unplayed", "window", s.window)
	} else if matchable == 0 {
		return nil, errors.New("tracearr returned watched titles without tmdb or tvdb ids")
	}

	log.Debug("Fetched watched titles from tracearr", "movies", len(movies), "series", len(series))
	return w, nil
}

// record keeps the latest day for an ID, skipping unknown IDs.
func record(days map[int32]time.Time, id int32, day time.Time) {
	if id != 0 {
		days[id] = latest(days[id], day)
	}
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func latestBefore(t, limit time.Time) time.Time {
	if t.After(limit) {
		return limit
	}
	return t
}
