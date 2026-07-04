# Config reference

See `configs/example.yaml` for a fully worked example. Config is YAML (JSON
also parses, since it's a YAML subset).

## `downstream_servers`

```yaml
downstream_servers:
  - name: github                 # unique; used in namespacing and rule applies_to.servers
    transport: stdio              # or: transport: http | sse
    command: npx                  # stdio only
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${GITHUB_TOKEN}"   # ${VAR} interpolates from shadow-mcp's own process env
    url: "https://mcp.example.com/mcp"  # http/sse only
    headers: { Authorization: "Bearer ${TOKEN}" }  # http/sse only
    connect_timeout: 30s
    call_timeout: 30s
    namespace: true                # default true; exposes tools as "github__toolname".
                                    # false tries bare names, but the aggregator always
                                    # force-namespaces on a real collision regardless.
```

`transport: http` speaks the newer "Streamable HTTP" protocol (single endpoint,
POST-based). `transport: sse` speaks the older HTTP+SSE protocol (`GET` on the
endpoint opens an event stream, which then hands back a separate POST
endpoint) - use this for servers whose URL is a dedicated `/sse` path and that
return "Method Not Allowed" on POST when tried as `transport: http`.

## `profiles`

A profile is a connecting client (typically one IDE). If no profiles are
configured, `serve stdio` / `serve http` expose everything, unfiltered.

```yaml
profiles:
  - name: vscode
    identify:
      stdio_arg: vscode           # `shadow-mcp serve stdio --profile vscode`
      http_path: /mcp/vscode      # optional; defaults to /mcp/<name> if unset
    tools:
      allow: ["github__*", "linear__list_issues"]  # wildcards via path/filepath.Match syntax
      deny: ["github__delete_repo"]                # deny always wins over an overlapping allow
    lazy_load:                     # optional; see below
      enabled: true
      limit: 5                      # optional, defaults to 5
```

An empty `allow` list permits nothing - list what you want to expose.

### `lazy_load` (optional, per profile)

When a profile allows many tools, registering all of them costs real context
in every `tools/list` response the LLM sees. `lazy_load` keeps only the
`limit` most useful ones directly registered, and gives the rest to the LLM
on demand through two synthetic meta-tools:

- **`list_all_tools`** - returns the profile's *entire* allowed tool set (name,
  description, input schema) as data, without paying the registration cost.
- **`call_deferred_tool(tool_name, arguments)`** - calls any tool from that
  list by name, going through the exact same pre/post rule pipeline as a
  directly-registered tool. It can only reach tools already inside the
  profile's allow/deny filter - lazy loading changes what's registered up
  front, never what's allowed.

"Most useful" means **most frequently called**, tracked per profile and
persisted to disk (`usage.json` next to the daemon's info file - see
`docs/ARCHITECTURE.md`) so the ranking survives a daemon restart. Every call -
whether direct or via `call_deferred_tool` - increments that tool's count.
Tools never called yet fall back to their catalog order, so a fresh profile
with no history behaves like plain first-N truncation. The ranking is applied
once per profile per server build (daemon start or `/reload`), not live
mid-session - a tool's visibility only changes on the next reload/restart, not
mid-conversation.

Off by default; existing profiles are unaffected unless you opt in.

## `rules`

Pre/post-hook scripts run around a tool call. Script paths are resolved
relative to the config file's own directory, not wherever the daemon happens
to be started from.

```yaml
rules:
  - name: mask-secrets
    script: ./rules/mask_secrets.py
    language: python              # or: language: js
    hooks: ["post"]                # "pre", "post", or both
    applies_to:
      tools: ["*"]                 # matched against the exposed tool name
      servers: []                  # if both tools and servers are set, a rule
                                    # applies only when BOTH match (AND)
    priority: 10                   # ascending; ties broken by declaration order; default 100
    timeout: 5s                    # default 5s
    on_error: reject               # "reject" (fail-closed, default) or "skip"
    env: []                        # python only: allowlist of env var names passed
                                    # to the subprocess (nothing is inherited by default)
```

### Rule script contract

Both JS and Python rules receive the same JSON shape and must return the same
JSON shape.

Input:

```json
{
  "hook": "pre",
  "rule_name": "mask-secrets",
  "client_profile": "vscode",
  "server_name": "github",
  "tool_name": "create_issue",
  "exposed_tool_name": "github__create_issue",
  "arguments": { "...": "..." },
  "result": null
}
```

(`result` is populated instead for `hook: "post"`.)

Output:

```json
{ "action": "continue", "arguments": { "...mutated...": "..." } }
{ "action": "reject", "reason": "explanation shown to the IDE as a tool error" }
```

For a post-hook, return `"result"` instead of `"arguments"` when mutating -
see `configs/rules/mask_secrets.py` for a worked example that edits
`result.content[].text` in place.

**JS rules** define a top-level `function onCall(input) { ... }` and run via
an embedded `goja` interpreter (no filesystem/network access by default).
**Python rules** read the input as JSON from stdin and must write the output
as JSON to stdout (stderr is logged, not parsed).

## `http`

```yaml
http:
  addr: "127.0.0.1:8090"   # optional; if unset, the daemon binds an
                            # OS-assigned loopback port and publishes it via
                            # its local info file (fine for `serve stdio` and
                            # `ui`, which discover it automatically - set this
                            # if you want a stable URL to paste into an IDE's
                            # remote-MCP config instead)
```
