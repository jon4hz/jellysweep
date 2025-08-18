package cmd

import (
	"fmt"

	"github.com/jon4hz/jellysweep/database"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	Long:  `Run database migrations to set up or update the database schema.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := database.New("./data/jellysweep.db")
		if err != nil {
			return fmt.Errorf("failed to initialize database: %w", err)
		}
		defer db.Close() //nolint: errcheck

		fmt.Println("Database migrations completed successfully!")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}
