package ntfy

import (
	"net/http"
	"testing"

	"github.com/jon4hz/jellysweep/internal/config"
	"github.com/jon4hz/jellysweep/internal/httptestutil"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, mutate func(*config.NtfyConfig)) (*Client, *httptestutil.Server) {
	t.Helper()
	server := httptestutil.New(t)
	server.OK("POST /")
	cfg := &config.NtfyConfig{ServerURL: server.URL, Topic: "jellysweep"}
	if mutate != nil {
		mutate(cfg)
	}
	return NewClient(cfg), server
}

func lastMessage(t *testing.T, server *httptestutil.Server) Message {
	t.Helper()
	posts := server.Requests(http.MethodPost, "/")
	require.NotEmpty(t, posts)
	var msg Message
	posts[len(posts)-1].JSONBody(t, &msg)
	return msg
}

func TestSendMessageUsesConfiguredTopicAndMarkdown(t *testing.T) {
	c, server := newTestClient(t, nil)
	require.NoError(t, c.SendMessage(t.Context(), Message{Topic: "ignored", Title: "Hi", Message: "there"}))

	posts := server.Requests(http.MethodPost, "/")
	require.Len(t, posts, 1)
	require.Equal(t, "application/json", posts[0].Header.Get("Content-Type"))
	require.Equal(t, "yes", posts[0].Header.Get("Markdown"))
	require.Equal(t, "jellysweep", lastMessage(t, server).Topic, "configured topic overrides the message topic")
}

func TestSendMessageAuthentication(t *testing.T) {
	t.Run("token takes precedence", func(t *testing.T) {
		c, server := newTestClient(t, func(cfg *config.NtfyConfig) {
			cfg.Token = "tok"
			cfg.Username = "u"
			cfg.Password = "p"
		})
		require.NoError(t, c.SendMessage(t.Context(), Message{Title: "x"}))
		require.Equal(t, "Bearer tok", server.Requests(http.MethodPost, "/")[0].Header.Get("Authorization"))
	})
	t.Run("basic auth fallback", func(t *testing.T) {
		c, server := newTestClient(t, func(cfg *config.NtfyConfig) {
			cfg.Username = "u"
			cfg.Password = "p"
		})
		require.NoError(t, c.SendMessage(t.Context(), Message{Title: "x"}))
		require.Equal(t, "Basic dTpw", server.Requests(http.MethodPost, "/")[0].Header.Get("Authorization"))
	})
	t.Run("no credentials", func(t *testing.T) {
		c, server := newTestClient(t, nil)
		require.NoError(t, c.SendMessage(t.Context(), Message{Title: "x"}))
		require.Empty(t, server.Requests(http.MethodPost, "/")[0].Header.Get("Authorization"))
	})
}

func TestSendMessageServerErrorIsReported(t *testing.T) {
	server := httptestutil.New(t)
	server.Handle("POST /", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden topic"))
	})
	c := NewClient(&config.NtfyConfig{ServerURL: server.URL})

	err := c.SendMessage(t.Context(), Message{Title: "x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "403")
	require.Contains(t, err.Error(), "forbidden topic")
}

func TestSendKeepRequest(t *testing.T) {
	c, server := newTestClient(t, nil)
	require.NoError(t, c.SendKeepRequest(t.Context(), "The Movie", "Movie", "alice"))

	msg := lastMessage(t, server)
	require.Contains(t, msg.Title, "🎬")
	require.Contains(t, msg.Message, "alice")
	require.Contains(t, msg.Message, "The Movie")
	require.Equal(t, 4, msg.Priority)
	require.Contains(t, msg.Tags, "keep-request")

	require.NoError(t, c.SendKeepRequest(t.Context(), "The Show", "tv", "bob"))
	require.Contains(t, lastMessage(t, server).Title, "📺")
}

func TestSendDeletionSummary(t *testing.T) {
	c, server := newTestClient(t, nil)

	require.NoError(t, c.SendDeletionSummary(t.Context(), 0, nil))
	require.Empty(t, server.Requests("", ""), "nothing to report means no notification")

	libraries := map[string][]MediaItem{
		"Movies":   {{Title: "Old Movie", Year: 1999}},
		"TV Shows": {{Title: "Old Show", Year: 2005}, {Title: "Older Show", Year: 2001}},
	}
	require.NoError(t, c.SendDeletionSummary(t.Context(), 3, libraries))
	msg := lastMessage(t, server)
	require.Equal(t, "🧹🪼 Cleanup Summary", msg.Title)
	require.Contains(t, msg.Message, "**Total Items:** 3")
	require.Contains(t, msg.Message, "**Movies:** 1 items")
	require.Contains(t, msg.Message, "**TV Shows:** 2 items")
	require.Contains(t, msg.Message, "Old Movie (1999)")
	require.Contains(t, msg.Message, "Older Show (2001)")
}

func TestSendDeletionCompletedSummary(t *testing.T) {
	c, server := newTestClient(t, nil)

	require.NoError(t, c.SendDeletionCompletedSummary(t.Context(), 0, nil))
	require.Empty(t, server.Requests("", ""))

	require.NoError(t, c.SendDeletionCompletedSummary(t.Context(), 1, map[string][]MediaItem{
		"Movies": {{Title: "Gone Movie", Year: 2010}},
	}))
	msg := lastMessage(t, server)
	require.Equal(t, "✅🪼 Cleanup Completed", msg.Title)
	require.Contains(t, msg.Message, "**Total Items Deleted:** 1")
	require.Contains(t, msg.Message, "Gone Movie (2010)")
	require.Contains(t, msg.Tags, "cleanup-completed")
}
