// Package aggregator merges tool (and, later, resource/prompt) lists from
// multiple downstream MCP servers into one collision-free catalog.
package aggregator

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/downstream"
)

// Entry is one tool in the aggregated catalog, mapping an exposed name (as seen
// by IDEs) back to the downstream server and original tool name (as seen by that
// downstream server).
type Entry struct {
	ExposedName  string
	ServerName   string
	OriginalName string
	// Tool is a copy of the downstream tool definition with Name rewritten to
	// ExposedName, ready to register directly on an aggregated mcp.Server.
	Tool *mcp.Tool
}

// Catalog is the merged, collision-free view of every downstream server's tools.
type Catalog struct {
	Entries []Entry
	// Warnings records any collisions that forced a tool to be namespaced
	// despite that server's namespace setting, for operators to review.
	Warnings []string

	byExposed map[string]Entry
}

// Lookup resolves an exposed tool name back to its downstream server/original name.
func (c *Catalog) Lookup(exposedName string) (Entry, bool) {
	e, ok := c.byExposed[exposedName]
	return e, ok
}

type rawTool struct {
	serverName string
	tool       *mcp.Tool
}

// Build lists tools from every downstream server known to dm and merges them
// into one Catalog. Tools are namespaced as `server__tool` per each server's
// `namespace` config (default true). Any tool whose candidate exposed name
// collides with another server's tool is force-namespaced regardless of that
// setting - collisions are logged as warnings, never silently dropped.
func Build(ctx context.Context, dm *downstream.Manager, servers []config.DownstreamServer) (*Catalog, error) {
	namespaceEnabled := make(map[string]bool, len(servers))
	for _, s := range servers {
		namespaceEnabled[s.Name] = s.NamespaceEnabled()
	}

	var raw []rawTool
	candidateCount := make(map[string]int)

	for _, serverName := range dm.ServerNames() {
		tools, err := dm.ListTools(ctx, serverName)
		if err != nil {
			return nil, fmt.Errorf("listing tools for %q: %w", serverName, err)
		}
		for _, tool := range tools {
			raw = append(raw, rawTool{serverName, tool})
			candidateCount[candidateName(serverName, tool.Name, namespaceEnabled[serverName])]++
		}
	}

	catalog := &Catalog{byExposed: make(map[string]Entry, len(raw))}

	for _, rt := range raw {
		exposed := candidateName(rt.serverName, rt.tool.Name, namespaceEnabled[rt.serverName])
		if candidateCount[exposed] > 1 {
			forced := namespacedName(rt.serverName, rt.tool.Name)
			catalog.Warnings = append(catalog.Warnings, fmt.Sprintf(
				"tool name %q collided across servers; %q force-namespaced to %q",
				exposed, rt.serverName+"/"+rt.tool.Name, forced))
			exposed = forced
		}

		toolCopy := *rt.tool
		toolCopy.Name = exposed

		entry := Entry{
			ExposedName:  exposed,
			ServerName:   rt.serverName,
			OriginalName: rt.tool.Name,
			Tool:         &toolCopy,
		}
		catalog.Entries = append(catalog.Entries, entry)
		catalog.byExposed[exposed] = entry
	}

	return catalog, nil
}
