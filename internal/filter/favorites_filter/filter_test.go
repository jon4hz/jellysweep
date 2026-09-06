package favoritesfilter

import (
	"context"
	"errors"
	"testing"

	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/stretchr/testify/require"
)

type stubSource struct {
	favorites map[string]bool
	err       error
	calls     int
}

func (s *stubSource) GetFavoriteItemIDs(context.Context) (map[string]bool, error) {
	s.calls++
	return s.favorites, s.err
}

func testConfig(moviesEnabled, tvEnabled bool) *config.Config {
	return &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {Filter: config.FilterConfig{FavoritesEnabled: moviesEnabled}},
			"TV":     {Filter: config.FilterConfig{FavoritesEnabled: tvEnabled}},
		},
	}
}

func testItems() []arr.MediaItem {
	return []arr.MediaItem{
		{Title: "Loved Movie", LibraryName: "Movies", JellyfinID: "jf-movie-fav", MediaType: models.MediaTypeMovie},
		{Title: "Plain Movie", LibraryName: "Movies", JellyfinID: "jf-movie-plain", MediaType: models.MediaTypeMovie},
		{Title: "Loved Show", LibraryName: "TV", JellyfinID: "jf-show-fav", MediaType: models.MediaTypeTV},
		{Title: "Plain Show", LibraryName: "TV", JellyfinID: "jf-show-plain", MediaType: models.MediaTypeTV},
	}
}

func titles(items []arr.MediaItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Title)
	}
	return out
}

func TestApplyExcludesFavoritedItems(t *testing.T) {
	src := &stubSource{favorites: map[string]bool{"jf-movie-fav": true, "jf-show-fav": true}}
	f := New(testConfig(true, true), src)

	got, err := f.Apply(t.Context(), testItems())
	require.NoError(t, err)
	require.Equal(t, []string{"Plain Movie", "Plain Show"}, titles(got))
	require.Equal(t, 1, src.calls, "favorites must be fetched once per Apply")
}

func TestApplyRespectsPerLibrarySetting(t *testing.T) {
	src := &stubSource{favorites: map[string]bool{"jf-movie-fav": true, "jf-show-fav": true}}
	f := New(testConfig(true, false), src)

	got, err := f.Apply(t.Context(), testItems())
	require.NoError(t, err)
	require.Equal(t, []string{"Plain Movie", "Loved Show", "Plain Show"}, titles(got))
}

func TestApplyKeepsItemsWithoutJellyfinID(t *testing.T) {
	src := &stubSource{favorites: map[string]bool{"": true}}
	f := New(testConfig(true, true), src)

	got, err := f.Apply(t.Context(), []arr.MediaItem{{Title: "No ID", LibraryName: "Movies"}})
	require.NoError(t, err)
	require.Equal(t, []string{"No ID"}, titles(got))
}

func TestApplySkipsFetchWhenNoLibraryEnabled(t *testing.T) {
	src := &stubSource{err: errors.New("must not be called")}
	f := New(testConfig(false, false), src)

	got, err := f.Apply(t.Context(), testItems())
	require.NoError(t, err)
	require.Len(t, got, 4)
	require.Equal(t, 0, src.calls)
}

func TestApplyFailsWhenFavoritesCannotBeFetched(t *testing.T) {
	src := &stubSource{err: errors.New("jellyfin down")}
	f := New(testConfig(true, true), src)

	got, err := f.Apply(t.Context(), testItems())
	require.Error(t, err)
	require.Nil(t, got)
}

func TestString(t *testing.T) {
	require.Equal(t, "Favorites Filter", New(testConfig(true, true), &stubSource{}).String())
}
