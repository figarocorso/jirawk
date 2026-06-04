package cmd

import (
	"context"

	"github.com/figarocorso/jirawk/internal/mcp"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run a stdio MCP server exposing your Jira issues to AI agents",
	Long: `mcp starts a Model Context Protocol server on stdio so AI agents
(Claude Code, Claude Desktop, ...) can query your Jira issues through tools:

  list_issues  — in-progress / done / all
  get_issue    — full detail for a single issue
  stats        — in-progress count + weekly resolved breakdown`,
	RunE: runMCP,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(_ *cobra.Command, _ []string) error {
	cfg, client, err := loadConfigAndClient()
	if err != nil {
		return err
	}
	srv := mcp.NewServer(mcp.Options{
		Config:  cfg,
		Client:  client,
		Version: buildVersion,
	})
	return srv.Run(context.Background())
}
