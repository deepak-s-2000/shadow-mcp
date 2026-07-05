// Package profile resolves which connecting client a request belongs to and
// filters the aggregated tool catalog down to what that client is allowed to see.
package profile

import (
	"path/filepath"

	"github.com/shadow-code/shadow-mcp/internal/aggregator"
	"github.com/shadow-code/shadow-mcp/internal/config"
)

// Allowed reports whether an exposed tool name is permitted under filter.
// Deny always wins over an overlapping allow pattern; an empty Allow list
// permits nothing (operators must opt in explicitly).
func Allowed(filter config.ToolFilter, exposedName string) bool {
	for _, pattern := range filter.Deny {
		if match(pattern, exposedName) {
			return false
		}
	}
	for _, pattern := range filter.Allow {
		if match(pattern, exposedName) {
			return true
		}
	}
	return false
}

func match(pattern, name string) bool {
	ok, _ := filepath.Match(pattern, name)
	return ok
}

// FilterEntries returns the subset of catalog entries permitted under filter.
func FilterEntries(catalog *aggregator.Catalog, filter config.ToolFilter) []aggregator.Entry {
	var out []aggregator.Entry
	for _, e := range catalog.Entries {
		if Allowed(filter, e.ExposedName) {
			out = append(out, e)
		}
	}
	return out
}
