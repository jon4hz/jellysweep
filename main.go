package main

import (
	"os"

	"github.com/jon4hz/jellysweep/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
