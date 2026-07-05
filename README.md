# shadow-mcp

An MCP gateway: point every IDE (VS Code, Cursor, Claude Code, ...) at one
shadow-mcp endpoint instead of configuring every real MCP server in each IDE
separately. shadow-mcp aggregates the real downstream servers, decides which
tools each IDE ("profile") is allowed to see, and can run pre/post-hook
scripts around every tool call (mask secrets, trim output, reject calls,
etc).

## Quick start

```sh
go build -o bin/shadow-mcp ./cmd/shadow-mcp

# See what a profile would be allowed to call, without any IDE:
./bin/shadow-mcp list-tools --config configs/example.yaml --profile vscode

# Point an IDE's MCP config at:
#   command: bin/shadow-mcp
#   args: ["serve", "stdio", "--config", "configs/example.yaml", "--profile", "vscode"]

# Or run it as a long-lived HTTP server (one process, every profile):
./bin/shadow-mcp serve http --config configs/example.yaml
# -> http://127.0.0.1:8090/mcp/vscode, /mcp/cursor, /mcp/claude-code

# Live dashboard + config editor (downstream health, recent calls,
# add/edit/delete servers/profiles/rules):
./bin/shadow-mcp ui --config configs/example.yaml
# Status tab: live health + recent calls. Servers/Profiles/Rules tabs:
# ↑/↓ select, a add, e/enter edit, d delete - guided field-by-field forms,
# no YAML hand-editing required.
```

See `configs/example.yaml` for a fully worked config (two downstream servers
with a deliberate tool-name collision, three profiles, two rules), and
`docs/CONFIG.md` for the full schema.

If you run any command without `--config` and there's no `shadow-mcp.yaml` in
the current directory either, shadow-mcp falls back to a per-user config
location (`%AppData%\shadow-mcp\shadow-mcp.yaml` on Windows,
`~/.config/shadow-mcp/shadow-mcp.yaml` on Linux, `~/Library/Application
Support/shadow-mcp/shadow-mcp.yaml` on macOS) - creating an empty starter file
there the first time, so a copy of just the binary is enough to get started.

## Docs

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — how the pieces fit
  together, and current known gaps
- [`docs/CONFIG.md`](docs/CONFIG.md) — full config schema and the rule script
  contract
- [`docs/VERIFICATION.md`](docs/VERIFICATION.md) — how to manually verify a
  setup end-to-end

## Testing

```sh
go test ./...
```
