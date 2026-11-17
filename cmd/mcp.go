package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/logger"
	"github.com/tk103331/oni3/mcp"
	mcpserver "github.com/tk103331/oni3/mcp/server"
	"github.com/tk103331/oni3/version"
)

const (
	mcpCallAgentToolName  = mcpserver.CallAgentToolName
	mcpListAgentsToolName = mcpserver.ListAgentsToolName
	mcpModelsToolName     = mcpserver.ListModelsToolName
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run Oni3 as an MCP server over stdio",
	Args:  cobra.NoArgs,
	Long: `Run Oni3 as a Model Context Protocol (MCP) server over stdio.

The server exposes three tools: call_agent(agent, prompt, model?), list_agents()
and list_models(). call_agent runs any configured agent, including the built-in
OniThree agent; model is an optional alias returned by list_models.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.GetConfig()
		if cfg == nil {
			return fmt.Errorf("global configuration not initialized")
		}
		if _, err := ensureBuiltinAgent(cfg); err != nil {
			return err
		}

		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		// Agent runs may use tools from external MCP servers, so initialize the
		// manager synchronously instead of the background setup used by other
		// commands.
		if err := mcp.InitializeGlobalManager(ctx, cfg); err != nil {
			return fmt.Errorf("failed to initialize MCP manager: %w", err)
		}
		defer mcp.CloseGlobalManager()

		exposed, err := mcpTools(cfg)
		if err != nil {
			return err
		}

		cwd, _ := os.Getwd()
		srv, err := mcpserver.New(mcpserver.Options{
			Name:    "oni3",
			Version: version.Version,
			Instructions: fmt.Sprintf(
				"Use list_agents to discover agents, call_agent to run one, and "+
					"list_models to discover model aliases. Relative paths resolve against %s.", cwd),
			Tools: exposed,
		})
		if err != nil {
			return fmt.Errorf("failed to create MCP server: %w", err)
		}

		logger.Info("MCP", "Serving call_agent, list_agents and list_models over stdio")
		// stdout carries the MCP protocol; logs go to ~/.oni3/oni3.log.
		if err := server.ServeStdio(srv); err != nil {
			return fmt.Errorf("MCP server failed: %w", err)
		}
		return nil
	},
}

// mcpTools is kept as a small wrapper so CLI tests and the command share the
// exact same tool surface as the web MCP server.
func mcpTools(cfg *config.Config) ([]tool.InvokableTool, error) {
	return mcpserver.AgentTools(cfg)
}

func init() {
	RootCmd.AddCommand(mcpCmd)
}
