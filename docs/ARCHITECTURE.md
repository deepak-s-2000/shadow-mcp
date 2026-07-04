# Architecture

shadow-mcp is an MCP gateway: IDEs point at one shadow-mcp endpoint (a stdio
command or an HTTP URL) instead of configuring every real MCP server
individually. shadow-mcp aggregates the real servers, filters what each IDE
sees, and runs pre/post hook scripts around every tool call.

## Components

- **`internal/config`** — loads and validates `shadow-mcp.yaml`. Rule script
  paths are resolved relative to the config file's own directory (not the
  process's working directory), since the daemon can be auto-started from
  anywhere. `starter.go` provides the empty starter config `cmd/shadow-mcp`
  writes out on first run when no config is found anywhere (see below).
- **`internal/downstream`** — owns connections to the real MCP servers.
  Connects to all of them **concurrently** (not one at a time — a single cold
  `npx`-spawned server can take several seconds, and that cost otherwise
  multiplies with every additional server). Also force-kills each stdio
  server's whole process tree on shutdown (`procgroup_windows.go` /
  `procgroup_unix.go`), since on Windows terminating a wrapper process (e.g.
  `npx`'s `cmd.exe`) does not terminate the `node` grandchild it spawns.
  `Manager.CallTool`/`Ping` detect a dead transport (`errors.Is(err,
  mcp.ErrConnectionClosed)` - an idle remote closing an SSE/HTTP connection, or
  the host sleeping and tearing down a stdio pipe) and `Manager.Reconnect`
  redials and swaps in a fresh session in place; a per-server mutex plus a
  "does the current session still match the one I saw fail" check keeps
  concurrent callers hitting the same dead session from redialing twice.
- **`internal/aggregator`** — merges every downstream server's tools into one
  catalog, namespacing exposed names as `server__tool` and force-namespacing
  on any real collision regardless of a server's own `namespace` setting.
- **`internal/profile`** — wildcard allow/deny filtering of the catalog per
  connecting client ("profile"), and resolving which profile a stdio launch
  belongs to.
- **`internal/rules`** — matches, orders (by `priority`), and chains
  pre/post-hook rules around a tool call. `internal/rules/jsrule` runs JS via
  an embedded `goja` interpreter (fresh runtime per call, hard timeout via
  `Interrupt`); `internal/rules/pyrule` runs Python as a real OS subprocess
  (stdin/stdout JSON, restricted env).
- **`internal/gateway`** — `Dispatch` is the single call path every tool
  handler goes through: pre-hooks → downstream call → post-hooks, with an
  optional `Recorder` callback for stats. `metatools.go` builds the optional
  lazy-loading path for a profile (`lazy_load` in config): only the
  most-frequently-used entries are registered directly, backed by
  `list_all_tools`/`call_deferred_tool` meta-tools that reach the rest through
  the same `Dispatch` path and the same allow/deny filter.
- **`internal/usage`** — persists per-profile tool call counts to
  `usage.json` (next to the daemon's info file), so lazy-loading profiles
  keep ranking tools by frequency across daemon restarts instead of
  resetting to catalog order every time.
- **`internal/daemon`** — the persistent process that actually owns the
  downstream connections, catalog, and rule engine. Exposes a loopback
  HTTP API: `/status`, `/calls/recent`, `/config`, `/reload` (admin), CRUD
  routes for servers/profiles/rules (`config_crud.go`, see below), and
  `/mcp/<profile>` (the actual MCP relay, one route per profile, built via
  `mcp.NewStreamableHTTPHandler`). All of it is protected by a random per-run
  bearer token published (with the bound port) in a local info file. Every
  dispatched call also feeds `internal/usage` for lazy-loading ranking.
  `health.go` runs a background loop (every 30s) that pings every downstream
  server and calls `Manager.Reconnect` on any that died, so a dropped
  connection self-heals without a manual `/reload` - `/status`'s own ping
  (used by the TUI) stays a plain read with no reconnect attempt, so a slow
  cold start on one server can't stall the whole status poll.
- **`internal/adminclient`** — talks to the daemon's admin API, auto-spawning
  a detached daemon process if none is reachable yet (guarded by a start-lock
  file so two simultaneous launches don't race into spawning two daemons).
- **`internal/transport`** — `RunStdio` is a *thin* adapter: it resolves the
  `--profile` flag to a profile name from the local config, ensures a daemon
  is running, connects to that daemon's `/mcp/<profile>` endpoint over HTTP,
  and relays every tool it finds there through its own stdio server. This is
  why N IDEs launching their own stdio subprocess still share one set of
  downstream connections - they're all thin clients of the same daemon.
- **`internal/tui`** — `shadow-mcp ui`, a bubbletea dashboard: a Status tab
  polling the admin API for live health/recent-calls, and Servers/Profiles/
  Rules tabs that both display and edit their entity (`a` add, `e`/`enter`
  edit, `d` delete, guided field-by-field forms - `field.go`/`form.go`/
  `form_convert.go`). Edits go through the daemon's CRUD admin routes, never
  the YAML file directly, so the daemon stays the single validator/writer.

## Config CRUD (`internal/daemon/config_crud.go`, `adminapi.go`)

The admin API exposes `GET/POST/PUT/DELETE /config/{servers,profiles,rules}
[/{name}]`, all going through one path, `Daemon.mutateRaw`: clone the current
config, apply the edit, `config.Validate` it, `config.Save` it to disk, then
run the same `reload()` a config-file hand-edit + `POST /reload` would. Two
correctness details worth knowing if you touch this code:

- **Never round-trip through the *interpolated* config.** `Load()` resolves
  `${VAR}` references into real values before parsing; if CRUD read/wrote
  through that, an untouched `env: { TOKEN: "${MY_TOKEN}" }` would get
  silently rewritten to the literal resolved secret the next time any edit
  saved the file. All CRUD reads/writes instead go through `LoadRaw`/`Save`,
  which never interpolates - `GetServer`/`GetProfile`/`GetRule` return exactly
  what's on disk (placeholder syntax included), which is also what the TUI's
  edit form pre-fills from. The existing `GET /config` (used for read-only
  display) is unaffected - it still reads the interpolated config and
  redacts secret-bearing values with `***`, which is fine for display but
  would corrupt an edit if you ever plumbed it into the write path instead.
- **A structurally-valid edit can still fail to *reload*** (e.g. a new
  downstream server's command/URL doesn't actually speak MCP). `mutateRaw`
  snapshots the file's bytes before writing, and restores them if `reload()`
  fails after `Save()` succeeded - otherwise the broken entry would persist
  despite the API reporting an error, and since every future edit also ends
  with a full reload, it would keep failing every *subsequent* edit too, not
  just ones touching the broken entry.

Every config type's struct tags carry **both** `yaml` and `json` (added
alongside this feature) - encoding/json's default case-insensitive field
matching does not bridge `snake_case` JSON keys to `CamelCase` Go field names
(e.g. `"stdio_arg"` doesn't match `StdioArg`), so without explicit `json` tags
most fields silently decoded as zero values through the CRUD endpoints.

## Why a daemon at all

A stdio-launched subprocess is short-lived and per-IDE - there's nothing
persistent to show "live status" about, and each one reconnecting to every
downstream server independently wastes connections and defeats any shared
state. So the actual work (downstream connections, catalog, rule engine)
lives in one persistent daemon; both `serve stdio` and `serve http` are just
different ways of reaching it - the stdio path proxies to it over HTTP, and
`serve http` (and the internal `daemon` command it's an alias for) *is* it.

## Default config location

Every command resolves `--config` in this order (`cmd/shadow-mcp/main.go`,
`resolveConfigPath`): an explicit `--config` value; else `./shadow-mcp.yaml`
in the current directory, if present (the original, still-supported
convention); else `<DataDir>/shadow-mcp.yaml` (the same per-user directory
`daemon.json` and `usage.json` already live in), writing an empty starter
config there first if nothing exists at all. `config.WriteStarter` uses
`O_CREATE|O_EXCL` so it can never clobber a config a user has since edited -
worst case on a race it fails with an already-exists error, which the
resolver treats as "someone already created it, use it."

## Known gaps / deliberate scope cuts

- **HTTP profile routing is path-based only.** Each profile is reachable at
  `/mcp/<name>` (or a configured `identify.http_path`). Header/token-based
  routing to a single shared `/mcp` path (an alternative sketched in early
  design discussion) isn't implemented.
- **goja's JS sandbox has no memory cap.** It has zero ambient
  filesystem/network access by default, but a script that allocates
  unboundedly isn't stopped short of the process's own memory limits. This is
  a "trusted operator writes their own rules" model, not a sandbox for
  arbitrary third-party scripts.
