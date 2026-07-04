package gateway

import (
	"context"
	"encoding/json"
	"log"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shadow-code/shadow-mcp/internal/aggregator"
	"github.com/shadow-code/shadow-mcp/internal/downstream"
	"github.com/shadow-code/shadow-mcp/internal/rules"
)

const (
	listAllToolsName     = "list_all_tools"
	callDeferredToolName = "call_deferred_tool"
)

// buildLazyServer registers only the limit most-frequently-used entries
// (per usageCounts, keyed by exposed tool name, with unseen/tied entries
// falling back to their original catalog order via a stable sort) directly,
// plus two synthetic meta-tools giving access to the full filtered set:
// list_all_tools (returns the whole catalog as data) and call_deferred_tool
// (a second entry point into Dispatch, resolving the real entry at call
// time - subject to exactly the same allowlist, since it can only resolve
// names already present in entries).
func buildLazyServer(entries []aggregator.Entry, limit int, usageCounts map[string]int64, dm *downstream.Manager, engine *rules.Engine, clientProfile string, rec Recorder) *mcp.Server {
	sorted := sortByUsage(entries, usageCounts)

	visible := sorted
	if limit < len(visible) {
		visible = visible[:limit]
	}

	server := BuildServer(visible, dm, engine, clientProfile, rec)
	addMetaTools(server, sorted, dm, engine, clientProfile, rec)
	return server
}

// sortByUsage returns a copy of entries ordered by descending usage count.
// Entries with equal (including zero) counts keep their relative catalog
// order, so a profile with no call history yet behaves like plain
// first-N-in-catalog-order truncation.
func sortByUsage(entries []aggregator.Entry, counts map[string]int64) []aggregator.Entry {
	sorted := make([]aggregator.Entry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		return counts[sorted[i].ExposedName] > counts[sorted[j].ExposedName]
	})
	return sorted
}

// addMetaTools registers list_all_tools and call_deferred_tool on server,
// scoped to the full (already profile-filtered) entries list. If a real
// downstream tool happens to already use one of these names, that meta-tool
// is skipped (logged) rather than silently shadowing the real tool.
func addMetaTools(server *mcp.Server, entries []aggregator.Entry, dm *downstream.Manager, engine *rules.Engine, clientProfile string, rec Recorder) {
	byName := make(map[string]aggregator.Entry, len(entries))
	for _, e := range entries {
		byName[e.ExposedName] = e
	}

	if _, collide := byName[listAllToolsName]; collide {
		log.Printf("shadow-mcp: profile %q already has a tool named %q; skipping the list_all_tools meta-tool", clientProfile, listAllToolsName)
	} else {
		server.AddTool(&mcp.Tool{
			Name: listAllToolsName,
			Description: "List every tool available to this profile, including ones not directly " +
				"registered in tools/list, with their descriptions and input schemas. Use " +
				"call_deferred_tool to invoke any of them by name.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return listAllToolsResult(entries)
		})
	}

	if _, collide := byName[callDeferredToolName]; collide {
		log.Printf("shadow-mcp: profile %q already has a tool named %q; skipping the call_deferred_tool meta-tool", clientProfile, callDeferredToolName)
		return
	}
	server.AddTool(&mcp.Tool{
		Name: callDeferredToolName,
		Description: "Call any tool returned by list_all_tools, by its exact name, even if it " +
			"wasn't directly listed in tools/list.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool_name": map[string]any{
					"type":        "string",
					"description": "exact tool name, as returned by list_all_tools",
				},
				"arguments": map[string]any{
					"type":        "object",
					"description": "arguments for the tool, per its input_schema",
				},
			},
			"required": []string{"tool_name"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return callDeferredTool(ctx, req, byName, dm, engine, clientProfile, rec)
	})
}

type toolSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema,omitempty"`
}

func listAllToolsResult(entries []aggregator.Entry) (*mcp.CallToolResult, error) {
	summaries := make([]toolSummary, 0, len(entries))
	for _, e := range entries {
		summaries = append(summaries, toolSummary{
			Name:        e.ExposedName,
			Description: e.Tool.Description,
			InputSchema: e.Tool.InputSchema,
		})
	}

	b, err := json.Marshal(summaries)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil
}

func callDeferredTool(ctx context.Context, req *mcp.CallToolRequest, byName map[string]aggregator.Entry, dm *downstream.Manager, engine *rules.Engine, clientProfile string, rec Recorder) (*mcp.CallToolResult, error) {
	var in struct {
		ToolName  string          `json:"tool_name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &in); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "invalid call_deferred_tool arguments: " + err.Error()}},
		}, nil
	}

	entry, ok := byName[in.ToolName]
	if !ok {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "unknown tool: " + in.ToolName}},
		}, nil
	}

	args := in.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	return Dispatch(ctx, dm, engine, rec, clientProfile, entry.ServerName, entry.OriginalName, entry.ExposedName, args)
}
