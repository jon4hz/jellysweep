package webpush

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/charmbracelet/log"
	"github.com/jon4hz/jellysweep/config"
)

// Config holds the configuration for webpush notifications.
type Config = config.WebPushConfig

// ErrAllSubscriptionsInvalid is returned when all subscriptions for a user are invalid (410/404).
type ErrAllSubscriptionsInvalid struct {
	UserID string
}

func (e *ErrAllSubscriptionsInvalid) Error() string {
	return fmt.Sprintf("all push subscriptions for user %s are invalid or expired", e.UserID)
}

// Client represents a webpush notification client.
type Client struct {
	config        *Config
	subscriptions map[string]map[string]*Subscription // userID -> subscriptionID -> subscription
	mu            sync.RWMutex
}

// Subscription represents a push subscription.
type Subscription struct {
	ID       string `json:"id"` // Unique identifier for this subscription
	UserID   string `json:"user_id"`
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	CreatedAt time.Time `json:"created_at"`
	UserAgent string    `json:"user_agent,omitempty"` // Optional: to identify the device/browser
}

// NotificationPayload represents the payload sent to the client.
type NotificationPayload struct {
	Title   string                 `json:"title"`
	Body    string                 `json:"body"`
	Icon    string                 `json:"icon"`
	Badge   string                 `json:"badge"`
	Data    map[string]interface{} `json:"data"`
	Actions []NotificationAction   `json:"actions,omitempty"`
}

// NotificationAction represents an action button in the notification.
type NotificationAction struct {
	Action string `json:"action"`
	Title  string `json:"title"`
	Icon   string `json:"icon,omitempty"`
}

// NewClient creates a new webpush client.
func NewClient(config *Config) *Client {
	return &Client{
		config:        config,
		subscriptions: make(map[string]map[string]*Subscription),
	}
}

// GenerateVAPIDKeys generates a new VAPID key pair.
func GenerateVAPIDKeys() (privateKey, publicKey string, err error) {
	return webpush.GenerateVAPIDKeys()
}

// GetPublicKey returns the VAPID public key for client subscription.
func (c *Client) GetPublicKey() string {
	return c.config.PublicKey
}

// Subscribe adds a new push subscription for a user.
func (c *Client) Subscribe(userID string, subscription *Subscription) error {
	if !c.config.Enabled {
		return fmt.Errorf("webpush notifications are disabled")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Generate a unique subscription ID based on endpoint
	hash := sha256.Sum256([]byte(subscription.Endpoint))
	subscriptionID := hex.EncodeToString(hash[:])[:16] // Use first 16 chars for brevity

	subscription.ID = subscriptionID
	subscription.UserID = userID
	subscription.CreatedAt = time.Now()

	// Initialize user's subscription map if it doesn't exist
	if c.subscriptions[userID] == nil {
		c.subscriptions[userID] = make(map[string]*Subscription)
	}

	c.subscriptions[userID][subscriptionID] = subscription

	log.Infof("Added push subscription %s for user %s", subscriptionID, userID)
	return nil
}

// Unsubscribe removes a push subscription for a user.
func (c *Client) Unsubscribe(userID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.subscriptions, userID)
	log.Infof("Removed all push subscriptions for user %s", userID)
	return nil
}

// UnsubscribeByID removes a specific push subscription by ID for a user.
func (c *Client) UnsubscribeByID(userID, subscriptionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if userSubs, exists := c.subscriptions[userID]; exists {
		delete(userSubs, subscriptionID)
		log.Infof("Removed push subscription %s for user %s", subscriptionID, userID)

		// Clean up empty user map
		if len(userSubs) == 0 {
			delete(c.subscriptions, userID)
		}
	}

	return nil
}

// UnsubscribeByEndpoint removes a subscription by endpoint for a user.
func (c *Client) UnsubscribeByEndpoint(userID, endpoint string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if userSubs, exists := c.subscriptions[userID]; exists {
		for subID, sub := range userSubs {
			if sub.Endpoint == endpoint {
				delete(userSubs, subID)
				log.Infof("Removed push subscription %s (endpoint match) for user %s", subID, userID)

				// Clean up empty user map
				if len(userSubs) == 0 {
					delete(c.subscriptions, userID)
				}
				break
			}
		}
	}

	return nil
}

// SendNotification sends a push notification to all subscriptions for a specific user.
func (c *Client) SendNotification(ctx context.Context, userID string, payload *NotificationPayload) error {
	if !c.config.Enabled {
		return fmt.Errorf("webpush notifications are disabled")
	}

	c.mu.RLock()
	userSubscriptions, exists := c.subscriptions[userID]
	c.mu.RUnlock()

	if !exists || len(userSubscriptions) == 0 {
		return fmt.Errorf("no push subscriptions found for user %s", userID)
	}

	// Marshal the payload once
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal notification payload: %w", err)
	}

	var lastError error
	successCount := 0
	invalidCount := 0
	totalCount := len(userSubscriptions)

	// Send to all subscriptions for this user
	for subscriptionID, subscription := range userSubscriptions {
		// Convert subscription to webpush format
		webpushSub := &webpush.Subscription{
			Endpoint: subscription.Endpoint,
			Keys: webpush.Keys{
				P256dh: subscription.Keys.P256dh,
				Auth:   subscription.Keys.Auth,
			},
		}

		// Send the notification
		resp, err := webpush.SendNotification(payloadBytes, webpushSub, &webpush.Options{
			Subscriber:      c.config.VAPIDEmail,
			VAPIDPublicKey:  c.config.PublicKey,
			VAPIDPrivateKey: c.config.PrivateKey,
			TTL:             30,
			RecordSize:      3000, // higher caused issues with firefox on android :(
		})

		if err != nil {
			log.Errorf("Failed to send push notification to subscription %s for user %s: %v", subscriptionID, userID, err)
			lastError = err

			// If the subscription is invalid, remove it and count it
			if resp != nil && (resp.StatusCode == 410 || resp.StatusCode == 404) {
				invalidCount++
				go func(uid, sid string) {
					_ = c.UnsubscribeByID(uid, sid)
				}(userID, subscriptionID)
			}
		} else {
			if resp != nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					successCount++
					log.Debugf("Sent push notification to subscription %s for user %s (status: %d)", subscriptionID, userID, resp.StatusCode)
				} else {
					log.Warnf("Push notification to subscription %s for user %s failed with status: %d", subscriptionID, userID, resp.StatusCode)

					// Check if this is an invalid subscription response
					if resp.StatusCode == 410 || resp.StatusCode == 404 {
						invalidCount++
						go func(uid, sid string) {
							_ = c.UnsubscribeByID(uid, sid)
						}(userID, subscriptionID)
					}

					lastError = fmt.Errorf("push notification failed with status %d", resp.StatusCode)
				}
			} else {
				successCount++
			}
		}
	}

	// If all subscriptions were invalid, return specific error
	if invalidCount == totalCount && totalCount > 0 {
		return &ErrAllSubscriptionsInvalid{UserID: userID}
	}

	if successCount > 0 {
		log.Infof("Sent push notification to user %s (%d/%d subscriptions successful)", userID, successCount, totalCount)
		return nil
	}

	if lastError != nil {
		return fmt.Errorf("failed to send push notification to any subscription for user %s: %w", userID, lastError)
	}

	return fmt.Errorf("failed to send push notification to user %s", userID)
}

// SendKeepRequestNotification sends a notification about a keep request decision.
func (c *Client) SendKeepRequestNotification(ctx context.Context, userID, mediaTitle, mediaType string, approved bool) error {
	var title, body string
	var icon string

	if approved {
		title = "✅ Keep Request Approved"
		body = fmt.Sprintf("Your request to keep \"%s\" has been approved!", mediaTitle)
		icon = "/static/icons/icon-192x192.png"
	} else {
		title = "❌ Keep Request Denied"
		body = fmt.Sprintf("Your request to keep \"%s\" has been denied.", mediaTitle)
		icon = "/static/icons/icon-192x192.png"
	}

	payload := &NotificationPayload{
		Title: title,
		Body:  body,
		Icon:  icon,
		Badge: "/static/icons/icon-192x192.png",
		Data: map[string]interface{}{
			"type":       "keep_request_decision",
			"approved":   approved,
			"mediaTitle": mediaTitle,
			"mediaType":  mediaType,
			"timestamp":  time.Now().Unix(),
		},
		Actions: []NotificationAction{
			{
				Action: "open_app",
				Title:  "Open JellySweep",
			},
		},
	}

	return c.SendNotification(ctx, userID, payload)
}

// GetSubscriptions returns all subscriptions for a user.
func (c *Client) GetSubscriptions(userID string) ([]*Subscription, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	userSubs, exists := c.subscriptions[userID]
	if !exists || len(userSubs) == 0 {
		return nil, false
	}

	subscriptions := make([]*Subscription, 0, len(userSubs))
	for _, sub := range userSubs {
		subscriptions = append(subscriptions, sub)
	}

	return subscriptions, true
}

// GetSubscription returns a specific subscription by ID for a user.
func (c *Client) GetSubscription(userID, subscriptionID string) (*Subscription, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if userSubs, exists := c.subscriptions[userID]; exists {
		subscription, exists := userSubs[subscriptionID]
		return subscription, exists
	}

	return nil, false
}

// GetAllUserIDs returns all user IDs that have active subscriptions.
func (c *Client) GetAllUserIDs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	userIDs := make([]string, 0, len(c.subscriptions))
	for userID, userSubs := range c.subscriptions {
		if len(userSubs) > 0 {
			userIDs = append(userIDs, userID)
		}
	}
	return userIDs
}

// GetSubscriptionByEndpoint finds a subscription by endpoint across all users.
func (c *Client) GetSubscriptionByEndpoint(endpoint string) (*Subscription, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for userID, userSubs := range c.subscriptions {
		for _, sub := range userSubs {
			if sub.Endpoint == endpoint {
				return sub, userID, true
			}
		}
	}
	return nil, "", false
}

// SendNotificationToAll sends a notification to all subscribed users.
func (c *Client) SendNotificationToAll(ctx context.Context, payload *NotificationPayload) error {
	if !c.config.Enabled {
		return fmt.Errorf("webpush notifications are disabled")
	}

	userIDs := c.GetAllUserIDs()
	if len(userIDs) == 0 {
		return fmt.Errorf("no users with push subscriptions found")
	}

	var lastError error
	successCount := 0

	for _, userID := range userIDs {
		if err := c.SendNotification(ctx, userID, payload); err != nil {
			log.Errorf("Failed to send notification to user %s: %v", userID, err)
			lastError = err
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		log.Infof("Sent push notification to %d/%d users", successCount, len(userIDs))
		return nil
	}

	if lastError != nil {
		return fmt.Errorf("failed to send push notification to any user: %w", lastError)
	}

	return fmt.Errorf("failed to send push notification to any user")
}

// GetSubscriptionCount returns the total number of active subscriptions across all users.
func (c *Client) GetSubscriptionCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := 0
	for _, userSubs := range c.subscriptions {
		count += len(userSubs)
	}
	return count
}

// GetUserSubscriptionCount returns the number of active subscriptions for a specific user.
func (c *Client) GetUserSubscriptionCount(userID string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if userSubs, exists := c.subscriptions[userID]; exists {
		return len(userSubs)
	}
	return 0
}

// CleanupExpiredSubscriptions removes old subscriptions (optional cleanup).
func (c *Client) CleanupExpiredSubscriptions(maxAge time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for userID, userSubs := range c.subscriptions {
		for subscriptionID, subscription := range userSubs {
			if subscription.CreatedAt.Before(cutoff) {
				delete(userSubs, subscriptionID)
				log.Infof("Removed expired push subscription %s for user %s", subscriptionID, userID)
			}
		}

		// Clean up empty user maps
		if len(userSubs) == 0 {
			delete(c.subscriptions, userID)
		}
	}
}

// TestNotification sends a test notification to verify the setup.
func (c *Client) TestNotification(ctx context.Context, userID string) error {
	payload := &NotificationPayload{
		Title: "🧹 JellySweep Test",
		Body:  "Push notifications are working correctly!",
		Icon:  "/static/icons/icon-192x192.png",
		Badge: "/static/icons/icon-192x192.png",
		Data: map[string]interface{}{
			"type":      "test",
			"timestamp": time.Now().Unix(),
		},
	}

	return c.SendNotification(ctx, userID, payload)
}

// ValidateConfig validates the webpush configuration.
func (c *Client) ValidateConfig() error {
	if !c.config.Enabled {
		return nil
	}

	if c.config.VAPIDEmail == "" {
		return fmt.Errorf("vapid_email is required when webpush is enabled")
	}

	if c.config.PublicKey == "" || c.config.PrivateKey == "" {
		return fmt.Errorf("both public_key and private_key are required when webpush is enabled")
	}

	// Validate key format
	if _, err := base64.RawURLEncoding.DecodeString(c.config.PublicKey); err != nil {
		return fmt.Errorf("invalid public key format: %w", err)
	}

	if _, err := base64.RawURLEncoding.DecodeString(c.config.PrivateKey); err != nil {
		return fmt.Errorf("invalid private key format: %w", err)
	}

	return nil
}
