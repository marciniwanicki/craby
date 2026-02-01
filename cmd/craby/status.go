package main

import (
	"context"
	"fmt"

	"github.com/marciniwanicki/craby/internal/client"
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check if daemon is running",
		Long:  "Check the status of the craby daemon and display information about the connected model.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := client.NewClient(port)
			ctx := context.Background()

			if !c.IsRunning(ctx) {
				fmt.Println("Daemon is not running")
				return nil
			}

			status, err := c.Status(ctx)
			if err != nil {
				return fmt.Errorf("failed to get status: %w", err)
			}

			fmt.Printf("Daemon: running\n")
			fmt.Printf("Version: %s\n", status.Version)
			fmt.Printf("Model: %s\n", status.Model)
			if status.Healthy {
				fmt.Printf("Ollama: healthy\n")
			} else {
				fmt.Printf("Ollama: not responding\n")
			}

			return nil
		},
	}
}
