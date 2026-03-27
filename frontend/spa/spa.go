package spa

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed dist/*
var frontendFS embed.FS

// SetupSPA configures the Gin engine to serve the React SPA.
// It serves static assets from the embedded dist/ directory and falls back to index.html
// for any unmatched route (client-side routing).
func SetupSPA(r *gin.Engine) error {
	distSub, err := fs.Sub(frontendFS, "dist")
	if err != nil {
		return err
	}
	fileServer := http.FileServer(http.FS(distSub))

	indexHTML, err := frontendFS.ReadFile("dist/index.html")
	if err != nil {
		return err
	}

	// Serve the SPA assets directory
	r.GET("/assets/*filepath", gin.WrapH(fileServer))

	// Catch-all: Serve index.html for all non-API, non-static routes
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Don't interfere with API, auth, static, plugin, or health routes
		if strings.HasPrefix(path, "/api/") ||
			strings.HasPrefix(path, "/admin/api/") ||
			strings.HasPrefix(path, "/auth/") ||
			strings.HasPrefix(path, "/plugin/") ||
			strings.HasPrefix(path, "/static/") ||
			strings.HasPrefix(path, "/health") ||
			strings.HasPrefix(path, "/logout") {
			c.Status(http.StatusNotFound)
			return
		}

		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})

	return nil
}
