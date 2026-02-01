package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/marciniwanicki/crabby/internal/client"
	"github.com/spf13/cobra"
)

const (
	colorReset       = "\033[0m"
	colorLightYellow = "\033[93m"
	colorGray        = "\033[90m"
	colorWhite       = "\033[97m"
	cursorShow       = "\033[?25h"
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

			// Start daemon if not running
			if err := ensureDaemonRunning(ctx, c); err != nil {
				return err
			}

			// Interactive REPL mode
			return runREPL(ctx, c, opts)
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show tool call details and results")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Only show assistant responses (hide tool info)")

	return cmd
}

// ensureDaemonRunning starts the daemon in the background if it's not already running.
// It waits for the daemon to become ready before returning.
func ensureDaemonRunning(ctx context.Context, c *client.Client) error {
	if c.IsRunning(ctx) {
		return nil
	}

	// Get the path to the current executable
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Build command with current flags
	args := []string{"daemon", fmt.Sprintf("--port=%d", port)}
	if ollamaURL != "" {
		args = append(args, fmt.Sprintf("--ollama-url=%s", ollamaURL))
	}
	if model != "" {
		args = append(args, fmt.Sprintf("--model=%s", model))
	}

	cmd := exec.Command(executable, args...)
	// Detach from parent process
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait for daemon to become ready
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for daemon to start")
		case <-ticker.C:
			if c.IsRunning(ctx) {
				return nil
			}
		}
	}
}

func printBanner(c *client.Client, ctx context.Context) {
	// Print crab ASCII art in orange
	fmt.Print(colorLightYellow)
	fmt.Print(crabASCII)
	fmt.Print(colorReset)

	// Get status for model info
	status, err := c.Status(ctx)
	if err == nil {
		fmt.Printf("%sModel: %s  •  Version: %s%s\n", colorGray, status.Model, status.Version, colorReset)
	}

	// Instructions in gray
	fmt.Printf("%sType '/exit' to leave  •  '/terminate' to stop daemon  •  Ctrl+C to interrupt%s\n\n", colorGray, colorReset)
}

func runREPL(ctx context.Context, c *client.Client, opts client.ChatOptions) error {
	// Ensure cursor is restored on exit (normal or interrupt)
	defer fmt.Print(cursorShow)

	// Handle interrupt signal to restore cursor
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Print(cursorShow)
		os.Exit(0)
	}()

	scanner := bufio.NewScanner(os.Stdin)
	printBanner(c, ctx)

	for {
		fmt.Printf("%s❯%s ", colorWhite, colorReset)
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Reprint the prompt line in gray (move up, clear, reprint)
		fmt.Printf("\033[F\033[K%s❯%s %s\n", colorGray, colorReset, input)

		if input == "/exit" {
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

		if input == "/terminate" {
			if err := c.Shutdown(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Error stopping daemon: %v\n", err)
			} else {
				fmt.Println("Daemon stopped.")
			}
			fmt.Println("Goodbye!")
			break
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
