# Manual verification

## Quick smoke test

```sh
go build -o bin/shadow-mcp ./cmd/shadow-mcp
./bin/shadow-mcp list-tools --config configs/example.yaml --profile vscode
```

This connects to both example downstream servers, prints the merged and
collision-namespaced tool catalog filtered to the `vscode` profile's
allowlist, and doesn't require a daemon or any IDE.

## Using the MCP Inspector

The official [MCP Inspector](https://github.com/modelcontextprotocol/inspector)
(`npx @modelcontextprotocol/inspector`) is the fastest way to drive shadow-mcp
manually without a real IDE, for both transports:

- **stdio**: point Inspector's command at
  `./bin/shadow-mcp serve stdio --config configs/example.yaml --profile vscode`
- **HTTP**: run `./bin/shadow-mcp serve http --config configs/example.yaml`,
  then point Inspector at `http://127.0.0.1:8090/mcp/vscode` with header
  `Authorization: Bearer <token>` (the token is in the daemon's info file -
  see below).

## Verifying against a real IDE

Point the IDE's MCP server config at the built binary:

```json
{
  "mcpServers": {
    "shadow-mcp": {
      "command": "/path/to/bin/shadow-mcp",
      "args": ["serve", "stdio", "--config", "/path/to/configs/example.yaml", "--profile", "vscode"]
    }
  }
}
```

Use a different `--profile` value per IDE from the *same* shared config file,
and confirm each IDE's tool list only shows what that profile's `tools.allow`
permits, with the expected `server__tool` namespacing.

For HTTP-capable clients (a "remote MCP server" URL field), point them at
`http://127.0.0.1:<port>/mcp/<profile>` instead - no local process needed once
the daemon (`shadow-mcp serve http`) is running.

## Where the daemon's connection info lives

`shadow-mcp serve stdio` and `shadow-mcp ui` both auto-start a daemon if none
is reachable. Its port and admin token are published to a local info file
(`%AppData%\shadow-mcp\daemon.json` on Windows, `~/.config/shadow-mcp/daemon.json`
elsewhere) - useful for manually curling the admin API:

```sh
curl -H "Authorization: Bearer <token>" http://127.0.0.1:<port>/status
curl -H "Authorization: Bearer <token>" "http://127.0.0.1:<port>/calls/recent?limit=20"
```

## Verifying rules

`configs/example.yaml` ships two rules:

- `validate-echo-args` (JS pre-hook): rejects an `echo` call whose `message`
  contains the word "forbidden".
- `mask-secrets` (Python post-hook): redacts `SECRET...`/`sk-...`/etc.-shaped
  tokens in tool output text.

Call `everything-a__echo` with a message containing a fake secret and confirm
the result is redacted; call it with the word "forbidden" and confirm the
call is rejected with that reason surfaced as a tool error, without the
downstream server ever being invoked (check `/calls/recent` - a rejected
pre-hook call still records `rules_fired` but the tool's own execution never
happens).

## Automated tests

```sh
go test ./...
```

Covers: config parsing/validation, aggregator collision/namespacing, profile
allowlist wildcard matching, rule engine ordering/chaining, the goja JS runner
(mutation, rejection, timeout via an infinite-loop fixture, malformed output),
the Python subprocess runner (mutation, rejection, timeout, env isolation -
skips gracefully if no Python interpreter is on `PATH`), and the TUI model's
tab switching/rendering. There is currently no automated integration test that
spawns real `npx`-based downstream servers end-to-end (all of that has been
verified manually per above) - a good next addition would be small fixture MCP
servers built from the SDK directly, run as real stdio subprocesses in a Go
test, avoiding the `npx` cold-start variance seen during manual testing.
