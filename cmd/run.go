package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/callbacks/langfuse"
	"github.com/cloudwego/eino/callbacks"
	"github.com/spf13/cobra"
	"github.com/tk103331/oni3/agent"
	"github.com/tk103331/oni3/config"
)

// printHeader prints a formatted header for better visual separation
func printHeader(title string) {
	fmt.Printf("\n🔹 %s\n", title)
	fmt.Println(strings.Repeat("─", len(title)+4))
}

// printSuccess prints a success message with timing
func printSuccess(message string, start time.Time) {
	duration := time.Since(start)
	fmt.Printf("\n✅ %s (completed in %v)\n", message, duration.Round(time.Millisecond))
}

// printError prints an error message with better formatting
func printError(message string, err error) {
	fmt.Printf("\n❌ %s: %v\n", message, err)
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the agent",
	Long:  `Run the agent with specified parameters`,
	RunE: func(cmd *cobra.Command, args []string) error {
		startTime := time.Now()

		cfg := config.GetConfig()

		// Get parameters
		agentName, _ := cmd.Flags().GetString("agent")
		prompt, _ := cmd.Flags().GetString("prompt")

		// Print execution header
		printHeader("Agent Execution")
		fmt.Printf("🤖 Agent: %s\n📝 Prompt: %s\n", agentName, prompt)

		// Initialize phase
		fmt.Printf("\n⚙️  Initializing...")
		initStart := time.Now()

		if cfg.Settings.Langfuse != nil {
			handler, flusher := langfuse.NewLangfuseHandler(cfg.Settings.Langfuse.SDKConfig())
			defer flusher()
			callbacks.AppendGlobalHandlers(handler) // Set langfuse as global callback
			fmt.Printf(" ✓ Langfuse enabled")
		}

		// Create Agent factory
		factory := agent.NewFactory(cfg)

		// Create Agent
		agentInstance, err := factory.CreateAgent(agentName)
		if err != nil {
			printError("Failed to create agent", err)
			return fmt.Errorf("failed to create Agent: %w", err)
		}

		printSuccess("Agent initialized", initStart)
		fmt.Printf("\n🚀 Executing agent...")
		fmt.Println() // Add spacing before agent output

		// Run Agent
		if err := agentInstance.Run(prompt); err != nil {
			printError("Agent execution failed", err)
			return fmt.Errorf("failed to run Agent: %w", err)
		}

		execStart := time.Now()
		printSuccess("Agent execution completed", execStart)
		printHeader("Summary")
		fmt.Printf("⏱️  Total execution time: %v\n", time.Since(startTime).Round(time.Millisecond))
		fmt.Println()

		return nil
	},
}

func init() {
	// Add run subcommand to root command
	RootCmd.AddCommand(runCmd)

	// Add parameters for run subcommand
	runCmd.Flags().StringP("agent", "a", "", "Specify the Agent to run")
	runCmd.Flags().StringP("prompt", "p", "", "Specify the prompt for Agent")

	// Set required parameters
	runCmd.MarkFlagRequired("agent")
	runCmd.MarkFlagRequired("prompt")
}
