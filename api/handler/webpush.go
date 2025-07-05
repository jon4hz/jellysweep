package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
	"github.com/jon4hz/jellysweep/notify/webpush"
)

// SubscribeRequest represents the request body for push notification subscription.
type SubscribeRequest struct {
	Subscription struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	} `json:"subscription"`
	Username string `json:"username"`
}

// WebPushHandler handles webpush-related API endpoints.
type WebPushHandler struct {
	webpush *webpush.Client
}

// NewWebPushHandler creates a new webpush API handler.
func NewWebPushHandler(webpushClient *webpush.Client) *WebPushHandler {
	return &WebPushHandler{
		webpush: webpushClient,
	}
}

// GetVAPIDKey returns the VAPID public key for client subscription.
func (h *WebPushHandler) GetVAPIDKey(c *gin.Context) {
	if h.webpush == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "webpush is not configured",
		})
		return
	}

	publicKey := h.webpush.GetPublicKey()
	if publicKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "VAPID public key not available",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"publicKey": publicKey,
	})
}

// Subscribe handles push notification subscription requests.
func (h *WebPushHandler) Subscribe(c *gin.Context) {
	if h.webpush == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "webpush is not configured",
		})
		return
	}

	// Parse subscription request data
	var subscribeReq SubscribeRequest
	if err := c.ShouldBindJSON(&subscribeReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid subscription data",
		})
		return
	}

	// Validate that username is provided
	if subscribeReq.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "username is required",
		})
		return
	}

	// Get user from session for verification
	user := c.MustGet("user").(*models.User)
	if user.Username != subscribeReq.Username {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "unauthorized action",
		})
		return
	}

	// Create webpush subscription object
	subscription := &webpush.Subscription{
		UserID:   subscribeReq.Username,
		Endpoint: subscribeReq.Subscription.Endpoint,
		Keys: struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		}{
			P256dh: subscribeReq.Subscription.Keys.P256dh,
			Auth:   subscribeReq.Subscription.Keys.Auth,
		},
		UserAgent: c.GetHeader("User-Agent"),
	}

	// Subscribe the user
	if err := h.webpush.Subscribe(subscribeReq.Username, subscription); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to subscribe user",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":         true,
		"message":         "successfully subscribed to push notifications",
		"subscription_id": subscription.ID,
	})
}

// Unsubscribe handles push notification unsubscription requests.
func (h *WebPushHandler) Unsubscribe(c *gin.Context) {
	if h.webpush == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "webpush is not configured",
		})
		return
	}

	// Get user from session
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "user not authenticated",
		})
		return
	}

	// Get username from session
	username := ""
	if userMap, ok := user.(map[string]interface{}); ok {
		if sessionUsername, exists := userMap["username"]; exists && sessionUsername != "" {
			username = sessionUsername.(string)
		}
	}

	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "unable to identify user",
		})
		return
	}

	// Unsubscribe the user
	if err := h.webpush.Unsubscribe(username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to unsubscribe user",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "successfully unsubscribed from push notifications",
	})
}

// UnsubscribeByEndpoint handles unsubscription by specific endpoint.
func (h *WebPushHandler) UnsubscribeByEndpoint(c *gin.Context) {
	if h.webpush == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "webpush is not configured",
		})
		return
	}

	// Parse the request body to get the endpoint
	var request struct {
		Endpoint string `json:"endpoint"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request data",
		})
		return
	}

	if request.Endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "endpoint is required",
		})
		return
	}

	// Get user from session
	user := c.MustGet("user").(*models.User)

	// Unsubscribe by endpoint
	if err := h.webpush.UnsubscribeByEndpoint(user.Username, request.Endpoint); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to unsubscribe endpoint",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "successfully unsubscribed endpoint",
	})
}

// TestNotification sends a test push notification to the current user.
func (h *WebPushHandler) TestNotification(c *gin.Context) {
	if h.webpush == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "webpush is not configured",
		})
		return
	}

	// Get user from session
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "user not authenticated",
		})
		return
	}

	// Get username from session
	username := ""
	if userMap, ok := user.(map[string]interface{}); ok {
		if sessionUsername, exists := userMap["username"]; exists && sessionUsername != "" {
			username = sessionUsername.(string)
		}
	}

	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "unable to identify user",
		})
		return
	}

	// Send test notification
	if err := h.webpush.TestNotification(c.Request.Context(), username); err != nil {
		// Check if all subscriptions are invalid
		var invalidErr *webpush.ErrAllSubscriptionsInvalid
		if errors.As(err, &invalidErr) {
			c.JSON(http.StatusGone, gin.H{
				"error":   "all push subscriptions are invalid or expired",
				"message": "Please re-enable push notifications",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to send test notification",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "test notification sent",
	})
}

// GetSubscriptionStatus checks if the current browser/endpoint has an active subscription.
func (h *WebPushHandler) GetSubscriptionStatus(c *gin.Context) {
	if h.webpush == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "webpush is not configured",
		})
		return
	}

	// Parse the request body to get the endpoint for verification
	var request struct {
		Endpoint string `json:"endpoint"`
	}

	// For GET requests, endpoint might be in query params or we check all user subscriptions
	endpoint := c.Query("endpoint")
	if endpoint == "" {
		// If no endpoint provided in GET, try to parse from body
		if c.Request.ContentLength > 0 {
			if err := c.ShouldBindJSON(&request); err == nil {
				endpoint = request.Endpoint
			}
		}
	}

	// Get user from session
	user := c.MustGet("user").(*models.User)

	// If we have a specific endpoint, check if that subscription exists
	if endpoint != "" {
		// Find subscription by endpoint
		subscription, userID, exists := h.webpush.GetSubscriptionByEndpoint(endpoint)
		if exists && userID == user.Username {
			c.JSON(http.StatusOK, gin.H{
				"subscribed":      true,
				"username":        user.Username,
				"endpoint":        endpoint,
				"subscription_id": subscription.ID,
				"count":           h.webpush.GetUserSubscriptionCount(user.Username),
			})
			return
		}

		// Endpoint not found or doesn't belong to user
		c.JSON(http.StatusOK, gin.H{
			"subscribed": false,
			"username":   user.Username,
			"endpoint":   endpoint,
			"count":      h.webpush.GetUserSubscriptionCount(user.Username),
		})
		return
	}

	subscriptionCount := h.webpush.GetUserSubscriptionCount(user.Username)
	c.JSON(http.StatusOK, gin.H{
		"subscribed": false,
		"username":   user.Username,
		"count":      subscriptionCount,
	})
}

// GetUserSubscriptions returns all subscriptions for the current user.
func (h *WebPushHandler) GetUserSubscriptions(c *gin.Context) {
	if h.webpush == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "webpush is not configured",
		})
		return
	}

	// Get user from session
	user := c.MustGet("user").(*models.User)

	// Get all subscriptions for the user
	subscriptions, exists := h.webpush.GetSubscriptions(user.Username)
	if !exists {
		c.JSON(http.StatusOK, gin.H{
			"subscriptions": []interface{}{},
			"count":         0,
		})
		return
	}

	// Convert to response format (hide sensitive keys)
	responseSubscriptions := make([]gin.H, len(subscriptions))
	for i, sub := range subscriptions {
		responseSubscriptions[i] = gin.H{
			"id":         sub.ID,
			"endpoint":   sub.Endpoint,
			"created_at": sub.CreatedAt,
			"user_agent": sub.UserAgent,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"subscriptions": responseSubscriptions,
		"count":         len(subscriptions),
	})
}
