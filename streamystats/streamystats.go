package streamystats

import (
	"net/http"

	"github.com/jon4hz/jellysweep/config"
)

type Client struct {
	cfg        *config.StreamystatsConfig
	httpClient *http.Client
}

func New(cfg *config.StreamystatsConfig) *Client {
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{},
	}
}
