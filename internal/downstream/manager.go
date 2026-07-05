// Package downstream owns connections to the real MCP servers that shadow-mcp aggregates.
package downstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shadow-code/shadow-mcp/internal/config"
)

// Manager owns one connected ClientSession per configured downstream server.
type Manager struct {
	mu          sync.RWMutex
	sessions    map[string]*mcp.ClientSession
	servers     map[string]config.DownstreamServer
	cmds        map[string]*exec.Cmd   // stdio servers only, for tree cleanup on Close
	reconnectMu map[string]*sync.Mutex // serializes concurrent Reconnect calls per server
	callTimeout map[string]time.Duration // configured timeout per server
}

// NewManager connects to every server in servers concurrently (spawning N
// stdio subprocesses or dialing N HTTP servers in parallel rather than one at
// a time - a cold `npx`-spawned server alone can take several seconds, and
// that cost otherwise multiplies with every additional downstream server),
// returning an error if any connection fails. Callers should Close the
// Manager when done to terminate downstream processes/connections.
func NewManager(ctx context.Context, servers []config.DownstreamServer) (*Manager, error) {
	m := &Manager{
		sessions:    make(map[string]*mcp.ClientSession, len(servers)),
		servers:     make(map[string]config.DownstreamServer, len(servers)),
		cmds:        make(map[string]*exec.Cmd, len(servers)),
		reconnectMu: make(map[string]*sync.Mutex, len(servers)),
		callTimeout: make(map[string]time.Duration, len(servers)),
	}
	for _, s := range servers {
		m.reconnectMu[s.Name] = &sync.Mutex{}
		m.callTimeout[s.Name] = s.CallTimeout
	}

	type result struct {
		name    string
		session *mcp.ClientSession
		cmd     *exec.Cmd
		err     error
	}

	results := make(chan result, len(servers))
	for _, s := range servers {
		s := s
		go func() {
			session, cmd, err := connect(ctx, s)
			results <- result{name: s.Name, session: session, cmd: cmd, err: err}
		}()
	}

	var firstErr error
	for range servers {
		r := <-results
		if r.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("connecting to downstream server %q: %w", r.name, r.err)
			}
			continue
		}
		m.sessions[r.name] = r.session
		if r.cmd != nil {
			m.cmds[r.name] = r.cmd
		}
	}
	for _, s := range servers {
		m.servers[s.Name] = s
	}

	if firstErr != nil {
		m.Close()
		return nil, firstErr
	}
	return m, nil
}

// connect returns the connected session and, for stdio servers, the spawned
// *exec.Cmd (nil for http servers) so Close can force-kill its process tree.
func connect(ctx context.Context, s config.DownstreamServer) (*mcp.ClientSession, *exec.Cmd, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "shadow-mcp", Version: "0.1.0"}, nil)

	var transport mcp.Transport
	var cmd *exec.Cmd
	switch s.Transport {
	case "stdio":
		cmd = exec.Command(s.Command, s.Args...)
		if len(s.Env) > 0 {
			// Overlay configured vars onto the inherited environment rather
			// than replacing it outright - exec.Cmd treats a non-nil Env as
			// the complete environment, so without this, configuring even a
			// single env var would silently drop everything the child
			// actually needs to run (PATH, SystemRoot, TEMP, etc.).
			cmd.Env = os.Environ()
			for k, v := range s.Env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
		}
		configureChildProcess(cmd)
		transport = &mcp.CommandTransport{Command: cmd}
	case "http":
		transport = &mcp.StreamableClientTransport{Endpoint: s.URL, HTTPClient: headerClient(s.Headers)}
	case "sse":
		transport = &mcp.SSEClientTransport{Endpoint: s.URL, HTTPClient: headerClient(s.Headers)}
	default:
		return nil, nil, fmt.Errorf("unknown transport %q", s.Transport)
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, nil, err
	}
	return session, cmd, nil
}

// headerClient returns an *http.Client that injects the given headers on
// every request, or nil (letting the transport fall back to its own default
// client) if none are configured.
func headerClient(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return nil
	}
	return &http.Client{Transport: &headerRoundTripper{headers: headers, base: http.DefaultTransport}}
}

type headerRoundTripper struct {
	headers map[string]string
	base    http.RoundTripper
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range rt.headers {
		req.Header.Set(k, v)
	}
	return rt.base.RoundTrip(req)
}

// Session returns the connected session for a downstream server by name.
func (m *Manager) Session(name string) (*mcp.ClientSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[name]
	return s, ok
}

// ServerNames returns the configured downstream server names.
func (m *Manager) ServerNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.sessions))
	for name := range m.sessions {
		names = append(names, name)
	}
	return names
}

// CallTool forwards a tool call to the named downstream server. If the
// session's transport has died (e.g. an idle remote closed the connection, or
// the host slept and tore down a stdio pipe), it transparently reconnects and
// retries once instead of leaving the server permanently "down" until a
// manual reload. If a call_timeout is configured for this server, it is
// applied to the context (unless the context already has a shorter deadline).
func (m *Manager) CallTool(ctx context.Context, server, tool string, args any) (*mcp.CallToolResult, error) {
	session, ok := m.Session(server)
	if !ok {
		return nil, fmt.Errorf("unknown downstream server %q", server)
	}

	callCtx := ctx
	if timeout, ok := m.callTimeout[server]; ok && timeout > 0 {
		if deadline, ok := ctx.Deadline(); !ok || time.Now().Add(timeout).Before(deadline) {
			var cancel context.CancelFunc
			callCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
	}

	res, err := session.CallTool(callCtx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err == nil || !errors.Is(err, mcp.ErrConnectionClosed) {
		return res, err
	}
	if rErr := m.Reconnect(ctx, server, session); rErr != nil {
		return nil, fmt.Errorf("%w (reconnect failed: %v)", err, rErr)
	}
	session, ok = m.Session(server)
	if !ok {
		return nil, fmt.Errorf("unknown downstream server %q", server)
	}
	return session.CallTool(callCtx, &mcp.CallToolParams{Name: tool, Arguments: args})
}

// Ping checks that the named server's session is alive. It does not
// reconnect on failure - callers that want self-healing (the daemon's
// background health loop) should call Reconnect themselves so a slow cold
// start doesn't block a caller that just wanted a quick health read.
func (m *Manager) Ping(ctx context.Context, name string) error {
	session, ok := m.Session(name)
	if !ok {
		return fmt.Errorf("unknown downstream server %q", name)
	}
	return session.Ping(ctx, nil)
}

// Reconnect redials the named downstream server and swaps in the new session,
// closing the old one (and force-killing its process tree, for stdio
// servers) afterward. bad should be the session the caller observed failing;
// if another caller has already reconnected this server in the meantime (the
// current session no longer matches bad), Reconnect is a no-op - this keeps
// concurrent callers hitting the same dead session from redialing twice.
func (m *Manager) Reconnect(ctx context.Context, name string, bad *mcp.ClientSession) error {
	m.mu.RLock()
	mu := m.reconnectMu[name]
	server, ok := m.servers[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown downstream server %q", name)
	}

	mu.Lock()
	defer mu.Unlock()

	m.mu.RLock()
	current := m.sessions[name]
	m.mu.RUnlock()
	if current != bad {
		return nil
	}

	session, cmd, err := connect(ctx, server)
	if err != nil {
		return err
	}

	m.mu.Lock()
	oldCmd := m.cmds[name]
	m.sessions[name] = session
	if cmd != nil {
		m.cmds[name] = cmd
	} else {
		delete(m.cmds, name)
	}
	m.mu.Unlock()

	current.Close()
	if oldCmd != nil && oldCmd.Process != nil {
		killTree(oldCmd.Process.Pid)
	}
	return nil
}

// ListTools returns the full tool list for a single downstream server.
func (m *Manager) ListTools(ctx context.Context, server string) ([]*mcp.Tool, error) {
	session, ok := m.Session(server)
	if !ok {
		return nil, fmt.Errorf("unknown downstream server %q", server)
	}

	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// Close closes every connected downstream session (which attempts its own
// graceful-then-terminate shutdown) and then force-kills the process tree of
// every stdio-spawned server. The second step matters because closing the
// session alone does not reliably terminate grandchildren a wrapper command
// spawns (notably `npx` spawning a `node` child on Windows, where terminating
// the immediate child does not propagate to it the way it does on Unix); by
// the time we reach it, the graceful attempt has already had its chance, so
// this runs synchronously and any already-exited process is killed harmlessly.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for _, s := range m.sessions {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	for _, cmd := range m.cmds {
		if cmd.Process != nil {
			killTree(cmd.Process.Pid)
		}
	}

	return firstErr
}
