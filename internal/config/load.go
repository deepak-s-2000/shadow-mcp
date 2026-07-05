package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Load reads and parses a shadow-mcp YAML config file from path, interpolating
// ${VAR} references against the current process environment, resolving
// relative rule script paths against the config file's own directory (not
// the current process's working directory - the daemon may be auto-started
// from anywhere), then validates it.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	interpolated := interpolateEnv(string(raw))

	var cfg Config
	if err := yaml.Unmarshal([]byte(interpolated), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving config path %s: %w", path, err)
	}
	configDir := filepath.Dir(absPath)
	for i, r := range cfg.Rules {
		if r.Script != "" && !filepath.IsAbs(r.Script) {
			cfg.Rules[i].Script = filepath.Join(configDir, r.Script)
		}
	}

	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}

	return &cfg, nil
}

// LoadRaw reads and parses path like Load, but WITHOUT ${VAR} interpolation
// or rule-script path absolutization - it's the literal on-disk shape,
// exactly as a human editing the file would see it. This is what the TUI/
// admin API CRUD path reads and writes back: interpolating first (like Load
// does) would bake resolved secret VALUES into env/header fields, silently
// turning a `${GITHUB_TOKEN}` placeholder into the literal token text
// permanently on disk the next time an edit is saved. Still structurally
// validated via Validate, which never depends on values a placeholder would
// resolve to.
func LoadRaw(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}

	return &cfg, nil
}

func interpolateEnv(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := envVarPattern.FindStringSubmatch(match)[1]
		return os.Getenv(name)
	})
}
