package config

import "os"

// StarterYAML is written to the default config path (see cmd/shadow-mcp's
// resolveConfigPath) the first time shadow-mcp runs and finds no config
// anywhere - so a user who only has the binary gets something that loads
// successfully instead of an error, and a file they can then edit.
const StarterYAML = `# shadow-mcp starter config - edit this file to add your MCP servers.
# Full schema: https://github.com/shadow-code/shadow-mcp/blob/main/docs/CONFIG.md
#
# Example:
#
# downstream_servers:
#   - name: github
#     transport: stdio
#     command: npx
#     args: ["-y", "@modelcontextprotocol/server-github"]
#     env:
#       GITHUB_TOKEN: "${GITHUB_TOKEN}"
#
# profiles:
#   - name: vscode
#     identify: { stdio_arg: vscode }
#     tools:
#       allow: ["*"]

downstream_servers: []
profiles: []
rules: []
`

// WriteStarter writes StarterYAML to path, creating the file only if it
// doesn't already exist (so this is safe to call speculatively without
// clobbering a config the user has since edited).
func WriteStarter(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(StarterYAML)
	return err
}
