package handler

import (
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

// GetSubscriptionStatus checks if the current browser/endpoint has an active subscription.
func (h *WebPushHandler) GetSubscriptionStatus(c *gin.Context) {
	if h.webpush == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "webpush is not configured",
		})
		return
	}

	// For GET requests, endpoint might be in query params or we check all user subscriptions
	endpoint := c.Query("endpoint")

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
