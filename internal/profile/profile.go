package profile

import (
	"fmt"

	"github.com/shadow-code/shadow-mcp/internal/config"
)

// ResolveStdio finds the profile whose identify.stdio_arg matches the value
// passed to `shadow-mcp serve stdio --profile <value>`.
func ResolveStdio(profiles []config.Profile, stdioArg string) (*config.Profile, error) {
	for i := range profiles {
		if profiles[i].Identify.StdioArg == stdioArg {
			return &profiles[i], nil
		}
	}
	return nil, fmt.Errorf("no profile configured with stdio_arg %q", stdioArg)
}
