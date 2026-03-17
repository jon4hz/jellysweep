package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// jsonError sends a JSON error response with {"success": false, "error": msg}.
func jsonError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{
		"success": false,
		"error":   msg,
	})
}

// jsonSuccess sends a JSON success response with {"success": true, "message": msg}.
func jsonSuccess(c *gin.Context, msg string) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": msg,
	})
}
