package downstream

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shadow-code/shadow-mcp/internal/config"
)

// newEchoHTTPServer starts a real in-process streamable-HTTP MCP server with
// one "echo" tool, so tests exercise the real transport/session machinery
// (jsonrpc2 connection state, ErrConnectionClosed, ...) rather than a fake.
func newEchoHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "echo", Version: "0.1.0"}, nil)
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "echo", Description: "echoes back"},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
		})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

func newTestManager(t *testing.T, url string) *Manager {
	t.Helper()
	m, err := NewManager(context.Background(), []config.DownstreamServer{
		{Name: "echo", Transport: "http", URL: url},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func TestCallToolReconnectsAfterSessionDies(t *testing.T) {
	ts := newEchoHTTPServer(t)
	m := newTestManager(t, ts.URL)

	before, ok := m.Session("echo")
	if !ok {
		t.Fatalf("expected a connected session")
	}

	// Simulate the transport dying out from under the manager (an idle
	// remote closing the connection, or the host sleeping) by closing the
	// client's own session directly - a subsequent call on it must fail with
	// ErrConnectionClosed, the exact class CallTool is meant to recover from.
	before.Close()

	res, err := m.CallTool(context.Background(), "echo", "echo", struct{}{})
	if err != nil {
		t.Fatalf("CallTool after a dead session should transparently reconnect and succeed, got: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("expected content in result, got %+v", res)
	}

	after, ok := m.Session("echo")
	if !ok {
		t.Fatalf("expected a session to still be registered")
	}
	if after == before {
		t.Fatalf("expected the session to have been swapped for a new one")
	}
}

func TestPingDoesNotReconnect(t *testing.T) {
	ts := newEchoHTTPServer(t)
	m := newTestManager(t, ts.URL)

	before, _ := m.Session("echo")
	before.Close()

	if err := m.Ping(context.Background(), "echo"); err == nil {
		t.Fatalf("expected Ping to surface the dead session's error, not silently reconnect")
	}

	after, _ := m.Session("echo")
	if after != before {
		t.Fatalf("Ping must never reconnect on its own - that's the health loop's job")
	}
}

func TestReconnectIsNoOpForAlreadyReplacedSession(t *testing.T) {
	ts := newEchoHTTPServer(t)
	m := newTestManager(t, ts.URL)

	stale, _ := m.Session("echo")

	// A real reconnect already happened (e.g. triggered by another caller).
	if err := m.Reconnect(context.Background(), "echo", stale); err != nil {
		t.Fatalf("first reconnect: %v", err)
	}
	current, _ := m.Session("echo")
	if current == stale {
		t.Fatalf("expected the session to change after the first reconnect")
	}

	// Calling Reconnect again with the now-stale pointer must be a no-op.
	if err := m.Reconnect(context.Background(), "echo", stale); err != nil {
		t.Fatalf("second reconnect (stale pointer) should be a harmless no-op, got: %v", err)
	}
	stillCurrent, _ := m.Session("echo")
	if stillCurrent != current {
		t.Fatalf("reconnect with a stale pointer must not touch the current session")
	}
}

func TestReconnectConcurrentCallersDedupe(t *testing.T) {
	ts := newEchoHTTPServer(t)
	m := newTestManager(t, ts.URL)

	bad, _ := m.Session("echo")

	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = m.Reconnect(context.Background(), "echo", bad)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Reconnect[%d]: %v", i, err)
		}
	}

	res, err := m.CallTool(context.Background(), "echo", "echo", struct{}{})
	if err != nil {
		t.Fatalf("CallTool after concurrent reconnects: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("expected content, got %+v", res)
	}
}

func TestCallToolReconnectFailureReportsBothErrors(t *testing.T) {
	ts := newEchoHTTPServer(t)
	m := newTestManager(t, ts.URL)

	before, _ := m.Session("echo")
	before.Close()

	// Take the real server down so the reconnect attempt itself fails too.
	ts.Close()

	_, err := m.CallTool(context.Background(), "echo", "echo", struct{}{})
	if err == nil {
		t.Fatalf("expected an error when both the original call and the reconnect fail")
	}
	if !errors.Is(err, mcp.ErrConnectionClosed) {
		t.Fatalf("expected the original ErrConnectionClosed to still be in the chain, got: %v", err)
	}
}
