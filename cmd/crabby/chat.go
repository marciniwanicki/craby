package main

import (
	"bufio"
	"context"
	"encoding/json"
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
	colorRed         = "\033[31m"
	colorLightYellow = "\033[93m"
	colorGray        = "\033[90m"
	colorWhite       = "\033[97m"
	colorWhiteBold   = "\033[1;97m"
	cursorShow       = "\033[?25h"
)

var (
	verbose bool
	quiet   bool
)

// Crab logo lines for side-by-side rendering with name
var crabLines = []string{
	" ▀▄  ▄▀",
	" ▄████▄",
	" ▀ ▀▀ ▀",
}

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

func printChatHelp() {
	fmt.Printf("\n%sAvailable commands:%s\n", colorWhite, colorReset)
	fmt.Printf("  %s/help%s        Show this help message\n", colorLightYellow, colorReset)
	fmt.Printf("  %s/exit%s        Exit the chat\n", colorLightYellow, colorReset)
	fmt.Printf("  %s/terminate%s   Stop the daemon and exit\n", colorLightYellow, colorReset)
	fmt.Printf("  %s/tools%s       List available external tools\n", colorLightYellow, colorReset)
	fmt.Printf("  %s/tool list%s   List all registered LLM tools\n", colorLightYellow, colorReset)
	fmt.Printf("  %s/tool run <name> key=value ...%s  Run a tool directly\n", colorLightYellow, colorReset)
	fmt.Printf("  %s/history%s     Show conversation history\n", colorLightYellow, colorReset)
	fmt.Printf("  %s/context%s     Show current context\n", colorLightYellow, colorReset)
	fmt.Printf("  %s/context <text>%s  Set context for the conversation\n", colorLightYellow, colorReset)
	fmt.Printf("  %s/context clear%s   Clear the context\n", colorLightYellow, colorReset)
	fmt.Println()
}

func printBanner(c *client.Client, ctx context.Context) {
	// Get status for version info
	status, err := c.Status(ctx)
	version := "0.0.0"
	model := "unknown"
	if err == nil {
		version = status.Version
		model = status.Model
	}

	// Print crab ASCII art with name and version next to it
	fmt.Println()
	fmt.Printf("%s%s%s  %sCraby%s\n", colorRed, crabLines[0], colorReset, colorWhiteBold, colorReset)
	fmt.Printf("%s%s%s  %sv%s%s\n", colorRed, crabLines[1], colorReset, colorGray, version, colorReset)
	fmt.Printf("%s%s%s\n", colorRed, crabLines[2], colorReset)

	// Model info
	fmt.Printf("%sModel: %s%s\n", colorGray, model, colorReset)

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

		if input == "/help" {
			printChatHelp()
			continue
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

		if input == "/tools" {
			if err := printToolsCompact(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
			continue
		}

		if input == "/tool list" {
			if err := printRegisteredTools(ctx, c); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
			continue
		}

		if strings.HasPrefix(input, "/tool run ") {
			if err := runToolCommand(ctx, c, strings.TrimPrefix(input, "/tool run ")); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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

// printRegisteredTools lists all tools registered with the daemon
func printRegisteredTools(ctx context.Context, c *client.Client) error {
	toolList, err := c.ListTools(ctx)
	if err != nil {
		return err
	}

	if len(toolList.Tools) == 0 {
		fmt.Printf("%sNo tools registered.%s\n\n", colorGray, colorReset)
		return nil
	}

	fmt.Printf("\n%sRegistered Tools:%s\n", colorWhite, colorReset)
	for _, tool := range toolList.Tools {
		fmt.Printf("  %s•%s %s%s%s\n", colorLightYellow, colorReset, colorWhite, tool.Name, colorReset)
		// Truncate description if too long
		desc := tool.Description
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
		fmt.Printf("    %s%s%s\n", colorGray, desc, colorReset)
	}
	fmt.Println()

	return nil
}

// runToolCommand parses and executes a tool command
// Format: <tool_name> key=value key2=value2 ...
func runToolCommand(ctx context.Context, c *client.Client, input string) error {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return fmt.Errorf("usage: /tool run <name> [key=value ...]")
	}

	toolName := parts[0]
	args := make(map[string]any)

	// Parse key=value pairs
	for _, part := range parts[1:] {
		if !strings.Contains(part, "=") {
			return fmt.Errorf("invalid argument format: %q (expected key=value)", part)
		}
		kv := strings.SplitN(part, "=", 2)
		key := kv[0]
		value := kv[1]

		// Try to parse as JSON for complex values, otherwise use as string
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			// Not valid JSON, use as string
			parsed = value
		}
		args[key] = parsed
	}

	fmt.Printf("%s⚡ Running %s...%s\n", colorGray, toolName, colorReset)

	resp, err := c.ExecuteTool(ctx, toolName, args)
	if err != nil {
		return err
	}

	if !resp.Success {
		fmt.Printf("%s✗ Error: %s%s\n", colorRed, resp.Error, colorReset)
	} else {
		fmt.Printf("%s✓ Success%s\n", "\033[32m", colorReset)
	}

	if resp.Output != "" {
		fmt.Printf("\n%s\n", resp.Output)
	}
	fmt.Println()

	return nil
}
