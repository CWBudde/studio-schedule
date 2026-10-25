package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/webpush"
)

// newVAPIDKeysCmd prints a new VAPID key pair for web push (ADR 0017), as
// lines for an environment file.
func newVAPIDKeysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "vapid-keys",
		Short: "Print a new VAPID key pair for web push",
		Long: "Print a new VAPID key pair for web push notifications, as " + config.EnvVAPIDPublicKey + " and " +
			config.EnvVAPIDPrivateKey + " lines for the environment. Set them once, together with " +
			config.EnvVAPIDSubject + ": a new pair invalidates every browser's subscription.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys, err := webpush.GenerateKeys()
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s=%s\n%s=%s\n",
				config.EnvVAPIDPublicKey, keys.PublicKey(), config.EnvVAPIDPrivateKey, keys.PrivateKey())

			return err
		},
	}
}
