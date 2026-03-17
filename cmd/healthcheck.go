package cmd

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var healthcheckFlags struct {
	URL     string
	Timeout time.Duration
}

func init() {
	healthcheckCmd.Flags().StringVar(&healthcheckFlags.URL, "url", "http://localhost:3002/health", "URL to check")
	healthcheckCmd.Flags().DurationVar(&healthcheckFlags.Timeout, "timeout", 3*time.Second, "HTTP client timeout")
	rootCmd.AddCommand(healthcheckCmd)
}

var healthcheckCmd = &cobra.Command{
	Use:   "healthcheck",
	Short: "Check if the Jellysweep server is healthy",
	Long:  `Perform an HTTP health check against the running server. Exits 0 if healthy, 1 otherwise.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := &http.Client{Timeout: healthcheckFlags.Timeout}
		resp, err := client.Get(healthcheckFlags.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "healthcheck failed: status %d\n", resp.StatusCode)
			os.Exit(1)
		}
	},
}
