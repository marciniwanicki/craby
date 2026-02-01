package main

import (
	"fmt"

	"github.com/marciniwanicki/craby/internal/config"
	"github.com/spf13/cobra"
)

func toolsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "List loaded external tools",
		Long:  "Display all external tools loaded from ~/.craby/tools/ with their status and descriptions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printTools()
		},
	}
}

func printTools() error {
	tools, statuses, err := config.LoadAndCheckTools()
	if err != nil {
		return fmt.Errorf("failed to load tools: %w", err)
	}

	// Also get all tool definitions (including unavailable ones)
	allTools, _ := config.LoadExternalTools()

	if len(allTools) == 0 {
		fmt.Printf("%sNo external tools found.%s\n", colorGray, colorReset)
		fmt.Printf("%sAdd tools to ~/.craby/tools/<name>/<name>.yaml%s\n", colorGray, colorReset)
		return nil
	}

	fmt.Printf("%s╭─ External Tools ─────────────────────────────────────────╮%s\n", colorGray, colorReset)
	fmt.Printf("%s│%s\n", colorGray, colorReset)

	for _, tool := range allTools {
		status, hasStatus := statuses[tool.Name]

		// Determine status indicator
		var statusIcon, statusColor string
		if hasStatus && status.Available {
			statusIcon = "●"
			statusColor = "\033[32m" // Green
		} else {
			statusIcon = "○"
			statusColor = "\033[31m" // Red
		}

		// Tool name and status
		fmt.Printf("%s│%s  %s%s%s %s%s%s\n",
			colorGray, colorReset,
			statusColor, statusIcon, colorReset,
			colorWhite, tool.Name, colorReset)

		// Command
		if tool.Access.Type == "shell" && tool.Access.Command != "" {
			fmt.Printf("%s│%s     Command: %s%s%s\n",
				colorGray, colorReset,
				colorLightYellow, tool.Access.Command, colorReset)
		}

		// Description
		if tool.Description != "" {
			fmt.Printf("%s│%s     %s\n", colorGray, colorReset, tool.Description)
		}

		// When to use
		if tool.WhenToUse != "" {
			fmt.Printf("%s│%s     %sWhen:%s %s\n",
				colorGray, colorReset,
				colorGray, colorReset, tool.WhenToUse)
		}

		// Status message if not available
		if hasStatus && !status.Available {
			fmt.Printf("%s│%s     %sStatus: %s%s\n",
				colorGray, colorReset,
				"\033[31m", status.Message, colorReset)
		}

		fmt.Printf("%s│%s\n", colorGray, colorReset)
	}

	fmt.Printf("%s╰──────────────────────────────────────────────────────────╯%s\n", colorGray, colorReset)

	// Summary
	available := len(tools)
	total := len(allTools)
	fmt.Printf("\n%s%d/%d tools available%s\n", colorGray, available, total, colorReset)

	if available > 0 {
		fmt.Printf("%sTools extend context via automatic --help discovery on first use.%s\n", colorGray, colorReset)
	}

	// Show tools directory
	toolsDir, _ := config.ToolsDir()
	fmt.Printf("%sTools directory: %s%s\n", colorGray, toolsDir, colorReset)

	return nil
}

// printToolsCompact prints a compact version for use in chat
func printToolsCompact() error {
	tools, statuses, err := config.LoadAndCheckTools()
	if err != nil {
		return fmt.Errorf("failed to load tools: %w", err)
	}

	allTools, _ := config.LoadExternalTools()

	if len(allTools) == 0 {
		fmt.Printf("%sNo external tools configured.%s\n", colorGray, colorReset)
		fmt.Printf("%sAdd tools to ~/.craby/tools/<name>/<name>.yaml%s\n\n", colorGray, colorReset)
		return nil
	}

	fmt.Printf("%sExternal Tools:%s\n", colorGray, colorReset)

	for _, tool := range allTools {
		status, hasStatus := statuses[tool.Name]

		var statusIcon, statusColor string
		if hasStatus && status.Available {
			statusIcon = "●"
			statusColor = "\033[32m"
		} else {
			statusIcon = "○"
			statusColor = "\033[31m"
		}

		fmt.Printf("  %s%s%s %s%s%s - %s",
			statusColor, statusIcon, colorReset,
			colorWhite, tool.Name, colorReset,
			tool.Description)

		if hasStatus && !status.Available {
			fmt.Printf(" %s(%s)%s", "\033[31m", status.Message, colorReset)
		}
		fmt.Println()
	}

	fmt.Printf("\n%s%d/%d available • Discovery via --help on first use%s\n\n",
		colorGray, len(tools), len(allTools), colorReset)

	return nil
}
