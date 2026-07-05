package gateway

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shadow-code/shadow-mcp/internal/aggregator"
	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/downstream"
	"github.com/shadow-code/shadow-mcp/internal/profile"
	"github.com/shadow-code/shadow-mcp/internal/rules"
)

// BuildServer registers the given catalog entries on one mcp.Server, each with
// a passthrough handler that forwards through Dispatch using the entry's
// original (downstream) server/tool name. Filtering happens once here, at
// server-build time - ListTools on the resulting server naturally only returns
// what's registered, with no per-call allow/deny check needed on the read path.
// engine and rec may both be nil.
func BuildServer(entries []aggregator.Entry, dm *downstream.Manager, engine *rules.Engine, clientProfile string, rec Recorder) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "shadow-mcp", Version: "0.1.0"}, nil)

	for _, entry := range entries {
		entry := entry // capture for closure

		server.AddTool(entry.Tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return Dispatch(ctx, dm, engine, rec, clientProfile, entry.ServerName, entry.OriginalName, entry.ExposedName, req.Params.Arguments)
		})
	}

	return server
}

// BuildProfileServer filters catalog down to what p.Tools allows and builds an
// mcp.Server exposing only that subset, tagging every dispatched call with
// p.Name as the client_profile rules see. If p.LazyLoad is enabled, only the
// most-frequently-used entries (per usageCounts, keyed by exposed tool name)
// are registered directly, backed by list_all_tools/call_deferred_tool
// meta-tools for the rest - see buildLazyServer. usageCounts may be nil.
func BuildProfileServer(catalog *aggregator.Catalog, p *config.Profile, dm *downstream.Manager, engine *rules.Engine, rec Recorder, usageCounts map[string]int64) *mcp.Server {
	entries := profile.FilterEntries(catalog, p.Tools)
	if p.LazyLoad != nil && p.LazyLoad.Enabled {
		return buildLazyServer(entries, p.LazyLoad.EffectiveLimit(), usageCounts, dm, engine, p.Name, rec)
	}
	return BuildServer(entries, dm, engine, p.Name, rec)
}
