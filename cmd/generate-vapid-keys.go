package cmd

import (
	"fmt"

	"github.com/jon4hz/jellysweep/notify/webpush"
	"github.com/spf13/cobra"
)

var generateKeysCmd = &cobra.Command{
	Use:   "generate-vapid-keys",
	Short: "Generate VAPID keys for web push notifications",
	Long: `Generate VAPID keys for web push notifications.

These keys are required for sending push notifications to PWA clients.
Add the generated keys to your configuration file under the webpush section.`,
	RunE: generateVAPIDKeys,
}

func init() {
	rootCmd.AddCommand(generateKeysCmd)
}

func generateVAPIDKeys(cmd *cobra.Command, args []string) error {
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return fmt.Errorf("failed to generate VAPID keys: %w", err)
	}

	fmt.Println("Generated VAPID keys for web push notifications:")
	fmt.Println()
	fmt.Printf("Private Key: %s\n", privateKey)
	fmt.Printf("Public Key:  %s\n", publicKey)
	fmt.Println()
	fmt.Println("Add these to your configuration file:")
	fmt.Println()
	fmt.Println("webpush:")
	fmt.Println("  enabled: true")
	fmt.Println("  vapid_email: \"your-email@example.com\"")
	fmt.Printf("  private_key: \"%s\"\n", privateKey)
	fmt.Printf("  public_key: \"%s\"\n", publicKey)
	fmt.Println()
	fmt.Println("Note: Keep the private key secure and never share it publicly!")

	return nil
}
