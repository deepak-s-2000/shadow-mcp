package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shadow-code/shadow-mcp/internal/aggregator"
)

func fakeEntries(names ...string) []aggregator.Entry {
	entries := make([]aggregator.Entry, len(names))
	for i, n := range names {
		entries[i] = aggregator.Entry{
			ExposedName:  n,
			ServerName:   "fake",
			OriginalName: n,
			Tool: &mcp.Tool{
				Name:        n,
				Description: "desc-" + n,
				InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}
	}
	return entries
}

func TestSortByUsagePrefersHigherCounts(t *testing.T) {
	entries := fakeEntries("a", "b", "c", "d")
	counts := map[string]int64{"c": 10, "a": 5}

	sorted := sortByUsage(entries, counts)

	got := []string{sorted[0].ExposedName, sorted[1].ExposedName, sorted[2].ExposedName, sorted[3].ExposedName}
	want := []string{"c", "a", "b", "d"} // b, d tied at 0 - keep original relative order
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortByUsage order = %v, want %v", got, want)
		}
	}
}

func TestSortByUsageStableWithNoHistory(t *testing.T) {
	entries := fakeEntries("a", "b", "c")
	sorted := sortByUsage(entries, nil)
	for i, e := range entries {
		if sorted[i].ExposedName != e.ExposedName {
			t.Fatalf("with no usage history, order should be unchanged: got %v", sorted)
		}
	}
}

// connectLazyServer builds a lazy server over entries and connects a client
// to it via an in-memory transport pair, for testing list_all_tools /
// call_deferred_tool without a real downstream connection.
func connectLazyServer(t *testing.T, entries []aggregator.Entry, limit int, counts map[string]int64) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server := buildLazyServer(entries, limit, counts, nil, nil, "vscode", nil)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server.Connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestBuildLazyServerRegistersLimitPlusMetaTools(t *testing.T) {
	entries := fakeEntries("alpha", "beta", "gamma", "delta", "epsilon")
	session := connectLazyServer(t, entries, 2, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("Tools: %v", err)
		}
		names = append(names, tool.Name)
	}

	// alpha, beta (the top-2 by catalog order, no usage history) + the 2 meta-tools.
	want := map[string]bool{"alpha": true, "beta": true, listAllToolsName: true, callDeferredToolName: true}
	if len(names) != len(want) {
		t.Fatalf("registered tools = %v, want exactly %v", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected registered tool %q", n)
		}
	}
}

func TestListAllToolsReturnsFullFilteredSet(t *testing.T) {
	entries := fakeEntries("alpha", "beta", "gamma")
	session := connectLazyServer(t, entries, 1, map[string]int64{"gamma": 100})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: listAllToolsName})
	if err != nil {
		t.Fatalf("CallTool(list_all_tools): %v", err)
	}
	if res.IsError {
		t.Fatalf("list_all_tools returned an error result: %+v", res.Content)
	}

	text := res.Content[0].(*mcp.TextContent).Text
	var summaries []toolSummary
	if err := json.Unmarshal([]byte(text), &summaries); err != nil {
		t.Fatalf("unmarshal list_all_tools output: %v", err)
	}
	if len(summaries) != 3 {
		t.Fatalf("list_all_tools returned %d tools, want 3 (the full filtered set, not just the registered 1)", len(summaries))
	}
	if summaries[0].Name != "gamma" {
		t.Errorf("list_all_tools[0] = %q, want %q (highest usage count first)", summaries[0].Name, "gamma")
	}
}

func TestCallDeferredToolUnknownName(t *testing.T) {
	entries := fakeEntries("alpha", "beta")
	session := connectLazyServer(t, entries, 1, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      callDeferredToolName,
		Arguments: map[string]any{"tool_name": "does-not-exist"},
	})
	if err != nil {
		t.Fatalf("CallTool(call_deferred_tool): %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected an error result for an unknown deferred tool name, got %+v", res.Content)
	}
}

func TestAddMetaToolsSkipsOnNameCollision(t *testing.T) {
	// A real downstream tool happens to be named "list_all_tools" - the
	// meta-tool must not be registered on top of it.
	entries := fakeEntries("list_all_tools", "beta")
	session := connectLazyServer(t, entries, 1, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count := 0
	for range session.Tools(ctx, nil) {
		count++
	}
	// "list_all_tools" (real, registered since limit>=1) + call_deferred_tool.
	// The synthetic list_all_tools meta-tool must have been skipped, not
	// double-registered (which would otherwise panic/error on the server).
	if count != 2 {
		t.Fatalf("got %d tools, want 2 (real list_all_tools + call_deferred_tool only)", count)
	}
}
