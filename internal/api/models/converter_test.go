package models

import (
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/stretchr/testify/require"
)

func sampleMedia() database.Media {
	tmdb := int32(42)
	protectedUntil := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)
	m := database.Media{
		JellyfinID:      "jf-1",
		LibraryName:     "Movies",
		ArrID:           7,
		Title:           "Movie",
		TmdbId:          &tmdb,
		Year:            2001,
		FileSize:        1234,
		MediaType:       database.MediaTypeMovie,
		RequestedBy:     "alice@example.com",
		DefaultDeleteAt: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		ProtectedUntil:  &protectedUntil,
		Unkeepable:      true,
		Request: database.Request{
			UserID: 9,
			Status: database.RequestStatusApproved,
			User:   database.User{Username: "alice"},
		},
	}
	m.ID = 3
	m.Request.ID = 11
	return m
}

func TestToUserMediaItemHidesSensitiveFields(t *testing.T) {
	item := ToUserMediaItem(sampleMedia(), &config.Config{})

	require.Equal(t, uint(3), item.ID)
	require.Equal(t, "Movie", item.Title)
	require.Equal(t, MediaTypeMovie, item.MediaType)
	require.True(t, item.Unkeepable)
	require.Empty(t, item.CleanupMode, "movies carry no cleanup mode")
	require.Zero(t, item.KeepCount)

	// Request visible by id/status only; the requester stays hidden.
	require.NotNil(t, item.Request)
	require.Equal(t, uint(11), item.Request.ID)
	require.Equal(t, "approved", item.Request.Status)
}

func TestToUserMediaItemWithoutRequest(t *testing.T) {
	m := sampleMedia()
	m.Request = database.Request{}
	require.Nil(t, ToUserMediaItem(m, &config.Config{}).Request)
}

func TestToUserMediaItemTVCarriesCleanupSettings(t *testing.T) {
	m := sampleMedia()
	m.MediaType = database.MediaTypeTV
	cfg := &config.Config{CleanupMode: config.CleanupModeKeepSeasons, KeepCount: 2}

	item := ToUserMediaItem(m, cfg)
	require.Equal(t, "keep_seasons", item.CleanupMode)
	require.Equal(t, 2, item.KeepCount)

	nilCfg := ToUserMediaItem(m, nil)
	require.Empty(t, nilCfg.CleanupMode, "a nil config must not panic")
}

func TestToAdminMediaItemExposesEverything(t *testing.T) {
	m := sampleMedia()
	item := ToAdminMediaItem(m, &config.Config{})

	require.Equal(t, "alice@example.com", item.RequestedBy)
	require.Equal(t, "jf-1", item.JellyfinID)
	require.Equal(t, int32(7), item.ArrID)
	require.Equal(t, m.TmdbId, item.TmdbId)
	require.Nil(t, item.TvdbId)
	require.Equal(t, m.ProtectedUntil, item.ProtectedUntil)

	require.NotNil(t, item.Request)
	require.Equal(t, uint(11), item.Request.ID)
	require.Equal(t, uint(9), item.Request.UserID)
	require.Equal(t, "alice", item.Request.Username)
	require.Equal(t, "approved", item.Request.Status)
}

func TestSliceConverters(t *testing.T) {
	items := []database.Media{sampleMedia(), sampleMedia()}
	require.Len(t, ToUserMediaItems(items, nil), 2)
	require.Len(t, ToAdminMediaItems(items, nil), 2)
	require.Empty(t, ToUserMediaItems(nil, nil))
	require.Empty(t, ToAdminMediaItems(nil, nil))
}

func TestToHistoryEventItem(t *testing.T) {
	media := sampleMedia()
	eventTime := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	event := database.HistoryEvent{
		MediaID:   media.ID,
		Media:     media,
		EventType: database.HistoryEventRequestApproved,
		User:      &database.User{Username: "admin"},
		EventTime: eventTime,
	}
	event.ID = 5

	item := ToHistoryEventItem(event)
	require.Equal(t, uint(5), item.ID)
	require.Equal(t, media.ID, item.MediaID)
	require.Equal(t, "jf-1", item.JellyfinID)
	require.Equal(t, "Movie", item.Title)
	require.Equal(t, MediaTypeMovie, item.MediaType)
	require.Equal(t, "request_approved", item.EventType)
	require.Equal(t, "admin", item.Username)
	require.Equal(t, eventTime, item.EventTime)

	event.User = nil
	require.Empty(t, ToHistoryEventItem(event).Username, "system events have no user")

	require.Len(t, ToHistoryEventItems([]database.HistoryEvent{event, event}), 2)
}

func TestEstimatedDeleteAtFallsBackToDefault(t *testing.T) {
	// Before the first estimation run the stored estimate is zero; the API
	// must expose the default date instead of 0001-01-01.
	m := sampleMedia()
	require.True(t, m.EstimatedDeleteAt.IsZero())

	user := ToUserMediaItem(m, &config.Config{})
	require.Equal(t, m.DefaultDeleteAt, user.EstimatedDeleteAt)
	admin := ToAdminMediaItem(m, &config.Config{})
	require.Equal(t, m.DefaultDeleteAt, admin.EstimatedDeleteAt)

	estimate := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	m.EstimatedDeleteAt = estimate
	require.Equal(t, estimate, ToUserMediaItem(m, &config.Config{}).EstimatedDeleteAt)
	require.Equal(t, estimate, ToAdminMediaItem(m, &config.Config{}).EstimatedDeleteAt)
}
