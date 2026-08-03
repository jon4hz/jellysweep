package agefilter

import (
	"testing"

	"github.com/jon4hz/jellysweep/internal/api/models"
	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database/databasetest"
	"github.com/jon4hz/jellysweep/internal/engine/arr"
	"github.com/stretchr/testify/require"
)

func TestApplyWithNilArrClientsDoesNotPanic(t *testing.T) {
	// Regression: with only one arr configured, the other client is nil and
	// looking up the added date used to panic on the nil interface.
	cfg := &config.Config{
		Libraries: map[string]*config.CleanupConfig{
			"Movies": {},
			"TV":     {},
		},
	}
	db, _ := databasetest.New(t)
	f := New(cfg, db, nil, nil)

	items := []arr.MediaItem{
		{Title: "Some Movie", LibraryName: "Movies", MediaType: models.MediaTypeMovie},
		{Title: "Some Show", LibraryName: "TV", MediaType: models.MediaTypeTV},
	}

	filtered, err := f.Apply(t.Context(), items)
	require.NoError(t, err)
	// Without an added date the filter fails open: items stay candidates.
	require.Len(t, filtered, 2)
}
