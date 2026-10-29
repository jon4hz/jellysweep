package filter

import (
	"context"
	"errors"
	"testing"

	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/stretchr/testify/require"
)

type stubFilter struct {
	name    string
	drop    string // title to drop
	err     error
	applied *[]string
}

func (s *stubFilter) String() string { return s.name }

func (s *stubFilter) Apply(_ context.Context, items []arr.MediaItem) ([]arr.MediaItem, error) {
	*s.applied = append(*s.applied, s.name)
	if s.err != nil {
		return nil, s.err
	}
	out := make([]arr.MediaItem, 0, len(items))
	for _, item := range items {
		if item.Title != s.drop {
			out = append(out, item)
		}
	}
	return out, nil
}

func TestApplyAllRunsSequentially(t *testing.T) {
	var order []string
	f := New(
		&stubFilter{name: "first", drop: "A", applied: &order},
		&stubFilter{name: "second", drop: "B", applied: &order},
	)

	got, err := f.ApplyAll(t.Context(), []arr.MediaItem{{Title: "A"}, {Title: "B"}, {Title: "C"}})
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second"}, order)
	require.Len(t, got, 1)
	require.Equal(t, "C", got[0].Title)
}

func TestApplyAllAbortsOnError(t *testing.T) {
	var order []string
	f := New(
		&stubFilter{name: "boom", err: errors.New("boom"), applied: &order},
		&stubFilter{name: "never", applied: &order},
	)

	got, err := f.ApplyAll(t.Context(), []arr.MediaItem{{Title: "A"}})
	require.Error(t, err)
	require.Nil(t, got)
	require.Equal(t, []string{"boom"}, order, "filters after a failing one must not run")
}
