package transport

import (
	"context"
	"fmt"
	"time"

	"github.com/shadow-code/shadow-mcp/internal/aggregator"
	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/downstream"
	"github.com/shadow-code/shadow-mcp/internal/profile"
)

// ListTools connects to every downstream server in cfg and prints the merged,
// collision-resolved tool catalog to stdout - a debug aid for inspecting what
// an IDE would see without needing a real IDE or MCP Inspector. If profileArg
// is non-empty, only the tools that profile's stdio_arg-matched profile allows
// are printed, so operators can verify allowlists without connecting a real IDE.
// timeoutSecs specifies the deadline for connecting to all downstream servers.
func ListTools(ctx context.Context, cfg *config.Config, profileArg string, timeoutSecs int) error {
	if timeoutSecs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
		defer cancel()
	}
	dm, err := downstream.NewManager(ctx, cfg.DownstreamServers)
	if err != nil {
		return err
	}
	defer dm.Close()

	catalog, err := aggregator.Build(ctx, dm, cfg.DownstreamServers)
	if err != nil {
		return err
	}

	for _, w := range catalog.Warnings {
		fmt.Println("warning:", w)
	}

	entries := catalog.Entries
	if profileArg != "" {
		p, err := profile.ResolveStdio(cfg.Profiles, profileArg)
		if err != nil {
			return err
		}
		entries = profile.FilterEntries(catalog, p.Tools)
	}

	fmt.Printf("\n%-40s %-15s %s\n", "EXPOSED NAME", "SERVER", "ORIGINAL NAME")
	for _, e := range entries {
		fmt.Printf("%-40s %-15s %s\n", e.ExposedName, e.ServerName, e.OriginalName)
	}
	fmt.Printf("\n%d tools from %d server(s)\n", len(entries), len(dm.ServerNames()))

	return nil
}
