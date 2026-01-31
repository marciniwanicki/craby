package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/marciniwanicki/crabby/internal/client"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	port      int
	ollamaURL string
	model     string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "crabby [message]",
		Short: "An open-source personal AI assistant designed for experimental learning and daily utility.",
		Long: `An open-source personal AI assistant designed for experimental learning and daily utility.

If a message is provided directly, it will be sent as a chat query.
Example: crabby "What is the weather today?"

For interactive chat, use: crabby chat`,
		// Allow arbitrary args so we can treat them as chat messages
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// If args provided, send as one-shot message
			if len(args) > 0 {
				c := client.NewClient(port)
				ctx := context.Background()

				if !c.IsRunning(ctx) {
					return fmt.Errorf("daemon is not running. Start it with: crabby daemon")
				}

				message := strings.Join(args, " ")
				fmt.Print("\033[90m") // grey
				err := c.Chat(ctx, message, os.Stdout, client.ChatOptions{})
				fmt.Print("\033[0m") // reset
				return err
			}
			// No args, show help
			return cmd.Help()
		},
	}

	// Global flags
	rootCmd.PersistentFlags().IntVar(&port, "port", 8787, "Daemon listen port")
	rootCmd.PersistentFlags().StringVar(&ollamaURL, "ollama-url", "http://localhost:11434", "Ollama API endpoint")
	rootCmd.PersistentFlags().StringVar(&model, "model", "qwen2.5:14b", "Model to use for chat")

	// Add subcommands
	rootCmd.AddCommand(daemonCmd())
	rootCmd.AddCommand(chatCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(stopCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
