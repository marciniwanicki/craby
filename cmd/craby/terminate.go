package main

import (
	"context"
	"fmt"

	"github.com/marciniwanicki/craby/internal/client"
	"github.com/spf13/cobra"
)

func terminateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "terminate",
		Short: "Stop the daemon",
		Long:  "Stop the running crabby daemon gracefully.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := client.NewClient(port)
			ctx := context.Background()

			if !c.IsRunning(ctx) {
				fmt.Println("Daemon is not running")
				return nil
			}

			if err := c.Shutdown(ctx); err != nil {
				return fmt.Errorf("failed to stop daemon: %w", err)
			}

			fmt.Println("Daemon stopped")
			return nil
		},
	}
}
