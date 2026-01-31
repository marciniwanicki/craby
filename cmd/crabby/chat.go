package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/marciniwanicki/crabby/internal/client"
	"github.com/spf13/cobra"
)

const (
	colorReset  = "\033[0m"
	colorOrange = "\033[38;5;215m"
	colorGray   = "\033[90m"
)

var (
	verbose bool
	quiet   bool
)

const crabASCII = `
 ▀▄  ▄▀
 ▄████▄
 ▀ ▀▀ ▀
`

func chatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start interactive chat",
		Long:  "Start an interactive REPL mode for chatting with the AI.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := client.NewClient(port)
			ctx := context.Background()

			// Determine verbosity
			verbosity := client.VerbosityNormal
			if quiet {
				verbosity = client.VerbosityQuiet
			} else if verbose {
				verbosity = client.VerbosityVerbose
			}

			opts := client.ChatOptions{
				Verbosity: verbosity,
			}

			// Check if daemon is running
			if !c.IsRunning(ctx) {
				return fmt.Errorf("daemon is not running. Start it with: crabby daemon")
			}

			// Interactive REPL mode
			return runREPL(ctx, c, opts)
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show tool call details and results")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Only show assistant responses (hide tool info)")

	return cmd
}

func printBanner(c *client.Client, ctx context.Context) {
	// Print crab ASCII art in orange
	fmt.Print(colorOrange)
	fmt.Print(crabASCII)
	fmt.Print(colorReset)

	// Get status for model info
	status, err := c.Status(ctx)
	if err == nil {
		fmt.Printf("%sModel: %s  •  Version: %s%s\n", colorGray, status.Model, status.Version, colorReset)
	}

	// Instructions in gray
	fmt.Printf("%sType 'exit' to leave  •  Ctrl+C to interrupt%s\n\n", colorGray, colorReset)
}

func runREPL(ctx context.Context, c *client.Client, opts client.ChatOptions) error {
	scanner := bufio.NewScanner(os.Stdin)
	printBanner(c, ctx)

	for {
		fmt.Print("❯ ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			break
		}

		if input == "/history" {
			if err := c.PrintHistory(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
			continue
		}

		if input == "/context" {
			context, err := c.GetContext(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				fmt.Printf("%s\n\n", context)
			}
			continue
		}

		if strings.HasPrefix(input, "/context ") {
			newContext := strings.TrimPrefix(input, "/context ")
			if newContext == "clear" {
				newContext = ""
			}
			if err := c.SetContext(ctx, newContext); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else if newContext == "" {
				fmt.Printf("%sContext cleared.%s\n\n", colorGray, colorReset)
			} else {
				fmt.Printf("%sContext set.%s\n\n", colorGray, colorReset)
			}
			continue
		}

		if err := c.Chat(ctx, input, os.Stdout, opts); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}
