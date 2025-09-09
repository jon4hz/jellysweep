package static

import (
	"embed"
	"fmt"
)

//go:embed static/*
var StaticFS embed.FS

// GetJellysweepLogo reads and returns the jellysweep.png logo from the embedded static files.
func GetJellysweepLogo() ([]byte, error) {
	logoData, err := StaticFS.ReadFile("static/jellysweep.png")
	if err != nil {
		return nil, fmt.Errorf("failed to read jellysweep logo from embedded files: %w", err)
	}
	return logoData, nil
}
