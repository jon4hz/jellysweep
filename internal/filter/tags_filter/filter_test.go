package tagsfilter

import (
	"testing"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/stretchr/testify/require"
)

func TestApplyExcludesIgnoreTag(t *testing.T) {
	f := New(&config.Config{})
	got, err := f.Apply(t.Context(), []arr.MediaItem{
		{Title: "Ignored", Tags: []string{"jellysweep-ignore"}},
		{Title: "Normal", Tags: []string{"other-tag"}},
		{Title: "Untagged"},
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "Normal", got[0].Title)
	require.Equal(t, "Untagged", got[1].Title)
}

func TestApplyExcludesConfiguredTags(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {Filter: config.FilterConfig{ExcludeTags: []string{"favorite"}}},
		},
	}
	f := New(cfg)
	got, err := f.Apply(t.Context(), []arr.MediaItem{
		{Title: "Kept", LibraryName: "Movies", Tags: []string{"favorite"}},
		{Title: "Candidate", LibraryName: "Movies", Tags: []string{"meh"}},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Candidate", got[0].Title)
}

func TestApplyDeprecatedExcludeTagsFallback(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {ExcludeTags: []string{"legacy-keep"}},
		},
	}
	f := New(cfg)
	got, err := f.Apply(t.Context(), []arr.MediaItem{
		{Title: "Kept", LibraryName: "Movies", Tags: []string{"legacy-keep"}},
	})
	require.NoError(t, err)
	require.Empty(t, got, "deprecated exclude_tags must still be honored")
}

func TestApplyExcludeTagsAreScopedPerLibrary(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {Filter: config.FilterConfig{ExcludeTags: []string{"favorite"}}},
			"TV":     {},
		},
	}
	f := New(cfg)
	got, err := f.Apply(t.Context(), []arr.MediaItem{
		{Title: "A Show", LibraryName: "TV", Tags: []string{"favorite"}},
	})
	require.NoError(t, err)
	require.Len(t, got, 1, "exclude tags from another library must not apply")
}

func TestExcludedTag(t *testing.T) {
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {Filter: config.FilterConfig{ExcludeTags: []string{"favorite"}}},
		},
	}

	tag, ok := ExcludedTag(cfg, "Movies", []string{"4k", "favorite"})
	require.True(t, ok)
	require.Equal(t, "favorite", tag)

	tag, ok = ExcludedTag(cfg, "TV", []string{"favorite", "jellysweep-ignore"})
	require.True(t, ok, "the ignore tag applies to every library")
	require.Equal(t, "jellysweep-ignore", tag)

	_, ok = ExcludedTag(cfg, "TV", []string{"favorite"})
	require.False(t, ok, "exclude tags are scoped per library")

	_, ok = ExcludedTag(cfg, "Unknown", []string{"jellysweep-ignore"})
	require.True(t, ok, "the ignore tag applies without a library config")

	_, ok = ExcludedTag(cfg, "Movies", nil)
	require.False(t, ok)
}
