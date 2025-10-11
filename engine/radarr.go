package engine

import (
	"strings"

	radarr "github.com/devopsarr/radarr-go/radarr"
	"github.com/jon4hz/jellysweep/config"
)

func newRadarrClient(cfg *config.RadarrConfig) *radarr.APIClient {
	rcfg := radarr.NewConfiguration()

	// Don't modify the original config URL, work with a copy
	url := cfg.URL

	if strings.HasPrefix(url, "http://") {
		rcfg.Scheme = "http"
		url = strings.TrimPrefix(url, "http://")
	} else if strings.HasPrefix(url, "https://") {
		rcfg.Scheme = "https"
		url = strings.TrimPrefix(url, "https://")
	}

	rcfg.Host = url

	return radarr.NewAPIClient(rcfg)
}
