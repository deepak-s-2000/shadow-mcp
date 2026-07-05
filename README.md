# shadow-mcp

An MCP gateway: point every IDE (VS Code, Cursor, Claude Code, ...) at one
shadow-mcp endpoint instead of configuring every real MCP server in each IDE
separately. shadow-mcp connects to your real downstream MCP servers once,
aggregates their tools into a single catalog, decides which tools each IDE
("profile") is allowed to see, and can run pre/post-hook scripts around every
tool call - reject a dangerous call before it happens, redact secrets from a
response, rewrite arguments, and so on. Its lazy-loading tool catalog can also
cut per-turn tool-schema tokens by **up to ~80%** on larger aggregated
catalogs - see [Lazy loading](#features) below.

## Features

- **Aggregation** — connect to any number of downstream MCP servers (`stdio`,
  `http`, or `sse` transport) and expose them all through one endpoint, tools
  namespaced as `server__tool` (with automatic collision handling).
- **Profiles** — each connecting client gets its own wildcard allow/deny tool
  filter, so a "personal" IDE and a "shared/CI" IDE can see a different slice
  of the same downstream servers from one config file.
- **Rules** — pre/post-hook scripts (JavaScript or Python) run around every
  tool call: reject a call outright, or mutate its arguments/result. Great for
  governance (block a destructive tool, require an argument shape) and data
  hygiene (redact secrets/PII from output before it reaches the LLM).
- **Lazy loading** — for profiles with a large tool catalog, register only the
  most-frequently-used tools directly and expose the rest through
  `list_all_tools` / `call_deferred_tool`, so the LLM doesn't pay full
  `tools/list` context cost up front. Usage ranking persists across restarts.

  This matters because full tool schemas are resent to the model on *every*
  turn of a conversation, not just once. For an aggregated catalog of, say,
  ~40 tools across a few downstream servers, registering everything directly
  can easily cost several thousand tokens of schema *per turn*. Capping the
  directly-registered set to a handful of tools (plus the two small
  `list_all_tools`/`call_deferred_tool` meta-tools) can cut that per-turn cost
  by roughly 80%+, and the savings compound across a long session - a
  deferred tool only costs its full schema once, when it's actually looked up
  or called, instead of on every single turn. The bigger and more rarely-used
  your tail of tools, the more this saves; a small catalog that already fits
  under the `limit` sees little benefit.
- **One shared daemon** — the actual downstream connections, catalog, and rule
  engine live in a single auto-started background daemon. Every IDE's stdio
  process (or an HTTP client) is a thin relay to it, so N IDEs share one set
  of connections instead of each spawning its own.
- **Live dashboard** *(experimental)* — `shadow-mcp ui` is a terminal
  dashboard showing live downstream health and recent calls, plus guided
  forms to add/edit/delete servers, profiles, and rules without hand-editing
  YAML. Still rough around the edges and not as battle-tested as the rest of
  the gateway - expect sharp corners.

## Install

### Option A: download a prebuilt binary (no Go, no clone required)

Every [tagged release](https://github.com/deepak-s-2000/shadow-mcp/releases/latest)
publishes binaries for every platform:

| OS | Arch | Asset |
|---|---|---|
| Linux | x86_64 | `shadow-mcp-linux-amd64` |
| Linux | arm64 | `shadow-mcp-linux-arm64` |
| macOS | Intel | `shadow-mcp-darwin-amd64` |
| macOS | Apple Silicon | `shadow-mcp-darwin-arm64` |
| Windows | x86_64 | `shadow-mcp-windows-amd64.exe` |

```sh
# macOS/Linux (swap in the asset name for your OS/arch from the table above):
curl -L -o shadow-mcp https://github.com/deepak-s-2000/shadow-mcp/releases/latest/download/shadow-mcp-darwin-arm64
chmod +x shadow-mcp
./shadow-mcp list-tools --profile vscode
```

On Windows, download `shadow-mcp-windows-amd64.exe` and run it directly from
PowerShell/cmd - no install step needed.

That's the whole setup: no Go toolchain, no cloning this repo. Running the
binary with no `--config` (as above) auto-creates an empty starter config the
first time, in a per-user data directory (see below), which you then edit to
add your own downstream servers/profiles/rules per `docs/CONFIG.md`. If you
want to run *this repo's* worked example (the filesystem server + rules
below) instead of starting from scratch, download
[`shadow-mcp.yaml`](shadow-mcp.yaml) and the [`rules/`](rules) directory
alongside the binary too.

### Option B: build from source

```sh
go build -o bin/shadow-mcp ./cmd/shadow-mcp
```

## Quick start

```sh
# See what a profile would be allowed to call, without any IDE:
./bin/shadow-mcp list-tools --config shadow-mcp.yaml --profile vscode

# Point an IDE's MCP config at:
#   command: bin/shadow-mcp
#   args: ["serve", "stdio", "--config", "shadow-mcp.yaml", "--profile", "vscode"]

# Or run it as a long-lived HTTP server (one process, every profile):
./bin/shadow-mcp serve http --config shadow-mcp.yaml
# -> http://127.0.0.1:8090/mcp/vscode

# Live dashboard + config editor (experimental - see Features below):
./bin/shadow-mcp ui --config shadow-mcp.yaml
# Status tab: live health + recent calls. Servers/Profiles/Rules tabs:
# ↑/↓ select, a add, e/enter edit, d delete - guided field-by-field forms,
# no YAML hand-editing required.
```

If you run any command without `--config` and there's no `shadow-mcp.yaml` in
the current directory either, shadow-mcp falls back to a per-user config
location (`%AppData%\shadow-mcp\shadow-mcp.yaml` on Windows,
`~/.config/shadow-mcp/shadow-mcp.yaml` on Linux, `~/Library/Application
Support/shadow-mcp/shadow-mcp.yaml` on macOS) - creating an empty starter file
there the first time, so a copy of just the binary is enough to get started.

## Adding shadow-mcp to your IDE

shadow-mcp itself just looks like a single, ordinary stdio MCP server to your
IDE. Here it is added to VS Code (`.vscode/mcp.json`), using the `vscode`
profile already defined in this repo's [`shadow-mcp.yaml`](shadow-mcp.yaml):

```json
{
  "servers": {
    "shadow-mcp": {
      "type": "stdio",
      "command": "/absolute/path/to/bin/shadow-mcp",
      "args": ["serve", "stdio", "--config", "/absolute/path/to/shadow-mcp.yaml", "--profile", "vscode"]
    }
  }
}
```

Other IDEs (Cursor, Claude Code, ...) take the same `command`/`args` shape in
their own MCP config file, just under a `mcpServers` key instead of `servers`.
Either way, `--profile` must match the name of a profile in your config's
`profiles:` list (see [`docs/CONFIG.md`](docs/CONFIG.md)) - use a distinct
profile per IDE if you want each to see a different slice of tools, or point
them all at the same one for a shared view. An HTTP-only client can instead
point at `http://127.0.0.1:<port>/mcp/<profile>` from `shadow-mcp serve http`.

## Worked example

This repo's own [`shadow-mcp.yaml`](shadow-mcp.yaml) is a runnable example: one
downstream server (the official filesystem MCP server, sandboxed to the
current directory) and two rules demonstrating both hook directions.

```yaml
downstream_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
    call_timeout: 60s

profiles:
  - name: vscode
    identify: { stdio_arg: vscode, http_path: /mcp/vscode }
    tools:
      allow: ["filesystem*"]
    lazy_load:
      enabled: true
      limit: 5

rules:
  - name: guard-write-path
    script: ./rules/guard_write_path.js
    language: js
    hooks: ["pre"]
    applies_to:
      tools: ["filesystem__write_file", "filesystem__edit_file", "filesystem__move_file", "filesystem__create_directory"]
    priority: 10

  - name: mask-file-secrets
    script: ./rules/mask_file_secrets.py
    language: python
    hooks: ["post"]
    applies_to:
      tools: ["filesystem__read_file", "filesystem__read_text_file"]
    priority: 10
```

- [`rules/guard_write_path.js`](rules/guard_write_path.js) is a **pre-hook**:
  it inspects the arguments of any write/edit/move/mkdir call and rejects it
  outright (before the downstream server ever sees it) unless the target path
  is under `rules/sandbox/`.
- [`rules/mask_file_secrets.py`](rules/mask_file_secrets.py) is a
  **post-hook**: it scans a file-read result for secret-looking tokens
  (`ghp_...`, `SECRET_...`, etc.) and redacts them before the result reaches
  the LLM.

Try it:

```sh
./bin/shadow-mcp list-tools --config shadow-mcp.yaml --profile vscode
```

Or drive it live with the [MCP Inspector](https://github.com/modelcontextprotocol/inspector):

```sh
npx @modelcontextprotocol/inspector ./bin/shadow-mcp serve stdio --config shadow-mcp.yaml --profile vscode
```

Then, from the Inspector:

- Call `filesystem__write_file` with a path outside `rules/sandbox/` → rejected
  by `guard-write-path`, the downstream server is never invoked.
- Call it again with a path under `rules/sandbox/` → succeeds.
- Write a file under `rules/sandbox/` containing something like
  `token=SECRET_abc123`, then read it back with `filesystem__read_text_file` →
  the secret comes back as `[REDACTED]`.

See `docs/CONFIG.md` for the full config schema (including the rule script
input/output contract) and `configs/example.yaml` for a second worked example
using two colliding demo servers and three profiles.

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
