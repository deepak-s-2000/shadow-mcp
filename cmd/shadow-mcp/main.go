// Command shadow-mcp is an MCP gateway that aggregates multiple downstream MCP
// servers behind a single stdio or HTTP endpoint for IDEs to connect to.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/daemon"
	"github.com/shadow-code/shadow-mcp/internal/transport"
	"github.com/shadow-code/shadow-mcp/internal/tui"
)

// defaultConfigFlag is the (empty) flag default for --config on every
// subcommand: leaving it unset (rather than baking in "shadow-mcp.yaml")
// lets resolveConfigPath tell "not passed" apart from an explicit value and
// apply its fallback search.
const defaultConfigFlag = ""

// resolveConfigPath decides which config file a command should use, in order:
//  1. an explicit --config value, used as-is
//  2. ./shadow-mcp.yaml in the current directory, if it exists (the
//     historical default, kept for existing setups)
//  3. <DataDir>/shadow-mcp.yaml, shadow-mcp's per-user data directory - the
//     same directory that already holds daemon.json/usage.json - creating a
//     starter config there on first run if nothing exists yet at all. This is
//     what lets a user who only has the shadow-mcp binary (no repo checkout,
//     no hand-authored config) get something running immediately.
func resolveConfigPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	if _, err := os.Stat("shadow-mcp.yaml"); err == nil {
		return "shadow-mcp.yaml", nil
	}

	dir, err := daemon.DataDir()
	if err != nil {
		return "", fmt.Errorf("resolving default config location: %w", err)
	}
	path := filepath.Join(dir, "shadow-mcp.yaml")

	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := config.WriteStarter(path); err != nil && !os.IsExist(err) {
		return "", fmt.Errorf("creating starter config at %s: %w", path, err)
	}
	fmt.Fprintf(os.Stderr, "shadow-mcp: no config found; created a starter config at %s\n", path)
	return path, nil
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "shadow-mcp",
		Short: "shadow-mcp aggregates multiple MCP servers behind one gateway",
	}
	root.AddCommand(newServeCmd())
	root.AddCommand(newDaemonCmd())
	root.AddCommand(newListToolsCmd())
	root.AddCommand(newUICmd())
	return root
}

func newUICmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open the terminal dashboard (live status + configured servers/profiles/rules)",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveConfigPath(configPath)
			if err != nil {
				return err
			}
			return tui.Run(path)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", defaultConfigFlag, "path to shadow-mcp config file (default: ./shadow-mcp.yaml, falling back to a per-user config location)")
	return cmd
}

func newListToolsCmd() *cobra.Command {
	var configPath, profileArg string
	var timeoutSecs int

	cmd := &cobra.Command{
		Use:   "list-tools",
		Short: "Print the aggregated, collision-resolved tool catalog",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveConfigPath(configPath)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			return transport.ListTools(cmd.Context(), cfg, profileArg, timeoutSecs)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", defaultConfigFlag, "path to shadow-mcp config file (default: ./shadow-mcp.yaml, falling back to a per-user config location)")
	cmd.Flags().StringVar(&profileArg, "profile", "", "only show tools allowed for this profile's stdio_arg (default: show everything)")
	cmd.Flags().IntVar(&timeoutSecs, "timeout", 30, "timeout in seconds for connecting to downstream servers (default: 30)")
	return cmd
}

func newDaemonCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Run the persistent gateway daemon (usually auto-started, not invoked directly)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveConfigPath(configPath)
			if err != nil {
				return err
			}
			return daemon.Run(signalContext(cmd), path)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", defaultConfigFlag, "path to shadow-mcp config file (default: ./shadow-mcp.yaml, falling back to a per-user config location)")
	return cmd
}

func newServeCmd() *cobra.Command {
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Expose the aggregated MCP gateway to an IDE",
	}
	serve.AddCommand(newServeStdioCmd())
	serve.AddCommand(newServeHTTPCmd())
	return serve
}

func newServeStdioCmd() *cobra.Command {
	var configPath, profileArg string

	cmd := &cobra.Command{
		Use:   "stdio",
		Short: "Expose the gateway over stdio (relays to an auto-started daemon)",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveConfigPath(configPath)
			if err != nil {
				return err
			}
			return transport.RunStdio(cmd.Context(), path, profileArg)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", defaultConfigFlag, "path to shadow-mcp config file (default: ./shadow-mcp.yaml, falling back to a per-user config location)")
	cmd.Flags().StringVar(&profileArg, "profile", "", "expose only the tools allowed for this profile's stdio_arg (default: expose everything)")
	return cmd
}

func newServeHTTPCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "http",
		Short: "Run the gateway as a long-lived HTTP server exposing every configured profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveConfigPath(configPath)
			if err != nil {
				return err
			}
			return daemon.Run(signalContext(cmd), path)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", defaultConfigFlag, "path to shadow-mcp config file (default: ./shadow-mcp.yaml, falling back to a per-user config location)")
	return cmd
}

// signalContext returns a context cancelled on SIGINT/SIGTERM, so long-running
// commands (daemon, serve http) shut down cleanly instead of being killed mid-write.
func signalContext(cmd *cobra.Command) context.Context {
	ctx, _ := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	return ctx
}
