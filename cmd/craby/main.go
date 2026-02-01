package main

import (
	"context"
	"os"
	"strings"

	"github.com/marciniwanicki/craby/internal/client"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	port      int
	ollamaURL string
	model     string
)

func main() {
	// Create chat command first so we can reference it
	chat := chatCmd()

	rootCmd := &cobra.Command{
		Use:   "craby [message]",
		Short: "An open-source personal AI assistant designed for experimental learning and daily utility.",
		Long: `An open-source personal AI assistant designed for experimental learning and daily utility.

If a message is provided, it will be sent as a one-shot query.
Example: craby "What is the weather today?"

Without arguments, starts interactive chat.`,
		// Allow arbitrary args so we can treat them as chat messages
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := client.NewClient(port)
			ctx := context.Background()

			// Start daemon if not running
			if err := ensureDaemonRunning(ctx, c); err != nil {
				return err
			}

			// If args provided, send as one-shot message
			if len(args) > 0 {
				message := strings.Join(args, " ")
				return c.Chat(ctx, message, os.Stdout, client.ChatOptions{})
			}

			// No args, start interactive chat
			return chat.RunE(chat, args)
		},
	}

	// Global flags
	rootCmd.PersistentFlags().IntVar(&port, "port", 8787, "Daemon listen port")
	rootCmd.PersistentFlags().StringVar(&ollamaURL, "ollama-url", "http://localhost:11434", "Ollama API endpoint")
	rootCmd.PersistentFlags().StringVar(&model, "model", "qwen2.5:14b", "Model to use for chat")

	// Add subcommands
	rootCmd.AddCommand(daemonCmd())
	rootCmd.AddCommand(chat)
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(terminateCmd())
	rootCmd.AddCommand(toolsCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
