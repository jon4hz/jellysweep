package engine

import (
	"strings"

	sonarr "github.com/devopsarr/sonarr-go/sonarr"
	"github.com/jon4hz/jellysweep/config"
)

func newSonarrClient(cfg *config.SonarrConfig) *sonarr.APIClient {
	scfg := sonarr.NewConfiguration()

	// Don't modify the original config URL, work with a copy
	url := cfg.URL

	if strings.HasPrefix(url, "http://") {
		scfg.Scheme = "http"
		url = strings.TrimPrefix(url, "http://")
	} else if strings.HasPrefix(url, "https://") {
		scfg.Scheme = "https"
		url = strings.TrimPrefix(url, "https://")
	}

	scfg.Host = url

	return sonarr.NewAPIClient(scfg)
}
