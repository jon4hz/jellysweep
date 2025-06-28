package auth

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jon4hz/jellysweep/api/models"
)

func (p *Provider) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		if userID == nil {
			c.Redirect(http.StatusFound, "/oauth/login")
			c.Abort()
			return
		}
		// create user model from session data
		user := &models.User{
			Sub:      userID.(string),
			Email:    session.Get("user_email").(string),
			Name:     session.Get("user_name").(string),
			Username: session.Get("user_username").(string),
			IsAdmin:  session.Get("user_is_admin").(bool),
		}

		c.Set("user_id", userID)
		c.Set("user", user)
	}
}

func (p *Provider) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := c.MustGet("user").(*models.User)
		if !ok || !user.IsAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			c.Abort()
			return
		}
		c.Next()
	}
}
