// Package transport wires the daemon's aggregated tool server to IDE-facing transports.
package transport

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shadow-code/shadow-mcp/internal/adminclient"
	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/profile"
)

// RunStdio is a thin adapter: it resolves profileArg to a profile name using
// the local config file, ensures a daemon is running for that config
// (auto-starting one if needed - see internal/adminclient), connects to the
// daemon's MCP relay endpoint for that profile over HTTP, and relays every
// tool listed there through its own stdio server. This means N IDEs
// connecting via stdio share one set of downstream connections in the daemon,
// rather than each spawning its own.
func RunStdio(ctx context.Context, configPath string, profileArg string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	var profileName string
	if profileArg != "" {
		p, err := profile.ResolveStdio(cfg.Profiles, profileArg)
		if err != nil {
			return err
		}
		profileName = p.Name
	}

	admin, err := adminclient.EnsureRunning(configPath)
	if err != nil {
		return fmt.Errorf("connecting to shadow-mcp daemon: %w", err)
	}

	path := "/mcp/_all"
	if profileName != "" {
		path = pathForProfile(cfg, profileName)
	}

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "shadow-mcp-stdio-adapter", Version: "0.1.0"}, nil)
	daemonTransport := &mcp.StreamableClientTransport{
		Endpoint:   admin.MCPEndpoint(path),
		HTTPClient: admin.AuthenticatedHTTPClient(),
	}
	daemonSession, err := mcpClient.Connect(ctx, daemonTransport, nil)
	if err != nil {
		return fmt.Errorf("connecting to shadow-mcp daemon's MCP endpoint: %w", err)
	}
	defer daemonSession.Close()

	relay := mcp.NewServer(&mcp.Implementation{Name: "shadow-mcp", Version: "0.1.0"}, nil)
	for tool, err := range daemonSession.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("listing tools from shadow-mcp daemon: %w", err)
		}
		toolName := tool.Name

		relay.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args any
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return nil, err
				}
			}
			return daemonSession.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: args})
		})
	}

	return relay.Run(ctx, &mcp.StdioTransport{})
}

// pathForProfile mirrors daemon.Daemon.PathForProfile without needing an
// in-process daemon reference: it's derived purely from the local config file.
func pathForProfile(cfg *config.Config, profileName string) string {
	for _, p := range cfg.Profiles {
		if p.Name == profileName {
			if p.Identify.HTTPPath != "" {
				return p.Identify.HTTPPath
			}
			return "/mcp/" + p.Name
		}
	}
	return "/mcp/" + profileName
}
