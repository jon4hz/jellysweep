package webpush

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func enabledClient() *Client {
	return NewClient(&Config{Enabled: true, PublicKey: "pub", PrivateKey: "priv"})
}

func subscription(endpoint string) *Subscription {
	return &Subscription{Endpoint: endpoint}
}

func TestSubscribeDisabled(t *testing.T) {
	c := NewClient(&Config{Enabled: false})
	require.Error(t, c.Subscribe("alice", subscription("https://push/1")))
	require.Zero(t, c.GetUserSubscriptionCount("alice"))
}

func TestSubscribeAssignsIDAndNormalizesUser(t *testing.T) {
	c := enabledClient()
	sub := subscription("https://push/1")
	require.NoError(t, c.Subscribe("Alice", sub))

	require.NotEmpty(t, sub.ID)
	require.Equal(t, "alice", sub.UserID)
	require.False(t, sub.CreatedAt.IsZero())
	require.Equal(t, 1, c.GetUserSubscriptionCount("ALICE"), "user IDs are case-insensitive")
	require.Equal(t, []string{"alice"}, c.GetAllUserIDs())

	// The same endpoint is the same subscription, not a duplicate.
	require.NoError(t, c.Subscribe("alice", subscription("https://push/1")))
	require.Equal(t, 1, c.GetUserSubscriptionCount("alice"))

	require.NoError(t, c.Subscribe("alice", subscription("https://push/2")))
	require.Equal(t, 2, c.GetUserSubscriptionCount("alice"))
}

func TestUnsubscribe(t *testing.T) {
	c := enabledClient()
	first := subscription("https://push/1")
	require.NoError(t, c.Subscribe("alice", first))
	require.NoError(t, c.Subscribe("alice", subscription("https://push/2")))

	require.NoError(t, c.UnsubscribeByID("alice", first.ID))
	require.Equal(t, 1, c.GetUserSubscriptionCount("alice"))

	require.NoError(t, c.UnsubscribeByEndpoint("alice", "https://push/2"))
	require.Zero(t, c.GetUserSubscriptionCount("alice"))
	require.Empty(t, c.GetAllUserIDs(), "users without subscriptions are cleaned up")

	require.NoError(t, c.UnsubscribeByID("nobody", "x"), "unknown users are a no-op")
}

func TestGetSubscriptionByEndpoint(t *testing.T) {
	c := enabledClient()
	require.NoError(t, c.Subscribe("alice", subscription("https://push/1")))

	sub, userID, ok := c.GetSubscriptionByEndpoint("https://push/1")
	require.True(t, ok)
	require.Equal(t, "alice", userID)
	require.Equal(t, "https://push/1", sub.Endpoint)

	_, _, ok = c.GetSubscriptionByEndpoint("https://push/other")
	require.False(t, ok)
}

func TestSendNotificationWithoutSubscriptions(t *testing.T) {
	disabled := NewClient(&Config{Enabled: false})
	require.Error(t, disabled.SendNotification(t.Context(), "alice", &NotificationPayload{Title: "x"}))

	c := enabledClient()
	require.Error(t, c.SendNotification(t.Context(), "alice", &NotificationPayload{Title: "x"}))
	require.Error(t, c.SendKeepRequestNotification(t.Context(), "alice", "Movie", "movie", true))
	require.Error(t, c.SendNotificationToAll(t.Context(), &NotificationPayload{Title: "x"}))
}

func TestGenerateVAPIDKeys(t *testing.T) {
	private, public, err := GenerateVAPIDKeys()
	require.NoError(t, err)
	require.NotEmpty(t, private)
	require.NotEmpty(t, public)
	require.Equal(t, "pub", enabledClient().GetPublicKey())
}
