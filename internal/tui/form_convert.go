package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shadow-code/shadow-mcp/internal/config"
)

func withVisible(f *formField, visible func(get func(string) string) bool) *formField {
	f.visible = visible
	return f
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func durationOrEmpty(d time.Duration) string {
	if d == 0 {
		return ""
	}
	return d.String()
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinCSV(items []string) string {
	return strings.Join(items, ", ")
}

func parseKV(s string) (map[string]string, error) {
	items := splitCSV(s)
	if len(items) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(items))
	for _, item := range items {
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("invalid KEY=VALUE pair %q", item)
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, nil
}

func joinKV(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ", ")
}

func parseDurationField(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}

func parseIntField(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

// --- Downstream server ---

func newServerForm(existing *config.DownstreamServer) *entryForm {
	var s config.DownstreamServer
	originalName := ""
	if existing != nil {
		s = *existing
		originalName = existing.Name
	} else {
		s.Transport = "stdio"
	}

	isStdio := func(get func(string) string) bool { return get("transport") == "stdio" }
	isHTTPish := func(get func(string) string) bool {
		v := get("transport")
		return v == "http" || v == "sse"
	}

	return &entryForm{
		kind:         entryServer,
		originalName: originalName,
		fields: []*formField{
			newTextField("name", "Name", s.Name),
			newEnumField("transport", "Transport", []string{"stdio", "http", "sse"}, s.Transport),
			withVisible(newTextField("command", "Command", s.Command), isStdio),
			withVisible(newTextField("args", "Args (comma-sep)", joinCSV(s.Args)), isStdio),
			withVisible(newTextField("env", "Env (KEY=VAL, comma-sep)", joinKV(s.Env)), isStdio),
			withVisible(newTextField("url", "URL", s.URL), isHTTPish),
			withVisible(newTextField("headers", "Headers (KEY=VAL, comma-sep)", joinKV(s.Headers)), isHTTPish),
			newTextField("connect_timeout", "Connect timeout (e.g. 30s)", durationOrEmpty(s.ConnectTimeout)),
			newTextField("call_timeout", "Call timeout (e.g. 30s)", durationOrEmpty(s.CallTimeout)),
			newBoolField("namespace", "Namespace tools (server__tool)", s.NamespaceEnabled()),
		},
	}
}

func (f *entryForm) collectServer() (config.DownstreamServer, error) {
	var s config.DownstreamServer
	s.Name = strings.TrimSpace(f.get("name"))
	if s.Name == "" {
		return s, fmt.Errorf("name is required")
	}
	s.Transport = f.get("transport")
	s.Command = strings.TrimSpace(f.get("command"))
	s.Args = splitCSV(f.get("args"))

	env, err := parseKV(f.get("env"))
	if err != nil {
		return s, fmt.Errorf("env: %w", err)
	}
	s.Env = env

	s.URL = strings.TrimSpace(f.get("url"))

	headers, err := parseKV(f.get("headers"))
	if err != nil {
		return s, fmt.Errorf("headers: %w", err)
	}
	s.Headers = headers

	connectTimeout, err := parseDurationField(f.get("connect_timeout"))
	if err != nil {
		return s, fmt.Errorf("connect_timeout: %w", err)
	}
	s.ConnectTimeout = connectTimeout

	callTimeout, err := parseDurationField(f.get("call_timeout"))
	if err != nil {
		return s, fmt.Errorf("call_timeout: %w", err)
	}
	s.CallTimeout = callTimeout

	ns := f.field("namespace").boolVal
	s.Namespace = &ns

	return s, nil
}

// --- Profile ---

func newProfileForm(existing *config.Profile) *entryForm {
	var p config.Profile
	originalName := ""
	if existing != nil {
		p = *existing
		originalName = existing.Name
	}

	lazyEnabled := p.LazyLoad != nil && p.LazyLoad.Enabled
	lazyLimit := config.DefaultLazyLoadLimit
	if p.LazyLoad != nil {
		lazyLimit = p.LazyLoad.EffectiveLimit()
	}
	lazyOn := func(get func(string) string) bool { return get("lazy_enabled") == "true" }

	return &entryForm{
		kind:         entryProfile,
		originalName: originalName,
		fields: []*formField{
			newTextField("name", "Name", p.Name),
			newTextField("stdio_arg", "stdio_arg", p.Identify.StdioArg),
			newTextField("http_path", "http_path", p.Identify.HTTPPath),
			newTextField("allow", "Allow (comma-sep)", joinCSV(p.Tools.Allow)),
			newTextField("deny", "Deny (comma-sep)", joinCSV(p.Tools.Deny)),
			newBoolField("lazy_enabled", "Lazy load", lazyEnabled),
			withVisible(newTextField("lazy_limit", "Lazy load limit", strconv.Itoa(lazyLimit)), lazyOn),
		},
	}
}

func (f *entryForm) collectProfile() (config.Profile, error) {
	var p config.Profile
	p.Name = strings.TrimSpace(f.get("name"))
	if p.Name == "" {
		return p, fmt.Errorf("name is required")
	}
	p.Identify.StdioArg = strings.TrimSpace(f.get("stdio_arg"))
	p.Identify.HTTPPath = strings.TrimSpace(f.get("http_path"))
	if p.Identify.StdioArg == "" && p.Identify.HTTPPath == "" {
		return p, fmt.Errorf("stdio_arg or http_path is required")
	}
	p.Tools.Allow = splitCSV(f.get("allow"))
	p.Tools.Deny = splitCSV(f.get("deny"))

	if f.field("lazy_enabled").boolVal {
		limit, err := parseIntField(f.get("lazy_limit"))
		if err != nil {
			return p, fmt.Errorf("lazy_load.limit: %w", err)
		}
		if limit < 0 {
			return p, fmt.Errorf("lazy_load.limit must be >= 0")
		}
		p.LazyLoad = &config.LazyLoad{Enabled: true, Limit: limit}
	}
	return p, nil
}

// --- Rule ---

func newRuleForm(existing *config.Rule) *entryForm {
	var r config.Rule
	originalName := ""
	if existing != nil {
		r = *existing
		originalName = existing.Name
	} else {
		r.Language = "js"
		r.OnError = config.OnErrorReject
	}

	priority := r.Priority
	if priority == 0 {
		priority = config.DefaultRulePriority
	}
	isPython := func(get func(string) string) bool { return get("language") == "python" }

	return &entryForm{
		kind:         entryRule,
		originalName: originalName,
		fields: []*formField{
			newTextField("name", "Name", r.Name),
			newTextField("script", "Script path", r.Script),
			newEnumField("language", "Language", []string{"js", "python"}, orDefault(r.Language, "js")),
			newBoolField("hook_pre", "Hook: pre", containsStr(r.Hooks, "pre")),
			newBoolField("hook_post", "Hook: post", containsStr(r.Hooks, "post")),
			newTextField("applies_tools", "Applies to tools (comma-sep, blank=all)", joinCSV(r.AppliesTo.Tools)),
			newTextField("applies_servers", "Applies to servers (comma-sep, blank=all)", joinCSV(r.AppliesTo.Servers)),
			newTextField("priority", "Priority (lower runs first)", strconv.Itoa(priority)),
			newTextField("timeout", "Timeout (e.g. 5s)", durationOrEmpty(r.Timeout)),
			newEnumField("on_error", "On error", []string{"reject", "skip"}, orDefault(r.OnError, config.OnErrorReject)),
			withVisible(newTextField("env", "Env vars allowlist (comma-sep, python only)", joinCSV(r.Env)), isPython),
		},
	}
}

func (f *entryForm) collectRule() (config.Rule, error) {
	var r config.Rule
	r.Name = strings.TrimSpace(f.get("name"))
	if r.Name == "" {
		return r, fmt.Errorf("name is required")
	}
	r.Script = strings.TrimSpace(f.get("script"))
	if r.Script == "" {
		return r, fmt.Errorf("script is required")
	}
	r.Language = f.get("language")

	if f.field("hook_pre").boolVal {
		r.Hooks = append(r.Hooks, "pre")
	}
	if f.field("hook_post").boolVal {
		r.Hooks = append(r.Hooks, "post")
	}
	if len(r.Hooks) == 0 {
		return r, fmt.Errorf("at least one of pre/post hook must be enabled")
	}

	r.AppliesTo.Tools = splitCSV(f.get("applies_tools"))
	r.AppliesTo.Servers = splitCSV(f.get("applies_servers"))

	priority, err := parseIntField(f.get("priority"))
	if err != nil {
		return r, fmt.Errorf("priority: %w", err)
	}
	r.Priority = priority

	timeout, err := parseDurationField(f.get("timeout"))
	if err != nil {
		return r, fmt.Errorf("timeout: %w", err)
	}
	r.Timeout = timeout

	r.OnError = f.get("on_error")
	r.Env = splitCSV(f.get("env"))

	return r, nil
}
