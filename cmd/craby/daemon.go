package main

import (
	"github.com/marciniwanicki/craby/internal/daemon"
	"github.com/spf13/cobra"
)

func daemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Start the daemon server",
		Long:  "Start the craby daemon server in the foreground. The daemon handles chat requests and communicates with Ollama.",
		RunE: func(cmd *cobra.Command, args []string) error {
			server := daemon.NewServer(port, ollamaURL, model)
			return server.Run()
		},
	}
}
