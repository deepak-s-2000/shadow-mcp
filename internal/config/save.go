package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Save marshals cfg as YAML and writes it to path via write-temp-then-rename,
// so a crash mid-write can't corrupt the previous good config. Callers must
// pass the raw (pre-interpolation) config - see LoadRaw - never the
// interpolated one Load returns, or resolved secret values get baked into
// the file permanently.
func Save(path string, cfg *Config) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
}
