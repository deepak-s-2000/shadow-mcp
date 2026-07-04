package config

import "time"

// Config is the root shadow-mcp configuration.
type Config struct {
	DownstreamServers []DownstreamServer `yaml:"downstream_servers" json:"downstream_servers"`
	Profiles          []Profile          `yaml:"profiles" json:"profiles"`
	Rules             []Rule             `yaml:"rules" json:"rules"`
	HTTP              HTTP               `yaml:"http" json:"http"`
}

// HTTP configures the daemon's loopback admin+MCP listener.
type HTTP struct {
	// Addr is the address to bind, e.g. "127.0.0.1:8090" or ":8090". If
	// empty, the daemon binds an OS-assigned port on 127.0.0.1 and publishes
	// it via the daemon info file - fine for the stdio adapter and `ui`,
	// which discover it automatically, but a fixed Addr gives a stable URL
	// to paste into an IDE's remote-MCP config directly.
	Addr string `yaml:"addr,omitempty" json:"addr,omitempty"`
}

// Rule describes a pre- or post-tool-call hook script.
//
// json tags mirror the yaml tags exactly (rather than relying on
// encoding/json's default case-insensitive field-name matching) because that
// default matching does NOT bridge "snake_case" JSON keys to "CamelCase" Go
// field names (e.g. a JSON key "on_error" does not match field OnError) - the
// admin API's CRUD endpoints round-trip these types through encoding/json, so
// without explicit tags most fields would silently decode as zero values.
type Rule struct {
	Name      string        `yaml:"name" json:"name"`
	Script    string        `yaml:"script" json:"script"`
	Language  string        `yaml:"language" json:"language"` // "js" or "python"
	Hooks     []string      `yaml:"hooks" json:"hooks"`       // "pre", "post"
	AppliesTo RuleAppliesTo `yaml:"applies_to" json:"applies_to"`
	Priority  int           `yaml:"priority,omitempty" json:"priority,omitempty"` // ascending; 0 means default (100)
	Timeout   time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	OnError   string        `yaml:"on_error,omitempty" json:"on_error,omitempty"` // "reject" (default, fail-closed) | "skip"
	Env       []string      `yaml:"env,omitempty" json:"env,omitempty"`           // env vars allowlisted through to the rule process (python only)
}

// RuleAppliesTo scopes a rule to specific tools and/or downstream servers,
// matched against the exposed tool name / server name with "*" wildcards. If
// both Tools and Servers are set, a rule applies only when both match (AND).
// An empty list means "no constraint" for that dimension.
type RuleAppliesTo struct {
	Tools   []string `yaml:"tools,omitempty" json:"tools,omitempty"`
	Servers []string `yaml:"servers,omitempty" json:"servers,omitempty"`
}

const (
	OnErrorReject = "reject"
	OnErrorSkip   = "skip"

	DefaultRulePriority = 100
)

// OnErrorMode returns the configured on_error mode, defaulting to "reject"
// (fail-closed) when unset.
func (r Rule) OnErrorMode() string {
	if r.OnError == "" {
		return OnErrorReject
	}
	return r.OnError
}

// EffectivePriority returns the rule's priority, defaulting to
// DefaultRulePriority when unset (zero value).
func (r Rule) EffectivePriority() int {
	if r.Priority == 0 {
		return DefaultRulePriority
	}
	return r.Priority
}

// Profile describes one connecting client (e.g. a specific IDE) and which
// aggregated tools it's allowed to see.
type Profile struct {
	Name     string          `yaml:"name" json:"name"`
	Identify ProfileIdentify `yaml:"identify" json:"identify"`
	Tools    ToolFilter      `yaml:"tools" json:"tools"`
	LazyLoad *LazyLoad       `yaml:"lazy_load,omitempty" json:"lazy_load,omitempty"`
}

// LazyLoad enables context-saving tool loading for a profile: instead of
// registering every allowed tool directly, only the Limit most-frequently
// -called ones (per persisted per-profile usage counts, falling back to
// catalog order for tools with no history) are registered, plus two
// synthetic meta-tools - "list_all_tools" and "call_deferred_tool" - that
// give access to the rest without paying their registration cost in every
// tools/list response.
type LazyLoad struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	Limit   int  `yaml:"limit,omitempty" json:"limit,omitempty"` // 0 means DefaultLazyLoadLimit
}

// DefaultLazyLoadLimit is the number of tools registered directly when a
// profile enables lazy loading without an explicit limit.
const DefaultLazyLoadLimit = 5

// EffectiveLimit returns the configured limit, defaulting to
// DefaultLazyLoadLimit when unset. l may be nil.
func (l *LazyLoad) EffectiveLimit() int {
	if l == nil || l.Limit <= 0 {
		return DefaultLazyLoadLimit
	}
	return l.Limit
}

// ProfileIdentify says how shadow-mcp recognizes which profile a connection
// belongs to: a stdio launch flag, or (added in M7) an HTTP path/header/token.
type ProfileIdentify struct {
	StdioArg string `yaml:"stdio_arg,omitempty" json:"stdio_arg,omitempty"`
	HTTPPath string `yaml:"http_path,omitempty" json:"http_path,omitempty"`
}

// ToolFilter is an allow/deny wildcard list matched against exposed tool
// names. Deny always wins over an overlapping allow pattern.
type ToolFilter struct {
	Allow []string `yaml:"allow,omitempty" json:"allow,omitempty"`
	Deny  []string `yaml:"deny,omitempty" json:"deny,omitempty"`
}

// DownstreamServer describes one real MCP server that shadow-mcp aggregates.
type DownstreamServer struct {
	Name           string            `yaml:"name" json:"name"`
	Transport      string            `yaml:"transport" json:"transport"` // "stdio" or "http"
	Command        string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args           []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env            map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	URL            string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers        map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	ConnectTimeout time.Duration     `yaml:"connect_timeout,omitempty" json:"connect_timeout,omitempty"`
	CallTimeout    time.Duration     `yaml:"call_timeout,omitempty" json:"call_timeout,omitempty"`
	// Namespace controls whether this server's tools are exposed as
	// `server__tool` (true, the default) or bare `tool` (false). Regardless of
	// this setting, the aggregator always force-namespaces a tool if its
	// candidate exposed name collides with another server's tool.
	Namespace *bool `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}

// NamespaceEnabled reports whether this server's tools should be namespaced as
// `server__tool` by default (true unless explicitly set to false).
func (s DownstreamServer) NamespaceEnabled() bool {
	return s.Namespace == nil || *s.Namespace
}
