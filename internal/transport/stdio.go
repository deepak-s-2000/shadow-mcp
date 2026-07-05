// Package transport wires the daemon's aggregated tool server to IDE-facing transports.
package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

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

	path := "/mcp/_all"
	if profileName != "" {
		path = pathForProfile(cfg, profileName)
	}

	daemonSession, err := connectDaemon(ctx, configPath, path)
	if err != nil {
		return err
	}
	dc := &daemonConn{configPath: configPath, path: path, session: daemonSession}
	defer dc.Close()

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
			return dc.CallTool(ctx, toolName, args)
		})
	}

	return relay.Run(ctx, &mcp.StdioTransport{})
}

// connectDaemon ensures a daemon is running for configPath (auto-starting one
// if needed) and connects to its MCP relay endpoint for path.
func connectDaemon(ctx context.Context, configPath, path string) (*mcp.ClientSession, error) {
	admin, err := adminclient.EnsureRunning(configPath)
	if err != nil {
		return nil, fmt.Errorf("connecting to shadow-mcp daemon: %w", err)
	}

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "shadow-mcp-stdio-adapter", Version: "0.1.0"}, nil)
	daemonTransport := &mcp.StreamableClientTransport{
		Endpoint:   admin.MCPEndpoint(path),
		HTTPClient: admin.AuthenticatedHTTPClient(),
	}
	session, err := mcpClient.Connect(ctx, daemonTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to shadow-mcp daemon's MCP endpoint: %w", err)
	}
	return session, nil
}

// daemonConn holds the relay's connection to the daemon's MCP endpoint,
// transparently reconnecting - which re-runs EnsureRunning, so a daemon that
// died outright gets auto-restarted too, not just a dropped connection to a
// live one - if a call finds the connection dead instead of leaving every IDE
// tool call broken until the relay itself is restarted.
type daemonConn struct {
	configPath string
	path       string

	mu      sync.Mutex
	session *mcp.ClientSession
}

func (d *daemonConn) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.session.Close()
}

func (d *daemonConn) CallTool(ctx context.Context, name string, args any) (*mcp.CallToolResult, error) {
	d.mu.Lock()
	session := d.session
	d.mu.Unlock()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err == nil || !errors.Is(err, mcp.ErrConnectionClosed) {
		return res, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.session == session {
		newSession, connErr := connectDaemon(ctx, d.configPath, d.path)
		if connErr != nil {
			return nil, fmt.Errorf("%w (reconnect failed: %v)", err, connErr)
		}
		session.Close()
		d.session = newSession
	}
	return d.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
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
