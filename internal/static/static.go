package static

import (
	"embed"
)

//go:embed static/*
var StaticFS embed.FS
