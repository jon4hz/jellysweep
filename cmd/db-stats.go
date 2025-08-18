package cmd

import (
	"fmt"
	"time"

	"github.com/jon4hz/jellysweep/database"
	"github.com/spf13/cobra"
)

var dbStatsCmd = &cobra.Command{
	Use:   "db-stats",
	Short: "Show database statistics",
	Long:  `Display statistics about cleanup runs and media processing.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := database.New("./data/jellysweep.db")
		if err != nil {
			return fmt.Errorf("failed to initialize database: %w", err)
		}
		defer db.Close() //nolint: errcheck

		stats, err := db.GetCleanupStats(cmd.Context(), nil)
		if err != nil {
			return fmt.Errorf("failed to get database stats: %w", err)
		}

		fmt.Println("Database Statistics:")
		fmt.Printf("Total Cleanup Runs: %d\n", stats.TotalRuns)
		fmt.Printf("Successful Runs: %d\n", stats.SuccessfulRuns)
		fmt.Printf("Failed Runs: %d\n", stats.FailedRuns)
		fmt.Printf("Total Items Processed: %d\n", stats.TotalItemsProcessed)
		fmt.Printf("Total Items Deleted: %d\n", stats.TotalItemsDeleted)
		fmt.Printf("Total Size Deleted: %d bytes\n", stats.TotalSizeDeleted)

		if stats.AverageRunDuration != nil {
			fmt.Printf("Average Run Duration: %s\n", *stats.AverageRunDuration)
		}

		if stats.LastSuccessfulRun != nil {
			fmt.Printf("Last Successful Run: %s\n", stats.LastSuccessfulRun.Format(time.RFC3339))
		}

		fmt.Printf("Most Deleted in Single Run: %d\n", stats.MostDeletedInSingleRun)

		// Get recent cleanup history
		history, err := db.GetCleanupRunHistory(cmd.Context(), 5, 0)
		if err == nil && len(history) > 0 {
			fmt.Println("\nRecent Cleanup Runs:")
			for _, run := range history {
				status := string(run.Status)
				fmt.Printf("  ID: %d, Started: %s, Status: %s, Items: %d\n",
					run.ID, run.StartedAt.Format("2006-01-02 15:04:05"), status, run.TotalItems)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(dbStatsCmd)
}
