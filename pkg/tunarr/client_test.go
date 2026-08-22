package tunarr

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/httptestutil"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T) (*Client, *httptestutil.Server) {
	t.Helper()
	server := httptestutil.New(t)
	return New(&config.TunarrConfig{URL: server.URL}), server
}

func TestGetChannels(t *testing.T) {
	c, server := newTestClient(t)
	server.JSON("GET /api/channels", []Channel{{ID: "ch1", Name: "One", Number: 1, ProgramCount: 3}})

	channels, err := c.GetChannels(t.Context())
	require.NoError(t, err)
	require.Equal(t, []Channel{{ID: "ch1", Name: "One", Number: 1, ProgramCount: 3}}, channels)
	require.Equal(t, "application/json", server.Requests(http.MethodGet, "/api/channels")[0].Header.Get("Accept"))
}

func TestGetChannelProgrammingQueryParams(t *testing.T) {
	c, server := newTestClient(t)
	server.JSON("GET /api/channels/ch1/programming", ProgrammingResponse{Name: "One"})

	_, err := c.GetChannelProgramming(t.Context(), "ch1", 25, 50)
	require.NoError(t, err)
	call := server.Requests(http.MethodGet, "/api/channels/ch1/programming")[0]
	require.Equal(t, "25", call.Query.Get("limit"))
	require.Equal(t, "50", call.Query.Get("offset"))

	_, err = c.GetChannelProgramming(t.Context(), "ch1", 0, 0)
	require.NoError(t, err)
	require.Empty(t, server.Requests(http.MethodGet, "/api/channels/ch1/programming")[1].Query, "zero values are omitted")
}

func TestGetAllChannelProgramsPaginates(t *testing.T) {
	c, server := newTestClient(t)
	const total = 150
	server.Handle("GET /api/channels/ch1/programming", func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		programs := map[string]Program{}
		for i := offset; i < offset+limit && i < total; i++ {
			id := fmt.Sprintf("p%d", i)
			programs[id] = Program{ID: id, Subtype: "movie"}
		}
		httptestutil.WriteJSON(t, w, ProgrammingResponse{TotalPrograms: total, Programs: programs})
	})

	programs, err := c.GetAllChannelPrograms(t.Context(), "ch1")
	require.NoError(t, err)
	require.Len(t, programs, total)
	require.Len(t, server.Requests(http.MethodGet, "/api/channels/ch1/programming"), 2)
}

func TestGetAllChannelProgramsStopsOnEmptyPage(t *testing.T) {
	// Tunarr sometimes reports more programs than it returns; the client must
	// not loop forever.
	c, server := newTestClient(t)
	server.JSON("GET /api/channels/ch1/programming", ProgrammingResponse{TotalPrograms: 10, Programs: map[string]Program{}})

	programs, err := c.GetAllChannelPrograms(t.Context(), "ch1")
	require.NoError(t, err)
	require.Empty(t, programs)
	require.Len(t, server.Requests(http.MethodGet, "/api/channels/ch1/programming"), 1)
}

func TestErrorStatusIncludesBody(t *testing.T) {
	c, server := newTestClient(t)
	server.Handle("GET /api/channels", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	})

	_, err := c.GetChannels(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "502")
	require.Contains(t, err.Error(), "upstream down")
}
