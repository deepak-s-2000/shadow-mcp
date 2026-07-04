package config

import "fmt"

// Validate checks cross-field invariants that YAML unmarshaling alone can't enforce.
func Validate(cfg *Config) error {
	seen := make(map[string]bool, len(cfg.DownstreamServers))

	for _, s := range cfg.DownstreamServers {
		if s.Name == "" {
			return fmt.Errorf("downstream_servers: entry missing name")
		}
		if seen[s.Name] {
			return fmt.Errorf("downstream_servers: duplicate name %q", s.Name)
		}
		seen[s.Name] = true

		switch s.Transport {
		case "stdio":
			if s.Command == "" {
				return fmt.Errorf("downstream_servers[%s]: transport=stdio requires command", s.Name)
			}
		case "http", "sse":
			if s.URL == "" {
				return fmt.Errorf("downstream_servers[%s]: transport=%s requires url", s.Name, s.Transport)
			}
		default:
			return fmt.Errorf("downstream_servers[%s]: unknown transport %q (want stdio, http, or sse)", s.Name, s.Transport)
		}
	}

	seenProfiles := make(map[string]bool, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		if p.Name == "" {
			return fmt.Errorf("profiles: entry missing name")
		}
		if seenProfiles[p.Name] {
			return fmt.Errorf("profiles: duplicate name %q", p.Name)
		}
		seenProfiles[p.Name] = true

		if p.Identify.StdioArg == "" && p.Identify.HTTPPath == "" {
			return fmt.Errorf("profiles[%s]: identify needs at least one of stdio_arg or http_path", p.Name)
		}

		if p.LazyLoad != nil && p.LazyLoad.Limit < 0 {
			return fmt.Errorf("profiles[%s]: lazy_load.limit must be >= 0", p.Name)
		}
	}

	seenRules := make(map[string]bool, len(cfg.Rules))
	for _, r := range cfg.Rules {
		if r.Name == "" {
			return fmt.Errorf("rules: entry missing name")
		}
		if seenRules[r.Name] {
			return fmt.Errorf("rules[%s]: duplicate name", r.Name)
		}
		seenRules[r.Name] = true

		if r.Script == "" {
			return fmt.Errorf("rules[%s]: missing script path", r.Name)
		}
		if r.Language != "js" && r.Language != "python" {
			return fmt.Errorf("rules[%s]: unknown language %q (want js or python)", r.Name, r.Language)
		}
		if len(r.Hooks) == 0 {
			return fmt.Errorf("rules[%s]: hooks must list at least one of pre, post", r.Name)
		}
		for _, h := range r.Hooks {
			if h != "pre" && h != "post" {
				return fmt.Errorf("rules[%s]: unknown hook %q (want pre or post)", r.Name, h)
			}
		}
		if r.OnError != "" && r.OnError != OnErrorReject && r.OnError != OnErrorSkip {
			return fmt.Errorf("rules[%s]: unknown on_error %q (want %s or %s)", r.Name, r.OnError, OnErrorReject, OnErrorSkip)
		}
	}

	return nil
}
