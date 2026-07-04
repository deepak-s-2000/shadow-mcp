// Package gateway holds the shared call-dispatch path between IDEs and downstream MCP servers.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shadow-code/shadow-mcp/internal/downstream"
	"github.com/shadow-code/shadow-mcp/internal/rules"
)

// Recorder observes the outcome of a dispatched call, for status/debugging
// display (e.g. the daemon's recent-calls ring buffer). May be nil.
type Recorder func(clientProfile, exposedName string, rulesFired []string, isError bool, err error)

// Dispatch is the single call path every registered tool handler goes
// through: run applicable pre-hook rules, forward to the downstream server,
// then run applicable post-hook rules. engine may be nil, meaning no rules are
// configured - Dispatch then behaves as a pure passthrough.
func Dispatch(ctx context.Context, dm *downstream.Manager, engine *rules.Engine, rec Recorder, clientProfile, serverName, toolName, exposedName string, rawArgs json.RawMessage) (*mcp.CallToolResult, error) {
	var args any
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return nil, err
		}
	}

	var fired []string

	if engine != nil {
		finalArgs, rejected, reason, preFired, err := engine.RunHook(ctx, rules.HookPre, clientProfile, serverName, toolName, exposedName, args)
		fired = append(fired, preFired...)
		if err != nil {
			record(rec, clientProfile, exposedName, fired, true, err)
			return nil, err
		}
		if rejected {
			record(rec, clientProfile, exposedName, fired, true, nil)
			return rejectedResult(reason), nil
		}
		args = finalArgs
	}

	result, err := dm.CallTool(ctx, serverName, toolName, args)
	if err != nil {
		record(rec, clientProfile, exposedName, fired, true, err)
		return nil, err
	}

	if engine != nil {
		finalResult, rejected, reason, postFired, err := engine.RunHook(ctx, rules.HookPost, clientProfile, serverName, toolName, exposedName, result)
		fired = append(fired, postFired...)
		if err != nil {
			record(rec, clientProfile, exposedName, fired, true, err)
			return nil, err
		}
		if rejected {
			record(rec, clientProfile, exposedName, fired, true, nil)
			return rejectedResult(reason), nil
		}
		result, err = toCallToolResult(finalResult)
		if err != nil {
			record(rec, clientProfile, exposedName, fired, true, err)
			return nil, err
		}
	}

	record(rec, clientProfile, exposedName, fired, result.IsError, nil)
	return result, nil
}

func record(rec Recorder, clientProfile, exposedName string, fired []string, isError bool, err error) {
	if rec != nil {
		rec(clientProfile, exposedName, fired, isError, err)
	}
}

func rejectedResult(reason string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: "rejected by rule: " + reason}},
	}
}

// toCallToolResult converts a rule's JSON-shaped post-hook result value (a
// map[string]any produced by round-tripping through the JS/Python contract)
// back into the SDK's typed CallToolResult.
func toCallToolResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshaling post-hook result: %w", err)
	}
	var out mcp.CallToolResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("rule post-hook returned a result that doesn't match the CallToolResult shape: %w", err)
	}
	return &out, nil
}
